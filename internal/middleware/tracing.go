package middleware

import (
	"context"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/traffic-tacos/gateway-api/internal/config"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
)

var tracer trace.Tracer

// InitTracing initializes OpenTelemetry tracing with OTLP exporter
func InitTracing(cfg *config.ObservabilityConfig, logger *logrus.Logger) (func(context.Context) error, error) {
	if !cfg.TracingEnabled {
		logger.Info("Tracing is disabled")
		return func(context.Context) error { return nil }, nil
	}

	ctx := context.Background()

	// Clean endpoint (remove http:// or https:// prefix)
	endpoint := strings.TrimPrefix(cfg.OTLPEndpoint, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")

	// Create OTLP HTTP exporter
	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(), // Use WithTLSClientConfig() for production with TLS
	)
	if err != nil {
		return nil, err
	}

	// Create resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("gateway-api"),
			semconv.ServiceVersionKey.String("1.3.1"),
			attribute.String("environment", "production"),
		),
	)
	if err != nil {
		return nil, err
	}

	// Create tracer provider with batch span processor
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithMaxExportBatchSize(512),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.SampleRate)),
	)

	// Set global tracer provider
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Initialize tracer
	tracer = tp.Tracer("gateway-api")

	logger.WithFields(logrus.Fields{
		"otlp_endpoint": endpoint,
		"sample_rate":   cfg.SampleRate,
	}).Info("OpenTelemetry tracing initialized")

	// Return shutdown function
	return tp.Shutdown, nil
}

// GetTracer returns the global tracer
func GetTracer() trace.Tracer {
	if tracer == nil {
		return otel.Tracer("gateway-api")
	}
	return tracer
}

// StartSpan starts a new span with the given name
func StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return GetTracer().Start(ctx, name, opts...)
}

// AddSpanAttributes adds attributes to the span
func AddSpanAttributes(span trace.Span, attrs map[string]interface{}) {
	if span == nil {
		return
	}

	attributes := make([]attribute.KeyValue, 0, len(attrs))
	for k, v := range attrs {
		switch val := v.(type) {
		case string:
			attributes = append(attributes, attribute.String(k, val))
		case int:
			attributes = append(attributes, attribute.Int(k, val))
		case int64:
			attributes = append(attributes, attribute.Int64(k, val))
		case float64:
			attributes = append(attributes, attribute.Float64(k, val))
		case bool:
			attributes = append(attributes, attribute.Bool(k, val))
		}
	}
	span.SetAttributes(attributes...)
}

// RecordError records an error in the span
func RecordError(span trace.Span, err error) {
	if span != nil && err != nil {
		span.RecordError(err)
	}
}
