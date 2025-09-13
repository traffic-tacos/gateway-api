package routes

import (
	"github.com/gofiber/fiber/v2"
	"github.com/traffic-tacos/gateway-api/internal/config"
	"github.com/traffic-tacos/gateway-api/internal/logging"
	"github.com/traffic-tacos/gateway-api/internal/metrics"
	"github.com/traffic-tacos/gateway-api/internal/middleware"
)

// SetupRoutes configures all API routes
func SetupRoutes(api fiber.Router, cfg *config.Config, logger *logging.Logger, metrics *metrics.Metrics, mw *middleware.Manager) error {
	// Apply common middleware
	api.Use(mw.Tracing())
	api.Use(mw.AuthRequired())
	api.Use(mw.SecurityHeaders())

	// Queue routes (may be anonymous)
	queueGroup := api.Group("/queue")
	queueHandler := setupQueueRoutes(queueGroup, cfg, logger, metrics, mw)
	queueHandler.SetTracing(mw.tracing)

	// Reservation routes (authenticated, idempotent)
	reservationGroup := api.Group("/reservations")
	reservationHandler := setupReservationRoutes(reservationGroup, cfg, logger, metrics, mw)
	reservationHandler.SetTracing(mw.tracing)

	// Payment routes (authenticated, idempotent)
	paymentGroup := api.Group("/payment")
	paymentHandler := setupPaymentRoutes(paymentGroup, cfg, logger, metrics, mw)
	paymentHandler.SetTracing(mw.tracing)

	return nil
}

// setupQueueRoutes configures queue-related routes
func setupQueueRoutes(router fiber.Router, cfg *config.Config, logger *logging.Logger, metrics *metrics.Metrics, mw *middleware.Manager) *QueueHandler {
	handler := NewQueueHandler(cfg, logger, metrics)

	router.Post("/join", handler.JoinQueue)
	router.Get("/status", handler.GetQueueStatus)

	return handler
}

// setupReservationRoutes configures reservation-related routes
func setupReservationRoutes(router fiber.Router, cfg *config.Config, logger *logging.Logger, metrics *metrics.Metrics, mw *middleware.Manager) *ReservationHandler {
	handler := NewReservationHandler(cfg, logger, metrics)

	// Apply idempotency to write operations
	router.Use("/create", mw.IdempotencyRequired())
	router.Use("/cancel", mw.IdempotencyRequired())
	router.Use("/confirm", mw.IdempotencyRequired())

	router.Post("/create", handler.CreateReservation)
	router.Post("/:id/cancel", handler.CancelReservation)
	router.Post("/:id/confirm", handler.ConfirmReservation)
	router.Get("/:id", handler.GetReservation)
	router.Get("/", handler.ListReservations)

	return handler
}

// setupPaymentRoutes configures payment-related routes
func setupPaymentRoutes(router fiber.Router, cfg *config.Config, logger *logging.Logger, metrics *metrics.Metrics, mw *middleware.Manager) *PaymentHandler {
	handler := NewPaymentHandler(cfg, logger, metrics)

	// Apply idempotency to payment operations
	router.Use("/intent", mw.IdempotencyRequired())

	router.Post("/intent", handler.CreatePaymentIntent)
	router.Get("/intent/:id", handler.GetPaymentIntent)

	return handler
}
