package observability

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// SetupLogger configures the global slog logger based on the provided config.
func SetupLogger(config Config) *slog.Logger {
	var level slog.Level
	switch config.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	var handler slog.Handler
	if config.LogFormat == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	// Add service name as a default attribute
	logger := slog.New(handler).With("service", config.ServiceName)
	slog.SetDefault(logger)

	return logger
}

// LoggingMiddleware logs HTTP requests with structured logging.
func LoggingMiddleware(logger *slog.Logger, getClientIP func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap response writer to capture status and size
			wrapped := &responseWriterWrapper{
				ResponseWriter: w,
				status:         http.StatusOK,
			}

			next.ServeHTTP(wrapped, r)

			duration := time.Since(start)
			clientIP := ""
			if getClientIP != nil {
				clientIP = getClientIP(r)
			} else {
				clientIP = defaultGetClientIP(r)
			}

			// Build log attributes
			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", wrapped.status),
				slog.Float64("duration_ms", float64(duration.Microseconds())/1000.0),
				slog.Int64("response_size", wrapped.size),
				slog.String("client_ip", clientIP),
			}

			// Add request ID if present
			if requestID := RequestIDFromContext(r.Context()); requestID != "" {
				attrs = append(attrs, slog.String("request_id", requestID))
			}

			// Add query string if present
			if r.URL.RawQuery != "" {
				attrs = append(attrs, slog.String("query", r.URL.RawQuery))
			}

			// Log at appropriate level based on status
			level := slog.LevelInfo
			msg := "HTTP request completed"
			if wrapped.status >= 500 {
				level = slog.LevelError
			} else if wrapped.status >= 400 {
				level = slog.LevelWarn
			}

			logger.LogAttrs(r.Context(), level, msg, attrs...)
		})
	}
}

// responseWriterWrapper wraps http.ResponseWriter to capture status code and response size.
type responseWriterWrapper struct {
	http.ResponseWriter
	status int
	size   int64
}

func (rw *responseWriterWrapper) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriterWrapper) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.size += int64(n)
	return n, err
}

// Unwrap returns the underlying ResponseWriter for middleware that need it.
func (rw *responseWriterWrapper) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// defaultGetClientIP extracts client IP from the request.
func defaultGetClientIP(r *http.Request) string {
	remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return remoteIP
}

// LogToolInvocation logs an MCP tool invocation.
func LogToolInvocation(ctx context.Context, logger *slog.Logger, toolName string, duration time.Duration, resultCount int, err error) {
	attrs := []slog.Attr{
		slog.String("tool_name", toolName),
		slog.Float64("duration_ms", float64(duration.Microseconds())/1000.0),
		slog.Int("result_count", resultCount),
	}

	level := slog.LevelInfo
	msg := "MCP tool invocation completed"

	if err != nil {
		level = slog.LevelError
		msg = "MCP tool invocation failed"
		attrs = append(attrs, slog.String("error", err.Error()))
	}

	logger.LogAttrs(ctx, level, msg, attrs...)
}

// LogStartup logs server startup information.
func LogStartup(logger *slog.Logger, serverType string, addr string, extra ...slog.Attr) {
	attrs := []slog.Attr{
		slog.String("type", serverType),
		slog.String("address", addr),
	}
	attrs = append(attrs, extra...)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "Server starting", attrs...)
}

// LogShutdown logs server shutdown.
func LogShutdown(logger *slog.Logger, serverType string) {
	logger.LogAttrs(context.Background(), slog.LevelInfo, "Server shutting down",
		slog.String("type", serverType))
}

// LogDataLoaded logs data loading statistics.
func LogDataLoaded(logger *slog.Logger, stats map[string]int) {
	attrs := make([]slog.Attr, 0, len(stats))
	for k, v := range stats {
		attrs = append(attrs, slog.Int(k, v))
	}
	logger.LogAttrs(context.Background(), slog.LevelInfo, "Data loaded", attrs...)
}

// GetClientIPFunc returns a function that extracts the client IP considering trusted proxies.
func GetClientIPFunc(trustedProxies []*net.IPNet) func(*http.Request) string {
	return func(r *http.Request) string {
		remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			remoteIP = r.RemoteAddr
		}

		trusted := false
		if len(trustedProxies) > 0 {
			ip := net.ParseIP(remoteIP)
			for _, cidr := range trustedProxies {
				if cidr.Contains(ip) {
					trusted = true
					break
				}
			}
		}

		// Only trust X-Forwarded-For from trusted proxies
		if trusted {
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				if idx := strings.Index(xff, ","); idx != -1 {
					return strings.TrimSpace(xff[:idx])
				}
				return strings.TrimSpace(xff)
			}

			if xri := r.Header.Get("X-Real-IP"); xri != "" {
				return strings.TrimSpace(xri)
			}
		}

		return remoteIP
	}
}
