package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// badPath is the shape that broke production: a request path carrying raw
// invalid UTF-8 bytes, recorded verbatim by otelhttp as a span attribute.
const badPath = "/v1/cards/\xff\xfe"

// recordingExporter captures what the wrapped exporter was actually handed.
type recordingExporter struct {
	spans []trace.ReadOnlySpan
	err   error
}

func (r *recordingExporter) ExportSpans(_ context.Context, spans []trace.ReadOnlySpan) error {
	r.spans = append(r.spans, spans...)
	return r.err
}

func (r *recordingExporter) Shutdown(context.Context) error { return nil }

// newSpan produces a real ReadOnlySpan via the SDK's snapshot helper so the
// test exercises the same type the batcher hands to the exporter.
func newSpan(t *testing.T, name string, attrs ...attribute.KeyValue) trace.ReadOnlySpan {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(rec))
	_, span := tp.Tracer("test").Start(context.Background(), name)
	span.SetAttributes(attrs...)
	span.End()
	ended := rec.Ended()
	if len(ended) != 1 {
		t.Fatalf("expected 1 recorded span, got %d", len(ended))
	}
	return ended[0]
}

func TestSanitizingExporter_ScrubsInvalidUTF8(t *testing.T) {
	rec := &recordingExporter{}
	exp := sanitizingExporter{SpanExporter: rec}

	span := newSpan(t, "HTTP GET",
		attribute.String("url.path", badPath),
		attribute.String("user_agent.original", "curl/\xff"),
		attribute.StringSlice("tags", []string{"ok", "bad\xfe"}),
		attribute.Int("http.response.status_code", 404),
	)

	if err := exp.ExportSpans(context.Background(), []trace.ReadOnlySpan{span}); err != nil {
		t.Fatalf("ExportSpans: %v", err)
	}
	if len(rec.spans) != 1 {
		t.Fatalf("expected 1 exported span, got %d", len(rec.spans))
	}

	got := rec.spans[0]
	if !utf8.ValidString(got.Name()) {
		t.Errorf("span name is not valid UTF-8: %q", got.Name())
	}
	for _, a := range got.Attributes() {
		if !utf8.ValidString(string(a.Key)) {
			t.Errorf("attribute key not valid UTF-8: %q", a.Key)
		}
		switch a.Value.Type() {
		case attribute.STRING:
			if !utf8.ValidString(a.Value.AsString()) {
				t.Errorf("attribute %q value not valid UTF-8: %q", a.Key, a.Value.AsString())
			}
		case attribute.STRINGSLICE:
			for _, v := range a.Value.AsStringSlice() {
				if !utf8.ValidString(v) {
					t.Errorf("attribute %q slice element not valid UTF-8: %q", a.Key, v)
				}
			}
		}
	}

	// The clean prefix of a poisoned value must survive, so the route is still
	// recognisable in Tempo rather than being dropped wholesale.
	var path string
	for _, a := range got.Attributes() {
		if a.Key == "url.path" {
			path = a.Value.AsString()
		}
	}
	if !strings.HasPrefix(path, "/v1/cards/") {
		t.Errorf("expected sanitized path to keep its clean prefix, got %q", path)
	}
	if !strings.Contains(path, invalidUTF8Replacement) {
		t.Errorf("expected replacement char in sanitized path, got %q", path)
	}
}

func TestSanitizingExporter_PassesValidSpansThrough(t *testing.T) {
	rec := &recordingExporter{}
	exp := sanitizingExporter{SpanExporter: rec}

	span := newSpan(t, "HTTP GET", attribute.String("url.path", "/v1/cards"))
	if err := exp.ExportSpans(context.Background(), []trace.ReadOnlySpan{span}); err != nil {
		t.Fatalf("ExportSpans: %v", err)
	}

	// Untouched spans must be forwarded by identity: no wrapper allocated on
	// the hot path.
	if rec.spans[0] != span {
		t.Error("valid span was wrapped; expected it to pass through unchanged")
	}
}

// A poisoned span must not take the clean ones in its batch down with it.
func TestSanitizingExporter_MixedBatchKeepsOrderAndCleanSpans(t *testing.T) {
	rec := &recordingExporter{}
	exp := sanitizingExporter{SpanExporter: rec}

	clean1 := newSpan(t, "clean-1", attribute.String("url.path", "/health"))
	dirty := newSpan(t, "dirty", attribute.String("url.path", badPath))
	clean2 := newSpan(t, "clean-2", attribute.String("url.path", "/v1/sets"))

	if err := exp.ExportSpans(context.Background(), []trace.ReadOnlySpan{clean1, dirty, clean2}); err != nil {
		t.Fatalf("ExportSpans: %v", err)
	}
	if len(rec.spans) != 3 {
		t.Fatalf("expected 3 spans, got %d", len(rec.spans))
	}
	if rec.spans[0] != clean1 || rec.spans[2] != clean2 {
		t.Error("clean spans were not passed through in their original positions")
	}
	if rec.spans[1].Name() != "dirty" {
		t.Errorf("batch order not preserved, got %q at index 1", rec.spans[1].Name())
	}
	for i, s := range rec.spans {
		for _, a := range s.Attributes() {
			if a.Value.Type() == attribute.STRING && !utf8.ValidString(a.Value.AsString()) {
				t.Errorf("span %d still carries invalid UTF-8", i)
			}
		}
	}
}

// The MCP tool path builds span names from client-supplied tool names, so a
// hostile name must be scrubbed too, not just HTTP attributes.
func TestSanitizingExporter_ScrubsSpanName(t *testing.T) {
	rec := &recordingExporter{}
	exp := sanitizingExporter{SpanExporter: rec}

	span := newSpan(t, "mcp.tool.search\xff")
	if err := exp.ExportSpans(context.Background(), []trace.ReadOnlySpan{span}); err != nil {
		t.Fatalf("ExportSpans: %v", err)
	}
	if name := rec.spans[0].Name(); !utf8.ValidString(name) {
		t.Errorf("span name not sanitized: %q", name)
	}
}

func TestToValidUTF8(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"clean ascii", "/v1/cards", "/v1/cards"},
		{"clean unicode", "/cards/Ægir/日本", "/cards/Ægir/日本"},
		{"empty", "", ""},
		{"invalid bytes", "/a/\xff\xfe", "/a/" + invalidUTF8Replacement},
		{"all invalid", "\xff\xfe", invalidUTF8Replacement},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toValidUTF8(tt.in)
			if got != tt.want {
				t.Errorf("toValidUTF8(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("result is not valid UTF-8: %q", got)
			}
		})
	}
}

// End-to-end proof against the real failure: a poisoned span pushed through an
// actual OTLP/HTTP exporter fails protobuf marshalling with the exact error
// seen in production ("string field contains invalid UTF-8"), and the same span
// exports cleanly once wrapped.
func TestSanitizingExporter_FixesRealOTLPMarshalFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	endpoint := strings.TrimPrefix(srv.URL, "http://")
	raw, err := otlptracehttp.New(context.Background(),
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		t.Fatalf("otlptracehttp.New: %v", err)
	}
	defer func() { _ = raw.Shutdown(context.Background()) }()

	poisoned := []trace.ReadOnlySpan{newSpan(t, "HTTP GET", attribute.String("url.path", badPath))}

	// Baseline: the unwrapped exporter must reproduce the production error.
	errRaw := raw.ExportSpans(context.Background(), poisoned)
	if errRaw == nil {
		t.Fatal("expected raw exporter to fail on invalid UTF-8, got nil")
	}
	if !strings.Contains(errRaw.Error(), "invalid UTF-8") {
		t.Fatalf("expected an invalid UTF-8 marshal error, got: %v", errRaw)
	}
	t.Logf("unwrapped exporter failed as in production: %v", errRaw)

	// With the wrapper the identical span exports successfully.
	if err := (sanitizingExporter{SpanExporter: raw}).ExportSpans(context.Background(), poisoned); err != nil {
		t.Fatalf("sanitizing exporter should have succeeded, got: %v", err)
	}
}
