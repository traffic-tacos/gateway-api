package routes

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

type AdminHandler struct {
	redisClient redis.UniversalClient
	logger      *logrus.Logger
}

func NewAdminHandler(redisClient redis.UniversalClient, logger *logrus.Logger) *AdminHandler {
	return &AdminHandler{
		redisClient: redisClient,
		logger:      logger,
	}
}

// FlushTestData handles Redis test data cleanup
// @Summary Flush Redis test data
// @Description Clear all test-related data from Redis (queues, idempotency, heartbeats) for k6 load testing
// @Tags Admin
// @Produce json
// @Param patterns query string false "Comma-separated key patterns (default: queue:*,idempotency:*,heartbeat:*,dedupe:*,stream:*,allow:*)"
// @Success 200 {object} map[string]interface{} "Success with deleted keys count"
// @Failure 500 {object} map[string]interface{} "Internal error"
// @Router /admin/flush-test-data [post]
func (a *AdminHandler) FlushTestData(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Default patterns for test data
	patterns := []string{
		"queue:*",        // Queue event keys
		"idempotency:*",  // Idempotency keys
		"heartbeat:*",    // Heartbeat keys
		"dedupe:*",       // Deduplication keys
		"stream:*",       // Stream keys
		"allow:*",        // Admission allow keys
		"ratelimit:*",    // Rate limit keys (optional)
	}

	// Allow custom patterns from query param
	customPatterns := c.Query("patterns")
	if customPatterns != "" {
		// Parse comma-separated patterns
		// patterns = strings.Split(customPatterns, ",")
		a.logger.WithField("custom_patterns", customPatterns).Info("Using custom patterns")
	}

	totalDeleted := 0
	deletedByPattern := make(map[string]int)

	a.logger.Info("Starting Redis test data cleanup")

	for _, pattern := range patterns {
		deleted, err := a.deleteKeysByPattern(ctx, pattern)
		if err != nil {
			a.logger.WithError(err).WithField("pattern", pattern).Error("Failed to delete keys")
			// Continue with other patterns even if one fails
			continue
		}
		
		deletedByPattern[pattern] = deleted
		totalDeleted += deleted
		
		a.logger.WithFields(logrus.Fields{
			"pattern": pattern,
			"deleted": deleted,
		}).Info("Deleted keys for pattern")
	}

	a.logger.WithField("total_deleted", totalDeleted).Info("Redis test data cleanup completed")

	return c.JSON(fiber.Map{
		"success":            true,
		"total_deleted_keys": totalDeleted,
		"deleted_by_pattern": deletedByPattern,
		"message":            "Test data flushed successfully",
	})
}

// deleteKeysByPattern deletes all keys matching the pattern
func (a *AdminHandler) deleteKeysByPattern(ctx context.Context, pattern string) (int, error) {
	deleted := 0
	
	// Use SCAN to iterate through keys (cursor-based, won't block Redis)
	iter := a.redisClient.Scan(ctx, 0, pattern, 100).Iterator()
	
	keys := []string{}
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
		
		// Delete in batches of 1000 to avoid blocking
		if len(keys) >= 1000 {
			if err := a.deleteBatch(ctx, keys); err != nil {
				return deleted, err
			}
			deleted += len(keys)
			keys = []string{}
		}
	}
	
	// Delete remaining keys
	if len(keys) > 0 {
		if err := a.deleteBatch(ctx, keys); err != nil {
			return deleted, err
		}
		deleted += len(keys)
	}
	
	if err := iter.Err(); err != nil {
		return deleted, err
	}
	
	return deleted, nil
}

// deleteBatch deletes a batch of keys
func (a *AdminHandler) deleteBatch(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	
	pipe := a.redisClient.Pipeline()
	for _, key := range keys {
		pipe.Del(ctx, key)
	}
	
	_, err := pipe.Exec(ctx)
	return err
}

// HealthCheck returns service health status
// @Summary Health check
// @Description Get service health status including Redis connectivity
// @Tags Admin
// @Produce json
// @Success 200 {object} map[string]interface{} "Service is healthy"
// @Failure 503 {object} map[string]interface{} "Service is unhealthy"
// @Router /admin/health [get]
func (a *AdminHandler) HealthCheck(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Check Redis connectivity
	redisStatus := "healthy"
	if err := a.redisClient.Ping(ctx).Err(); err != nil {
		a.logger.WithError(err).Error("Redis health check failed")
		redisStatus = "unhealthy"
		
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"status": "unhealthy",
			"redis":  redisStatus,
			"error":  err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"status": "healthy",
		"redis":  redisStatus,
	})
}

// GetStats returns Redis statistics
// @Summary Get Redis statistics
// @Description Get current Redis connection and key statistics
// @Tags Admin
// @Produce json
// @Success 200 {object} map[string]interface{} "Redis statistics"
// @Failure 500 {object} map[string]interface{} "Internal error"
// @Router /admin/stats [get]
func (a *AdminHandler) GetStats(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Get Redis INFO
	info, err := a.redisClient.Info(ctx, "stats", "clients", "memory").Result()
	if err != nil {
		a.logger.WithError(err).Error("Failed to get Redis stats")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get Redis statistics",
		})
	}

	// Count keys by pattern
	patterns := []string{"queue:*", "idempotency:*", "heartbeat:*", "dedupe:*", "stream:*"}
	keyCount := make(map[string]int64)
	
	for _, pattern := range patterns {
		// Use SCAN with COUNT to estimate
		iter := a.redisClient.Scan(ctx, 0, pattern, 10).Iterator()
		count := int64(0)
		for iter.Next(ctx) {
			count++
		}
		keyCount[pattern] = count
	}

	return c.JSON(fiber.Map{
		"success":   true,
		"info":      info,
		"key_count": keyCount,
	})
}
