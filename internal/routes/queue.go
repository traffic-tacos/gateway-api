package routes

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/traffic-tacos/gateway-api/internal/clients"
	"github.com/traffic-tacos/gateway-api/internal/config"
	"github.com/traffic-tacos/gateway-api/internal/logging"
	"github.com/traffic-tacos/gateway-api/internal/metrics"
)

// QueueHandler handles queue-related requests
type QueueHandler struct {
	config   *config.Config
	logger   *logging.Logger
	metrics  *metrics.Metrics
	reservationClient *clients.ReservationClient
}

// NewQueueHandler creates a new queue handler
func NewQueueHandler(cfg *config.Config, logger *logging.Logger, metrics *metrics.Metrics) *QueueHandler {
	return &QueueHandler{
		config:   cfg,
		logger:   logger,
		metrics:  metrics,
		reservationClient: clients.NewReservationClient(cfg, logger, metrics),
	}
}

// SetTracing sets the tracing middleware on the client
func (h *QueueHandler) SetTracing(tracing *middleware.TracingMiddleware) {
	h.reservationClient.SetTracing(tracing)
}

// JoinQueue handles queue join requests
func (h *QueueHandler) JoinQueue(c *fiber.Ctx) error {
	start := time.Now()
	defer func() {
		h.metrics.RecordHTTPRequest(c.Method(), c.Path(), c.Response().StatusCode(), time.Since(start))
	}()

	// Parse request
	var req struct {
		EventID string `json:"event_id" validate:"required"`
		UserID  string `json:"user_id,omitempty"` // May be anonymous
	}

	if err := c.BodyParser(&req); err != nil {
		h.logger.WarnWithContext(c.Context(), "Failed to parse queue join request", "error", err.Error())
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "Invalid request body",
			},
		})
	}

	if req.EventID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "event_id is required",
			},
		})
	}

	// Get user ID from context (may be anonymous)
	userID := c.Locals("user_id")
	if userID == nil {
		userID = ""
	}

	h.logger.InfoWithContext(c.Context(), "Processing queue join request",
		"event_id", req.EventID, "user_id", userID)

	// Forward to reservation service
	response, err := h.reservationClient.JoinQueue(c.Context(), req.EventID, userID.(string))
	if err != nil {
		h.logger.ErrorWithContext(c.Context(), "Failed to join queue", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "INTERNAL_ERROR",
				"message": "Failed to join queue",
			},
		})
	}

	return c.Status(fiber.StatusOK).JSON(response)
}

// GetQueueStatus handles queue status requests
func (h *QueueHandler) GetQueueStatus(c *fiber.Ctx) error {
	start := time.Now()
	defer func() {
		h.metrics.RecordHTTPRequest(c.Method(), c.Path(), c.Response().StatusCode(), time.Since(start))
	}()

	// Parse query parameters
	queueToken := c.Query("token")
	if queueToken == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "token parameter is required",
			},
		})
	}

	h.logger.InfoWithContext(c.Context(), "Processing queue status request", "token", queueToken)

	// Forward to reservation service
	response, err := h.reservationClient.GetQueueStatus(c.Context(), queueToken)
	if err != nil {
		h.logger.ErrorWithContext(c.Context(), "Failed to get queue status", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "INTERNAL_ERROR",
				"message": "Failed to get queue status",
			},
		})
	}

	return c.Status(fiber.StatusOK).JSON(response)
}
