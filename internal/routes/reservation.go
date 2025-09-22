package routes

import (
	"github.com/traffic-tacos/gateway-api/internal/clients"
	"github.com/traffic-tacos/gateway-api/internal/middleware"
	"github.com/traffic-tacos/gateway-api/internal/utils"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

type ReservationHandler struct {
	client *clients.ReservationClient
	logger *logrus.Logger
}

func NewReservationHandler(client *clients.ReservationClient, logger *logrus.Logger) *ReservationHandler {
	return &ReservationHandler{
		client: client,
		logger: logger,
	}
}

// Create handles reservation creation
// @Summary Create a new reservation
// @Description Create a new ticket reservation for an event
// @Tags Reservations
// @Accept json
// @Produce json
// @Security Bearer
// @Param Idempotency-Key header string true "Idempotency key (UUID v4)"
// @Param request body clients.CreateReservationRequest true "Reservation request"
// @Success 201 {object} clients.ReservationResponse
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 409 {object} map[string]interface{} "Conflict"
// @Failure 500 {object} map[string]interface{} "Internal error"
// @Router /reservations [post]
func (r *ReservationHandler) Create(c *fiber.Ctx) error {
	var req clients.CreateReservationRequest
	if err := c.BodyParser(&req); err != nil {
		return r.badRequestError(c, "INVALID_REQUEST", "Invalid request body")
	}

	// Validate required fields
	if req.EventID == "" {
		return r.badRequestError(c, "MISSING_EVENT_ID", "event_id is required")
	}

	if len(req.SeatIDs) == 0 && req.Quantity <= 0 {
		return r.badRequestError(c, "MISSING_SEATS", "Either seat_ids or quantity must be provided")
	}

	// Get user ID from auth context
	userID := middleware.GetUserID(c)
	if userID == "" {
		return r.unauthorizedError(c, "MISSING_USER", "User authentication required")
	}
	req.UserID = userID

	// Prepare headers for backend call
	headers := r.prepareHeaders(c)

	// Call reservation API
	reservation, err := r.client.CreateReservation(c.Context(), &req, headers)
	if err != nil {
		r.logger.WithError(err).WithFields(logrus.Fields{
			"event_id": req.EventID,
			"user_id":  req.UserID,
			"quantity": req.Quantity,
		}).Error("Failed to create reservation")

		return r.handleClientError(c, err, "create reservation")
	}

	r.logger.WithFields(logrus.Fields{
		"reservation_id": reservation.ReservationID,
		"event_id":       req.EventID,
		"user_id":        req.UserID,
		"status":         reservation.Status,
	}).Info("Reservation created successfully")

	return c.Status(fiber.StatusCreated).JSON(reservation)
}

// Get handles reservation retrieval
// @Summary Get reservation details
// @Description Retrieve details of a specific reservation
// @Tags Reservations
// @Produce json
// @Security Bearer
// @Param id path string true "Reservation ID"
// @Success 200 {object} clients.ReservationResponse
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 403 {object} map[string]interface{} "Forbidden"
// @Failure 404 {object} map[string]interface{} "Not found"
// @Failure 500 {object} map[string]interface{} "Internal error"
// @Router /reservations/{id} [get]
func (r *ReservationHandler) Get(c *fiber.Ctx) error {
	reservationID := c.Params("id")
	if reservationID == "" {
		return r.badRequestError(c, "MISSING_ID", "Reservation ID is required")
	}

	// Prepare headers for backend call
	headers := r.prepareHeaders(c)

	// Call reservation API
	reservation, err := r.client.GetReservation(c.Context(), reservationID, headers)
	if err != nil {
		r.logger.WithError(err).WithField("reservation_id", reservationID).Error("Failed to get reservation")
		return r.handleClientError(c, err, "get reservation")
	}

	// Check if user owns this reservation (if user context available)
	userID := middleware.GetUserID(c)
	if userID != "" && reservation.UserID != userID {
		return r.forbiddenError(c, "ACCESS_DENIED", "You can only access your own reservations")
	}

	return c.JSON(reservation)
}

// Confirm handles reservation confirmation
// @Summary Confirm a reservation
// @Description Confirm a reservation after payment approval
// @Tags Reservations
// @Accept json
// @Produce json
// @Security Bearer
// @Param Idempotency-Key header string false "Idempotency key (UUID v4)"
// @Param id path string true "Reservation ID"
// @Param request body clients.ConfirmReservationRequest false "Confirmation request"
// @Success 200 {object} clients.ConfirmReservationResponse
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 404 {object} map[string]interface{} "Not found"
// @Failure 412 {object} map[string]interface{} "Payment not approved"
// @Failure 500 {object} map[string]interface{} "Internal error"
// @Router /reservations/{id}/confirm [post]
func (r *ReservationHandler) Confirm(c *fiber.Ctx) error {
	reservationID := c.Params("id")
	if reservationID == "" {
		return r.badRequestError(c, "MISSING_ID", "Reservation ID is required")
	}

	var req clients.ConfirmReservationRequest
	if err := c.BodyParser(&req); err != nil {
		// Allow empty body for confirmation
		req = clients.ConfirmReservationRequest{}
	}

	// Prepare headers for backend call
	headers := r.prepareHeaders(c)

	// Call reservation API
	confirmation, err := r.client.ConfirmReservation(c.Context(), reservationID, &req, headers)
	if err != nil {
		r.logger.WithError(err).WithField("reservation_id", reservationID).Error("Failed to confirm reservation")
		return r.handleClientError(c, err, "confirm reservation")
	}

	r.logger.WithFields(logrus.Fields{
		"reservation_id": reservationID,
		"order_id":       confirmation.OrderID,
		"status":         confirmation.Status,
		"user_id":        middleware.GetUserID(c),
	}).Info("Reservation confirmed successfully")

	return c.JSON(confirmation)
}

// Cancel handles reservation cancellation
// @Summary Cancel a reservation
// @Description Cancel an existing reservation and release the seats
// @Tags Reservations
// @Produce json
// @Security Bearer
// @Param Idempotency-Key header string false "Idempotency key (UUID v4)"
// @Param id path string true "Reservation ID"
// @Success 200 {object} map[string]interface{} "Success"
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 404 {object} map[string]interface{} "Not found"
// @Failure 500 {object} map[string]interface{} "Internal error"
// @Router /reservations/{id}/cancel [post]
func (r *ReservationHandler) Cancel(c *fiber.Ctx) error {
	reservationID := c.Params("id")
	if reservationID == "" {
		return r.badRequestError(c, "MISSING_ID", "Reservation ID is required")
	}

	// Prepare headers for backend call
	headers := r.prepareHeaders(c)

	// Call reservation API
	if err := r.client.CancelReservation(c.Context(), reservationID, headers); err != nil {
		r.logger.WithError(err).WithField("reservation_id", reservationID).Error("Failed to cancel reservation")
		return r.handleClientError(c, err, "cancel reservation")
	}

	r.logger.WithFields(logrus.Fields{
		"reservation_id": reservationID,
		"user_id":        middleware.GetUserID(c),
	}).Info("Reservation cancelled successfully")

	return c.JSON(fiber.Map{
		"status": "CANCELLED",
	})
}

// prepareHeaders prepares headers for backend API calls
func (r *ReservationHandler) prepareHeaders(c *fiber.Ctx) map[string]string {
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
func (r *ReservationHandler) handleClientError(c *fiber.Ctx, err error, operation string) error {
	// Map common client errors to appropriate HTTP status codes
	errorMsg := err.Error()

	// Check for specific error patterns
	switch {
	case utils.ContainsSubstring(errorMsg, "404") || utils.ContainsSubstring(errorMsg, "not found"):
		return r.notFoundError(c, "RESERVATION_NOT_FOUND", "Reservation not found")
	case utils.ContainsSubstring(errorMsg, "409") || utils.ContainsSubstring(errorMsg, "conflict"):
		return r.conflictError(c, "RESERVATION_CONFLICT", "Reservation conflict")
	case utils.ContainsSubstring(errorMsg, "400") || utils.ContainsSubstring(errorMsg, "bad request"):
		return r.badRequestError(c, "INVALID_RESERVATION", "Invalid reservation request")
	case utils.ContainsSubstring(errorMsg, "412") || utils.ContainsSubstring(errorMsg, "payment not approved"):
		return r.preconditionError(c, "PAYMENT_NOT_APPROVED", "Payment approval required before confirmation")
	case utils.ContainsSubstring(errorMsg, "timeout"):
		return r.gatewayTimeoutError(c, "UPSTREAM_TIMEOUT", "Backend service timeout")
	default:
		return r.internalError(c, "RESERVATION_ERROR", "Failed to "+operation)
	}
}

// Error response helpers
func (r *ReservationHandler) badRequestError(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     code,
			"message":  message,
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}

func (r *ReservationHandler) unauthorizedError(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     code,
			"message":  message,
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}

func (r *ReservationHandler) forbiddenError(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     code,
			"message":  message,
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}

func (r *ReservationHandler) notFoundError(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     code,
			"message":  message,
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}

func (r *ReservationHandler) conflictError(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusConflict).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     code,
			"message":  message,
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}

func (r *ReservationHandler) preconditionError(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusPreconditionFailed).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     code,
			"message":  message,
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}

func (r *ReservationHandler) gatewayTimeoutError(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusGatewayTimeout).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     code,
			"message":  message,
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}

func (r *ReservationHandler) internalError(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     code,
			"message":  message,
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}

