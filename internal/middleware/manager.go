package middleware

import (
	"fmt"

	"github.com/traffic-tacos/gateway-api/internal/config"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// Manager holds all middleware instances
type Manager struct {
	Auth        *AuthMiddleware
	Idempotency *IdempotencyMiddleware
	RateLimit   *RateLimitMiddleware
	RedisClient *redis.Client
	Config      *config.Config
	Logger      *logrus.Logger
}

// NewManager creates a new middleware manager with all middleware initialized
func NewManager(cfg *config.Config, logger *logrus.Logger) (*Manager, error) {
	// Initialize Redis client
	redisClient, err := NewRedisClient(&cfg.Redis, &cfg.AWS, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create Redis client: %w", err)
	}

	// Initialize authentication middleware
	authMiddleware, err := NewAuthMiddleware(&cfg.JWT, redisClient, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth middleware: %w", err)
	}

	// Initialize idempotency middleware
	idempotencyMiddleware := NewIdempotencyMiddleware(redisClient, logger)

	// Initialize rate limit middleware
	rateLimitMiddleware := NewRateLimitMiddleware(&cfg.RateLimit, redisClient, logger)

	return &Manager{
		Auth:        authMiddleware,
		Idempotency: idempotencyMiddleware,
		RateLimit:   rateLimitMiddleware,
		RedisClient: redisClient,
		Config:      cfg,
		Logger:      logger,
	}, nil
}

// Close closes all middleware resources
func (m *Manager) Close() error {
	if m.RedisClient != nil {
		return m.RedisClient.Close()
	}
	return nil
}