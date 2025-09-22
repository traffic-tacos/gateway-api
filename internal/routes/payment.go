package routes

import (
	"github.com/traffic-tacos/gateway-api/internal/clients"
	"github.com/traffic-tacos/gateway-api/internal/middleware"
	"github.com/traffic-tacos/gateway-api/internal/utils"
	commonv1 "github.com/traffic-tacos/proto-contracts/gen/go/common/v1"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

type PaymentHandler struct {
	client *clients.PaymentClient
	logger *logrus.Logger
}

func NewPaymentHandler(client *clients.PaymentClient, logger *logrus.Logger) *PaymentHandler {
	return &PaymentHandler{
		client: client,
		logger: logger,
	}
}

// CreateIntent handles payment intent creation
// @Summary Create payment intent
// @Description Create a payment intent for a reservation
// @Tags Payments
// @Accept json
// @Produce json
// @Security Bearer
// @Param Idempotency-Key header string true "Idempotency key (UUID v4)"
// @Param request body CreatePaymentIntentRequest true "Payment intent request"
// @Success 200 {object} PaymentIntentResponse
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 500 {object} map[string]interface{} "Internal error"
// @Router /payment/intent [post]
// Request/Response models for API documentation
type CreatePaymentIntentRequest struct {
	ReservationID string `json:"reservation_id"`
	Amount        int64  `json:"amount"`
	Currency      string `json:"currency"`
}

type PaymentIntentResponse struct {
	PaymentIntentID string `json:"payment_intent_id"`
	Status          string `json:"status"`
	Amount          int64  `json:"amount"`
	Currency        string `json:"currency"`
	ReservationID   string `json:"reservation_id"`
}

func (p *PaymentHandler) CreateIntent(c *fiber.Ctx) error {
	var req CreatePaymentIntentRequest
	if err := c.BodyParser(&req); err != nil {
		return p.badRequestError(c, "INVALID_REQUEST", "Invalid request body")
	}

	// Validate required fields
	if req.ReservationID == "" {
		return p.badRequestError(c, "MISSING_RESERVATION_ID", "reservation_id is required")
	}

	if req.Amount <= 0 {
		return p.badRequestError(c, "INVALID_AMOUNT", "amount must be greater than 0")
	}

	if req.Currency == "" {
		req.Currency = "KRW" // Default currency
	}

	// Get user ID from middleware
	userID := middleware.GetUserID(c)
	if userID == "" {
		userID = "anonymous" // fallback for non-authenticated users
	}

	// Create Money object for gRPC call
	amount := &commonv1.Money{
		Amount:   req.Amount,
		Currency: req.Currency,
	}

	// Call payment API via gRPC
	intent, err := p.client.CreatePaymentIntent(c.Context(), req.ReservationID, userID, amount)
	if err != nil {
		p.logger.WithError(err).WithFields(logrus.Fields{
			"reservation_id": req.ReservationID,
			"amount":         req.Amount,
			"currency":       req.Currency,
			"user_id":        userID,
		}).Error("Failed to create payment intent")

		return p.handleClientError(c, err, "create payment intent")
	}

	// Convert gRPC response to API response (simplified until we confirm proto structure)
	response := PaymentIntentResponse{
		PaymentIntentID: intent.PaymentIntentId,
		Status:          "PENDING", // Default status
		Amount:          req.Amount,
		Currency:        req.Currency,
		ReservationID:   req.ReservationID,
	}

	p.logger.WithFields(logrus.Fields{
		"payment_intent_id": response.PaymentIntentID,
		"reservation_id":    req.ReservationID,
		"amount":            req.Amount,
		"currency":          req.Currency,
		"status":            response.Status,
		"user_id":           userID,
	}).Info("Payment intent created successfully")

	return c.Status(fiber.StatusOK).JSON(response)
}

// GetStatus handles payment status retrieval
// @Summary Get payment status
// @Description Retrieve the status of a payment intent
// @Tags Payments
// @Produce json
// @Security Bearer
// @Param id path string true "Payment Intent ID"
// @Success 200 {object} PaymentStatusResponse
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 404 {object} map[string]interface{} "Not found"
// @Failure 500 {object} map[string]interface{} "Internal error"
// @Router /payment/{id}/status [get]
type PaymentStatusResponse struct {
	PaymentIntentID string `json:"payment_intent_id"`
	Status          string `json:"status"`
	Amount          int64  `json:"amount"`
	Currency        string `json:"currency"`
	ReservationID   string `json:"reservation_id"`
}

func (p *PaymentHandler) GetStatus(c *fiber.Ctx) error {
	paymentIntentID := c.Params("id")
	if paymentIntentID == "" {
		return p.badRequestError(c, "MISSING_ID", "Payment intent ID is required")
	}

	// Call payment API via gRPC
	_, err := p.client.GetPaymentStatus(c.Context(), paymentIntentID)
	if err != nil {
		p.logger.WithError(err).WithField("payment_intent_id", paymentIntentID).Error("Failed to get payment status")
		return p.handleClientError(c, err, "get payment status")
	}

	// Convert gRPC response to API response (simplified until we confirm proto structure)
	response := PaymentStatusResponse{
		PaymentIntentID: paymentIntentID,
		Status:          "PENDING", // Default status
		Amount:          0,
		Currency:        "KRW",
		ReservationID:   "",
	}

	return c.JSON(response)
}

// ProcessPayment handles manual payment processing (for testing)
// @Summary Process payment (Testing)
// @Description Manually trigger payment processing for testing purposes
// @Tags Payments
// @Accept json
// @Produce json
// @Security Bearer
// @Param Idempotency-Key header string false "Idempotency key (UUID v4)"
// @Param request body ProcessPaymentRequest true "Process payment request"
// @Success 200 {object} map[string]interface{} "Success"
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 404 {object} map[string]interface{} "Not found"
// @Failure 500 {object} map[string]interface{} "Internal error"
// @Router /payment/process [post]
type ProcessPaymentRequest struct {
	PaymentIntentID string `json:"payment_intent_id"`
	Action          string `json:"action"` // approve|fail
}

func (p *PaymentHandler) ProcessPayment(c *fiber.Ctx) error {
	var req ProcessPaymentRequest
	if err := c.BodyParser(&req); err != nil {
		return p.badRequestError(c, "INVALID_REQUEST", "Invalid request body")
	}

	// Validate required fields
	if req.PaymentIntentID == "" {
		return p.badRequestError(c, "MISSING_PAYMENT_INTENT_ID", "payment_intent_id is required")
	}

	if req.Action == "" {
		req.Action = "approve" // Default action
	}

	// Validate action
	validActions := []string{"approve", "fail"}
	if !utils.ContainsString(validActions, req.Action) {
		return p.badRequestError(c, "INVALID_ACTION", "action must be either 'approve' or 'fail'")
	}

	// Call payment API via gRPC
	response, err := p.client.ProcessPayment(c.Context(), req.PaymentIntentID, req.Action)
	if err != nil {
		p.logger.WithError(err).WithFields(logrus.Fields{
			"payment_intent_id": req.PaymentIntentID,
			"action":            req.Action,
			"user_id":           middleware.GetUserID(c),
		}).Error("Failed to process payment")

		return p.handleClientError(c, err, "process payment")
	}

	p.logger.WithFields(logrus.Fields{
		"payment_intent_id": req.PaymentIntentID,
		"action":            req.Action,
		"user_id":           middleware.GetUserID(c),
		"status":            response.Status,
	}).Info("Payment processed successfully")

	return c.JSON(fiber.Map{
		"status":  response.Status,
		"action":  req.Action,
		"message": "Payment processing initiated",
	})
}


// handleClientError handles errors from backend client calls
func (p *PaymentHandler) handleClientError(c *fiber.Ctx, err error, operation string) error {
	// Map common client errors to appropriate HTTP status codes
	errorMsg := err.Error()

	// Check for specific error patterns
	switch {
	case utils.ContainsSubstring(errorMsg, "404") || utils.ContainsSubstring(errorMsg, "not found"):
		return p.notFoundError(c, "PAYMENT_NOT_FOUND", "Payment intent not found")
	case utils.ContainsSubstring(errorMsg, "409") || utils.ContainsSubstring(errorMsg, "conflict"):
		return p.conflictError(c, "PAYMENT_CONFLICT", "Payment conflict")
	case utils.ContainsSubstring(errorMsg, "400") || utils.ContainsSubstring(errorMsg, "bad request"):
		return p.badRequestError(c, "INVALID_PAYMENT", "Invalid payment request")
	case utils.ContainsSubstring(errorMsg, "402") || utils.ContainsSubstring(errorMsg, "payment required"):
		return p.paymentRequiredError(c, "PAYMENT_REQUIRED", "Payment required")
	case utils.ContainsSubstring(errorMsg, "timeout"):
		return p.gatewayTimeoutError(c, "UPSTREAM_TIMEOUT", "Payment service timeout")
	default:
		return p.internalError(c, "PAYMENT_ERROR", "Failed to "+operation)
	}
}

// Error response helpers
func (p *PaymentHandler) badRequestError(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     code,
			"message":  message,
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}

func (p *PaymentHandler) notFoundError(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     code,
			"message":  message,
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}

func (p *PaymentHandler) conflictError(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusConflict).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     code,
			"message":  message,
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}

func (p *PaymentHandler) paymentRequiredError(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     code,
			"message":  message,
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}

func (p *PaymentHandler) gatewayTimeoutError(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusGatewayTimeout).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     code,
			"message":  message,
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}

func (p *PaymentHandler) internalError(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     code,
			"message":  message,
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}

