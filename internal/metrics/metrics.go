package metrics

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

var (
	// HTTP metrics
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_server_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "route", "status_code"},
	)

	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_server_requests_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "route", "status_code"},
	)

	// Backend call metrics
	backendCallDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "backend_call_duration_seconds",
			Help:    "Backend API call duration in seconds",
			Buckets: []float64{0.001, 0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0},
		},
		[]string{"service", "method", "status_code"},
	)

	// Rate limiting metrics
	rateLimitDroppedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ratelimit_dropped_total",
			Help: "Total number of requests dropped due to rate limiting",
		},
		[]string{"key_type"}, // user or ip
	)

	// Idempotency metrics
	idempotencyHitsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "idempotency_hits_total",
			Help: "Total number of idempotency hits",
		},
		[]string{"type"}, // hit or miss
	)

	// Queue metrics
	queueOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "queue_operations_total",
			Help: "Total number of queue operations",
		},
		[]string{"operation", "status"}, // join/status/enter/leave, success/failure
	)

	queueWaitTime = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "queue_wait_time_seconds",
			Help:    "Time spent waiting in queue",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 300, 600, 1200, 1800},
		},
		[]string{"event_id"},
	)

	// Redis metrics
	redisOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "redis_operations_total",
			Help: "Total number of Redis operations",
		},
		[]string{"operation", "status"}, // get/set/del, success/failure
	)

	redisOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "redis_operation_duration_seconds",
			Help:    "Redis operation duration in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
		},
		[]string{"operation"},
	)
)

// Init initializes the metrics
func Init() error {
	// Register metrics
	prometheus.MustRegister(
		httpRequestsTotal,
		httpRequestDuration,
		backendCallDuration,
		rateLimitDroppedTotal,
		idempotencyHitsTotal,
		queueOperationsTotal,
		queueWaitTime,
		redisOperationsTotal,
		redisOperationDuration,
	)

	return nil
}

// HTTPMetricsMiddleware records HTTP metrics
func HTTPMetricsMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()

		// Process request
		err := c.Next()

		// Record metrics
		duration := time.Since(start).Seconds()
		method := c.Method()
		route := c.Route().Path
		if route == "" {
			route = c.Path()
		}
		statusCode := strconv.Itoa(c.Response().StatusCode())

		httpRequestsTotal.WithLabelValues(method, route, statusCode).Inc()
		httpRequestDuration.WithLabelValues(method, route, statusCode).Observe(duration)

		return err
	}
}

// RecordBackendCall records metrics for backend API calls
func RecordBackendCall(service, method string, statusCode int, duration time.Duration) {
	statusStr := strconv.Itoa(statusCode)
	backendCallDuration.WithLabelValues(service, method, statusStr).Observe(duration.Seconds())
}

// RecordRateLimitDrop records rate limit drops
func RecordRateLimitDrop(keyType string) {
	rateLimitDroppedTotal.WithLabelValues(keyType).Inc()
}

// RecordIdempotencyHit records idempotency cache hits/misses
func RecordIdempotencyHit(hitType string) {
	idempotencyHitsTotal.WithLabelValues(hitType).Inc()
}

// RecordQueueOperation records queue operations
func RecordQueueOperation(operation, status string) {
	queueOperationsTotal.WithLabelValues(operation, status).Inc()
}

// RecordQueueWaitTime records time spent waiting in queue
func RecordQueueWaitTime(eventID string, waitTime time.Duration) {
	queueWaitTime.WithLabelValues(eventID).Observe(waitTime.Seconds())
}

// RecordRedisOperation records Redis operations
func RecordRedisOperation(operation, status string, duration time.Duration) {
	redisOperationsTotal.WithLabelValues(operation, status).Inc()
	redisOperationDuration.WithLabelValues(operation).Observe(duration.Seconds())
}

// PrometheusHandler returns the Prometheus metrics handler
func PrometheusHandler() fiber.Handler {
	promHandler := promhttp.Handler()
	return func(c *fiber.Ctx) error {
		promHandler.ServeHTTP(c.Response(), c.Request())
		return nil
	}
}