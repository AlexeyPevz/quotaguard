package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus metrics for the application
type Metrics struct {
	// RequestLatency tracks HTTP request latency by endpoint and method
	RequestLatency *prometheus.HistogramVec
	// QuotaUtilization tracks quota usage percentage by account and provider
	QuotaUtilization *prometheus.GaugeVec
	// RouterDecisions counts router decisions by policy and outcome
	RouterDecisions *prometheus.CounterVec
	// ReservationMetrics tracks reservation operations
	ReservationMetrics *prometheus.CounterVec
	// CollectorMetrics tracks collector operations
	CollectorMetrics *prometheus.CounterVec
	// ErrorCounter counts errors by type and endpoint
	ErrorCounter *prometheus.CounterVec
	// AccountHealth tracks health status for account
	AccountHealth *prometheus.GaugeVec
	// HTTPRequestsTotal total HTTP requests
	HTTPRequestsTotal *prometheus.CounterVec
	// HTTPRequestsInFlight current HTTP requests being processed
	HTTPRequestsInFlight prometheus.Gauge
	// LimiterAcquiresTotal tracks total number of token acquire attempts
	LimiterAcquiresTotal *prometheus.CounterVec
	// LimiterReleasesTotal tracks total number of token releases
	LimiterReleasesTotal prometheus.Counter
	// LimiterWaitDuration tracks time spent waiting for a token
	LimiterWaitDuration *prometheus.HistogramVec
	// LimiterTokensAvailable tracks number of available tokens
	LimiterTokensAvailable *prometheus.GaugeVec
	// LimiterCapacity tracks limiter capacity
	LimiterCapacity *prometheus.GaugeVec
	// registry is the custom registry for this metrics instance
	registry *prometheus.Registry
}

// NewMetrics creates and registers all Prometheus metrics
func NewMetrics(namespace string) *Metrics {
	registry := prometheus.NewRegistry()

	m := &Metrics{
		registry: registry,
		RequestLatency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "request_latency_seconds",
				Help:      "HTTP request latency in seconds",
				Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0},
			},
			[]string{"endpoint", "method", "status"},
		),
		QuotaUtilization: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "quota_utilization_percent",
				Help:      "Current quota utilization percentage",
			},
			[]string{"account_id", "provider", "dimension"},
		),
		RouterDecisions: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "router_decisions_total",
				Help:      "Total number of router decisions",
			},
			[]string{"policy", "outcome", "provider"},
		),
		ReservationMetrics: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "reservation_operations_total",
				Help:      "Total number of reservation operations",
			},
			[]string{"operation", "status"},
		),
		CollectorMetrics: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "collector_operations_total",
				Help:      "Total number of collector operations",
			},
			[]string{"operation", "status", "source"},
		),
		ErrorCounter: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "errors_total",
				Help:      "Total number of errors",
			},
			[]string{"type", "endpoint", "method"},
		),
		AccountHealth: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "account_health_status",
				Help:      "Health status of accounts (1=healthy, 0=unhealthy)",
			},
			[]string{"account_id", "provider"},
		),
		HTTPRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "http_requests_total",
				Help:      "Total number of HTTP requests",
			},
			[]string{"endpoint", "method", "status"},
		),
		HTTPRequestsInFlight: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "http_requests_in_flight",
				Help:      "Current number of HTTP requests being processed",
			},
		),
		LimiterAcquiresTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "limiter_acquires_total",
				Help:      "Total number of token acquire attempts",
			},
			[]string{"result"},
		),
		LimiterReleasesTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "limiter_releases_total",
				Help:      "Total number of token releases",
			},
		),
		LimiterWaitDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "limiter_wait_duration_seconds",
				Help:      "Time spent waiting for a token",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"result"},
		),
		LimiterTokensAvailable: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "limiter_tokens_available",
				Help:      "Number of available tokens",
			},
			[]string{"account_id"},
		),
		LimiterCapacity: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "limiter_capacity",
				Help:      "Limiter capacity",
			},
			[]string{"account_id"},
		),
	}

	// Register metrics with custom registry
	registry.MustRegister(
		m.RequestLatency,
		m.QuotaUtilization,
		m.RouterDecisions,
		m.ReservationMetrics,
		m.CollectorMetrics,
		m.ErrorCounter,
		m.AccountHealth,
		m.HTTPRequestsTotal,
		m.HTTPRequestsInFlight,
		m.LimiterAcquiresTotal,
		m.LimiterReleasesTotal,
		m.LimiterWaitDuration,
		m.LimiterTokensAvailable,
		m.LimiterCapacity,
	)

	return m
}

// Handler returns a Prometheus handler for these metrics
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// RecordRequestLatency records the latency of an HTTP request
func (m *Metrics) RecordRequestLatency(endpoint, method, status string, durationSeconds float64) {
	m.RequestLatency.WithLabelValues(endpoint, method, status).Observe(durationSeconds)
}

// RecordQuotaUtilization records quota utilization for an account
func (m *Metrics) RecordQuotaUtilization(accountID, provider, dimension string, percent float64) {
	m.QuotaUtilization.WithLabelValues(accountID, provider, dimension).Set(percent)
}

// RecordRouterDecision records a router decision
func (m *Metrics) RecordRouterDecision(policy, outcome, provider string) {
	m.RouterDecisions.WithLabelValues(policy, outcome, provider).Inc()
}

// RecordReservation records a reservation operation
func (m *Metrics) RecordReservation(operation, status string) {
	m.ReservationMetrics.WithLabelValues(operation, status).Inc()
}

// RecordCollector records a collector operation
func (m *Metrics) RecordCollector(operation, status, source string) {
	m.CollectorMetrics.WithLabelValues(operation, status, source).Inc()
}

// RecordError records an error
func (m *Metrics) RecordError(errorType, endpoint, method string) {
	m.ErrorCounter.WithLabelValues(errorType, endpoint, method).Inc()
}

// SetAccountHealth sets the health status for an account
func (m *Metrics) SetAccountHealth(accountID, provider string, healthy bool) {
	value := 1.0
	if !healthy {
		value = 0.0
	}
	m.AccountHealth.WithLabelValues(accountID, provider).Set(value)
}

// RecordHTTPRequest records an HTTP request
func (m *Metrics) RecordHTTPRequest(endpoint, method, status string) {
	m.HTTPRequestsTotal.WithLabelValues(endpoint, method, status).Inc()
}

// IncHTTPRequestsInFlight increments the in-flight requests counter
func (m *Metrics) IncHTTPRequestsInFlight() {
	m.HTTPRequestsInFlight.Inc()
}

// DecHTTPRequestsInFlight decrements the in-flight requests counter
func (m *Metrics) DecHTTPRequestsInFlight() {
	m.HTTPRequestsInFlight.Dec()
}

// RecordLimiterAcquire records a token acquire attempt with result
func (m *Metrics) RecordLimiterAcquire(result string) {
	m.LimiterAcquiresTotal.WithLabelValues(result).Inc()
}

// RecordLimiterRelease records a token release
func (m *Metrics) RecordLimiterRelease() {
	m.LimiterReleasesTotal.Inc()
}

// RecordLimiterWaitDuration records the time spent waiting for a token
func (m *Metrics) RecordLimiterWaitDuration(result string, durationSeconds float64) {
	m.LimiterWaitDuration.WithLabelValues(result).Observe(durationSeconds)
}

// SetLimiterTokensAvailable sets the number of available tokens for an account
func (m *Metrics) SetLimiterTokensAvailable(accountID string, count int) {
	m.LimiterTokensAvailable.WithLabelValues(accountID).Set(float64(count))
}

// SetLimiterCapacity sets the limiter capacity for an account
func (m *Metrics) SetLimiterCapacity(accountID string, capacity int) {
	m.LimiterCapacity.WithLabelValues(accountID).Set(float64(capacity))
}
