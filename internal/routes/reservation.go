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

// ReservationHandler handles reservation-related requests
type ReservationHandler struct {
	config   *config.Config
	logger   *logging.Logger
	metrics  *metrics.Metrics
	client   *clients.ReservationClient
}

// NewReservationHandler creates a new reservation handler
func NewReservationHandler(cfg *config.Config, logger *logging.Logger, metrics *metrics.Metrics) *ReservationHandler {
	client := clients.NewReservationClient(cfg, logger, metrics)
	return &ReservationHandler{
		config:  cfg,
		logger:  logger,
		metrics: metrics,
		client:  client,
	}
}

// SetTracing sets the tracing middleware on the client
func (h *ReservationHandler) SetTracing(tracing *middleware.TracingMiddleware) {
	h.client.SetTracing(tracing)
}

// CreateReservation handles reservation creation
func (h *ReservationHandler) CreateReservation(c *fiber.Ctx) error {
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
		EventID    string `json:"event_id" validate:"required"`
		SeatIDs    []string `json:"seat_ids" validate:"required"`
		Quantity   int    `json:"quantity,omitempty"`
		TotalPrice float64 `json:"total_price,omitempty"`
	}

	if err := c.BodyParser(&req); err != nil {
		h.logger.WarnWithContext(c.Context(), "Failed to parse reservation request", "error", err.Error())
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "Invalid request body",
			},
		})
	}

	if req.EventID == "" || len(req.SeatIDs) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "event_id and seat_ids are required",
			},
		})
	}

	requestBody := map[string]interface{}{
		"user_id":     userID,
		"event_id":    req.EventID,
		"seat_ids":    req.SeatIDs,
		"quantity":    req.Quantity,
		"total_price": req.TotalPrice,
	}

	h.logger.InfoWithContext(c.Context(), "Creating reservation",
		"user_id", userID, "event_id", req.EventID, "seats", len(req.SeatIDs))

	// Forward to reservation service
	response, err := h.client.CreateReservation(c.Context(), requestBody)
	if err != nil {
		h.logger.ErrorWithContext(c.Context(), "Failed to create reservation", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "INTERNAL_ERROR",
				"message": "Failed to create reservation",
			},
		})
	}

	return c.Status(fiber.StatusCreated).JSON(response)
}

// GetReservation handles reservation retrieval
func (h *ReservationHandler) GetReservation(c *fiber.Ctx) error {
	start := time.Now()
	defer func() {
		h.metrics.RecordHTTPRequest(c.Method(), c.Path(), c.Response().StatusCode(), time.Since(start))
	}()

	reservationID := c.Params("id")
	if reservationID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "Reservation ID is required",
			},
		})
	}

	userID := c.Locals("user_id")

	h.logger.InfoWithContext(c.Context(), "Getting reservation",
		"reservation_id", reservationID, "user_id", userID)

	response, err := h.client.GetReservation(c.Context(), reservationID)
	if err != nil {
		h.logger.ErrorWithContext(c.Context(), "Failed to get reservation", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "INTERNAL_ERROR",
				"message": "Failed to get reservation",
			},
		})
	}

	return c.Status(fiber.StatusOK).JSON(response)
}

// ListReservations handles reservation listing
func (h *ReservationHandler) ListReservations(c *fiber.Ctx) error {
	start := time.Now()
	defer func() {
		h.metrics.RecordHTTPRequest(c.Method(), c.Path(), c.Response().StatusCode(), time.Since(start))
	}()

	userID := c.Locals("user_id")

	// Parse query parameters
	params := make(map[string]string)
	if limit := c.Query("limit"); limit != "" {
		params["limit"] = limit
	}
	if offset := c.Query("offset"); offset != "" {
		params["offset"] = offset
	}
	if status := c.Query("status"); status != "" {
		params["status"] = status
	}

	h.logger.InfoWithContext(c.Context(), "Listing reservations", "user_id", userID, "params", params)

	response, err := h.client.ListReservations(c.Context(), params)
	if err != nil {
		h.logger.ErrorWithContext(c.Context(), "Failed to list reservations", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "INTERNAL_ERROR",
				"message": "Failed to list reservations",
			},
		})
	}

	return c.Status(fiber.StatusOK).JSON(response)
}

// CancelReservation handles reservation cancellation
func (h *ReservationHandler) CancelReservation(c *fiber.Ctx) error {
	start := time.Now()
	defer func() {
		h.metrics.RecordHTTPRequest(c.Method(), c.Path(), c.Response().StatusCode(), time.Since(start))
	}()

	reservationID := c.Params("id")
	if reservationID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "Reservation ID is required",
			},
		})
	}

	userID := c.Locals("user_id")

	requestBody := map[string]interface{}{
		"user_id": userID,
		"reason":  c.Query("reason", "user_cancelled"),
	}

	h.logger.InfoWithContext(c.Context(), "Cancelling reservation",
		"reservation_id", reservationID, "user_id", userID)

	response, err := h.client.CancelReservation(c.Context(), reservationID, requestBody)
	if err != nil {
		h.logger.ErrorWithContext(c.Context(), "Failed to cancel reservation", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "INTERNAL_ERROR",
				"message": "Failed to cancel reservation",
			},
		})
	}

	return c.Status(fiber.StatusOK).JSON(response)
}

// ConfirmReservation handles reservation confirmation
func (h *ReservationHandler) ConfirmReservation(c *fiber.Ctx) error {
	start := time.Now()
	defer func() {
		h.metrics.RecordHTTPRequest(c.Method(), c.Path(), c.Response().StatusCode(), time.Since(start))
	}()

	reservationID := c.Params("id")
	if reservationID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "Reservation ID is required",
			},
		})
	}

	userID := c.Locals("user_id")

	requestBody := map[string]interface{}{
		"user_id": userID,
	}

	h.logger.InfoWithContext(c.Context(), "Confirming reservation",
		"reservation_id", reservationID, "user_id", userID)

	response, err := h.client.ConfirmReservation(c.Context(), reservationID, requestBody)
	if err != nil {
		h.logger.ErrorWithContext(c.Context(), "Failed to confirm reservation", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "INTERNAL_ERROR",
				"message": "Failed to confirm reservation",
			},
		})
	}

	return c.Status(fiber.StatusOK).JSON(response)
}
