package middleware

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/gofiber/fiber/v2"
	"github.com/traffic-tacos/gateway-api/internal/config"
	"github.com/traffic-tacos/gateway-api/internal/logging"
	"github.com/traffic-tacos/gateway-api/internal/metrics"
	"github.com/traffic-tacos/gateway-api/pkg/errors"
)

// RateLimitMiddleware handles rate limiting using Redis
type RateLimitMiddleware struct {
	rps    int
	burst  int
	redis  redis.Cmdable
	logger *logging.Logger
	metrics *metrics.Metrics
}

// NewRateLimitMiddleware creates a new rate limit middleware
func NewRateLimitMiddleware(cfg config.RateLimitConfig, redisClient redis.Cmdable, logger *logging.Logger, metrics *metrics.Metrics) (*RateLimitMiddleware, error) {
	return &RateLimitMiddleware{
		rps:     cfg.RPS,
		burst:   cfg.Burst,
		redis:   redisClient,
		logger:  logger,
		metrics: metrics,
	}, nil
}

// RateLimit returns rate limiting middleware
func (r *RateLimitMiddleware) RateLimit() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Get client identifier (IP address for now)
		clientID := c.IP()

		// Check rate limit
		allowed, retryAfter, err := r.checkRateLimit(c.Context(), clientID)
		if err != nil {
			r.logger.ErrorWithContext(c.Context(), "Rate limit check failed", err)
			// Allow request on error to avoid blocking legitimate traffic
			return c.Next()
		}

		if !allowed {
			r.metrics.RecordRateLimitDrop()

			c.Set("Retry-After", fmt.Sprintf("%.0f", retryAfter.Seconds()))
			return errors.NewAppError(errors.CodeRateLimited, "Rate limit exceeded", nil)
		}

		return c.Next()
	}
}

// checkRateLimit implements token bucket algorithm using Redis
func (r *RateLimitMiddleware) checkRateLimit(ctx context.Context, clientID string) (bool, time.Duration, error) {
	key := fmt.Sprintf("ratelimit:%s", clientID)

	// Use Lua script for atomic operations
	luaScript := `
		local key = KEYS[1]
		local now = tonumber(ARGV[1])
		local window = tonumber(ARGV[2])
		local limit = tonumber(ARGV[3])

		-- Get current count
		local current = redis.call('GET', key)
		if not current then
			current = 0
		else
			current = tonumber(current)
		end

		-- Reset if window has passed
		local last_reset = redis.call('GET', key .. ':reset')
		if last_reset then
			last_reset = tonumber(last_reset)
			if now - last_reset >= window then
				current = 0
				redis.call('SET', key .. ':reset', now)
			end
		else
			redis.call('SET', key .. ':reset', now)
		end

		-- Check if limit exceeded
		if current >= limit then
			local retry_after = window - (now - (last_reset or now))
			return {0, retry_after}
		end

		-- Increment counter
		redis.call('INCR', key)
		if current == 0 then
			redis.call('EXPIRE', key, window)
		end

		return {1, 0}
	`

	now := time.Now().Unix()
	window := int64(1) // 1 second window
	limit := int64(r.rps)

	result, err := r.redis.Eval(ctx, luaScript, []string{key}, now, window, limit).Result()
	if err != nil {
		return false, 0, err
	}

	results := result.([]interface{})
	allowed := results[0].(int64) == 1
	retryAfterSeconds := results[1].(int64)

	return allowed, time.Duration(retryAfterSeconds) * time.Second, nil
}

// AdvancedRateLimit returns more sophisticated rate limiting with burst handling
func (r *RateLimitMiddleware) AdvancedRateLimit() fiber.Handler {
	return func(c *fiber.Ctx) error {
		clientID := c.IP()

		allowed, retryAfter, err := r.checkAdvancedRateLimit(c.Context(), clientID)
		if err != nil {
			r.logger.ErrorWithContext(c.Context(), "Advanced rate limit check failed", err)
			return c.Next()
		}

		if !allowed {
			r.metrics.RecordRateLimitDrop()

			c.Set("Retry-After", fmt.Sprintf("%.0f", retryAfter.Seconds()))
			return errors.NewAppError(errors.CodeRateLimited, "Rate limit exceeded", nil)
		}

		return c.Next()
	}
}

// checkAdvancedRateLimit implements sliding window with burst allowance
func (r *RateLimitMiddleware) checkAdvancedRateLimit(ctx context.Context, clientID string) (bool, time.Duration, error) {
	key := fmt.Sprintf("ratelimit:advanced:%s", clientID)

	// Lua script for sliding window rate limiting with burst
	luaScript := `
		local key = KEYS[1]
		local now = tonumber(ARGV[1])
		local window = 60  -- 60 second sliding window
		local limit = tonumber(ARGV[2])
		local burst = tonumber(ARGV[3])

		-- Add current timestamp to sorted set
		redis.call('ZADD', key, now, now)
		redis.call('ZREMRANGEBYSCORE', key, 0, now - window)

		-- Count requests in current window
		local count = redis.call('ZCARD', key)

		-- Allow burst up to burst limit
		local effective_limit = limit
		if count <= burst then
			effective_limit = burst
		end

		if count > effective_limit then
			-- Calculate retry after based on oldest request in window
			local oldest = redis.call('ZRANGE', key, 0, 0)[1]
			if oldest then
				local retry_after = window - (now - tonumber(oldest))
				return {0, retry_after}
			else
				return {0, window}
			end
		end

		-- Set expiry on the key
		redis.call('EXPIRE', key, window)

		return {1, 0}
	`

	result, err := r.redis.Eval(ctx, luaScript, []string{key}, time.Now().Unix(), r.rps, r.burst).Result()
	if err != nil {
		return false, 0, err
	}

	results := result.([]interface{})
	allowed := results[0].(int64) == 1
	retryAfterSeconds := results[1].(int64)

	return allowed, time.Duration(retryAfterSeconds) * time.Second, nil
}
