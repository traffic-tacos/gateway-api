package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/helmet"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/traffic-tacos/gateway-api/internal/config"
	"github.com/traffic-tacos/gateway-api/internal/logging"
	"github.com/traffic-tacos/gateway-api/internal/metrics"
	"github.com/traffic-tacos/gateway-api/internal/middleware"
	"github.com/traffic-tacos/gateway-api/internal/routes"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize logger
	logger, err := logging.NewLogger(cfg.Observability.ServiceName)
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	logger.Info("Starting gateway-api service", "version", cfg.Observability.ServiceVersion, "port", cfg.Server.Port)

	// Initialize metrics
	metricsCollector := metrics.NewMetrics(cfg.Observability.ServiceName)

	// Initialize middleware manager
	middlewareManager, err := middleware.NewManager(cfg, logger, metricsCollector)
	if err != nil {
		logger.Fatal("Failed to initialize middleware manager", "error", err)
	}

	// Create Fiber app
	app := fiber.New(fiber.Config{
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
		BodyLimit:    cfg.Server.MaxBodySize,
		ErrorHandler: middlewareManager.ErrorHandler(),
	})

	// Global middleware
	app.Use(recover.New())
	app.Use(helmet.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins:     "https://tickets.example.com,https://*.tickets.example.com",
		AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS",
		AllowHeaders:     "Origin,Content-Type,Accept,Authorization,X-Requested-With,X-Idempotency-Key",
		AllowCredentials: true,
		MaxAge:           86400, // 24 hours
	}))

	// Health check endpoints (before auth middleware)
	app.Get("/healthz", func(c *fiber.Ctx) error {
		return c.Status(http.StatusOK).JSON(fiber.Map{
			"status": "ok",
			"time":   time.Now().Unix(),
		})
	})

	app.Get("/readyz", func(c *fiber.Ctx) error {
		return c.Status(http.StatusOK).JSON(fiber.Map{
			"status": "ready",
			"time":   time.Now().Unix(),
		})
	})

	app.Get("/version", func(c *fiber.Ctx) error {
		return c.Status(http.StatusOK).JSON(fiber.Map{
			"service": cfg.Observability.ServiceName,
			"version": cfg.Observability.ServiceVersion,
			"build":   "dev", // TODO: Add build info
		})
	})

	// Metrics endpoint
	app.Get("/metrics", promhttp.Handler())

	// Rate limiting middleware (global)
	app.Use(limiter.New(limiter.Config{
		Max: cfg.RateLimit.RPS,
		KeyGenerator: func(c *fiber.Ctx) string {
			// Use IP address for rate limiting
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			metricsCollector.RecordRateLimitDrop()
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": fiber.Map{
					"code":    "RATE_LIMITED",
					"message": "Rate limit exceeded",
				},
			})
		},
	}))

	// Request logging and metrics middleware
	app.Use(middlewareManager.RequestLogger())

	// API routes group
	api := app.Group("/api/v1")

	// Initialize routes
	if err := routes.SetupRoutes(api, cfg, logger, metricsCollector, middlewareManager); err != nil {
		logger.Fatal("Failed to setup routes", "error", err)
	}

	// Graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		addr := fmt.Sprintf(":%d", cfg.Server.Port)
		logger.Info("Server starting", "addr", addr)

		if err := app.Listen(addr); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Server failed to start", "error", err)
		}
	}()

	<-c
	logger.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.ShutdownWithContext(ctx); err != nil {
		logger.Error("Server forced to shutdown", "error", err)
	}

	logger.Info("Server shutdown complete")
}
