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

	"github.com/sirupsen/logrus"
)

type PaymentClient struct {
	baseURL    string
	httpClient *http.Client
	logger     *logrus.Logger
}

// Payment request/response models
type CreatePaymentIntentRequest struct {
	ReservationID string `json:"reservation_id"`
	Amount        int64  `json:"amount"`
	Currency      string `json:"currency"`
	Scenario      string `json:"scenario"` // approve|fail|delay|random
}

type PaymentIntentResponse struct {
	PaymentIntentID string `json:"payment_intent_id"`
	Status          string `json:"status"`
	Amount          int64  `json:"amount"`
	Currency        string `json:"currency"`
	ReservationID   string `json:"reservation_id"`
	CreatedAt       time.Time `json:"created_at"`
	Next            string `json:"next,omitempty"`
}

type PaymentStatusResponse struct {
	PaymentIntentID string    `json:"payment_intent_id"`
	Status          string    `json:"status"`
	Amount          int64     `json:"amount"`
	Currency        string    `json:"currency"`
	ReservationID   string    `json:"reservation_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	ProcessedAt     *time.Time `json:"processed_at,omitempty"`
}

func NewPaymentClient(cfg *config.PaymentAPIConfig, logger *logrus.Logger) *PaymentClient {
	// Create HTTP client with timeout and retry configuration
	client := &http.Client{
		Timeout: cfg.Timeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 20,
			IdleConnTimeout:     90 * time.Second,
			DisableKeepAlives:   false,
		},
	}

	return &PaymentClient{
		baseURL:    cfg.BaseURL,
		httpClient: client,
		logger:     logger,
	}
}

// CreatePaymentIntent creates a new payment intent
func (p *PaymentClient) CreatePaymentIntent(ctx context.Context, req *CreatePaymentIntentRequest, headers map[string]string) (*PaymentIntentResponse, error) {
	url := fmt.Sprintf("%s/v1/sim/intent", p.baseURL)

	resp, err := p.makeRequest(ctx, "POST", url, req, headers)
	if err != nil {
		return nil, fmt.Errorf("failed to create payment intent: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, p.handleErrorResponse(resp, "create payment intent")
	}

	var intent PaymentIntentResponse
	if err := json.NewDecoder(resp.Body).Decode(&intent); err != nil {
		return nil, fmt.Errorf("failed to decode payment intent response: %w", err)
	}

	return &intent, nil
}

// GetPaymentStatus retrieves payment status by intent ID
func (p *PaymentClient) GetPaymentStatus(ctx context.Context, paymentIntentID string, headers map[string]string) (*PaymentStatusResponse, error) {
	url := fmt.Sprintf("%s/v1/sim/intents/%s", p.baseURL, paymentIntentID)

	resp, err := p.makeRequest(ctx, "GET", url, nil, headers)
	if err != nil {
		return nil, fmt.Errorf("failed to get payment status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, p.handleErrorResponse(resp, "get payment status")
	}

	var status PaymentStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode payment status response: %w", err)
	}

	return &status, nil
}

// ProcessPayment manually triggers payment processing (for testing)
type ProcessPaymentRequest struct {
	PaymentIntentID string `json:"payment_intent_id"`
	Action          string `json:"action"` // approve|fail
}

func (p *PaymentClient) ProcessPayment(ctx context.Context, req *ProcessPaymentRequest, headers map[string]string) error {
	url := fmt.Sprintf("%s/v1/sim/webhook/test", p.baseURL)

	resp, err := p.makeRequest(ctx, "POST", url, req, headers)
	if err != nil {
		return fmt.Errorf("failed to process payment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return p.handleErrorResponse(resp, "process payment")
	}

	return nil
}

// makeRequest makes an HTTP request with proper headers and context
func (p *PaymentClient) makeRequest(ctx context.Context, method, url string, body interface{}, headers map[string]string) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set default headers
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Set custom headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Log request
	p.logger.WithFields(logrus.Fields{
		"method": method,
		"url":    url,
		"headers": p.sanitizeHeaders(headers),
	}).Debug("Making payment API request")

	start := time.Now()
	resp, err := p.httpClient.Do(req)
	latency := time.Since(start)

	// Log response
	p.logger.WithFields(logrus.Fields{
		"method":      method,
		"url":         url,
		"status_code": p.getStatusCode(resp),
		"latency_ms":  latency.Milliseconds(),
	}).Debug("Payment API response")

	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

// handleErrorResponse handles error responses from the payment API
func (p *PaymentClient) handleErrorResponse(resp *http.Response, operation string) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%s failed with status %d: failed to read error response", operation, resp.StatusCode)
	}

	var errorResp ErrorResponse
	if err := json.Unmarshal(body, &errorResp); err != nil {
		return fmt.Errorf("%s failed with status %d: %s", operation, resp.StatusCode, string(body))
	}

	return fmt.Errorf("%s failed: %s (%s)", operation, errorResp.Error.Message, errorResp.Error.Code)
}

// sanitizeHeaders removes sensitive headers for logging
func (p *PaymentClient) sanitizeHeaders(headers map[string]string) map[string]string {
	sanitized := make(map[string]string)
	for key, value := range headers {
		if key == "Authorization" {
			sanitized[key] = "Bearer [REDACTED]"
		} else {
			sanitized[key] = value
		}
	}
	return sanitized
}

// getStatusCode safely gets status code from response
func (p *PaymentClient) getStatusCode(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}