package logging

import (
	"os"

	"github.com/traffic-tacos/gateway-api/internal/config"

	"github.com/sirupsen/logrus"
)

// New creates a new structured logger
func New(cfg *config.Config) *logrus.Logger {
	logger := logrus.New()

	// Set log level
	level, err := logrus.ParseLevel(cfg.Log.Level)
	if err != nil {
		logger.Warn("Invalid log level, defaulting to info")
		level = logrus.InfoLevel
	}
	logger.SetLevel(level)

	// Set output format
	if cfg.Log.Format == "json" {
		logger.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.000Z",
			FieldMap: logrus.FieldMap{
				logrus.FieldKeyTime:  "ts",
				logrus.FieldKeyLevel: "level",
				logrus.FieldKeyMsg:   "msg",
			},
		})
	} else {
		logger.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: "2006-01-02T15:04:05.000Z",
		})
	}

	logger.SetOutput(os.Stdout)

	// Add default fields
	logger = logger.WithFields(logrus.Fields{
		"service":     "gateway-api",
		"version":     getVersion(),
		"environment": cfg.Server.Environment,
	}).Logger

	return logger
}

// getVersion returns the application version
func getVersion() string {
	if version := os.Getenv("APP_VERSION"); version != "" {
		return version
	}
	return "dev"
}

// WithTraceID adds trace ID to logger context
func WithTraceID(logger *logrus.Logger, traceID string) *logrus.Entry {
	return logger.WithField("trace_id", traceID)
}

// WithUserID adds user ID to logger context
func WithUserID(logger *logrus.Logger, userID string) *logrus.Entry {
	return logger.WithField("user_id", userID)
}

// WithRequest adds request context to logger
func WithRequest(logger *logrus.Logger, method, path string, statusCode int, latencyMs float64) *logrus.Entry {
	return logger.WithFields(logrus.Fields{
		"http": map[string]interface{}{
			"method": method,
			"route":  path,
			"status": statusCode,
		},
		"latency_ms": latencyMs,
	})
}