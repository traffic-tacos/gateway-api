package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// StreamQueue implements per-user FIFO queue using Redis Streams
// This solves the ordering problem that ZSet + CompositeScore couldn't solve
// due to float64 precision limitations.
type StreamQueue struct {
	redis  *redis.Client
	logger *logrus.Logger
}

// NewStreamQueue creates a new stream-based queue
func NewStreamQueue(redis *redis.Client, logger *logrus.Logger) *StreamQueue {
	return &StreamQueue{
		redis:  redis,
		logger: logger,
	}
}

// EnqueueResult contains the result of an enqueue operation
type EnqueueResult struct {
	StreamID string // Redis Stream message ID (e.g., "1728123456789-0")
	Position int    // Global position in queue
	UserPos  int    // Position within user's stream
}

// Enqueue adds a request to the per-user stream
// Uses hash tags {} to ensure same event goes to same Redis shard
func (sq *StreamQueue) Enqueue(
	ctx context.Context,
	eventID string,
	userID string,
	token string,
) (*EnqueueResult, error) {
	// Per-user stream key with hash tag for consistent sharding
	// Format: stream:event:{eventID}:user:userID
	// Hash tag {eventID} ensures all streams for same event are on same shard
	streamKey := fmt.Sprintf("stream:event:{%s}:user:%s", eventID, userID)

	// XAdd with auto-generated ID (timestamp-sequence)
	streamID, err := sq.redis.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{
			"token":     token,
			"event_id":  eventID,
			"user_id":   userID,
			"timestamp": time.Now().Unix(),
		},
	}).Result()

	if err != nil {
		sq.logger.WithError(err).WithFields(logrus.Fields{
			"event_id": eventID,
			"user_id":  userID,
		}).Error("Failed to add to stream")
		return nil, fmt.Errorf("xadd failed: %w", err)
	}

	// Set TTL on stream to prevent memory leak (if user abandons without calling Leave)
	// TTL = 1 hour to match ZSET expiration
	sq.redis.Expire(ctx, streamKey, 1*time.Hour)

	// Get user's position in their own stream
	userPos, err := sq.redis.XLen(ctx, streamKey).Result()
	if err != nil {
		sq.logger.WithError(err).Warn("Failed to get stream length")
		userPos = 1 // Fallback
	}

	// Calculate global position (across all users)
	globalPos := sq.calculateGlobalPosition(ctx, eventID, userID, streamID)

	sq.logger.WithFields(logrus.Fields{
		"stream_id":  streamID,
		"event_id":   eventID,
		"user_id":    userID,
		"user_pos":   userPos,
		"global_pos": globalPos,
	}).Debug("Enqueued to stream")

	return &EnqueueResult{
		StreamID: streamID,
		Position: globalPos,
		UserPos:  int(userPos),
	}, nil
}

// calculateGlobalPosition estimates global position across all user streams
// Note: This is an approximation due to distributed nature
func (sq *StreamQueue) calculateGlobalPosition(
	ctx context.Context,
	eventID string,
	userID string,
	streamID string,
) int {
	// Pattern to find all streams for this event
	pattern := fmt.Sprintf("stream:event:{%s}:user:*", eventID)

	// Get all stream keys for this event
	keys, err := sq.redis.Keys(ctx, pattern).Result()
	if err != nil {
		sq.logger.WithError(err).Warn("Failed to get stream keys")
		return 1 // Fallback
	}

	userStreamKey := fmt.Sprintf("stream:event:{%s}:user:%s", eventID, userID)
	totalAhead := 0

	// Count messages ahead of us
	for _, key := range keys {
		if key == userStreamKey {
			// For our own stream, count messages before our ID
			entries, err := sq.redis.XRange(ctx, key, "-", streamID).Result()
			if err == nil && len(entries) > 0 {
				// Subtract 1 because XRange includes the end ID
				totalAhead += len(entries) - 1
			}
			continue
		}

		// For other streams, count all messages
		length, err := sq.redis.XLen(ctx, key).Result()
		if err == nil {
			totalAhead += int(length)
		}
	}

	return totalAhead + 1
}

// GetPosition returns current position for a specific stream message
func (sq *StreamQueue) GetPosition(
	ctx context.Context,
	eventID string,
	userID string,
	streamID string,
) (int, error) {
	position := sq.calculateGlobalPosition(ctx, eventID, userID, streamID)

	sq.logger.WithFields(logrus.Fields{
		"stream_id": streamID,
		"position":  position,
	}).Debug("Calculated position")

	return position, nil
}

// GetGlobalPosition is an alias for GetPosition (for backward compatibility)
func (sq *StreamQueue) GetGlobalPosition(
	ctx context.Context,
	eventID string,
	userID string,
	streamID string,
) (int, error) {
	return sq.GetPosition(ctx, eventID, userID, streamID)
}

// GetUserMessages returns all messages for a specific user
func (sq *StreamQueue) GetUserMessages(
	ctx context.Context,
	eventID string,
	userID string,
) ([]redis.XMessage, error) {
	streamKey := fmt.Sprintf("stream:event:{%s}:user:%s", eventID, userID)

	messages, err := sq.redis.XRange(ctx, streamKey, "-", "+").Result()
	if err != nil {
		return nil, fmt.Errorf("xrange failed: %w", err)
	}

	return messages, nil
}

// DequeueForUser removes processed messages from user's stream
func (sq *StreamQueue) DequeueForUser(
	ctx context.Context,
	eventID string,
	userID string,
	streamID string,
) error {
	streamKey := fmt.Sprintf("stream:event:{%s}:user:%s", eventID, userID)

	deleted, err := sq.redis.XDel(ctx, streamKey, streamID).Result()
	if err != nil {
		return fmt.Errorf("xdel failed: %w", err)
	}

	sq.logger.WithFields(logrus.Fields{
		"stream_id": streamID,
		"deleted":   deleted,
	}).Debug("Dequeued from stream")

	return nil
}

// CleanupExpiredStreams removes old stream entries
// Should be called periodically (e.g., every 5 minutes)
func (sq *StreamQueue) CleanupExpiredStreams(
	ctx context.Context,
	eventID string,
	maxAge time.Duration,
) (int, error) {
	pattern := fmt.Sprintf("stream:event:{%s}:user:*", eventID)
	keys, err := sq.redis.Keys(ctx, pattern).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get keys: %w", err)
	}

	totalCleaned := 0
	cutoffTime := time.Now().Add(-maxAge).Unix()
	cutoffID := fmt.Sprintf("%d-0", cutoffTime*1000) // Convert to ms

	for _, key := range keys {
		// Remove messages older than cutoff
		deleted, err := sq.redis.XTrimMinID(ctx, key, cutoffID).Result()
		if err != nil {
			sq.logger.WithError(err).WithField("key", key).Warn("Failed to trim stream")
			continue
		}

		totalCleaned += int(deleted)

		// If stream is empty, delete the key
		length, _ := sq.redis.XLen(ctx, key).Result()
		if length == 0 {
			sq.redis.Del(ctx, key)
		}
	}

	sq.logger.WithFields(logrus.Fields{
		"event_id": eventID,
		"cleaned":  totalCleaned,
	}).Info("Cleaned up expired streams")

	return totalCleaned, nil
}

// GetQueueStats returns statistics for the queue
type QueueStats struct {
	TotalUsers    int
	TotalMessages int
	AvgPerUser    float64
}

func (sq *StreamQueue) GetQueueStats(
	ctx context.Context,
	eventID string,
) (*QueueStats, error) {
	pattern := fmt.Sprintf("stream:event:{%s}:user:*", eventID)
	keys, err := sq.redis.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, err
	}

	totalMessages := int64(0)
	for _, key := range keys {
		length, err := sq.redis.XLen(ctx, key).Result()
		if err == nil {
			totalMessages += length
		}
	}

	stats := &QueueStats{
		TotalUsers:    len(keys),
		TotalMessages: int(totalMessages),
	}

	if len(keys) > 0 {
		stats.AvgPerUser = float64(totalMessages) / float64(len(keys))
	}

	return stats, nil
}
