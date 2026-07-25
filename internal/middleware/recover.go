package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recover returns a middleware that catches panics from downstream handlers,
// logs them with the request_id (read from the response header set upstream
// by RequestIDMiddleware), and writes a generic 500 if the inner handler has
// not already committed a response. Without this, otelhttp does not recover
// panics and a panicking handler can corrupt the HTTP/1.1 connection.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := &recoverResponseWriter{ResponseWriter: w}
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				requestID := w.Header().Get("X-Request-ID")
				logger.LogAttrs(r.Context(), slog.LevelError, "HTTP handler panic",
					slog.Any("panic", rec),
					slog.String("request_id", requestID),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.String("stack", string(debug.Stack())),
				)
				if !rw.wroteHeader {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":"internal server error"}`))
				}
			}()
			next.ServeHTTP(rw, r)
		})
	}
}

// recoverResponseWriter tracks whether the inner handler has committed a
// response, so Recover knows whether it is safe to write its own 500 body.
type recoverResponseWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

var _ http.Flusher = (*recoverResponseWriter)(nil)

func (rw *recoverResponseWriter) WriteHeader(code int) {
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *recoverResponseWriter) Write(b []byte) (int, error) {
	rw.wroteHeader = true
	return rw.ResponseWriter.Write(b)
}

// Flush implements http.Flusher so streaming handlers work through this
// wrapper. mcp-go type-asserts w.(http.Flusher) directly and refuses SSE
// ("Streaming unsupported") when the assertion fails; Unwrap alone is not
// enough because mcp-go does not use http.ResponseController. A flush
// commits an implicit 200 just like Write does, so mark wroteHeader the same
// way Write does, without calling the underlying WriteHeader ourselves (the
// underlying Flush issues its own implicit 200).
func (rw *recoverResponseWriter) Flush() {
	rw.wroteHeader = true
	_ = http.NewResponseController(rw.ResponseWriter).Flush()
}

func (rw *recoverResponseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}
