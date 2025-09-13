package logging

import (
	"context"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger wraps zap.Logger with additional context methods
type Logger struct {
	*zap.Logger
}

// NewLogger creates a new structured logger
func NewLogger(serviceName string) (*Logger, error) {
	config := zap.NewProductionConfig()

	// Set log level from environment
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	level, err := zapcore.ParseLevel(logLevel)
	if err != nil {
		level = zapcore.InfoLevel
	}
	config.Level = zap.NewAtomicLevelAt(level)

	// JSON encoder for structured logging
	config.Encoding = "json"
	config.EncoderConfig.TimeKey = "ts"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.EncoderConfig.EncodeLevel = zapcore.LowercaseLevelEncoder

	// Add service name
	config.InitialFields = map[string]interface{}{
		"service": serviceName,
	}

	zapLogger, err := config.Build(
		zap.AddCaller(),
		zap.AddStacktrace(zapcore.ErrorLevel),
	)
	if err != nil {
		return nil, err
	}

	return &Logger{zapLogger}, nil
}

// WithContext adds context fields to logger
func (l *Logger) WithContext(ctx context.Context) *Logger {
	if traceID := getTraceIDFromContext(ctx); traceID != "" {
		return &Logger{l.Logger.With(zap.String("trace_id", traceID))}
	}
	return l
}

// WithFields adds additional fields to logger
func (l *Logger) WithFields(fields ...zap.Field) *Logger {
	return &Logger{l.Logger.With(fields...)}
}

// WithUser adds user information to logger
func (l *Logger) WithUser(userID string) *Logger {
	return &Logger{l.Logger.With(zap.String("user_id", userID))}
}

// WithRequest adds HTTP request information to logger
func (l *Logger) WithRequest(method, path string, status int, latency float64) *Logger {
	return &Logger{l.Logger.With(
		zap.String("http.method", method),
		zap.String("http.path", path),
		zap.Int("http.status", status),
		zap.Float64("latency_ms", latency),
	)}
}

// RequestLogger logs HTTP request details
func (l *Logger) RequestLogger(method, path string, status int, latency float64, userID, traceID string) {
	fields := []zap.Field{
		zap.String("http.method", method),
		zap.String("http.path", path),
		zap.Int("http.status", status),
		zap.Float64("latency_ms", latency),
	}

	if userID != "" {
		fields = append(fields, zap.String("user_id", userID))
	}
	if traceID != "" {
		fields = append(fields, zap.String("trace_id", traceID))
	}

	l.Logger.Info("request_completed", fields...)
}

// Error logs error with context
func (l *Logger) ErrorWithContext(ctx context.Context, msg string, err error, fields ...zap.Field) {
	logger := l.WithContext(ctx)
	allFields := append(fields, zap.Error(err))
	logger.Logger.Error(msg, allFields...)
}

// WarnWithContext logs warning with context
func (l *Logger) WarnWithContext(ctx context.Context, msg string, fields ...zap.Field) {
	logger := l.WithContext(ctx)
	logger.Logger.Warn(msg, fields...)
}

// InfoWithContext logs info with context
func (l *Logger) InfoWithContext(ctx context.Context, msg string, fields ...zap.Field) {
	logger := l.WithContext(ctx)
	logger.Logger.Info(msg, fields...)
}

// DebugWithContext logs debug with context
func (l *Logger) DebugWithContext(ctx context.Context, msg string, fields ...zap.Field) {
	logger := l.WithContext(ctx)
	logger.Logger.Debug(msg, fields...)
}

// getTraceIDFromContext extracts trace ID from context
// This will be implemented when we add tracing context
func getTraceIDFromContext(ctx context.Context) string {
	// TODO: Implement when tracing is added
	return ""
}
