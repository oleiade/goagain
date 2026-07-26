package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/oleiade/goagain/internal/data"
	"github.com/oleiade/goagain/internal/domain"
	"github.com/oleiade/goagain/internal/observability"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// --- Helper-function tests ---

func TestGetStringArg(t *testing.T) {
	cases := []struct {
		name string
		args any
		key  string
		want string
	}{
		{"present string", map[string]any{"k": "v"}, "k", "v"},
		{"missing key", map[string]any{"x": "v"}, "k", ""},
		{"wrong type (number)", map[string]any{"k": 42}, "k", ""},
		{"nil args", nil, "k", ""},
		{"non-map args", "scalar", "k", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := getStringArg(tc.args, tc.key); got != tc.want {
				t.Errorf("getStringArg(%v, %q) = %q, want %q", tc.args, tc.key, got, tc.want)
			}
		})
	}
}

func TestGetIntArg(t *testing.T) {
	cases := []struct {
		name       string
		args       any
		key        string
		defaultVal int
		want       int
	}{
		// JSON unmarshals numeric values as float64; this is the most important case.
		{"float64 from JSON", map[string]any{"limit": float64(20)}, "limit", 5, 20},
		{"native int", map[string]any{"limit": 30}, "limit", 5, 30},
		{"missing key uses default", map[string]any{}, "limit", 5, 5},
		{"wrong type uses default", map[string]any{"limit": "20"}, "limit", 5, 5},
		{"nil args uses default", nil, "limit", 7, 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := getIntArg(tc.args, tc.key, tc.defaultVal); got != tc.want {
				t.Errorf("getIntArg = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestGetBoolArg(t *testing.T) {
	cases := []struct {
		name string
		args any
		key  string
		want bool
	}{
		{"true", map[string]any{"k": true}, "k", true},
		{"false", map[string]any{"k": false}, "k", false},
		{"missing", map[string]any{}, "k", false},
		{"wrong type", map[string]any{"k": "true"}, "k", false},
		{"nil", nil, "k", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := getBoolArg(tc.args, tc.key); got != tc.want {
				t.Errorf("getBoolArg = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFormatJSON(t *testing.T) {
	out := formatJSON(map[string]any{"a": 1, "b": "two"})
	var round map[string]any
	if err := json.Unmarshal([]byte(out), &round); err != nil {
		t.Fatalf("formatJSON produced invalid JSON: %v\n%s", err, out)
	}
	if round["b"] != "two" {
		t.Errorf("roundtrip mismatch: %v", round)
	}
}

func TestFormatCardSummary_OmitsEmpty(t *testing.T) {
	card := &domain.Card{UniqueID: "id1", Name: "Test", TypeText: "Action"}
	out := formatCardSummary(card)

	// Required fields present
	for _, k := range []string{"unique_id", "name", "type_text"} {
		if _, ok := out[k]; !ok {
			t.Errorf("missing required key %q", k)
		}
	}
	// Empty-string optional fields are dropped
	for _, k := range []string{"pitch", "cost", "power", "defense"} {
		if _, ok := out[k]; ok {
			t.Errorf("unexpected key %q for zero-value card", k)
		}
	}

	// Populated optional fields show up
	card.Pitch, card.Cost = "1", "2"
	out = formatCardSummary(card)
	if out["pitch"] != "1" || out["cost"] != "2" {
		t.Errorf("expected pitch=1 cost=2; got %v", out)
	}
}

// --- recordResultCount integration tests ---

// TestRecordResultCount_NoRecorderIsNoOp documents that recordResultCount
// silently does nothing outside an instrumentTool wrapper. Handlers must
// be safe to call from tests / direct invocation.
func TestRecordResultCount_NoRecorderIsNoOp(t *testing.T) {
	// Should not panic; nothing observable to assert.
	recordResultCount(context.Background(), 42)
}

func TestRecordResultCount_WritesToRecorder(t *testing.T) {
	rec := &resultRecorder{}
	ctx := context.WithValue(context.Background(), resultCountCtxKey{}, rec)
	recordResultCount(ctx, 17)
	if rec.count != 17 {
		t.Errorf("recorder count = %d, want 17", rec.count)
	}
}

// capturingHandler stores slog.Records for later inspection. It's safe for
// concurrent use because the OTel slog bridge sometimes flushes async.
type capturingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(string) slog.Handler      { return h }

// attrInt returns the int64 value of an attribute by key, or -1 if absent.
func attrInt(r slog.Record, key string) int64 {
	got := int64(-1)
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			got = a.Value.Int64()
			return false
		}
		return true
	})
	return got
}

func TestInstrumentTool_PropagatesRecordedResultCount(t *testing.T) {
	store, err := data.NewStore(nil)
	if err != nil {
		t.Fatalf("data.NewStore: %v", err)
	}
	cap := &capturingHandler{}
	logger := slog.New(cap)

	s := NewServer(store, logger, nil)

	handler := func(ctx context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		recordResultCount(ctx, 42)
		return mcpgo.NewToolResultText("ok"), nil
	}

	wrapped := s.instrumentTool("test_tool", handler)
	if _, err := wrapped(context.Background(), mcpgo.CallToolRequest{}); err != nil {
		t.Fatalf("wrapped handler returned error: %v", err)
	}

	cap.mu.Lock()
	defer cap.mu.Unlock()
	if len(cap.records) == 0 {
		t.Fatalf("expected at least one log record from instrumentTool")
	}
	found := false
	for _, r := range cap.records {
		if attrInt(r, "result_count") == 42 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no log record with result_count=42; saw %d records", len(cap.records))
	}
}

func TestInstrumentTool_FallsBackToOneForSingleResult(t *testing.T) {
	// Single-result handlers (get_card, get_keyword, ...) don't call
	// recordResultCount. The wrapper should infer 1 from a non-error result.
	store, err := data.NewStore(nil)
	if err != nil {
		t.Fatalf("data.NewStore: %v", err)
	}
	cap := &capturingHandler{}
	s := NewServer(store, slog.New(cap), nil)

	handler := func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return mcpgo.NewToolResultText("ok"), nil // no recordResultCount
	}
	if _, err := s.instrumentTool("test_tool", handler)(context.Background(), mcpgo.CallToolRequest{}); err != nil {
		t.Fatalf("wrapped handler returned error: %v", err)
	}

	cap.mu.Lock()
	defer cap.mu.Unlock()
	found := false
	for _, r := range cap.records {
		if attrInt(r, "result_count") == 1 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected result_count=1 fallback; saw %d records", len(cap.records))
	}
}

func TestInstrumentTool_RecordsZeroOnError(t *testing.T) {
	store, err := data.NewStore(nil)
	if err != nil {
		t.Fatalf("data.NewStore: %v", err)
	}
	cap := &capturingHandler{}
	s := NewServer(store, slog.New(cap), nil)

	handler := func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return nil, errors.New("boom")
	}
	if _, err := s.instrumentTool("test_tool", handler)(context.Background(), mcpgo.CallToolRequest{}); err == nil {
		t.Fatalf("expected error from handler, got nil")
	}

	cap.mu.Lock()
	defer cap.mu.Unlock()
	found := false
	for _, r := range cap.records {
		if attrInt(r, "result_count") == 0 && r.Level == slog.LevelError {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error log with result_count=0; saw %d records", len(cap.records))
	}
}

// --- Session lifecycle metrics ---

// fakeSession is a minimal mcpserver.ClientSession implementation, just
// enough to drive RegisterSession/UnregisterSession in a test without
// standing up a real stdio or HTTP transport.
type fakeSession struct {
	id string
}

func (f *fakeSession) SessionID() string { return f.id }
func (f *fakeSession) NotificationChannel() chan<- mcpgo.JSONRPCNotification {
	return make(chan mcpgo.JSONRPCNotification, 1)
}
func (f *fakeSession) Initialize()       {}
func (f *fakeSession) Initialized() bool { return true }

// sumMetric returns the summed int64 data-point values for the named Sum
// metric across a collected snapshot, or 0 if the metric wasn't recorded.
func sumMetric(rm metricdata.ResourceMetrics, name string) int64 {
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, met := range sm.Metrics {
			if met.Name != name {
				continue
			}
			sum, ok := met.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
		}
	}
	return total
}

// TestNewServer_RecordsSessionLifecycle verifies the mcp-go OnRegisterSession
// / OnUnregisterSession hooks wired in NewServer actually drive
// mcp.sessions.total and mcp.sessions.active, so those instruments stop
// being permanently empty in Prometheus.
func TestNewServer_RecordsSessionLifecycle(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(prev)

	metrics := observability.NewMetrics("test-mcp-sessions")

	store, err := data.NewStore(nil)
	if err != nil {
		t.Fatalf("data.NewStore: %v", err)
	}
	cap := &capturingHandler{}
	s := NewServer(store, slog.New(cap), metrics)

	session := &fakeSession{id: "sess-1"}
	ctx := context.Background()

	if err := s.MCPServer().RegisterSession(ctx, session); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got := sumMetric(rm, "mcp.sessions.total"); got != 1 {
		t.Errorf("mcp.sessions.total after register = %d, want 1", got)
	}
	if got := sumMetric(rm, "mcp.sessions.active"); got != 1 {
		t.Errorf("mcp.sessions.active after register = %d, want 1", got)
	}

	s.MCPServer().UnregisterSession(ctx, session.SessionID())

	rm = metricdata.ResourceMetrics{}
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got := sumMetric(rm, "mcp.sessions.total"); got != 1 {
		t.Errorf("mcp.sessions.total after unregister = %d, want 1 (unchanged)", got)
	}
	if got := sumMetric(rm, "mcp.sessions.active"); got != 0 {
		t.Errorf("mcp.sessions.active after unregister = %d, want 0", got)
	}
}

// TestNewServer_NilMetricsSkipsHooks verifies NewServer doesn't panic or
// register hooks when metrics is nil (the stdio-mode / metrics-disabled
// path), mirroring the nil-metrics guard used everywhere else in this
// package.
func TestNewServer_NilMetricsSkipsHooks(t *testing.T) {
	store, err := data.NewStore(nil)
	if err != nil {
		t.Fatalf("data.NewStore: %v", err)
	}
	cap := &capturingHandler{}
	s := NewServer(store, slog.New(cap), nil)

	session := &fakeSession{id: "sess-nil"}
	ctx := context.Background()

	if err := s.MCPServer().RegisterSession(ctx, session); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}
	s.MCPServer().UnregisterSession(ctx, session.SessionID())
	// No metrics wired: nothing to assert beyond "did not panic".
}

var _ mcpserver.ClientSession = (*fakeSession)(nil)
