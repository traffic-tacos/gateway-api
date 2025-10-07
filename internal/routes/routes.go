package routes

import (
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/traffic-tacos/gateway-api/internal/clients"
	"github.com/traffic-tacos/gateway-api/internal/config"
	"github.com/traffic-tacos/gateway-api/internal/metrics"
	"github.com/traffic-tacos/gateway-api/internal/middleware"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/swagger"
	"github.com/sirupsen/logrus"
)

// Setup configures all API routes
func Setup(app *fiber.App, cfg *config.Config, logger *logrus.Logger, middlewareManager *middleware.Manager, dynamoClient *dynamodb.Client) {
	// Initialize gRPC clients
	reservationClient, err := clients.NewReservationClient(&cfg.Backend.ReservationAPI, logger)
	if err != nil {
		logger.WithError(err).Fatal("Failed to create reservation client")
	}

	paymentClient, err := clients.NewPaymentClient(&cfg.Backend.PaymentAPI, logger)
	if err != nil {
		logger.WithError(err).Fatal("Failed to create payment client")
	}

	// Create route handlers
	queueHandler := NewQueueHandler(middlewareManager.RedisClient, logger)
	reservationHandler := NewReservationHandler(reservationClient, logger)
	paymentHandler := NewPaymentHandler(paymentClient, logger)
	authHandler := NewAuthHandler(dynamoClient, cfg.DynamoDB.UsersTableName, cfg.JWT.Secret, logger)
	adminHandler := NewAdminHandler(middlewareManager.RedisClient, logger)

	// Health check endpoints (no auth required)
	app.Get("/healthz", healthCheck)
	app.Get("/readyz", readinessCheck(middlewareManager))
	app.Get("/version", versionHandler)

	// Metrics endpoint (no auth required)
	app.Get(cfg.Observability.MetricsPath, metrics.PrometheusHandler())

	// Swagger documentation endpoint (no auth required)
	app.Get("/swagger/*", swagger.HandlerDefault)

	// API routes with middleware
	api := app.Group("/api/v1")

	// Apply global middleware to API routes
	api.Use(metrics.HTTPMetricsMiddleware())
	api.Use(middlewareManager.RateLimit.Handle())
	api.Use(middlewareManager.Idempotency.Handle())
	api.Use(middlewareManager.Idempotency.ResponseCapture())

	// Auth routes (public endpoints - no auth required)
	authRoutes := api.Group("/auth")
	authRoutes.Post("/login", authHandler.Login)
	authRoutes.Post("/register", authHandler.Register)

	// Queue management routes (public endpoints - no auth required)
	queueRoutes := api.Group("/queue")
	queueRoutes.Post("/join", queueHandler.Join)
	queueRoutes.Get("/status", queueHandler.Status)
	queueRoutes.Post("/enter", queueHandler.Enter)
	queueRoutes.Delete("/leave", queueHandler.Leave)

	// Admin routes (public for PoC testing - consider adding auth for production)
	adminRoutes := api.Group("/admin")
	adminRoutes.Post("/flush-test-data", adminHandler.FlushTestData)
	adminRoutes.Get("/health", adminHandler.HealthCheck)
	adminRoutes.Get("/stats", adminHandler.GetStats)

	// Protected routes (require authentication)
	// Auth 미들웨어를 보호된 라우트에만 적용
	protected := api.Group("")
	protected.Use(middlewareManager.Auth.Authenticate([]string{"/healthz", "/readyz", "/version", "/metrics", "/swagger"}))

	// Reservation routes
	reservationRoutes := protected.Group("/reservations")
	reservationRoutes.Post("/", reservationHandler.Create)
	reservationRoutes.Get("/:id", reservationHandler.Get)
	reservationRoutes.Post("/:id/confirm", reservationHandler.Confirm)
	reservationRoutes.Post("/:id/cancel", reservationHandler.Cancel)

	// Payment routes
	paymentRoutes := protected.Group("/payment")
	paymentRoutes.Post("/intent", paymentHandler.CreateIntent)
	paymentRoutes.Get("/:id/status", paymentHandler.GetStatus)
	paymentRoutes.Post("/process", paymentHandler.ProcessPayment)

	// 404 handler
	app.Use(notFoundHandler)
}

// healthCheck returns the health status of the service
// @Summary Health check
// @Description Check if the service is healthy
// @Tags System
// @Produce json
// @Success 200 {object} map[string]interface{} "Healthy"
// @Router /healthz [get]
func healthCheck(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"service":   "gateway-api",
	})
}

// readinessCheck checks if the service is ready to accept traffic
// @Summary Readiness check
// @Description Check if the service is ready to accept traffic
// @Tags System
// @Produce json
// @Success 200 {object} map[string]interface{} "Ready"
// @Failure 503 {object} map[string]interface{} "Not ready"
// @Router /readyz [get]
func readinessCheck(middlewareManager *middleware.Manager) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Check Redis connectivity
		redisHealthCheck := middleware.RedisHealthCheck(middlewareManager.RedisClient, middlewareManager.Logger)
		if err := redisHealthCheck(); err != nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"status":    "not ready",
				"reason":    "redis unavailable",
				"error":     err.Error(),
				"timestamp": time.Now().UTC(),
			})
		}

		return c.JSON(fiber.Map{
			"status":    "ready",
			"timestamp": time.Now().UTC(),
			"service":   "gateway-api",
		})
	}
}

// versionHandler returns version information
// @Summary Version information
// @Description Get service version and build information
// @Tags System
// @Produce json
// @Success 200 {object} map[string]interface{} "Version info"
// @Router /version [get]
func versionHandler(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"service": "gateway-api",
		"version": getVersion(),
		"commit":  getCommit(),
		"built":   getBuildTime(),
	})
}

// notFoundHandler handles 404 errors
func notFoundHandler(c *fiber.Ctx) error {
	return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     "NOT_FOUND",
			"message":  "The requested resource was not found",
			"path":     c.Path(),
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}

// Helper functions for version info
func getVersion() string {
	// This would typically be set during build
	return "dev"
}

func getCommit() string {
	// This would typically be set during build
	return "unknown"
}

func getBuildTime() string {
	// This would typically be set during build
	return "unknown"
}
