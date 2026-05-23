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

func (rw *recoverResponseWriter) WriteHeader(code int) {
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *recoverResponseWriter) Write(b []byte) (int, error) {
	rw.wroteHeader = true
	return rw.ResponseWriter.Write(b)
}

func (rw *recoverResponseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}
