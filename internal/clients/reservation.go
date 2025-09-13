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

// ReservationClient handles communication with reservation-api
type ReservationClient struct {
	baseURL    string
	httpClient *http.Client
	logger     *logging.Logger
	metrics    *metrics.Metrics
	tracing    *middleware.TracingMiddleware
}

// NewReservationClient creates a new reservation client
func NewReservationClient(cfg *config.Config, logger *logging.Logger, metrics *metrics.Metrics) *ReservationClient {
	// Create HTTP client with timeout
	httpClient := &http.Client{
		Timeout: cfg.Upstream.Reservation.Timeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxConnsPerHost:     10,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	// Initialize tracing (will be set later if available)
	var tracing *middleware.TracingMiddleware

	return &ReservationClient{
		baseURL:    cfg.Upstream.Reservation.BaseURL,
		httpClient: httpClient,
		logger:     logger,
		metrics:    metrics,
		tracing:    tracing,
	}
}

// SetTracing sets the tracing middleware
func (c *ReservationClient) SetTracing(tracing *middleware.TracingMiddleware) {
	c.tracing = tracing
}

// JoinQueue forwards queue join request to reservation service
func (c *ReservationClient) JoinQueue(ctx context.Context, eventID, userID string) (map[string]interface{}, error) {
	requestBody := map[string]interface{}{
		"event_id": eventID,
		"user_id":  userID,
	}

	return c.post(ctx, "/v1/queue/join", requestBody)
}

// GetQueueStatus forwards queue status request to reservation service
func (c *ReservationClient) GetQueueStatus(ctx context.Context, token string) (map[string]interface{}, error) {
	params := map[string]string{
		"token": token,
	}

	return c.get(ctx, "/v1/queue/status", params)
}

// CreateReservation forwards reservation creation request
func (c *ReservationClient) CreateReservation(ctx context.Context, request map[string]interface{}) (map[string]interface{}, error) {
	return c.post(ctx, "/v1/reservations", request)
}

// GetReservation forwards reservation retrieval request
func (c *ReservationClient) GetReservation(ctx context.Context, reservationID string) (map[string]interface{}, error) {
	return c.get(ctx, fmt.Sprintf("/v1/reservations/%s", reservationID), nil)
}

// ListReservations forwards reservation list request
func (c *ReservationClient) ListReservations(ctx context.Context, params map[string]string) (map[string]interface{}, error) {
	return c.get(ctx, "/v1/reservations", params)
}

// CancelReservation forwards reservation cancellation request
func (c *ReservationClient) CancelReservation(ctx context.Context, reservationID string, request map[string]interface{}) (map[string]interface{}, error) {
	return c.post(ctx, fmt.Sprintf("/v1/reservations/%s/cancel", reservationID), request)
}

// ConfirmReservation forwards reservation confirmation request
func (c *ReservationClient) ConfirmReservation(ctx context.Context, reservationID string, request map[string]interface{}) (map[string]interface{}, error) {
	return c.post(ctx, fmt.Sprintf("/v1/reservations/%s/confirm", reservationID), request)
}

// Helper methods

func (c *ReservationClient) get(ctx context.Context, path string, params map[string]string) (map[string]interface{}, error) {
	return c.doRequest(ctx, "GET", path, params, nil)
}

func (c *ReservationClient) post(ctx context.Context, path string, body interface{}) (map[string]interface{}, error) {
	return c.doRequest(ctx, "POST", path, nil, body)
}

func (c *ReservationClient) doRequest(ctx context.Context, method, path string, params map[string]string, body interface{}) (map[string]interface{}, error) {
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
		c.recordMetrics("reservation", method, 0, time.Since(start))
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Record metrics
	c.recordMetrics("reservation", method, resp.StatusCode, time.Since(start))

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

func (c *ReservationClient) recordMetrics(service, method string, statusCode int, duration time.Duration) {
	c.metrics.RecordBackendCall(service, method, statusCode, duration)
}
