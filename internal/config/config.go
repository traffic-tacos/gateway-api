package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	Server        ServerConfig        `envconfig:"SERVER"`
	Redis         RedisConfig         `envconfig:"REDIS"`
	JWT           JWTConfig           `envconfig:"JWT"`
	DynamoDB      DynamoDBConfig      `envconfig:"DYNAMODB"`
	Backend       BackendConfig       `envconfig:"BACKEND"`
	RateLimit     RateLimitConfig     `envconfig:"RATE_LIMIT"`
	Observability ObservabilityConfig `envconfig:"OBSERVABILITY"`
	CORS          CORSConfig          `envconfig:"CORS"`
	Log           LogConfig           `envconfig:"LOG"`
	AWS           AWSConfig           `envconfig:"AWS"`
}

type AWSConfig struct {
	Region     string `envconfig:"REGION" default:"ap-northeast-2"`
	Profile    string `envconfig:"PROFILE" default:""`
	SecretName string `envconfig:"SECRET_NAME" default:""`
}

type ServerConfig struct {
	Port         string        `envconfig:"PORT" default:"8000"`
	Environment  string        `envconfig:"ENVIRONMENT" default:"development"`
	ReadTimeout  time.Duration `envconfig:"READ_TIMEOUT" default:"30s"`
	WriteTimeout time.Duration `envconfig:"WRITE_TIMEOUT" default:"30s"`
	IdleTimeout  time.Duration `envconfig:"IDLE_TIMEOUT" default:"120s"`
}

type RedisConfig struct {
	Address             string        `envconfig:"ADDRESS" default:"localhost:6379"`
	Password            string        `envconfig:"PASSWORD" default:""`
	Database            int           `envconfig:"DATABASE" default:"0"`
	MaxRetries          int           `envconfig:"MAX_RETRIES" default:"3"`
	PoolSize            int           `envconfig:"POOL_SIZE" default:"1000"`     // 🔴 Increased for 30k RPS (was 100)
	PoolTimeout         time.Duration `envconfig:"POOL_TIMEOUT" default:"10s"`   // 🔴 Increased timeout (was 4s)
	MinIdleConns        int           `envconfig:"MIN_IDLE_CONNS" default:"100"` // 🔴 Keep connections warm
	MaxConnAge          time.Duration `envconfig:"MAX_CONN_AGE" default:"30m"`   // 🔴 Connection refresh
	TLSEnabled          bool          `envconfig:"TLS_ENABLED" default:"false"`
	PasswordFromSecrets bool          `envconfig:"PASSWORD_FROM_SECRETS" default:"false"`
	ClusterMode         bool          `envconfig:"CLUSTER_MODE" default:"false"`    // ElastiCache Cluster Mode Enabled
	RouteByLatency      bool          `envconfig:"ROUTE_BY_LATENCY" default:"true"` // Route reads to fastest replica
	RouteRandomly       bool          `envconfig:"ROUTE_RANDOMLY" default:"false"`  // Random read replica routing
	ReadOnly            bool          `envconfig:"READ_ONLY" default:"true"`        // Prefer replicas for read commands
}

type JWTConfig struct {
	JWKSEndpoint string        `envconfig:"JWKS_ENDPOINT" required:"false"` // Optional for custom auth
	CacheTTL     time.Duration `envconfig:"CACHE_TTL" default:"10m"`
	Issuer       string        `envconfig:"ISSUER" required:"false"`                  // Optional for custom auth
	Audience     string        `envconfig:"AUDIENCE" required:"false"`                // Optional for custom auth
	Secret       string        `envconfig:"SECRET" default:"change-me-in-production"` // For self-issued JWT
}

type DynamoDBConfig struct {
	UsersTableName string `envconfig:"USERS_TABLE_NAME" default:"traffic-tacos-users"`
	Region         string `envconfig:"REGION" default:"ap-northeast-2"`
}

type BackendConfig struct {
	ReservationAPI ReservationAPIConfig `envconfig:"RESERVATION_API"`
	PaymentAPI     PaymentAPIConfig     `envconfig:"PAYMENT_API"`
}

type ReservationAPIConfig struct {
	GRPCAddress string        `envconfig:"GRPC_ADDRESS" default:"reservation-api.tickets-api.svc.cluster.local:9090"`
	Timeout     time.Duration `envconfig:"TIMEOUT" default:"600ms"`
	TLSEnabled  bool          `envconfig:"TLS_ENABLED" default:"false"`
}

type PaymentAPIConfig struct {
	GRPCAddress string        `envconfig:"GRPC_ADDRESS" default:"payment-sim-api.tickets-api.svc.cluster.local:9090"`
	Timeout     time.Duration `envconfig:"TIMEOUT" default:"400ms"`
	TLSEnabled  bool          `envconfig:"TLS_ENABLED" default:"false"`
}

type RateLimitConfig struct {
	RPS         int           `envconfig:"RPS" default:"50"`
	Burst       int           `envconfig:"BURST" default:"100"`
	WindowSize  time.Duration `envconfig:"WINDOW_SIZE" default:"1s"`
	Enabled     bool          `envconfig:"ENABLED" default:"true"`
	ExemptPaths []string      `envconfig:"EXEMPT_PATHS" default:"/healthz,/readyz,/metrics"`
}

type ObservabilityConfig struct {
	MetricsPath    string  `envconfig:"METRICS_PATH" default:"/metrics"`
	OTLPEndpoint   string  `envconfig:"OTLP_ENDPOINT" default:"http://localhost:4318"`
	TracingEnabled bool    `envconfig:"TRACING_ENABLED" default:"true"`
	SampleRate     float64 `envconfig:"SAMPLE_RATE" default:"0.1"`
}

type CORSConfig struct {
	AllowOrigins string `envconfig:"ALLOW_ORIGINS" default:"*"`
}

type LogConfig struct {
	Level  string `envconfig:"LEVEL" default:"info"`
	Format string `envconfig:"FORMAT" default:"json"`
}

func Load() (*Config, error) {
	var cfg Config

	// Load from environment variables
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, fmt.Errorf("failed to process environment config: %w", err)
	}

	// Additional processing for slice fields that envconfig doesn't handle well
	if exemptPaths := os.Getenv("RATE_LIMIT_EXEMPT_PATHS"); exemptPaths != "" {
		cfg.RateLimit.ExemptPaths = strings.Split(exemptPaths, ",")
		for i := range cfg.RateLimit.ExemptPaths {
			cfg.RateLimit.ExemptPaths[i] = strings.TrimSpace(cfg.RateLimit.ExemptPaths[i])
		}
	}

	// Validate required fields
	if err := validateConfig(&cfg); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

func validateConfig(cfg *Config) error {
	// JWT fields are optional for self-issued tokens
	// When using self-issued JWT (with JWT_SECRET), JWKS_ENDPOINT, ISSUER, and AUDIENCE are not required
	// When using external OIDC/OAuth, these fields should be provided

	// Validate port
	if port, err := strconv.Atoi(cfg.Server.Port); err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid server port: %s", cfg.Server.Port)
	}

	// Validate sample rate
	if cfg.Observability.SampleRate < 0 || cfg.Observability.SampleRate > 1 {
		return fmt.Errorf("invalid tracing sample rate: %f", cfg.Observability.SampleRate)
	}

	return nil
}
