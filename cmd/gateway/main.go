package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/traffic-tacos/gateway-api/internal/config"
	"github.com/traffic-tacos/gateway-api/internal/logging"
	"github.com/traffic-tacos/gateway-api/internal/metrics"
	"github.com/traffic-tacos/gateway-api/internal/middleware"
	"github.com/traffic-tacos/gateway-api/internal/routes"
	_ "github.com/traffic-tacos/gateway-api/docs" // Swagger docs

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/helmet"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/sirupsen/logrus"
)

// @title Gateway API
// @version 1.0
// @description High-performance BFF for Traffic Tacos ticket reservation system
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.url http://www.swagger.io/support
// @contact.email support@swagger.io

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @host localhost:8000
// @BasePath /api/v1

// @securityDefinitions.apikey Bearer
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token.

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logrus.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize logger
	logger := logging.New(cfg)

	// Initialize metrics
	if err := metrics.Init(); err != nil {
		logger.WithError(err).Fatal("Failed to initialize metrics")
	}

	// Initialize tracing
	tracingShutdown, err := setupTracing(cfg, logger)
	if err != nil {
		logger.WithError(err).Fatal("Failed to setup tracing")
	}
	defer tracingShutdown()

	// Create Fiber app
	app := fiber.New(fiber.Config{
		AppName:      "Gateway API",
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}

			logger.WithError(err).WithFields(logrus.Fields{
				"method": c.Method(),
				"path":   c.Path(),
				"status": code,
			}).Error("Request error")

			return c.Status(code).JSON(fiber.Map{
				"error": fiber.Map{
					"code":     "INTERNAL_ERROR",
					"message":  "Internal server error",
					"trace_id": c.Get("X-Trace-ID"),
				},
			})
		},
	})

	// Global middleware
	app.Use(recover.New())
	app.Use(requestid.New())
	app.Use(helmet.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins:     cfg.CORS.AllowOrigins,
		AllowMethods:     "GET,POST,HEAD,PUT,DELETE,PATCH,OPTIONS",
		AllowHeaders:     "Origin,Content-Type,Accept,Authorization,X-Requested-With,Idempotency-Key,X-Trace-Id",
		AllowCredentials: true,
		MaxAge:           86400,
	}))

	// Initialize middleware manager
	middlewareManager, err := middleware.NewManager(cfg, logger)
	if err != nil {
		logger.WithError(err).Fatal("Failed to initialize middleware manager")
	}

	// Setup routes
	routes.Setup(app, cfg, logger, middlewareManager)

	// Graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		logger.Info("Gracefully shutting down...")
		app.Shutdown()
	}()

	// Start server
	logger.WithField("port", cfg.Server.Port).Info("Starting Gateway API server")
	if err := app.Listen(":" + cfg.Server.Port); err != nil {
		logger.WithError(err).Fatal("Server failed to start")
	}
}

func setupTracing(cfg *config.Config, logger *logrus.Logger) (func(), error) {
	shutdown, err := middleware.InitTracing(&cfg.Observability, logger)
	if err != nil {
		return nil, err
	}

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(ctx); err != nil {
			logger.WithError(err).Error("Failed to shutdown tracing")
		}
	}, nil
}