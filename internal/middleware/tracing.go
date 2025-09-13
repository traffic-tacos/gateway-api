package middleware

import (
	"context"
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/traffic-tacos/gateway-api/internal/config"
	"github.com/traffic-tacos/gateway-api/internal/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
)

// TracingMiddleware handles OpenTelemetry tracing
type TracingMiddleware struct {
	tracer trace.Tracer
	logger *logging.Logger
}

// NewTracingMiddleware creates a new tracing middleware
func NewTracingMiddleware(cfg config.ObservabilityConfig, logger *logging.Logger) (*TracingMiddleware, error) {
	// Initialize OTLP exporter
	exporter, err := otlptracegrpc.New(context.Background(),
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		otlptracegrpc.WithInsecure(), // Use TLS in production
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	// Create resource
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.ServiceVersionKey.String(cfg.ServiceVersion),
			semconv.ServiceNamespaceKey.String("tickets-api"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.TraceSampling)),
	)

	otel.SetTracerProvider(tp)

	tracer := otel.Tracer(cfg.ServiceName)

	return &TracingMiddleware{
		tracer: tracer,
		logger: logger,
	}, nil
}

// Tracing returns the tracing middleware
func (t *TracingMiddleware) Tracing() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Extract or generate trace ID
		traceID := c.Get("traceparent")
		if traceID == "" {
			traceID = generateTraceID()
		}

		// Store trace ID in context for logging
		c.Locals("trace_id", traceID)

		// Start span
		ctx, span := t.tracer.Start(c.Context(), fmt.Sprintf("%s %s", c.Method(), c.Path()),
			trace.WithAttributes(
				attribute.String("http.method", c.Method()),
				attribute.String("http.url", c.OriginalURL()),
				attribute.String("http.user_agent", c.Get("User-Agent")),
				attribute.String("http.remote_addr", c.IP()),
			),
		)
		defer span.End()

		// Update context
		c.SetUserContext(ctx)

		// Add trace ID to response header
		c.Set("x-trace-id", traceID)

		// Process request
		err := c.Next()

		// Record span information
		span.SetAttributes(
			attribute.Int("http.status_code", c.Response().StatusCode()),
			attribute.Int("http.response_size", len(c.Response().Body())),
		)

		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}

		return err
	}
}

// generateTraceID generates a new trace ID
func generateTraceID() string {
	return uuid.New().String()
}

// GetTraceIDFromContext extracts trace ID from context
func GetTraceIDFromContext(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if span == nil {
		return ""
	}

	spanContext := span.SpanContext()
	if !spanContext.IsValid() {
		return ""
	}

	return spanContext.TraceID().String()
}

// StartSpan starts a new span for backend calls
func (t *TracingMiddleware) StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, name, trace.WithAttributes(attrs...))
}

// InjectHeaders injects tracing headers into the request context
func (t *TracingMiddleware) InjectHeaders(ctx context.Context) map[string]string {
	headers := make(map[string]string)

	span := trace.SpanFromContext(ctx)
	if span != nil {
		spanContext := span.SpanContext()
		if spanContext.IsValid() {
			// Inject W3C trace context
			headers["traceparent"] = fmt.Sprintf("00-%s-%s-%s",
				spanContext.TraceID().String(),
				spanContext.SpanID().String(),
				spanContext.TraceFlags().String(),
			)
		}
	}

	return headers
}
