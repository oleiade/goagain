package observability

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const meterName = "github.com/oleiade/goagain"

// Metrics holds all OpenTelemetry metrics for the application.
type Metrics struct {
	meter metric.Meter

	// HTTP metrics
	httpRequestsTotal     metric.Int64Counter
	httpRequestDuration   metric.Float64Histogram
	httpRequestsInFlight  metric.Int64UpDownCounter
	httpResponseSize      metric.Int64Histogram
	httpRateLimitRejected metric.Int64Counter

	// MCP metrics
	mcpToolInvocationsTotal metric.Int64Counter
	mcpToolDuration         metric.Float64Histogram
	mcpToolResultCount      metric.Int64Histogram
	mcpToolInFlight         metric.Int64UpDownCounter
	mcpSessionsTotal        metric.Int64Counter
	mcpSessionsActive       metric.Int64UpDownCounter

	// Application metrics (using callbacks for gauges)
	dataCardsTotal     int64
	dataSetsTotal      int64
	dataKeywordsTotal  int64
	dataAbilitiesTotal int64
	dataIndexEntries   map[string]int64
}

// NewMetrics creates and registers all OpenTelemetry metrics.
func NewMetrics(serviceName string) *Metrics {
	meter := otel.Meter(meterName,
		metric.WithInstrumentationVersion("0.1.0"),
	)

	m := &Metrics{
		meter:            meter,
		dataIndexEntries: make(map[string]int64),
	}

	var err error

	// HTTP metrics
	m.httpRequestsTotal, err = meter.Int64Counter("http.server.request.total",
		metric.WithDescription("Total number of HTTP requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		otel.Handle(err)
	}

	m.httpRequestDuration, err = meter.Float64Histogram("http.server.request.duration",
		metric.WithDescription("HTTP request duration in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		otel.Handle(err)
	}

	m.httpRequestsInFlight, err = meter.Int64UpDownCounter("http.server.active_requests",
		metric.WithDescription("Current number of HTTP requests being processed"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		otel.Handle(err)
	}

	m.httpResponseSize, err = meter.Int64Histogram("http.server.response.size",
		metric.WithDescription("HTTP response size in bytes"),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(100, 500, 1000, 5000, 10000, 50000, 100000, 500000, 1000000),
	)
	if err != nil {
		otel.Handle(err)
	}

	m.httpRateLimitRejected, err = meter.Int64Counter("http.server.rate_limit.rejected",
		metric.WithDescription("Total number of requests rejected due to rate limiting"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		otel.Handle(err)
	}

	// MCP metrics
	m.mcpToolInvocationsTotal, err = meter.Int64Counter("mcp.tool.invocations.total",
		metric.WithDescription("Total number of MCP tool invocations"),
		metric.WithUnit("{invocation}"),
	)
	if err != nil {
		otel.Handle(err)
	}

	m.mcpToolDuration, err = meter.Float64Histogram("mcp.tool.duration",
		metric.WithDescription("MCP tool execution duration in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5),
	)
	if err != nil {
		otel.Handle(err)
	}

	m.mcpToolResultCount, err = meter.Int64Histogram("mcp.tool.result_count",
		metric.WithDescription("Number of results returned by MCP tools"),
		metric.WithUnit("{result}"),
		metric.WithExplicitBucketBoundaries(0, 1, 5, 10, 20, 50, 100, 200, 500),
	)
	if err != nil {
		otel.Handle(err)
	}

	m.mcpToolInFlight, err = meter.Int64UpDownCounter("mcp.tool.active",
		metric.WithDescription("Current number of MCP tool invocations being processed"),
		metric.WithUnit("{invocation}"),
	)
	if err != nil {
		otel.Handle(err)
	}

	m.mcpSessionsTotal, err = meter.Int64Counter("mcp.sessions.total",
		metric.WithDescription("Total number of MCP sessions started"),
		metric.WithUnit("{session}"),
	)
	if err != nil {
		otel.Handle(err)
	}

	m.mcpSessionsActive, err = meter.Int64UpDownCounter("mcp.sessions.active",
		metric.WithDescription("Current number of active MCP sessions"),
		metric.WithUnit("{session}"),
	)
	if err != nil {
		otel.Handle(err)
	}

	// Register async gauges for application data metrics
	m.registerDataGauges()

	return m
}

func (m *Metrics) registerDataGauges() {
	// Cards gauge
	_, err := m.meter.Int64ObservableGauge("goagain.data.cards",
		metric.WithDescription("Total number of cards loaded"),
		metric.WithUnit("{card}"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(m.dataCardsTotal)
			return nil
		}),
	)
	if err != nil {
		otel.Handle(err)
	}

	// Sets gauge
	_, err = m.meter.Int64ObservableGauge("goagain.data.sets",
		metric.WithDescription("Total number of sets loaded"),
		metric.WithUnit("{set}"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(m.dataSetsTotal)
			return nil
		}),
	)
	if err != nil {
		otel.Handle(err)
	}

	// Keywords gauge
	_, err = m.meter.Int64ObservableGauge("goagain.data.keywords",
		metric.WithDescription("Total number of keywords loaded"),
		metric.WithUnit("{keyword}"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(m.dataKeywordsTotal)
			return nil
		}),
	)
	if err != nil {
		otel.Handle(err)
	}

	// Abilities gauge
	_, err = m.meter.Int64ObservableGauge("goagain.data.abilities",
		metric.WithDescription("Total number of abilities loaded"),
		metric.WithUnit("{ability}"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(m.dataAbilitiesTotal)
			return nil
		}),
	)
	if err != nil {
		otel.Handle(err)
	}

	// Index entries gauge
	_, err = m.meter.Int64ObservableGauge("goagain.data.index_entries",
		metric.WithDescription("Total number of entries in each data index"),
		metric.WithUnit("{entry}"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			for name, value := range m.dataIndexEntries {
				o.Observe(value, metric.WithAttributes(attribute.String("index_name", name)))
			}
			return nil
		}),
	)
	if err != nil {
		otel.Handle(err)
	}
}

// SetDataStats sets the application data metrics.
func (m *Metrics) SetDataStats(stats map[string]int) {
	if v, ok := stats["cards"]; ok {
		m.dataCardsTotal = int64(v)
	}
	if v, ok := stats["sets"]; ok {
		m.dataSetsTotal = int64(v)
	}
	if v, ok := stats["keywords"]; ok {
		m.dataKeywordsTotal = int64(v)
	}
	if v, ok := stats["abilities"]; ok {
		m.dataAbilitiesTotal = int64(v)
	}
}

// SetIndexStats sets the application index metrics.
func (m *Metrics) SetIndexStats(stats map[string]int) {
	for name, value := range stats {
		m.dataIndexEntries[name] = int64(value)
	}
}

// MetricsMiddleware creates middleware that records HTTP metrics.
func (m *Metrics) MetricsMiddleware(pathNormalizer func(string) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			m.httpRequestsInFlight.Add(ctx, 1)
			defer m.httpRequestsInFlight.Add(ctx, -1)

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

			attrs := []attribute.KeyValue{
				attribute.String("http.request.method", r.Method),
				attribute.String("http.route", path),
				attribute.Int("http.response.status_code", wrapped.status),
			}

			m.httpRequestsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
			m.httpRequestDuration.Record(ctx, duration, metric.WithAttributes(attrs...))
			m.httpResponseSize.Record(ctx, wrapped.size, metric.WithAttributes(attrs...))
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
	m.httpRateLimitRejected.Add(context.Background(), 1)
}

// RecordToolInvocation records an MCP tool invocation.
func (m *Metrics) RecordToolInvocation(toolName string, duration time.Duration, resultCount int, err error) {
	ctx := context.Background()
	status := "success"
	if err != nil {
		status = "error"
	}

	attrs := []attribute.KeyValue{
		attribute.String("tool.name", toolName),
		attribute.String("tool.status", status),
	}

	m.mcpToolInvocationsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.mcpToolDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	m.mcpToolResultCount.Record(ctx, int64(resultCount), metric.WithAttributes(attribute.String("tool.name", toolName)))
}

// ToolInFlightInc increments the in-flight gauge for a tool.
func (m *Metrics) ToolInFlightInc(toolName string) {
	m.mcpToolInFlight.Add(context.Background(), 1, metric.WithAttributes(attribute.String("tool.name", toolName)))
}

// ToolInFlightDec decrements the in-flight gauge for a tool.
func (m *Metrics) ToolInFlightDec(toolName string) {
	m.mcpToolInFlight.Add(context.Background(), -1, metric.WithAttributes(attribute.String("tool.name", toolName)))
}

// RecordSessionStart records a new MCP session.
func (m *Metrics) RecordSessionStart() {
	ctx := context.Background()
	m.mcpSessionsTotal.Add(ctx, 1)
	m.mcpSessionsActive.Add(ctx, 1)
}

// RecordSessionEnd records an MCP session ending.
func (m *Metrics) RecordSessionEnd() {
	m.mcpSessionsActive.Add(context.Background(), -1)
}

// PathNormalizer returns a function that normalizes URL paths for metrics labels.
// This prevents high-cardinality labels from dynamic path segments.
func PathNormalizer() func(string) string {
	// Patterns to normalize
	patterns := []struct {
		pattern *regexp.Regexp
		replace string
	}{
		// /v1/cards/{id} - card unique IDs
		{regexp.MustCompile(`^/v1/cards/[^/]+$`), "/v1/cards/{id}"},
		// /v1/cards/{id}/legality
		{regexp.MustCompile(`^/v1/cards/[^/]+/legality$`), "/v1/cards/{id}/legality"},
		// /v1/sets/{id}
		{regexp.MustCompile(`^/v1/sets/[^/]+$`), "/v1/sets/{id}"},
		// /v1/keywords/{name}
		{regexp.MustCompile(`^/v1/keywords/[^/]+$`), "/v1/keywords/{name}"},
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
