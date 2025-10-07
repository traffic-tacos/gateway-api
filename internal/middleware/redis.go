package middleware

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"github.com/traffic-tacos/gateway-api/internal/config"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// NewRedisClient creates a new Redis client with proper configuration
// Automatically chooses between Standalone and Cluster mode based on config
func NewRedisClient(cfg *config.RedisConfig, awsCfg *config.AWSConfig, logger *logrus.Logger) (*redis.Client, error) {
	// Cluster Mode: use ClusterClient with Read Replica support
	if cfg.ClusterMode {
		logger.WithField("cluster_mode", true).Info("Initializing Redis Cluster Mode with Read Replica support")
		_, err := newRedisClusterClient(cfg, awsCfg, logger)
		if err != nil {
			return nil, err
		}
		// Note: This function is deprecated for Cluster Mode
		// Use NewRedisUniversalClient instead for full Cluster support
		logger.Warn("ClusterClient created but not returned - use NewRedisUniversalClient instead")
		return nil, fmt.Errorf("cluster mode requires code migration to redis.UniversalClient - please use NewRedisUniversalClient instead")
	}

	// Standalone Mode: traditional single-node client
	options := &redis.Options{
		Addr:         cfg.Address,
		Password:     cfg.Password,
		DB:           cfg.Database,
		MaxRetries:   cfg.MaxRetries,
		PoolSize:     cfg.PoolSize,
		PoolTimeout:  cfg.PoolTimeout,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		DialTimeout:  5 * time.Second,

		// Connection pool settings
		MinIdleConns:    10,
		MaxIdleConns:    50,
		ConnMaxIdleTime: 10 * time.Minute,
		ConnMaxLifetime: 30 * time.Minute,

		// Retry settings
		MinRetryBackoff: 8 * time.Millisecond,
		MaxRetryBackoff: 512 * time.Millisecond,
	}

	// Fetch password from AWS Secrets Manager if enabled
	if cfg.PasswordFromSecrets {
		password, err := getSecretValue(awsCfg, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to get Redis password from secrets: %w", err)
		}
		options.Password = password
		logger.Info("Redis password fetched from AWS Secrets Manager")
	}

	// Configure TLS for ElastiCache in-transit encryption
	if cfg.TLSEnabled {
		options.TLSConfig = &tls.Config{
			ServerName: extractHostname(cfg.Address),
		}
		logger.WithField("address", cfg.Address).Info("Redis TLS encryption enabled")
	}

	rdb := redis.NewClient(options)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	logger.WithFields(logrus.Fields{
		"address": cfg.Address,
		"db":      cfg.Database,
		"mode":    "standalone",
	}).Info("Connected to Redis")

	return rdb, nil
}

// newRedisClusterClient creates a Redis Cluster client with Read Replica support
func newRedisClusterClient(cfg *config.RedisConfig, awsCfg *config.AWSConfig, logger *logrus.Logger) (*redis.ClusterClient, error) {
	// Fetch password from AWS Secrets Manager if enabled
	password := cfg.Password
	if cfg.PasswordFromSecrets {
		pwd, err := getSecretValue(awsCfg, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to get Redis password from secrets: %w", err)
		}
		password = pwd
		logger.Info("Redis password fetched from AWS Secrets Manager for Cluster Mode")
	}

	options := &redis.ClusterOptions{
		Addrs:        []string{cfg.Address}, // Configuration Endpoint
		Password:     password,
		MaxRetries:   cfg.MaxRetries,
		PoolSize:     cfg.PoolSize,
		PoolTimeout:  cfg.PoolTimeout,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		DialTimeout:  5 * time.Second,

		// Connection pool settings
		MinIdleConns:    10,
		ConnMaxIdleTime: 10 * time.Minute,
		ConnMaxLifetime: 30 * time.Minute,

		// Retry settings
		MinRetryBackoff: 8 * time.Millisecond,
		MaxRetryBackoff: 512 * time.Millisecond,

		// 🔴 Read Replica Optimization
		RouteByLatency: cfg.RouteByLatency, // Route to the fastest node (includes replicas)
		RouteRandomly:  cfg.RouteRandomly,  // Random routing across replicas
		ReadOnly:       cfg.ReadOnly,       // Prefer replicas for read commands

		// Cluster topology discovery
		MaxRedirects: 3,
	}

	// Configure TLS for ElastiCache in-transit encryption
	if cfg.TLSEnabled {
		options.TLSConfig = &tls.Config{
			ServerName: extractHostname(cfg.Address),
		}
		logger.WithField("address", cfg.Address).Info("Redis Cluster TLS encryption enabled")
	}

	clusterClient := redis.NewClusterClient(options)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := clusterClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis Cluster: %w", err)
	}

	// Log cluster topology for debugging
	nodes, err := clusterClient.ClusterNodes(ctx).Result()
	if err == nil {
		logger.WithField("topology", nodes).Debug("Redis Cluster topology discovered")
	}

	logger.WithFields(logrus.Fields{
		"address":          cfg.Address,
		"mode":             "cluster",
		"route_by_latency": cfg.RouteByLatency,
		"route_randomly":   cfg.RouteRandomly,
		"read_only":        cfg.ReadOnly,
	}).Info("Connected to Redis Cluster with Read Replica support")

	return clusterClient, nil
}

// NewRedisUniversalClient creates a universal client that works with both standalone and cluster
// This is the recommended approach for new code or refactoring
func NewRedisUniversalClient(cfg *config.RedisConfig, awsCfg *config.AWSConfig, logger *logrus.Logger) (redis.UniversalClient, error) {
	// Fetch password from AWS Secrets Manager if enabled
	password := cfg.Password
	if cfg.PasswordFromSecrets {
		pwd, err := getSecretValue(awsCfg, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to get Redis password from secrets: %w", err)
		}
		password = pwd
		logger.Info("Redis password fetched from AWS Secrets Manager")
	}

	// Configure TLS
	var tlsConfig *tls.Config
	if cfg.TLSEnabled {
		tlsConfig = &tls.Config{
			ServerName: extractHostname(cfg.Address),
		}
		logger.WithField("address", cfg.Address).Info("Redis TLS encryption enabled")
	}

	// Universal options work for both Standalone and Cluster
	options := &redis.UniversalOptions{
		Addrs:        []string{cfg.Address},
		Password:     password,
		DB:           cfg.Database, // Ignored in cluster mode
		MaxRetries:   cfg.MaxRetries,
		PoolSize:     cfg.PoolSize,
		PoolTimeout:  cfg.PoolTimeout,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		DialTimeout:  5 * time.Second,

		// Connection pool settings
		MinIdleConns:    10,
		ConnMaxIdleTime: 10 * time.Minute,

		// Retry settings
		MinRetryBackoff: 8 * time.Millisecond,
		MaxRetryBackoff: 512 * time.Millisecond,

		// TLS
		TLSConfig: tlsConfig,

		// 🔴 Read Replica Optimization (only for cluster mode)
		RouteByLatency: cfg.RouteByLatency,
		RouteRandomly:  cfg.RouteRandomly,
		ReadOnly:       cfg.ReadOnly,
	}

	client := redis.NewUniversalClient(options)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	mode := "standalone"
	if cfg.ClusterMode {
		mode = "cluster"
	}

	logger.WithFields(logrus.Fields{
		"address": cfg.Address,
		"mode":    mode,
	}).Info("Connected to Redis via UniversalClient")

	return client, nil
}

// RedisHealthCheck middleware that checks Redis connectivity
func RedisHealthCheck(redisClient redis.UniversalClient, logger *logrus.Logger) func() error {
	return func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		if err := redisClient.Ping(ctx).Err(); err != nil {
			logger.WithError(err).Error("Redis health check failed")
			return fmt.Errorf("redis unavailable: %w", err)
		}

		return nil
	}
}

// extractHostname extracts hostname from address (host:port -> host)
func extractHostname(address string) string {
	if idx := strings.LastIndex(address, ":"); idx != -1 {
		return address[:idx]
	}
	return address
}

// getSecretValue retrieves the Redis password from AWS Secrets Manager
func getSecretValue(awsCfg *config.AWSConfig, logger *logrus.Logger) (string, error) {
	// Create AWS session
	sessConfig := &aws.Config{
		Region: aws.String(awsCfg.Region),
	}

	// Use specific profile if provided
	if awsCfg.Profile != "" {
		sessConfig.WithCredentialsChainVerboseErrors(true)
	}

	sess, err := session.NewSession(sessConfig)
	if err != nil {
		return "", fmt.Errorf("failed to create AWS session: %w", err)
	}

	// Create Secrets Manager client
	svc := secretsmanager.New(sess)

	// Get secret value
	result, err := svc.GetSecretValue(&secretsmanager.GetSecretValueInput{
		SecretId: aws.String(awsCfg.SecretName),
	})
	if err != nil {
		return "", fmt.Errorf("failed to retrieve secret '%s': %w", awsCfg.SecretName, err)
	}

	if result.SecretString == nil {
		return "", fmt.Errorf("secret '%s' has no string value", awsCfg.SecretName)
	}

	logger.WithField("secret_name", awsCfg.SecretName).Info("Successfully retrieved Redis password from Secrets Manager")
	return *result.SecretString, nil
}
