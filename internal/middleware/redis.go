package middleware

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/traffic-tacos/gateway-api/internal/config"
)

// NewRedisClient creates a new Redis client
func NewRedisClient(cfg config.RedisConfig) (redis.Cmdable, error) {
	opts := &redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	}

	if cfg.TLS {
		opts.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	client := redis.NewClient(opts)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return client, nil
}
