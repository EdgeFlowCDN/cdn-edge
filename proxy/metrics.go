package proxy

import (
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus metrics for the edge proxy.
type Metrics struct {
	RequestsTotal   *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
	CacheHits       *prometheus.CounterVec
	BytesSent       *prometheus.CounterVec
	OriginRequests  *prometheus.CounterVec
	OriginDuration  *prometheus.HistogramVec
	ActiveConns     prometheus.Gauge
	activeConns     atomic.Int64
	registry        *prometheus.Registry
}

// NewMetrics creates and registers Prometheus metrics.
func NewMetrics() *Metrics {
	m := &Metrics{
		RequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cdn_requests_total",
			Help: "Total number of HTTP requests",
		}, []string{"method", "status", "host"}),

		RequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "cdn_request_duration_seconds",
			Help:    "Request duration in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5},
		}, []string{"method", "cache_status"}),

		CacheHits: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cdn_cache_hits_total",
			Help: "Cache hit/miss counts",
		}, []string{"status"}),

		BytesSent: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cdn_bytes_sent_total",
			Help: "Total bytes sent to clients",
		}, []string{"host"}),

		OriginRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cdn_origin_requests_total",
			Help: "Total origin fetch requests",
		}, []string{"status"}),

		OriginDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "cdn_origin_duration_seconds",
			Help:    "Origin fetch duration in seconds",
			Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30},
		}, []string{"host"}),

		ActiveConns: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "cdn_active_connections",
			Help: "Number of active connections",
		}),
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(m.RequestsTotal, m.RequestDuration, m.CacheHits,
		m.BytesSent, m.OriginRequests, m.OriginDuration, m.ActiveConns)
	m.registry = reg

	return m
}

func (m *Metrics) RecordRequest(method string, status int, host string, cacheStatus string, bodyBytes int64, duration time.Duration) {
	m.RequestsTotal.WithLabelValues(method, strconv.Itoa(status), host).Inc()
	m.RequestDuration.WithLabelValues(method, cacheStatus).Observe(duration.Seconds())
	m.CacheHits.WithLabelValues(cacheStatus).Inc()
	m.BytesSent.WithLabelValues(host).Add(float64(bodyBytes))
}

func (m *Metrics) RecordOriginRequest(status string, host string, duration time.Duration) {
	m.OriginRequests.WithLabelValues(status).Inc()
	m.OriginDuration.WithLabelValues(host).Observe(duration.Seconds())
}

func (m *Metrics) ConnOpen() {
	m.activeConns.Add(1)
	m.ActiveConns.Inc()
}

func (m *Metrics) ConnClose() {
	m.activeConns.Add(-1)
	m.ActiveConns.Dec()
}

// MetricsHandler returns the Prometheus metrics HTTP handler for this metrics instance.
func (m *Metrics) MetricsHandler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// HealthHandler returns a simple health check handler.
func HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}
}
