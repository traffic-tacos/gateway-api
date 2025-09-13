package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/traffic-tacos/gateway-api/internal/config"
	"github.com/traffic-tacos/gateway-api/internal/logging"
	"github.com/traffic-tacos/gateway-api/internal/metrics"
	"github.com/traffic-tacos/gateway-api/internal/middleware"
)

// PaymentClient handles communication with payment-sim-api
type PaymentClient struct {
	baseURL    string
	httpClient *http.Client
	logger     *logging.Logger
	metrics    *metrics.Metrics
	tracing    *middleware.TracingMiddleware
}

// NewPaymentClient creates a new payment client
func NewPaymentClient(cfg *config.Config, logger *logging.Logger, metrics *metrics.Metrics) *PaymentClient {
	// Create HTTP client with timeout
	httpClient := &http.Client{
		Timeout: cfg.Upstream.Payment.Timeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxConnsPerHost:     10,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	return &PaymentClient{
		baseURL:    cfg.Upstream.Payment.BaseURL,
		httpClient: httpClient,
		logger:     logger,
		metrics:    metrics,
	}
}

// SetTracing sets the tracing middleware
func (c *PaymentClient) SetTracing(tracing *middleware.TracingMiddleware) {
	c.tracing = tracing
}

// CreatePaymentIntent forwards payment intent creation request
func (c *PaymentClient) CreatePaymentIntent(ctx context.Context, request map[string]interface{}) (map[string]interface{}, error) {
	return c.post(ctx, "/v1/sim/intent", request)
}

// GetPaymentIntent forwards payment intent retrieval request
func (c *PaymentClient) GetPaymentIntent(ctx context.Context, intentID string) (map[string]interface{}, error) {
	return c.get(ctx, fmt.Sprintf("/v1/sim/intent/%s", intentID), nil)
}

// Helper methods

func (c *PaymentClient) get(ctx context.Context, path string, params map[string]string) (map[string]interface{}, error) {
	return c.doRequest(ctx, "GET", path, params, nil)
}

func (c *PaymentClient) post(ctx context.Context, path string, body interface{}) (map[string]interface{}, error) {
	return c.doRequest(ctx, "POST", path, nil, body)
}

func (c *PaymentClient) doRequest(ctx context.Context, method, path string, params map[string]string, body interface{}) (map[string]interface{}, error) {
	start := time.Now()

	// Build URL
	url := c.baseURL + path
	if params != nil && len(params) > 0 {
		url += "?"
		for k, v := range params {
			url += fmt.Sprintf("%s=%s&", k, v)
		}
		url = url[:len(url)-1] // Remove trailing &
	}

	// Create request
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Inject tracing headers if available
	if c.tracing != nil {
		tracingHeaders := c.tracing.InjectHeaders(ctx)
		for k, v := range tracingHeaders {
			req.Header.Set(k, v)
		}
	}

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.recordMetrics("payment", method, 0, time.Since(start))
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Record metrics
	c.recordMetrics("payment", method, resp.StatusCode, time.Since(start))

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check status
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("upstream error: %s", string(respBody))
	}

	// Parse JSON response
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result, nil
}

func (c *PaymentClient) recordMetrics(service, method string, statusCode int, duration time.Duration) {
	c.metrics.RecordBackendCall(service, method, statusCode, duration)
}
