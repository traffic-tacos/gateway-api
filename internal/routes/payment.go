package routes

import (
	"github.com/traffic-tacos/gateway-api/internal/clients"
	"github.com/traffic-tacos/gateway-api/internal/middleware"

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
// @Param request body clients.CreatePaymentIntentRequest true "Payment intent request"
// @Success 200 {object} clients.PaymentIntentResponse
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 500 {object} map[string]interface{} "Internal error"
// @Router /payment/intent [post]
func (p *PaymentHandler) CreateIntent(c *fiber.Ctx) error {
	var req clients.CreatePaymentIntentRequest
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

	if req.Scenario == "" {
		req.Scenario = "approve" // Default scenario
	}

	// Validate scenario
	validScenarios := []string{"approve", "fail", "delay", "random"}
	if !contains(validScenarios, req.Scenario) {
		return p.badRequestError(c, "INVALID_SCENARIO", "scenario must be one of: approve, fail, delay, random")
	}

	// Prepare headers for backend call
	headers := p.prepareHeaders(c)

	// Call payment API
	intent, err := p.client.CreatePaymentIntent(c.Context(), &req, headers)
	if err != nil {
		p.logger.WithError(err).WithFields(logrus.Fields{
			"reservation_id": req.ReservationID,
			"amount":         req.Amount,
			"currency":       req.Currency,
			"scenario":       req.Scenario,
			"user_id":        middleware.GetUserID(c),
		}).Error("Failed to create payment intent")

		return p.handleClientError(c, err, "create payment intent")
	}

	p.logger.WithFields(logrus.Fields{
		"payment_intent_id": intent.PaymentIntentID,
		"reservation_id":    req.ReservationID,
		"amount":            req.Amount,
		"currency":          req.Currency,
		"scenario":          req.Scenario,
		"status":            intent.Status,
		"user_id":           middleware.GetUserID(c),
	}).Info("Payment intent created successfully")

	return c.Status(fiber.StatusOK).JSON(intent)
}

// GetStatus handles payment status retrieval
// @Summary Get payment status
// @Description Retrieve the status of a payment intent
// @Tags Payments
// @Produce json
// @Security Bearer
// @Param id path string true "Payment Intent ID"
// @Success 200 {object} clients.PaymentStatusResponse
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 404 {object} map[string]interface{} "Not found"
// @Failure 500 {object} map[string]interface{} "Internal error"
// @Router /payment/{id}/status [get]
func (p *PaymentHandler) GetStatus(c *fiber.Ctx) error {
	paymentIntentID := c.Params("id")
	if paymentIntentID == "" {
		return p.badRequestError(c, "MISSING_ID", "Payment intent ID is required")
	}

	// Prepare headers for backend call
	headers := p.prepareHeaders(c)

	// Call payment API
	status, err := p.client.GetPaymentStatus(c.Context(), paymentIntentID, headers)
	if err != nil {
		p.logger.WithError(err).WithField("payment_intent_id", paymentIntentID).Error("Failed to get payment status")
		return p.handleClientError(c, err, "get payment status")
	}

	return c.JSON(status)
}

// ProcessPayment handles manual payment processing (for testing)
// @Summary Process payment (Testing)
// @Description Manually trigger payment processing for testing purposes
// @Tags Payments
// @Accept json
// @Produce json
// @Security Bearer
// @Param Idempotency-Key header string false "Idempotency key (UUID v4)"
// @Param request body clients.ProcessPaymentRequest true "Process payment request"
// @Success 200 {object} map[string]interface{} "Success"
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 404 {object} map[string]interface{} "Not found"
// @Failure 500 {object} map[string]interface{} "Internal error"
// @Router /payment/process [post]
func (p *PaymentHandler) ProcessPayment(c *fiber.Ctx) error {
	var req clients.ProcessPaymentRequest
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
	if !contains(validActions, req.Action) {
		return p.badRequestError(c, "INVALID_ACTION", "action must be either 'approve' or 'fail'")
	}

	// Prepare headers for backend call
	headers := p.prepareHeaders(c)

	// Call payment API
	if err := p.client.ProcessPayment(c.Context(), &req, headers); err != nil {
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
	}).Info("Payment processed successfully")

	return c.JSON(fiber.Map{
		"status":  "processed",
		"action":  req.Action,
		"message": "Payment processing initiated",
	})
}

// prepareHeaders prepares headers for backend API calls
func (p *PaymentHandler) prepareHeaders(c *fiber.Ctx) map[string]string {
	headers := make(map[string]string)

	// Forward authorization header
	if auth := c.Get("Authorization"); auth != "" {
		headers["Authorization"] = auth
	}

	// Forward request ID for tracing
	if requestID := c.Get("X-Request-ID"); requestID != "" {
		headers["X-Request-ID"] = requestID
	}

	// Forward idempotency key
	if idempotencyKey := c.Get("Idempotency-Key"); idempotencyKey != "" {
		headers["Idempotency-Key"] = idempotencyKey
	}

	// Forward user agent
	if userAgent := c.Get("User-Agent"); userAgent != "" {
		headers["User-Agent"] = userAgent
	}

	// Add tracing headers if available
	if traceParent := c.Get("traceparent"); traceParent != "" {
		headers["traceparent"] = traceParent
	}

	return headers
}

// handleClientError handles errors from backend client calls
func (p *PaymentHandler) handleClientError(c *fiber.Ctx, err error, operation string) error {
	// Map common client errors to appropriate HTTP status codes
	errorMsg := err.Error()

	// Check for specific error patterns
	switch {
	case contains(errorMsg, "404") || contains(errorMsg, "not found"):
		return p.notFoundError(c, "PAYMENT_NOT_FOUND", "Payment intent not found")
	case contains(errorMsg, "409") || contains(errorMsg, "conflict"):
		return p.conflictError(c, "PAYMENT_CONFLICT", "Payment conflict")
	case contains(errorMsg, "400") || contains(errorMsg, "bad request"):
		return p.badRequestError(c, "INVALID_PAYMENT", "Invalid payment request")
	case contains(errorMsg, "402") || contains(errorMsg, "payment required"):
		return p.paymentRequiredError(c, "PAYMENT_REQUIRED", "Payment required")
	case contains(errorMsg, "timeout"):
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

// Helper function to check if slice contains string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}