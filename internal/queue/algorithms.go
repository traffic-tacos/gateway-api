package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// AdmissionMetrics tracks admission rates for ETA calculation
type AdmissionMetrics struct {
	redisClient redis.UniversalClient // 🔴 Changed to UniversalClient for Cluster support
	eventID     string
	logger      *logrus.Logger
}

// NewAdmissionMetrics creates a new metrics tracker
func NewAdmissionMetrics(redis redis.UniversalClient, eventID string, logger *logrus.Logger) *AdmissionMetrics {
	return &AdmissionMetrics{
		redisClient: redis,
		eventID:     eventID,
		logger:      logger,
	}
}

// GetAdmissionRate calculates current admission rate (users per second)
// Uses Exponential Moving Average over the last minute
func (m *AdmissionMetrics) GetAdmissionRate(ctx context.Context) (float64, error) {
	key := fmt.Sprintf("metrics:admission:%s", m.eventID)

	// Query last 1 minute of admissions
	now := time.Now().Unix()
	oneMinuteAgo := now - 60

	count, err := m.redisClient.ZCount(ctx, key,
		fmt.Sprintf("%d", oneMinuteAgo),
		fmt.Sprintf("%d", now)).Result()

	if err != nil {
		return 0, err
	}

	// Calculate rate: admissions per second
	rate := float64(count) / 60.0

	m.logger.WithFields(logrus.Fields{
		"event_id":   m.eventID,
		"count":      count,
		"rate":       rate,
		"time_range": "1min",
	}).Debug("Calculated admission rate")

	return rate, nil
}

// RecordAdmission records an admission event for metrics tracking
func (m *AdmissionMetrics) RecordAdmission(ctx context.Context, userID string) error {
	key := fmt.Sprintf("metrics:admission:%s", m.eventID)

	// Add to sorted set with current timestamp as score
	now := time.Now().Unix()
	err := m.redisClient.ZAdd(ctx, key, redis.Z{
		Score:  float64(now),
		Member: userID,
	}).Err()

	if err != nil {
		return err
	}

	// Clean up old data (older than 1 hour) to save memory
	oneHourAgo := now - 3600
	m.redisClient.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%d", oneHourAgo))

	return nil
}

// CalculateSmartETA calculates ETA based on real-time admission rate
func (m *AdmissionMetrics) CalculateSmartETA(ctx context.Context, position int) int {
	rate, err := m.GetAdmissionRate(ctx)

	// Fallback to default rate if metrics unavailable
	if err != nil || rate == 0 {
		m.logger.WithError(err).Warn("Failed to get admission rate, using default")
		return position * 2 // Default: 2 seconds per person
	}

	// ETA = position / rate (with 10% buffer for safety)
	eta := float64(position) / rate * 1.1

	// Clamp between 1 and 600 seconds
	if eta < 1 {
		return 1
	} else if eta > 600 {
		return 600
	}

	return int(eta)
}

// TokenBucketAdmission implements Token Bucket algorithm for rate limiting
type TokenBucketAdmission struct {
	redisClient redis.UniversalClient // 🔴 Changed to UniversalClient for Cluster support
	eventID     string
	capacity    int     // Maximum burst size
	refillRate  float64 // Tokens per second (steady-state rate)
	logger      *logrus.Logger
}

// NewTokenBucketAdmission creates a new token bucket rate limiter
func NewTokenBucketAdmission(redis redis.UniversalClient, eventID string, logger *logrus.Logger) *TokenBucketAdmission {
	return &TokenBucketAdmission{
		redisClient: redis,
		eventID:     eventID,
		capacity:    100,  // Allow burst of 100 users
		refillRate:  10.0, // Steady state: 10 users/second
		logger:      logger,
	}
}

// Token Bucket Lua script for atomic execution
var tokenBucketLuaScript = `
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local refill_rate = tonumber(ARGV[2])
local requested = tonumber(ARGV[3])
local now = tonumber(ARGV[4])

-- Get current bucket state
local bucket = redis.call('HMGET', key, 'tokens', 'last_refill')
local tokens = tonumber(bucket[1]) or capacity
local last_refill = tonumber(bucket[2]) or now

-- Calculate elapsed time since last refill
local elapsed = now - last_refill

-- Refill tokens based on elapsed time
local new_tokens = tokens + (elapsed * refill_rate)
if new_tokens > capacity then
    new_tokens = capacity
end

-- Check if enough tokens available
if new_tokens >= requested then
    -- Consume tokens and grant admission
    new_tokens = new_tokens - requested
    redis.call('HMSET', key, 'tokens', new_tokens, 'last_refill', now)
    redis.call('EXPIRE', key, 3600)  -- 1 hour TTL
    return 1  -- Admitted
else
    -- Not enough tokens, deny admission
    redis.call('HMSET', key, 'tokens', new_tokens, 'last_refill', now)
    redis.call('EXPIRE', key, 3600)
    return 0  -- Denied
end
`

// TryAdmit attempts to admit a user using token bucket algorithm
func (t *TokenBucketAdmission) TryAdmit(ctx context.Context, userID string) (bool, error) {
	key := fmt.Sprintf("admission:bucket:%s", t.eventID)

	// Execute Lua script atomically
	result, err := t.redisClient.Eval(ctx, tokenBucketLuaScript,
		[]string{key},
		t.capacity,
		t.refillRate,
		1, // Request 1 token
		time.Now().Unix()).Result()

	if err != nil {
		t.logger.WithError(err).Error("Token bucket admission failed")
		return false, err
	}

	admitted := result.(int64) == 1

	t.logger.WithFields(logrus.Fields{
		"event_id": t.eventID,
		"user_id":  userID,
		"admitted": admitted,
	}).Debug("Token bucket admission check")

	return admitted, nil
}

// SetCapacity updates the bucket capacity
func (t *TokenBucketAdmission) SetCapacity(capacity int) {
	t.capacity = capacity
}

// SetRefillRate updates the refill rate
func (t *TokenBucketAdmission) SetRefillRate(rate float64) {
	t.refillRate = rate
}
