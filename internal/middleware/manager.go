package middleware

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/traffic-tacos/gateway-api/internal/config"
	"github.com/traffic-tacos/gateway-api/internal/logging"
	"github.com/traffic-tacos/gateway-api/internal/metrics"
	"github.com/traffic-tacos/gateway-api/pkg/errors"
)

// Manager manages all middleware components
type Manager struct {
	config   *config.Config
	logger   *logging.Logger
	metrics  *metrics.Metrics
	auth     *AuthMiddleware
	idempotency *IdempotencyMiddleware
	rateLimit  *RateLimitMiddleware
	tracing    *TracingMiddleware
}

// NewManager creates a new middleware manager
func NewManager(cfg *config.Config, logger *logging.Logger, metrics *metrics.Metrics) (*Manager, error) {
	// Initialize Redis client for shared use
	redisClient, err := NewRedisClient(cfg.Redis)
	if err != nil {
		return nil, errors.WrapError(err, "failed to create Redis client")
	}

	// Initialize middleware components
	auth, err := NewAuthMiddleware(cfg.JWT, logger)
	if err != nil {
		return nil, errors.WrapError(err, "failed to create auth middleware")
	}

	idempotency, err := NewIdempotencyMiddleware(cfg.Idempotency, redisClient, logger)
	if err != nil {
		return nil, errors.WrapError(err, "failed to create idempotency middleware")
	}

	rateLimit, err := NewRateLimitMiddleware(cfg.RateLimit, redisClient, logger, metrics)
	if err != nil {
		return nil, errors.WrapError(err, "failed to create rate limit middleware")
	}

	tracing, err := NewTracingMiddleware(cfg.Observability, logger)
	if err != nil {
		return nil, errors.WrapError(err, "failed to create tracing middleware")
	}

	return &Manager{
		config:      cfg,
		logger:      logger,
		metrics:     metrics,
		auth:        auth,
		idempotency: idempotency,
		rateLimit:   rateLimit,
		tracing:     tracing,
	}, nil
}

// ErrorHandler returns a Fiber error handler
func (m *Manager) ErrorHandler() fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		// Get trace ID from context
		traceID := c.Locals("trace_id")
		if traceID == nil {
			traceID = ""
		}

		// Default error response
		code := errors.CodeInternalError
		message := "Internal server error"
		status := fiber.StatusInternalServerError

		// Handle application errors
		if appErr, ok := err.(*errors.AppError); ok {
			code = appErr.Code
			message = appErr.Message
			status = appErr.HTTPStatus()

			// Log error with appropriate level
			if status >= 500 {
				m.logger.ErrorWithContext(c.Context(), "Application error", appErr)
			} else {
				m.logger.WarnWithContext(c.Context(), "Client error", "code", string(code), "message", message)
			}
		} else {
			// Log unexpected errors
			m.logger.ErrorWithContext(c.Context(), "Unexpected error", err)
		}

		// Record metrics
		m.metrics.RecordHTTPRequest(c.Method(), c.Path(), status, time.Since(c.Context().Time()))

		// Return standardized error response
		return c.Status(status).JSON(errors.ErrorResponse{
			Error: struct {
				Code    errors.ErrorCode `json:"code"`
				Message string           `json:"message"`
				TraceID string           `json:"trace_id,omitempty"`
			}{
				Code:    code,
				Message: message,
				TraceID: traceID.(string),
			},
		})
	}
}

// RequestLogger returns request logging middleware
func (m *Manager) RequestLogger() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()

		// Process request
		err := c.Next()

		// Calculate latency
		latency := time.Since(start).Milliseconds()

		// Get user ID and trace ID from context
		userID := c.Locals("user_id")
		if userID == nil {
			userID = ""
		}
		traceID := c.Locals("trace_id")
		if traceID == nil {
			traceID = ""
		}

		// Log request
		m.logger.RequestLogger(
			c.Method(),
			c.Path(),
			c.Response().StatusCode(),
			float64(latency),
			userID.(string),
			traceID.(string),
		)

		// Record metrics
		m.metrics.RecordHTTPRequest(c.Method(), c.Path(), c.Response().StatusCode(), time.Since(start))

		return err
	}
}

// AuthRequired returns JWT authentication middleware
func (m *Manager) AuthRequired() fiber.Handler {
	return m.auth.AuthRequired()
}

// IdempotencyRequired returns idempotency middleware for write operations
func (m *Manager) IdempotencyRequired() fiber.Handler {
	return m.idempotency.IdempotencyRequired()
}

// Tracing returns tracing middleware
func (m *Manager) Tracing() fiber.Handler {
	return m.tracing.Tracing()
}

// SecurityHeaders returns security headers middleware
func (m *Manager) SecurityHeaders() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Frame-Options", "DENY")
		c.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'")
		c.Set("X-XSS-Protection", "1; mode=block")
		c.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		return c.Next()
	}
}

// AnonymousRoutes returns routes that don't require authentication
func (m *Manager) AnonymousRoutes() []string {
	return []string{
		"/healthz",
		"/readyz",
		"/version",
		"/metrics",
		"/api/v1/queue/join",    // Queue join might be anonymous
		"/api/v1/queue/status",  // Queue status might be anonymous
	}
}
