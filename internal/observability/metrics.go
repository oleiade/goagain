package observability

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus metrics for the application.
type Metrics struct {
	// HTTP metrics
	HTTPRequestsTotal     *prometheus.CounterVec
	HTTPRequestDuration   *prometheus.HistogramVec
	HTTPRequestsInFlight  prometheus.Gauge
	HTTPResponseSize      *prometheus.HistogramVec
	HTTPRateLimitRejected prometheus.Counter

	// MCP metrics
	MCPToolInvocationsTotal *prometheus.CounterVec
	MCPToolDuration         *prometheus.HistogramVec
	MCPToolResultCount      *prometheus.HistogramVec
	MCPToolInFlight         *prometheus.GaugeVec
	MCPSessionsTotal        prometheus.Counter
	MCPSessionsActive       prometheus.Gauge

	// Application metrics
	DataCardsTotal     prometheus.Gauge
	DataSetsTotal      prometheus.Gauge
	DataKeywordsTotal  prometheus.Gauge
	DataAbilitiesTotal prometheus.Gauge

	// Registry for this metrics instance
	Registry *prometheus.Registry
}

// NewMetrics creates and registers all Prometheus metrics.
func NewMetrics(serviceName string) *Metrics {
	reg := prometheus.NewRegistry()

	// Add standard Go collectors
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	m := &Metrics{
		Registry: reg,

		// HTTP metrics
		HTTPRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name:        "http_requests_total",
				Help:        "Total number of HTTP requests",
				ConstLabels: prometheus.Labels{"service": serviceName},
			},
			[]string{"method", "path", "status_code"},
		),

		HTTPRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:        "http_request_duration_seconds",
				Help:        "HTTP request duration in seconds",
				ConstLabels: prometheus.Labels{"service": serviceName},
				Buckets:     []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
			},
			[]string{"method", "path", "status_code"},
		),

		HTTPRequestsInFlight: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name:        "http_requests_in_flight",
				Help:        "Current number of HTTP requests being processed",
				ConstLabels: prometheus.Labels{"service": serviceName},
			},
		),

		HTTPResponseSize: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:        "http_response_size_bytes",
				Help:        "HTTP response size in bytes",
				ConstLabels: prometheus.Labels{"service": serviceName},
				Buckets:     []float64{100, 500, 1000, 5000, 10000, 50000, 100000, 500000, 1000000},
			},
			[]string{"method", "path", "status_code"},
		),

		HTTPRateLimitRejected: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name:        "http_rate_limit_rejections_total",
				Help:        "Total number of requests rejected due to rate limiting",
				ConstLabels: prometheus.Labels{"service": serviceName},
			},
		),

		// MCP metrics
		MCPToolInvocationsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name:        "mcp_tool_invocations_total",
				Help:        "Total number of MCP tool invocations",
				ConstLabels: prometheus.Labels{"service": serviceName},
			},
			[]string{"tool_name", "status"},
		),

		MCPToolDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:        "mcp_tool_duration_seconds",
				Help:        "MCP tool execution duration in seconds",
				ConstLabels: prometheus.Labels{"service": serviceName},
				Buckets:     []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
			},
			[]string{"tool_name", "status"},
		),

		MCPToolResultCount: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:        "mcp_tool_result_count",
				Help:        "Number of results returned by MCP tools",
				ConstLabels: prometheus.Labels{"service": serviceName},
				Buckets:     []float64{0, 1, 5, 10, 20, 50, 100, 200, 500},
			},
			[]string{"tool_name"},
		),

		MCPToolInFlight: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name:        "mcp_tool_in_flight",
				Help:        "Current number of MCP tool invocations being processed",
				ConstLabels: prometheus.Labels{"service": serviceName},
			},
			[]string{"tool_name"},
		),

		MCPSessionsTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name:        "mcp_sessions_total",
				Help:        "Total number of MCP sessions started",
				ConstLabels: prometheus.Labels{"service": serviceName},
			},
		),

		MCPSessionsActive: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name:        "mcp_sessions_active",
				Help:        "Current number of active MCP sessions",
				ConstLabels: prometheus.Labels{"service": serviceName},
			},
		),

		// Application metrics
		DataCardsTotal: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name:        "goagain_data_cards_total",
				Help:        "Total number of cards loaded",
				ConstLabels: prometheus.Labels{"service": serviceName},
			},
		),

		DataSetsTotal: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name:        "goagain_data_sets_total",
				Help:        "Total number of sets loaded",
				ConstLabels: prometheus.Labels{"service": serviceName},
			},
		),

		DataKeywordsTotal: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name:        "goagain_data_keywords_total",
				Help:        "Total number of keywords loaded",
				ConstLabels: prometheus.Labels{"service": serviceName},
			},
		),

		DataAbilitiesTotal: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name:        "goagain_data_abilities_total",
				Help:        "Total number of abilities loaded",
				ConstLabels: prometheus.Labels{"service": serviceName},
			},
		),
	}

	// Register all metrics
	reg.MustRegister(
		m.HTTPRequestsTotal,
		m.HTTPRequestDuration,
		m.HTTPRequestsInFlight,
		m.HTTPResponseSize,
		m.HTTPRateLimitRejected,
		m.MCPToolInvocationsTotal,
		m.MCPToolDuration,
		m.MCPToolResultCount,
		m.MCPToolInFlight,
		m.MCPSessionsTotal,
		m.MCPSessionsActive,
		m.DataCardsTotal,
		m.DataSetsTotal,
		m.DataKeywordsTotal,
		m.DataAbilitiesTotal,
	)

	return m
}

// SetDataStats sets the application data metrics.
func (m *Metrics) SetDataStats(stats map[string]int) {
	if v, ok := stats["cards"]; ok {
		m.DataCardsTotal.Set(float64(v))
	}
	if v, ok := stats["sets"]; ok {
		m.DataSetsTotal.Set(float64(v))
	}
	if v, ok := stats["keywords"]; ok {
		m.DataKeywordsTotal.Set(float64(v))
	}
	if v, ok := stats["abilities"]; ok {
		m.DataAbilitiesTotal.Set(float64(v))
	}
}

// Handler returns an HTTP handler for the metrics endpoint.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// MetricsMiddleware creates middleware that records HTTP metrics.
func (m *Metrics) MetricsMiddleware(pathNormalizer func(string) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip metrics for the metrics endpoint itself
			if r.URL.Path == "/metrics" {
				next.ServeHTTP(w, r)
				return
			}

			m.HTTPRequestsInFlight.Inc()
			defer m.HTTPRequestsInFlight.Dec()

			start := time.Now()

			wrapped := &metricsResponseWriter{
				ResponseWriter: w,
				status:         http.StatusOK,
			}

			next.ServeHTTP(wrapped, r)

			duration := time.Since(start).Seconds()
			path := r.URL.Path
			if pathNormalizer != nil {
				path = pathNormalizer(path)
			}
			statusCode := strconv.Itoa(wrapped.status)

			m.HTTPRequestsTotal.WithLabelValues(r.Method, path, statusCode).Inc()
			m.HTTPRequestDuration.WithLabelValues(r.Method, path, statusCode).Observe(duration)
			m.HTTPResponseSize.WithLabelValues(r.Method, path, statusCode).Observe(float64(wrapped.size))
		})
	}
}

// metricsResponseWriter wraps http.ResponseWriter to capture status and size for metrics.
type metricsResponseWriter struct {
	http.ResponseWriter
	status int
	size   int64
}

func (rw *metricsResponseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *metricsResponseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.size += int64(n)
	return n, err
}

func (rw *metricsResponseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// RecordRateLimitRejection records a rate limit rejection.
func (m *Metrics) RecordRateLimitRejection() {
	m.HTTPRateLimitRejected.Inc()
}

// RecordToolInvocation records an MCP tool invocation.
func (m *Metrics) RecordToolInvocation(toolName string, duration time.Duration, resultCount int, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}

	m.MCPToolInvocationsTotal.WithLabelValues(toolName, status).Inc()
	m.MCPToolDuration.WithLabelValues(toolName, status).Observe(duration.Seconds())
	m.MCPToolResultCount.WithLabelValues(toolName).Observe(float64(resultCount))
}

// ToolInFlightInc increments the in-flight gauge for a tool.
func (m *Metrics) ToolInFlightInc(toolName string) {
	m.MCPToolInFlight.WithLabelValues(toolName).Inc()
}

// ToolInFlightDec decrements the in-flight gauge for a tool.
func (m *Metrics) ToolInFlightDec(toolName string) {
	m.MCPToolInFlight.WithLabelValues(toolName).Dec()
}

// RecordSessionStart records a new MCP session.
func (m *Metrics) RecordSessionStart() {
	m.MCPSessionsTotal.Inc()
	m.MCPSessionsActive.Inc()
}

// RecordSessionEnd records an MCP session ending.
func (m *Metrics) RecordSessionEnd() {
	m.MCPSessionsActive.Dec()
}

// PathNormalizer returns a function that normalizes URL paths for metrics labels.
// This prevents high-cardinality labels from dynamic path segments.
func PathNormalizer() func(string) string {
	// Patterns to normalize
	patterns := []struct {
		pattern *regexp.Regexp
		replace string
	}{
		// /cards/{id} - card unique IDs
		{regexp.MustCompile(`^/cards/[^/]+$`), "/cards/{id}"},
		// /cards/{id}/legality
		{regexp.MustCompile(`^/cards/[^/]+/legality$`), "/cards/{id}/legality"},
		// /sets/{id}
		{regexp.MustCompile(`^/sets/[^/]+$`), "/sets/{id}"},
		// /keywords/{name}
		{regexp.MustCompile(`^/keywords/[^/]+$`), "/keywords/{name}"},
	}

	return func(path string) string {
		// Normalize trailing slashes
		path = strings.TrimSuffix(path, "/")
		if path == "" {
			path = "/"
		}

		for _, p := range patterns {
			if p.pattern.MatchString(path) {
				return p.replace
			}
		}

		return path
	}
}
