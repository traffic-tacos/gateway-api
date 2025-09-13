package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/traffic-tacos/gateway-api/pkg/errors"
)

// Config holds all configuration for the gateway
type Config struct {
	Server   ServerConfig   `json:"server"`
	Upstream UpstreamConfig `json:"upstream"`
	JWT      JWTConfig      `json:"jwt"`
	Redis    RedisConfig    `json:"redis"`
	RateLimit RateLimitConfig `json:"rate_limit"`
	Idempotency IdempotencyConfig `json:"idempotency"`
	Observability ObservabilityConfig `json:"observability"`
}

// ServerConfig holds server-related configuration
type ServerConfig struct {
	Port         int           `json:"port"`
	ReadTimeout  time.Duration `json:"read_timeout"`
	WriteTimeout time.Duration `json:"write_timeout"`
	IdleTimeout  time.Duration `json:"idle_timeout"`
	MaxBodySize  int           `json:"max_body_size"` // in bytes
}

// UpstreamConfig holds upstream service configurations
type UpstreamConfig struct {
	Reservation ReservationConfig `json:"reservation"`
	Payment     PaymentConfig     `json:"payment"`
}

// ReservationConfig holds reservation-api configuration
type ReservationConfig struct {
	BaseURL string        `json:"base_url"`
	Timeout time.Duration `json:"timeout"`
}

// PaymentConfig holds payment-sim-api configuration
type PaymentConfig struct {
	BaseURL string        `json:"base_url"`
	Timeout time.Duration `json:"timeout"`
}

// JWTConfig holds JWT-related configuration
type JWTConfig struct {
	Issuer      string        `json:"issuer"`
	Audience    string        `json:"audience"`
	JWKSURL     string        `json:"jwks_url"`
	CacheTTL    time.Duration `json:"cache_ttl"`
	SkipVerify  bool          `json:"skip_verify"` // for development
}

// RedisConfig holds Redis configuration
type RedisConfig struct {
	Addr     string `json:"addr"`
	Password string `json:"password"`
	DB       int    `json:"db"`
	TLS      bool   `json:"tls"`
}

// RateLimitConfig holds rate limiting configuration
type RateLimitConfig struct {
	RPS   int `json:"rps"`
	Burst int `json:"burst"`
}

// IdempotencyConfig holds idempotency configuration
type IdempotencyConfig struct {
	TTLSeconds int `json:"ttl_seconds"`
}

// ObservabilityConfig holds observability configuration
type ObservabilityConfig struct {
	ServiceName    string `json:"service_name"`
	ServiceVersion string `json:"service_version"`
	OTLPEndpoint   string `json:"otlp_endpoint"`
	TraceSampling  float64 `json:"trace_sampling"`
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{}

	// Server config
	cfg.Server.Port = getEnvAsInt("PORT", 8080)
	cfg.Server.ReadTimeout = getEnvAsDuration("SERVER_READ_TIMEOUT", 30*time.Second)
	cfg.Server.WriteTimeout = getEnvAsDuration("SERVER_WRITE_TIMEOUT", 30*time.Second)
	cfg.Server.IdleTimeout = getEnvAsDuration("SERVER_IDLE_TIMEOUT", 120*time.Second)
	cfg.Server.MaxBodySize = getEnvAsInt("MAX_BODY_SIZE", 1024*1024) // 1MB

	// Upstream config
	cfg.Upstream.Reservation.BaseURL = getEnv("UPSTREAM_RESERVATION_BASE", "http://reservation-api.tickets-api.svc.cluster.local:8080")
	cfg.Upstream.Reservation.Timeout = getEnvAsDuration("UPSTREAM_RESERVATION_TIMEOUT", 600*time.Millisecond)
	cfg.Upstream.Payment.BaseURL = getEnv("UPSTREAM_PAYMENT_BASE", "http://payment-sim-api.tickets-api.svc.cluster.local:8080")
	cfg.Upstream.Payment.Timeout = getEnvAsDuration("UPSTREAM_PAYMENT_TIMEOUT", 400*time.Millisecond)

	// JWT config
	cfg.JWT.Issuer = getEnv("JWT_ISSUER", "")
	cfg.JWT.Audience = getEnv("JWT_AUDIENCE", "")
	cfg.JWT.JWKSURL = getEnv("JWT_JWKS_URL", "")
	cfg.JWT.CacheTTL = getEnvAsDuration("JWT_CACHE_TTL", 10*time.Minute)
	cfg.JWT.SkipVerify = getEnvAsBool("JWT_SKIP_VERIFY", false)

	// Redis config
	cfg.Redis.Addr = getEnv("REDIS_ADDR", "redis:6379")
	cfg.Redis.Password = getEnv("REDIS_PASSWORD", "")
	cfg.Redis.DB = getEnvAsInt("REDIS_DB", 0)
	cfg.Redis.TLS = getEnvAsBool("REDIS_TLS", true)

	// Rate limit config
	cfg.RateLimit.RPS = getEnvAsInt("RATE_LIMIT_RPS", 50)
	cfg.RateLimit.Burst = getEnvAsInt("RATE_LIMIT_BURST", 100)

	// Idempotency config
	cfg.Idempotency.TTLSeconds = getEnvAsInt("IDEMPOTENCY_TTL_SECONDS", 300)

	// Observability config
	cfg.Observability.ServiceName = getEnv("SERVICE_NAME", "gateway-api")
	cfg.Observability.ServiceVersion = getEnv("SERVICE_VERSION", "1.0.0")
	cfg.Observability.OTLPEndpoint = getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://otel-collector:4317")
	cfg.Observability.TraceSampling = getEnvAsFloat("TRACE_SAMPLING", 0.1) // 10%

	// Validate required fields
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.JWT.Issuer == "" {
		return errors.NewAppError(errors.CodeBadRequest, "JWT_ISSUER is required", nil)
	}
	if c.JWT.Audience == "" {
		return errors.NewAppError(errors.CodeBadRequest, "JWT_AUDIENCE is required", nil)
	}
	if c.JWT.JWKSURL == "" {
		return errors.NewAppError(errors.CodeBadRequest, "JWT_JWKS_URL is required", nil)
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return errors.NewAppError(errors.CodeBadRequest, "PORT must be between 1 and 65535", nil)
	}
	return nil
}

// Helper functions for environment variable parsing

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvAsBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func getEnvAsDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

func getEnvAsFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if floatValue, err := strconv.ParseFloat(value, 64); err == nil {
			return floatValue
		}
	}
	return defaultValue
}
