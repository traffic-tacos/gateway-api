package middleware

import (
	"context"
	"github.com/traffic-tacos/gateway-api/internal/config"
	"github.com/sirupsen/logrus"
)

// InitTracing initializes tracing (temporarily disabled)
func InitTracing(cfg *config.ObservabilityConfig, logger *logrus.Logger) (func(context.Context) error, error) {
	logger.Info("Tracing temporarily disabled - OpenTelemetry dependencies removed")
	return func(context.Context) error { return nil }, nil
}

// GetTracer returns a no-op tracer
func GetTracer() interface{} {
	return nil
}

// StartSpan returns a no-op span
func StartSpan(ctx context.Context, name string, opts ...interface{}) (context.Context, interface{}) {
	return ctx, nil
}

// AddSpanAttributes is a no-op
func AddSpanAttributes(span interface{}, attrs map[string]interface{}) {
	// No-op for now
}

// RecordError is a no-op
func RecordError(span interface{}, err error) {
	// No-op for now
}