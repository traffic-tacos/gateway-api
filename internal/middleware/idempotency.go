package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/traffic-tacos/gateway-api/internal/config"
	"github.com/traffic-tacos/gateway-api/internal/logging"
	"github.com/traffic-tacos/gateway-api/pkg/errors"
)

// IdempotencyMiddleware handles idempotency key validation
type IdempotencyMiddleware struct {
	ttlSeconds int
	redis      redis.Cmdable
	logger     *logging.Logger
}

// IdempotencyRecord represents stored idempotency data
type IdempotencyRecord struct {
	RequestHash string    `json:"request_hash"`
	Response    string    `json:"response,omitempty"`
	StatusCode  int       `json:"status_code,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// NewIdempotencyMiddleware creates a new idempotency middleware
func NewIdempotencyMiddleware(cfg config.IdempotencyConfig, redisClient redis.Cmdable, logger *logging.Logger) (*IdempotencyMiddleware, error) {
	return &IdempotencyMiddleware{
		ttlSeconds: cfg.TTLSeconds,
		redis:      redisClient,
		logger:     logger,
	}, nil
}

// IdempotencyRequired returns the idempotency middleware for write operations
func (i *IdempotencyMiddleware) IdempotencyRequired() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Only apply to write operations
		if !i.isWriteOperation(c.Method()) {
			return c.Next()
		}

		// Extract idempotency key
		key := c.Get("Idempotency-Key")
		if key == "" {
			return errors.NewAppError(errors.CodeIdempotencyRequired, "Idempotency-Key header is required for write operations", nil)
		}

		// Validate key format (UUID v4)
		if _, err := uuid.Parse(key); err != nil {
			return errors.NewAppError(errors.CodeBadRequest, "Idempotency-Key must be a valid UUID v4", nil)
		}

		// Calculate request hash
		requestHash, err := i.calculateRequestHash(c)
		if err != nil {
			i.logger.ErrorWithContext(c.Context(), "Failed to calculate request hash", err)
			return errors.NewAppError(errors.CodeInternalError, "Failed to process request", err)
		}

		// Check for existing record
		existingRecord, err := i.getIdempotencyRecord(c.Context(), key)
		if err != nil {
			i.logger.ErrorWithContext(c.Context(), "Failed to check idempotency record", err)
			return errors.NewAppError(errors.CodeInternalError, "Failed to process request", err)
		}

		if existingRecord != nil {
			// Check if request hash matches
			if existingRecord.RequestHash != requestHash {
				return errors.NewAppError(errors.CodeIdempotencyConflict,
					"Idempotency-Key already used with different request", nil)
			}

			// Return cached response if available
			if existingRecord.Response != "" {
				i.logger.InfoWithContext(c.Context(), "Returning cached idempotent response",
					"key", key, "status", existingRecord.StatusCode)

				// Set cached response
				c.Response().SetStatusCode(existingRecord.StatusCode)
				c.Response().SetBodyString(existingRecord.Response)
				c.Response().Header.SetContentType("application/json")

				return nil
			}
		}

		// Store initial record
		record := &IdempotencyRecord{
			RequestHash: requestHash,
			CreatedAt:   time.Now(),
		}

		if err := i.storeIdempotencyRecord(c.Context(), key, record); err != nil {
			i.logger.ErrorWithContext(c.Context(), "Failed to store idempotency record", err)
			return errors.NewAppError(errors.CodeInternalError, "Failed to process request", err)
		}

		// Store key in context for response caching
		c.Locals("idempotency_key", key)
		c.Locals("idempotency_record", record)

		return c.Next()
	}
}

// isWriteOperation checks if the HTTP method is a write operation
func (i *IdempotencyMiddleware) isWriteOperation(method string) bool {
	return method == fiber.MethodPost || method == fiber.MethodPut || method == fiber.MethodPatch || method == fiber.MethodDelete
}

// calculateRequestHash calculates a hash of the request body and key parameters
func (i *IdempotencyMiddleware) calculateRequestHash(c *fiber.Ctx) (string, error) {
	body := c.Body()

	// Create hash of body + method + path
	hash := sha256.New()
	hash.Write([]byte(c.Method()))
	hash.Write([]byte(c.Path()))
	hash.Write(body)

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// getIdempotencyRecord retrieves an idempotency record from Redis
func (i *IdempotencyMiddleware) getIdempotencyRecord(ctx context.Context, key string) (*IdempotencyRecord, error) {
	redisKey := i.buildRedisKey(key)

	val, err := i.redis.Get(ctx, redisKey).Result()
	if err == redis.Nil {
		return nil, nil // Key doesn't exist
	}
	if err != nil {
		return nil, err
	}

	// Parse JSON record
	var record IdempotencyRecord
	if err := json.Unmarshal([]byte(val), &record); err != nil {
		return nil, err
	}

	return &record, nil
}

// storeIdempotencyRecord stores an idempotency record in Redis
func (i *IdempotencyMiddleware) storeIdempotencyRecord(ctx context.Context, key string, record *IdempotencyRecord) error {
	redisKey := i.buildRedisKey(key)

	data, err := json.Marshal(record)
	if err != nil {
		return err
	}

	return i.redis.Set(ctx, redisKey, data, time.Duration(i.ttlSeconds)*time.Second).Err()
}

// updateIdempotencyRecord updates the response in an existing idempotency record
func (i *IdempotencyMiddleware) updateIdempotencyRecord(ctx context.Context, key string, statusCode int, responseBody string) error {
	record, err := i.getIdempotencyRecord(ctx, key)
	if err != nil {
		return err
	}
	if record == nil {
		return fmt.Errorf("idempotency record not found for key: %s", key)
	}

	// Update record with response
	record.StatusCode = statusCode
	record.Response = responseBody

	return i.storeIdempotencyRecord(ctx, key, record)
}

// buildRedisKey builds the Redis key for idempotency records
func (i *IdempotencyMiddleware) buildRedisKey(key string) string {
	return fmt.Sprintf("idempotency:%s", key)
}

// CacheResponseIfNeeded caches the response for idempotency if applicable
func (i *IdempotencyMiddleware) CacheResponseIfNeeded(c *fiber.Ctx) {
	key := c.Locals("idempotency_key")
	if key == nil {
		return // Not an idempotent request
	}

	keyStr, ok := key.(string)
	if !ok {
		return
	}

	// Only cache successful responses (2xx)
	statusCode := c.Response().StatusCode()
	if statusCode < 200 || statusCode >= 300 {
		return
	}

	// Get response body
	responseBody := string(c.Response().Body())

	// Update record with response
	if err := i.updateIdempotencyRecord(c.Context(), keyStr, statusCode, responseBody); err != nil {
		i.logger.ErrorWithContext(c.Context(), "Failed to cache idempotent response", err)
	} else {
		i.logger.DebugWithContext(c.Context(), "Cached idempotent response", "key", keyStr)
	}
}
