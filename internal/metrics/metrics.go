package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics
type Metrics struct {
	HTTPRequestDuration    *prometheus.HistogramVec
	HTTPRequestTotal       *prometheus.CounterVec
	BackendCallDuration    *prometheus.HistogramVec
	RateLimitDroppedTotal  prometheus.Counter
	IdempotencyHitsTotal   *prometheus.CounterVec
	ActiveConnections      prometheus.Gauge
}

// NewMetrics creates and registers all metrics
func NewMetrics(serviceName string) *Metrics {
	m := &Metrics{}

	// HTTP request metrics
	m.HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_server_requests_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "route", "status_code"},
	)

	m.HTTPRequestTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_server_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "route", "status_code"},
	)

	// Backend call metrics
	m.BackendCallDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "backend_call_duration_seconds",
			Help:    "Backend service call duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"service", "method", "status_code"},
	)

	// Rate limiting metrics
	m.RateLimitDroppedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "rate_limit_dropped_total",
			Help: "Total number of requests dropped due to rate limiting",
		},
	)

	// Idempotency metrics
	m.IdempotencyHitsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "idempotency_hits_total",
			Help: "Total number of idempotency key hits",
		},
		[]string{"type"}, // "hit" or "miss"
	)

	// Active connections
	m.ActiveConnections = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "active_connections",
			Help: "Number of active connections",
		},
	)

	return m
}

// RecordHTTPRequest records HTTP request metrics
func (m *Metrics) RecordHTTPRequest(method, route string, statusCode int, duration time.Duration) {
	durationSeconds := duration.Seconds()
	statusCodeStr := strconv.Itoa(statusCode)

	m.HTTPRequestDuration.WithLabelValues(method, route, statusCodeStr).Observe(durationSeconds)
	m.HTTPRequestTotal.WithLabelValues(method, route, statusCodeStr).Inc()
}

// RecordBackendCall records backend service call metrics
func (m *Metrics) RecordBackendCall(service, method string, statusCode int, duration time.Duration) {
	durationSeconds := duration.Seconds()
	statusCodeStr := strconv.Itoa(statusCode)

	m.BackendCallDuration.WithLabelValues(service, method, statusCodeStr).Observe(durationSeconds)
}

// RecordRateLimitDrop increments rate limit drop counter
func (m *Metrics) RecordRateLimitDrop() {
	m.RateLimitDroppedTotal.Inc()
}

// RecordIdempotencyHit records idempotency key hit
func (m *Metrics) RecordIdempotencyHit(hitType string) {
	m.IdempotencyHitsTotal.WithLabelValues(hitType).Inc()
}

// SetActiveConnections sets the number of active connections
func (m *Metrics) SetActiveConnections(count float64) {
	m.ActiveConnections.Set(count)
}

// IncrementActiveConnections increments active connections counter
func (m *Metrics) IncrementActiveConnections() {
	m.ActiveConnections.Inc()
}

// DecrementActiveConnections decrements active connections counter
func (m *Metrics) DecrementActiveConnections() {
	m.ActiveConnections.Dec()
}
