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

type ReservationClient struct {
	baseURL    string
	httpClient *http.Client
	logger     *logrus.Logger
}

// Reservation request/response models
type CreateReservationRequest struct {
	EventID            string   `json:"event_id"`
	SeatIDs            []string `json:"seat_ids"`
	Quantity           int      `json:"quantity"`
	ReservationToken   string   `json:"reservation_token,omitempty"`
	UserID             string   `json:"user_id,omitempty"`
}

type ReservationResponse struct {
	ReservationID   string    `json:"reservation_id"`
	Status          string    `json:"status"`
	HoldExpiresAt   time.Time `json:"hold_expires_at"`
	EventID         string    `json:"event_id"`
	SeatIDs         []string  `json:"seat_ids"`
	Quantity        int       `json:"quantity"`
	UserID          string    `json:"user_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type ConfirmReservationRequest struct {
	PaymentIntentID string `json:"payment_intent_id"`
}

type ConfirmReservationResponse struct {
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
}

type ErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		TraceID string `json:"trace_id"`
	} `json:"error"`
}

func NewReservationClient(cfg *config.ReservationAPIConfig, logger *logrus.Logger) *ReservationClient {
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

	return &ReservationClient{
		baseURL:    cfg.BaseURL,
		httpClient: client,
		logger:     logger,
	}
}

// CreateReservation creates a new reservation
func (r *ReservationClient) CreateReservation(ctx context.Context, req *CreateReservationRequest, headers map[string]string) (*ReservationResponse, error) {
	url := fmt.Sprintf("%s/v1/reservations", r.baseURL)

	resp, err := r.makeRequest(ctx, "POST", url, req, headers)
	if err != nil {
		return nil, fmt.Errorf("failed to create reservation: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, r.handleErrorResponse(resp, "create reservation")
	}

	var reservation ReservationResponse
	if err := json.NewDecoder(resp.Body).Decode(&reservation); err != nil {
		return nil, fmt.Errorf("failed to decode reservation response: %w", err)
	}

	return &reservation, nil
}

// GetReservation retrieves a reservation by ID
func (r *ReservationClient) GetReservation(ctx context.Context, reservationID string, headers map[string]string) (*ReservationResponse, error) {
	url := fmt.Sprintf("%s/v1/reservations/%s", r.baseURL, reservationID)

	resp, err := r.makeRequest(ctx, "GET", url, nil, headers)
	if err != nil {
		return nil, fmt.Errorf("failed to get reservation: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, r.handleErrorResponse(resp, "get reservation")
	}

	var reservation ReservationResponse
	if err := json.NewDecoder(resp.Body).Decode(&reservation); err != nil {
		return nil, fmt.Errorf("failed to decode reservation response: %w", err)
	}

	return &reservation, nil
}

// ConfirmReservation confirms a reservation
func (r *ReservationClient) ConfirmReservation(ctx context.Context, reservationID string, req *ConfirmReservationRequest, headers map[string]string) (*ConfirmReservationResponse, error) {
	url := fmt.Sprintf("%s/v1/reservations/%s/confirm", r.baseURL, reservationID)

	resp, err := r.makeRequest(ctx, "POST", url, req, headers)
	if err != nil {
		return nil, fmt.Errorf("failed to confirm reservation: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, r.handleErrorResponse(resp, "confirm reservation")
	}

	var confirmation ConfirmReservationResponse
	if err := json.NewDecoder(resp.Body).Decode(&confirmation); err != nil {
		return nil, fmt.Errorf("failed to decode confirmation response: %w", err)
	}

	return &confirmation, nil
}

// CancelReservation cancels a reservation
func (r *ReservationClient) CancelReservation(ctx context.Context, reservationID string, headers map[string]string) error {
	url := fmt.Sprintf("%s/v1/reservations/%s/cancel", r.baseURL, reservationID)

	resp, err := r.makeRequest(ctx, "POST", url, nil, headers)
	if err != nil {
		return fmt.Errorf("failed to cancel reservation: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return r.handleErrorResponse(resp, "cancel reservation")
	}

	return nil
}

// makeRequest makes an HTTP request with proper headers and context
func (r *ReservationClient) makeRequest(ctx context.Context, method, url string, body interface{}, headers map[string]string) (*http.Response, error) {
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
	r.logger.WithFields(logrus.Fields{
		"method": method,
		"url":    url,
		"headers": r.sanitizeHeaders(headers),
	}).Debug("Making reservation API request")

	start := time.Now()
	resp, err := r.httpClient.Do(req)
	latency := time.Since(start)

	// Log response
	r.logger.WithFields(logrus.Fields{
		"method":      method,
		"url":         url,
		"status_code": r.getStatusCode(resp),
		"latency_ms":  latency.Milliseconds(),
	}).Debug("Reservation API response")

	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

// handleErrorResponse handles error responses from the reservation API
func (r *ReservationClient) handleErrorResponse(resp *http.Response, operation string) error {
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
func (r *ReservationClient) sanitizeHeaders(headers map[string]string) map[string]string {
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
func (r *ReservationClient) getStatusCode(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}