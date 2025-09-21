package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

type IdempotencyMiddleware struct {
	redisClient *redis.Client
	logger      *logrus.Logger
	ttl         time.Duration
}

type IdempotencyRecord struct {
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
	CreatedAt  time.Time         `json:"created_at"`
}

func NewIdempotencyMiddleware(redisClient *redis.Client, logger *logrus.Logger) *IdempotencyMiddleware {
	return &IdempotencyMiddleware{
		redisClient: redisClient,
		logger:      logger,
		ttl:         5 * time.Minute, // 5-minute TTL as per spec
	}
}

// Idempotency middleware for handling duplicate requests
func (i *IdempotencyMiddleware) Handle() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Only apply to state-changing methods
		method := c.Method()
		if !isIdempotentMethod(method) {
			idempotencyKey := c.Get("Idempotency-Key")
			if idempotencyKey == "" {
				return i.badRequestError(c, "IDEMPOTENCY_REQUIRED", "Idempotency-Key header is required for "+method+" requests")
			}

			// Validate idempotency key format (should be UUID v4)
			if _, err := uuid.Parse(idempotencyKey); err != nil {
				return i.badRequestError(c, "INVALID_IDEMPOTENCY_KEY", "Idempotency-Key must be a valid UUID v4")
			}

			// Generate request fingerprint
			fingerprint := i.generateFingerprint(c)
			redisKey := fmt.Sprintf("idempotency:%s", idempotencyKey)

			// Check if request exists in Redis
			ctx := context.Background()
			existingRecord, err := i.getIdempotencyRecord(ctx, redisKey)
			if err != nil && err != redis.Nil {
				i.logger.WithError(err).Error("Failed to get idempotency record")
				// Continue with request rather than failing
			}

			if existingRecord != nil {
				// Verify request fingerprint matches
				existingFingerprint, err := i.redisClient.Get(ctx, redisKey+":fingerprint").Result()
				if err != nil && err != redis.Nil {
					i.logger.WithError(err).Error("Failed to get fingerprint")
				}

				if existingFingerprint != "" && existingFingerprint != fingerprint {
					return i.conflictError(c, "IDEMPOTENCY_CONFLICT", "Request body differs from original request with same Idempotency-Key")
				}

				// Return cached response
				return i.returnCachedResponse(c, existingRecord)
			}

			// Store fingerprint for conflict detection
			if err := i.redisClient.Set(ctx, redisKey+":fingerprint", fingerprint, i.ttl).Err(); err != nil {
				i.logger.WithError(err).Error("Failed to store fingerprint")
			}

			// Set up response capture
			c.Locals("idempotency_key", idempotencyKey)
			c.Locals("redis_key", redisKey)
		}

		return c.Next()
	}
}

// ResponseCapture middleware to capture and cache successful responses
func (i *IdempotencyMiddleware) ResponseCapture() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Get idempotency context
		idempotencyKey, ok := c.Locals("idempotency_key").(string)
		if !ok {
			return c.Next()
		}

		redisKey, ok := c.Locals("redis_key").(string)
		if !ok {
			return c.Next()
		}

		// Process the request
		err := c.Next()

		// Only cache successful responses (2xx status codes)
		statusCode := c.Response().StatusCode()
		if statusCode >= 200 && statusCode < 300 {
			// Capture response for caching
			record := IdempotencyRecord{
				StatusCode: statusCode,
				Headers:    make(map[string]string),
				Body:       string(c.Response().Body()),
				CreatedAt:  time.Now(),
			}

			// Capture relevant headers
			c.Response().Header.VisitAll(func(key, value []byte) {
				keyStr := string(key)
				// Only cache specific headers
				if i.shouldCacheHeader(keyStr) {
					record.Headers[keyStr] = string(value)
				}
			})

			// Store in Redis
			ctx := context.Background()
			if err := i.storeIdempotencyRecord(ctx, redisKey, &record); err != nil {
				i.logger.WithError(err).WithField("idempotency_key", idempotencyKey).Error("Failed to store idempotency record")
			} else {
				i.logger.WithFields(logrus.Fields{
					"idempotency_key": idempotencyKey,
					"status_code":     statusCode,
				}).Debug("Stored idempotency record")
			}
		}

		return err
	}
}

// generateFingerprint creates a unique fingerprint for the request
func (i *IdempotencyMiddleware) generateFingerprint(c *fiber.Ctx) string {
	h := sha256.New()

	// Include method
	h.Write([]byte(c.Method()))
	h.Write([]byte(":"))

	// Include path
	h.Write([]byte(c.Path()))
	h.Write([]byte(":"))

	// Include query parameters
	h.Write([]byte(c.Request().URI().QueryString()))
	h.Write([]byte(":"))

	// Include body
	h.Write(c.Body())
	h.Write([]byte(":"))

	// Include user ID if available
	if userID := GetUserID(c); userID != "" {
		h.Write([]byte(userID))
	}

	return hex.EncodeToString(h.Sum(nil))
}

// getIdempotencyRecord retrieves a stored idempotency record
func (i *IdempotencyMiddleware) getIdempotencyRecord(ctx context.Context, key string) (*IdempotencyRecord, error) {
	data, err := i.redisClient.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	var record IdempotencyRecord
	if err := json.Unmarshal([]byte(data), &record); err != nil {
		return nil, fmt.Errorf("failed to unmarshal idempotency record: %w", err)
	}

	return &record, nil
}

// storeIdempotencyRecord stores an idempotency record in Redis
func (i *IdempotencyMiddleware) storeIdempotencyRecord(ctx context.Context, key string, record *IdempotencyRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal idempotency record: %w", err)
	}

	return i.redisClient.Set(ctx, key, data, i.ttl).Err()
}

// returnCachedResponse returns a previously cached response
func (i *IdempotencyMiddleware) returnCachedResponse(c *fiber.Ctx, record *IdempotencyRecord) error {
	// Set cached headers
	for key, value := range record.Headers {
		c.Set(key, value)
	}

	// Add idempotency header to indicate this is a cached response
	c.Set("X-Idempotency-Cached", "true")

	return c.Status(record.StatusCode).SendString(record.Body)
}

// shouldCacheHeader determines if a header should be cached
func (i *IdempotencyMiddleware) shouldCacheHeader(header string) bool {
	header = strings.ToLower(header)

	// Headers to cache
	cacheable := []string{
		"content-type",
		"content-length",
		"location",
		"x-request-id",
	}

	for _, h := range cacheable {
		if header == h {
			return true
		}
	}

	return false
}

// isIdempotentMethod checks if the HTTP method is naturally idempotent
func isIdempotentMethod(method string) bool {
	idempotentMethods := []string{"GET", "HEAD", "OPTIONS", "PUT", "DELETE"}
	for _, m := range idempotentMethods {
		if method == m {
			return true
		}
	}
	return false
}

// badRequestError returns a standardized bad request error
func (i *IdempotencyMiddleware) badRequestError(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     code,
			"message":  message,
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}

// conflictError returns a standardized conflict error
func (i *IdempotencyMiddleware) conflictError(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusConflict).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     code,
			"message":  message,
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}