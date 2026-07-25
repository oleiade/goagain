package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oleiade/goagain/internal/observability"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// TestRecoverResponseWriter_Flush covers the mcp-go SSE regression: mcp-go
// type-asserts w.(http.Flusher) directly, so recoverResponseWriter must
// implement Flush and forward it to the underlying ResponseWriter. It also
// checks the committed-flag bookkeeping: a flush before any WriteHeader call
// must mark wroteHeader, the same way Write does, so Recover never writes a
// second header over a response a streaming handler already flushed.
func TestRecoverResponseWriter_Flush(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &recoverResponseWriter{ResponseWriter: rec}

	var _ http.Flusher = rw
	rw.Flush()

	if !rec.Flushed {
		t.Error("Flush did not reach the underlying ResponseRecorder")
	}
	if !rw.wroteHeader {
		t.Error("wroteHeader should be true after Flush, so Recover does not double-write a header after a stream flush")
	}
}

// TestMCPMiddlewareChain_FlushSupported reproduces the production failure
// mode: mcp-go's streamable HTTP handler asserts w.(http.Flusher) directly on
// the ResponseWriter it is handed, and refuses SSE ("Streaming unsupported")
// if that fails. This builds the exact middleware chain cmd/mcp/main.go
// assembles (SecurityHeaders, RateLimit, Metrics, Logging, RequestID,
// Recover, otelhttp) and asserts the innermost handler sees a ResponseWriter
// that satisfies http.Flusher despite being wrapped by all three wrappers.
func TestMCPMiddlewareChain_FlushSupported(t *testing.T) {
	logger := observability.DiscardLogger()
	metrics := observability.NewMetrics("test-mcp-chain")
	clientIP := func(r *http.Request) string { return r.RemoteAddr }

	var sawFlusher bool
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		sawFlusher = ok
		if !ok {
			// This is exactly the mcp-go failure mode: it does not fall back
			// to http.ResponseController, so a failed assertion here means
			// "Streaming unsupported" in production.
			return
		}
		flusher.Flush()
		w.WriteHeader(http.StatusOK)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var handler http.Handler = inner
	handler = SecurityHeaders()(handler)
	handler = RateLimit(ctx, RateLimitConfig{RPS: 100}, metrics, clientIP)(handler)
	handler = metrics.MetricsMiddleware(func(p string) string { return p })(handler)
	handler = observability.LoggingMiddleware(logger, clientIP)(handler)
	handler = observability.RequestIDMiddleware(handler)
	handler = Recover(logger)(handler)
	handler = otelhttp.NewHandler(handler, "test-mcp-chain")

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Accept", "text/event-stream")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !sawFlusher {
		t.Fatal("innermost handler's ResponseWriter does not implement http.Flusher through the full MCP middleware chain")
	}
	if !rec.Flushed {
		t.Error("Flush did not propagate to the outermost ResponseRecorder through the full middleware chain")
	}
}
