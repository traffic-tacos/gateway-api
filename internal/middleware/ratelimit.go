package middleware

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/traffic-tacos/gateway-api/internal/config"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

type RateLimitMiddleware struct {
	config      *config.RateLimitConfig
	redisClient *redis.Client
	logger      *logrus.Logger
	luaScript   string
}

func NewRateLimitMiddleware(cfg *config.RateLimitConfig, redisClient *redis.Client, logger *logrus.Logger) *RateLimitMiddleware {
	// Token bucket Lua script for atomic operations
	luaScript := `
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local tokens = tonumber(ARGV[2])
local interval_ms = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])

local bucket = redis.call("HMGET", key, "tokens", "last_refill")
local current_tokens = tonumber(bucket[1]) or capacity
local last_refill = tonumber(bucket[2]) or 0

local now = redis.call("TIME")
local now_ms = now[1] * 1000 + math.floor(now[2] / 1000)

-- Calculate tokens to add based on time elapsed
if last_refill > 0 then
    local elapsed = now_ms - last_refill
    local tokens_to_add = math.floor(elapsed / interval_ms * tokens)
    current_tokens = math.min(capacity, current_tokens + tokens_to_add)
end

-- Check if request can be fulfilled
if current_tokens >= requested then
    current_tokens = current_tokens - requested

    -- Update bucket
    redis.call("HMSET", key, "tokens", current_tokens, "last_refill", now_ms)
    redis.call("EXPIRE", key, 3600) -- 1 hour TTL

    return {1, current_tokens, capacity}
else
    -- Update last_refill even on rejection
    redis.call("HMSET", key, "tokens", current_tokens, "last_refill", now_ms)
    redis.call("EXPIRE", key, 3600)

    return {0, current_tokens, capacity}
end`

	return &RateLimitMiddleware{
		config:      cfg,
		redisClient: redisClient,
		logger:      logger,
		luaScript:   luaScript,
	}
}

// Handle rate limiting middleware
func (r *RateLimitMiddleware) Handle() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Skip if rate limiting is disabled
		if !r.config.Enabled {
			return c.Next()
		}

		// Check if path is exempt from rate limiting
		path := c.Path()
		for _, exemptPath := range r.config.ExemptPaths {
			if strings.HasPrefix(path, exemptPath) {
				return c.Next()
			}
		}

		// Generate rate limit key
		key := r.generateKey(c)

		// Check rate limit
		allowed, remaining, resetTime, err := r.checkRateLimit(c.Context(), key)
		if err != nil {
			r.logger.WithError(err).Error("Rate limit check failed")
			// Allow request on Redis failure to avoid blocking traffic
			return c.Next()
		}

		// Set rate limit headers
		r.setRateLimitHeaders(c, remaining, resetTime)

		if !allowed {
			r.logger.WithFields(logrus.Fields{
				"key":       key,
				"path":      path,
				"method":    c.Method(),
				"user_id":   GetUserID(c),
				"remaining": remaining,
			}).Warn("Rate limit exceeded")

			return r.rateLimitError(c)
		}

		return c.Next()
	}
}

// generateKey creates a rate limit key based on user and IP
func (r *RateLimitMiddleware) generateKey(c *fiber.Ctx) string {
	// Try to use user ID if available (more specific)
	if userID := GetUserID(c); userID != "" {
		return fmt.Sprintf("ratelimit:user:%s", userID)
	}

	// Fall back to IP address
	ip := r.getClientIP(c)
	return fmt.Sprintf("ratelimit:ip:%s", ip)
}

// getClientIP extracts the real client IP
func (r *RateLimitMiddleware) getClientIP(c *fiber.Ctx) string {
	// Check X-Forwarded-For header (from load balancer)
	if xff := c.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the chain
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	if realIP := c.Get("X-Real-IP"); realIP != "" {
		return realIP
	}

	// Fall back to remote IP
	return c.IP()
}

// checkRateLimit checks if request is within rate limit using token bucket algorithm
func (r *RateLimitMiddleware) checkRateLimit(ctx context.Context, key string) (allowed bool, remaining int, resetTime time.Time, err error) {
	// Token bucket parameters
	capacity := r.config.Burst
	tokensPerSecond := r.config.RPS
	intervalMs := int(r.config.WindowSize.Milliseconds())
	requested := 1

	// Execute Lua script
	result, err := r.redisClient.Eval(ctx, r.luaScript, []string{key}, capacity, tokensPerSecond, intervalMs, requested).Result()
	if err != nil {
		return false, 0, time.Time{}, fmt.Errorf("failed to execute rate limit script: %w", err)
	}

	// Parse result
	resultSlice, ok := result.([]interface{})
	if !ok || len(resultSlice) != 3 {
		return false, 0, time.Time{}, fmt.Errorf("unexpected script result format")
	}

	allowedInt, ok := resultSlice[0].(int64)
	if !ok {
		return false, 0, time.Time{}, fmt.Errorf("failed to parse allowed result")
	}

	remainingInt, ok := resultSlice[1].(int64)
	if !ok {
		return false, 0, time.Time{}, fmt.Errorf("failed to parse remaining result")
	}

	// Calculate reset time (next second)
	resetTime = time.Now().Add(r.config.WindowSize).Truncate(time.Second)

	return allowedInt == 1, int(remainingInt), resetTime, nil
}

// setRateLimitHeaders sets standard rate limit headers
func (r *RateLimitMiddleware) setRateLimitHeaders(c *fiber.Ctx, remaining int, resetTime time.Time) {
	c.Set("X-RateLimit-Limit", strconv.Itoa(r.config.RPS))
	c.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
	c.Set("X-RateLimit-Reset", strconv.FormatInt(resetTime.Unix(), 10))
	c.Set("X-RateLimit-Window", r.config.WindowSize.String())

	// If rate limited, add Retry-After header
	if remaining <= 0 {
		retryAfter := int(time.Until(resetTime).Seconds()) + 1
		if retryAfter < 1 {
			retryAfter = 1
		}
		c.Set("Retry-After", strconv.Itoa(retryAfter))
	}
}

// rateLimitError returns a rate limit exceeded error
func (r *RateLimitMiddleware) rateLimitError(c *fiber.Ctx) error {
	return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     "RATE_LIMITED",
			"message":  "Rate limit exceeded. Please try again later.",
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}