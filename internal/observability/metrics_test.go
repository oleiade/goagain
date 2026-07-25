package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestMetrics_RaceFreeStats exercises the concurrent path that triggered the High
// finding in the review: async-gauge callbacks read mutable counter and map fields
// from the SDK's collection goroutine, while SetDataStats / SetIndexStats write them
// from the loader goroutine. Pre-fix the scalars were plain int64 (data race) and
// SetIndexStats mutated the map in place (concurrent map write / iteration would
// panic). With the atomic.Int64 / atomic.Pointer fix this test runs cleanly under
// `go test -race`.
func TestMetrics_RaceFreeStats(t *testing.T) {
	m := NewMetrics("test-race")

	const iterations = 2000
	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Simulate the gauge callback's reads. The real callback runs on an OTel SDK
	// goroutine; we hit the same atomic getters directly.
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = m.dataCardsTotal.Load()
			_ = m.dataSetsTotal.Load()
			_ = m.dataKeywordsTotal.Load()
			_ = m.dataAbilitiesTotal.Load()
			if snap := m.dataIndexEntries.Load(); snap != nil {
				for k, v := range *snap {
					_, _ = k, v
				}
			}
		}
	})

	for i := range iterations {
		m.SetDataStats(map[string]int{
			"cards":     i,
			"sets":      i,
			"keywords":  i,
			"abilities": i,
		})
		m.SetIndexStats(map[string]int{
			"cards_by_id":   i,
			"cards_by_name": i,
			"cards_by_type": i,
		})
	}

	close(stop)
	wg.Wait()

	// Sanity check: last write is visible.
	if got := m.dataCardsTotal.Load(); got != int64(iterations-1) {
		t.Errorf("dataCardsTotal = %d, want %d", got, iterations-1)
	}
	if snap := m.dataIndexEntries.Load(); snap == nil || (*snap)["cards_by_id"] != int64(iterations-1) {
		t.Errorf("dataIndexEntries missing latest write, snap=%v", snap)
	}
}

// TestMetricsResponseWriter_Flush covers the mcp-go SSE regression: mcp-go
// type-asserts w.(http.Flusher) directly, so metricsResponseWriter must
// implement Flush and forward it to the underlying ResponseWriter. It also
// checks that flushing before any WriteHeader call leaves status at its
// implicit-200 default, matching what Write already does in that case.
func TestMetricsResponseWriter_Flush(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &metricsResponseWriter{ResponseWriter: rec, status: http.StatusOK}

	var _ http.Flusher = rw
	rw.Flush()

	if !rec.Flushed {
		t.Error("Flush did not reach the underlying ResponseRecorder")
	}
	if rw.status != http.StatusOK {
		t.Errorf("status = %d, want %d (implicit 200 before any WriteHeader)", rw.status, http.StatusOK)
	}
}

// TestPathNormalizer covers the allowlist behavior: regex patterns still
// normalize dynamic segments, static routes from internal/api/router.go pass
// through unchanged (including trailing-slash handling), and everything else
// -- unknown paths and invalid UTF-8 alike -- collapses to "/other". Before
// the fix the fallthrough case returned the raw path, which is how a single
// invalid-UTF-8 request poisoned the cumulative OTLP counter for 9 days.
func TestPathNormalizer(t *testing.T) {
	normalize := PathNormalizer()

	cases := []struct {
		name string
		path string
		want string
	}{
		{"card id", "/v1/cards/abc123", "/v1/cards/{id}"},
		{"card legality", "/v1/cards/abc123/legality", "/v1/cards/{id}/legality"},
		{"set id", "/v1/sets/wtr", "/v1/sets/{id}"},
		{"keyword name", "/v1/keywords/go-again", "/v1/keywords/{name}"},
		{"root", "/", "/"},
		{"empty collapses to root", "", "/"},
		{"static health", "/health", "/health"},
		{"static with trailing slash", "/health/", "/health"},
		{"static collection route", "/v1/cards", "/v1/cards"},
		{"static well-known", "/.well-known/mcp/server-card.json", "/.well-known/mcp/server-card.json"},
		{"unknown scanner path", "/wp-admin/setup.php", "/other"},
		{"unknown dotfile", "/.env", "/other"},
		// [^/]+ matches any non-"/" byte, including invalid UTF-8, so a
		// bogus id segment still hits the card-id pattern; the replacement
		// is the fixed template string, never the raw bytes, so the output
		// stays clean regardless.
		{"invalid utf8 in a matched dynamic segment", "/v1/cards/\xff\xfe", "/v1/cards/{id}"},
		// Invalid UTF-8 in a position no pattern matches (the shape of the
		// actual production incident: a percent-decoded path that wasn't a
		// card ID at all) must fall through to "/other" rather than being
		// returned verbatim.
		{"invalid utf8 in an unmatched path", "/\xff\xfe", "/other"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalize(tc.path); got != tc.want {
				t.Errorf("PathNormalizer()(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestPathNormalizer_AlwaysBounded is a property check: whatever hostile input
// the normalizer receives, the output must always be valid UTF-8 and always a
// member of the finite set of values it is allowed to produce. This is what
// actually bounds cardinality and keeps invalid bytes out of the exporter.
func TestPathNormalizer_AlwaysBounded(t *testing.T) {
	normalize := PathNormalizer()

	expected := map[string]bool{
		"/v1/cards/{id}":          true,
		"/v1/cards/{id}/legality": true,
		"/v1/sets/{id}":           true,
		"/v1/keywords/{name}":     true,
		"/other":                  true,
	}
	for route := range staticRoutes {
		expected[route] = true
	}

	hostile := []string{
		"",
		"/",
		"/v1/cards/\xff\xfe",
		"/v1/cards/\xff\xfe/legality",
		string([]byte{0x00, 0x01, 0xff, 0xfe, 0xfd}),
		"/" + strings.Repeat("a", 100_000),
		"/v1/cards/" + strings.Repeat("\U0001F4A5", 1000),
		"/../../etc/passwd",
		"/wp-admin/setup.php",
		"/.env",
		"/v1/cards",
		"/v1/cards/",
		"/\u202e/reversed",       // RTL override, valid UTF-8 but not allowlisted
		"/v1/cards/\xed\xa0\x80", // CESU-8 encoded surrogate half, invalid UTF-8
	}

	for _, in := range hostile {
		got := normalize(in)
		if !utf8.ValidString(got) {
			t.Errorf("PathNormalizer()(%q) = %q, not valid UTF-8", in, got)
		}
		if !expected[got] {
			t.Errorf("PathNormalizer()(%q) = %q, not a member of the finite expected set", in, got)
		}
	}
}

// TestMetricsMiddleware_UTF8Guard exercises the defense-in-depth guard in
// MetricsMiddleware directly: even when the injected normalizer regresses and
// hands back invalid UTF-8 (simulating a future bug in PathNormalizer or
// mcpPathNormalizer), the http.route attribute actually recorded on the OTel
// counter must be "/invalid", never the poisoned value.
func TestMetricsMiddleware_UTF8Guard(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(prev)

	m := NewMetrics("test-utf8-guard")

	hostileNormalizer := func(string) string { return "/\xff\xfe" }

	handler := m.MetricsMiddleware(hostileNormalizer)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	route, ok := findHTTPRoute(rm, "http.server.request.total")
	if !ok {
		t.Fatal("http.server.request.total metric with http.route attribute not found")
	}
	if route != "/invalid" {
		t.Errorf("http.route = %q, want /invalid", route)
	}
	if !utf8.ValidString(route) {
		t.Errorf("http.route = %q is not valid UTF-8", route)
	}
}

// findHTTPRoute digs the http.route attribute value out of the first data
// point of the named int64 Sum metric.
func findHTTPRoute(rm metricdata.ResourceMetrics, metricName string) (string, bool) {
	for _, sm := range rm.ScopeMetrics {
		for _, met := range sm.Metrics {
			if met.Name != metricName {
				continue
			}
			sum, ok := met.Data.(metricdata.Sum[int64])
			if !ok || len(sum.DataPoints) == 0 {
				return "", false
			}
			val, ok := sum.DataPoints[0].Attributes.Value(attribute.Key("http.route"))
			if !ok {
				return "", false
			}
			return val.AsString(), true
		}
	}
	return "", false
}
