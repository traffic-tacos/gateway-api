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

	// Default patterns for test data (more specific for Cluster Mode)
	patterns := []string{
		// Queue patterns (specific for better Cluster Mode compatibility)
		"queue:waiting:*",     // Queue waiting tokens
		"queue:event:*",       // Queue event data (ZSET for global queue)
		"queue:reservation:*", // Queue reservation tokens
		"position_index:*",    // ✅ NEW: Position index ZSET for fast O(log N) lookup
		// Stream patterns
		"stream:event:*", // Redis Streams for events
		// Token and auth patterns
		"allow:*", // Admission allow tokens
		// Idempotency and deduplication
		"idempotency:*", // Idempotency keys
		"dedupe:*",      // Deduplication keys
		// User activity
		"heartbeat:*", // User heartbeat keys
		// Rate limiting (optional, may contain active limits)
		"ratelimit:*", // Rate limit counters
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

	a.logger.WithField("pattern", pattern).Info("Starting key deletion")

	// Use SCAN to iterate through keys (cursor-based, won't block Redis)
	iter := a.redisClient.Scan(ctx, 0, pattern, 100).Iterator()

	keys := []string{}
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())

		// Delete in batches of 100 (smaller for Cluster Mode compatibility)
		if len(keys) >= 100 {
			count, err := a.deleteBatch(ctx, keys)
			if err != nil {
				a.logger.WithError(err).WithField("pattern", pattern).Error("Failed to delete batch")
				// Continue with next batch even if one fails
			}
			deleted += count
			keys = []string{}
		}
	}

	// Delete remaining keys
	if len(keys) > 0 {
		count, err := a.deleteBatch(ctx, keys)
		if err != nil {
			a.logger.WithError(err).WithField("pattern", pattern).Error("Failed to delete remaining keys")
		}
		deleted += count
	}

	if err := iter.Err(); err != nil {
		a.logger.WithError(err).WithField("pattern", pattern).Error("SCAN iteration error")
		return deleted, err
	}

	a.logger.WithFields(map[string]interface{}{
		"pattern": pattern,
		"deleted": deleted,
	}).Info("Completed key deletion for pattern")

	return deleted, nil
}

// deleteBatch deletes a batch of keys (individual DEL for Cluster Mode compatibility)
func (a *AdminHandler) deleteBatch(ctx context.Context, keys []string) (int, error) {
	if len(keys) == 0 {
		return 0, nil
	}

	deleted := 0

	// 🔴 Redis Cluster Mode: Delete keys individually to avoid CROSSSLOT errors
	// Pipeline doesn't work when keys are in different hash slots
	for _, key := range keys {
		result, err := a.redisClient.Del(ctx, key).Result()
		if err != nil {
			a.logger.WithError(err).WithField("key", key).Warn("Failed to delete key")
			continue
		}
		deleted += int(result)
	}

	return deleted, nil
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
	patterns := []string{"queue:*", "position_index:*", "idempotency:*", "heartbeat:*", "dedupe:*", "stream:*"}
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
