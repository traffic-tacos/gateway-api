package routes

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/traffic-tacos/gateway-api/internal/clients"
	"github.com/traffic-tacos/gateway-api/internal/config"
	"github.com/traffic-tacos/gateway-api/internal/logging"
	"github.com/traffic-tacos/gateway-api/internal/metrics"
	"github.com/traffic-tacos/gateway-api/internal/middleware"
)

// PaymentHandler handles payment-related requests
type PaymentHandler struct {
	config  *config.Config
	logger  *logging.Logger
	metrics *metrics.Metrics
	client  *clients.PaymentClient
}

// NewPaymentHandler creates a new payment handler
func NewPaymentHandler(cfg *config.Config, logger *logging.Logger, metrics *metrics.Metrics) *PaymentHandler {
	client := clients.NewPaymentClient(cfg, logger, metrics)
	return &PaymentHandler{
		config:  cfg,
		logger:  logger,
		metrics: metrics,
		client:  client,
	}
}

// SetTracing sets the tracing middleware on the client
func (h *PaymentHandler) SetTracing(tracing *middleware.TracingMiddleware) {
	h.client.SetTracing(tracing)
}

// CreatePaymentIntent handles payment intent creation
func (h *PaymentHandler) CreatePaymentIntent(c *fiber.Ctx) error {
	start := time.Now()
	defer func() {
		h.metrics.RecordHTTPRequest(c.Method(), c.Path(), c.Response().StatusCode(), time.Since(start))
	}()

	userID := c.Locals("user_id")
	if userID == nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "UNAUTHENTICATED",
				"message": "User not authenticated",
			},
		})
	}

	// Parse request
	var req struct {
		Amount      float64 `json:"amount" validate:"required,min=0"`
		Currency    string  `json:"currency" validate:"required"`
		ReservationID string `json:"reservation_id" validate:"required"`
		Description string  `json:"description,omitempty"`
	}

	if err := c.BodyParser(&req); err != nil {
		h.logger.WarnWithContext(c.Context(), "Failed to parse payment intent request", "error", err.Error())
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "Invalid request body",
			},
		})
	}

	if req.Amount <= 0 || req.Currency == "" || req.ReservationID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "amount (>0), currency, and reservation_id are required",
			},
		})
	}

	requestBody := map[string]interface{}{
		"user_id":         userID,
		"amount":          req.Amount,
		"currency":        req.Currency,
		"reservation_id":  req.ReservationID,
		"description":     req.Description,
	}

	h.logger.InfoWithContext(c.Context(), "Creating payment intent",
		"user_id", userID, "amount", req.Amount, "currency", req.Currency)

	// Forward to payment service
	response, err := h.client.CreatePaymentIntent(c.Context(), requestBody)
	if err != nil {
		h.logger.ErrorWithContext(c.Context(), "Failed to create payment intent", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "INTERNAL_ERROR",
				"message": "Failed to create payment intent",
			},
		})
	}

	return c.Status(fiber.StatusCreated).JSON(response)
}

// GetPaymentIntent handles payment intent retrieval
func (h *PaymentHandler) GetPaymentIntent(c *fiber.Ctx) error {
	start := time.Now()
	defer func() {
		h.metrics.RecordHTTPRequest(c.Method(), c.Path(), c.Response().StatusCode(), time.Since(start))
	}()

	intentID := c.Params("id")
	if intentID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "Payment intent ID is required",
			},
		})
	}

	userID := c.Locals("user_id")

	h.logger.InfoWithContext(c.Context(), "Getting payment intent",
		"intent_id", intentID, "user_id", userID)

	response, err := h.client.GetPaymentIntent(c.Context(), intentID)
	if err != nil {
		h.logger.ErrorWithContext(c.Context(), "Failed to get payment intent", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "INTERNAL_ERROR",
				"message": "Failed to get payment intent",
			},
		})
	}

	return c.Status(fiber.StatusOK).JSON(response)
}
