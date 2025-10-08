package middleware

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

type ErrorLoggerMiddleware struct {
	logger *logrus.Logger
}

func NewErrorLoggerMiddleware(logger *logrus.Logger) *ErrorLoggerMiddleware {
	return &ErrorLoggerMiddleware{
		logger: logger,
	}
}

// Handle logs 4xx and 5xx responses with detailed context
func (e *ErrorLoggerMiddleware) Handle() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Record start time
		startTime := time.Now()

		// Continue with request
		err := c.Next()

		// Get response status code
		statusCode := c.Response().StatusCode()

		// Log 4xx and 5xx errors
		if statusCode >= 400 {
			duration := time.Since(startTime)

			logFields := logrus.Fields{
				"status_code":   statusCode,
				"method":        c.Method(),
				"path":          c.Path(),
				"ip":            c.IP(),
				"user_agent":    c.Get("User-Agent"),
				"request_id":    c.Get("X-Request-ID"),
				"trace_id":      c.Get("X-Trace-ID"),
				"duration_ms":   duration.Milliseconds(),
				"response_size": len(c.Response().Body()),
			}

			// Add user ID if available
			if userID := GetUserID(c); userID != "" {
				logFields["user_id"] = userID
			}

			// Add idempotency key if present
			if idempotencyKey := c.Get("Idempotency-Key"); idempotencyKey != "" {
				logFields["idempotency_key"] = idempotencyKey
			}

			// Add query parameters if present
			if len(c.Request().URI().QueryString()) > 0 {
				logFields["query"] = string(c.Request().URI().QueryString())
			}

			// Add request body for POST/PUT/PATCH (truncate if too long)
			if c.Method() == "POST" || c.Method() == "PUT" || c.Method() == "PATCH" {
				body := string(c.Body())
				if len(body) > 500 {
					body = body[:500] + "...(truncated)"
				}
				if len(body) > 0 {
					logFields["request_body"] = body
				}
			}

			// Add response body (truncate if too long)
			responseBody := string(c.Response().Body())
			if len(responseBody) > 500 {
				responseBody = responseBody[:500] + "...(truncated)"
			}
			if len(responseBody) > 0 {
				logFields["response_body"] = responseBody
			}

			// Determine log level based on status code
			logEntry := e.logger.WithFields(logFields)

			if statusCode >= 500 {
				// 5xx errors are server errors - log as Error
				if err != nil {
					logEntry = logEntry.WithError(err)
				}
				logEntry.Error("Server error response")
			} else if statusCode >= 400 {
				// 4xx errors are client errors - log as Warning
				logEntry.Warn("Client error response")
			}
		}

		return err
	}
}
