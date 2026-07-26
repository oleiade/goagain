package observability

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/log/global"
)

// SetupLogger configures the global slog logger based on the provided config.
// It creates a multi-handler that writes to both stdout and OTel.
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

	// Create stdout handler for local output
	var stdoutHandler slog.Handler
	if config.LogFormat == "text" {
		stdoutHandler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		stdoutHandler = slog.NewJSONHandler(os.Stdout, opts)
	}

	// Create OTel handler that bridges to OTel log provider
	otelHandler := otelslog.NewHandler(config.ServiceName,
		otelslog.WithLoggerProvider(global.GetLoggerProvider()),
	)

	// Combine handlers: logs go to both stdout and OTel
	multiHandler := &multiHandler{
		handlers: []slog.Handler{stdoutHandler, otelHandler},
	}

	// Add service name as a default attribute
	logger := slog.New(multiHandler).With("service", config.ServiceName)
	slog.SetDefault(logger)

	return logger
}

// multiHandler fans out log records to multiple handlers.
type multiHandler struct {
	handlers []slog.Handler
}

func (h *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	// Call every enabled handler regardless of intermediate failures: the whole point of a
	// fan-out handler is durability via independence. Aggregate errors with errors.Join so
	// the caller still sees what went wrong.
	var errs error
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, r.Level) {
			errs = errors.Join(errs, handler.Handle(ctx, r))
		}
	}
	return errs
}

func (h *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithAttrs(attrs)
	}
	return &multiHandler{handlers: handlers}
}

func (h *multiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithGroup(name)
	}
	return &multiHandler{handlers: handlers}
}

// DiscardLogger returns a logger that discards all output.
// Useful for testing or when logging should be suppressed.
func DiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
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

			// Build log attributes. Path and query are attacker-controlled
			// surfaces (control bytes, ANSI escapes, oversize) — sanitize before
			// they reach a slog text handler, where they would render verbatim.
			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("path", sanitizeLogField(r.URL.Path, maxLogFieldLen)),
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
				attrs = append(attrs, slog.String("query", sanitizeLogField(r.URL.RawQuery, maxLogFieldLen)))
			}

			// Log at appropriate level based on status
			level := slog.LevelInfo
			msg := "HTTP request completed"
			if wrapped.status >= 500 {
				level = slog.LevelError
			} else if wrapped.status >= 400 {
				level = slog.LevelWarn
			}

			// /health is polled constantly and a successful check carries no
			// information; skip the log line for it. A failing health check
			// (status >= 400) is information and still gets logged.
			if r.URL.Path == "/health" && wrapped.status < 400 {
				return
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

var _ http.Flusher = (*responseWriterWrapper)(nil)

func (rw *responseWriterWrapper) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriterWrapper) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.size += int64(n)
	return n, err
}

// Flush implements http.Flusher so streaming handlers (e.g. MCP SSE) work
// through this wrapper. mcp-go type-asserts w.(http.Flusher) directly; Unwrap
// alone is not enough since mcp-go does not use http.ResponseController.
// status is already initialized to http.StatusOK by LoggingMiddleware and,
// like Write, a flush never changes it, so no extra bookkeeping is needed
// here to match the implicit-200 semantics of a raw Flush before WriteHeader.
func (rw *responseWriterWrapper) Flush() {
	_ = http.NewResponseController(rw.ResponseWriter).Flush()
}

// Unwrap returns the underlying ResponseWriter for middleware that need it.
func (rw *responseWriterWrapper) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// maxLogFieldLen caps individual log field lengths to defang
// log-volume-amplification attacks (e.g. a 1 MiB URL path that lands in every
// request log line). 2 KiB comfortably covers any legitimate API path or query.
const maxLogFieldLen = 2 << 10

// sanitizeLogField strips bytes that corrupt downstream log shippers and
// terminals — ASCII control characters (CR, LF, NUL, ANSI escape ESC) and
// 0x7F — and caps the result at max bytes. JSON handlers escape these already
// but the text handler does not, and many log pipelines split on \n before
// parsing structured records. Truncation is enforced at byte boundaries, which
// is safe because the dropped bytes were already outside the printable range.
func sanitizeLogField(s string, max int) string {
	if len(s) > max {
		s = s[:max]
	}
	// Fast path: most paths are already clean ASCII without control bytes.
	clean := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 0x20 && c != '\t') || c == 0x7f {
			clean = false
			break
		}
	}
	if clean {
		return toValidUTF8(s)
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 0x20 && c != '\t') || c == 0x7f {
			continue
		}
		b = append(b, c)
	}
	// The control-byte filter above only covers bytes below 0x20, so raw
	// invalid UTF-8 (0xff, 0xfe, ...) survives it, and the length cap can split
	// a multi-byte rune and manufacture invalid UTF-8 from valid input. Either
	// one poisons the OTLP log batch it lands in, so validate last.
	return toValidUTF8(string(b))
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

// maxXFFEntries bounds how many X-Forwarded-For entries we inspect. Real chains
// are short (≤4); an attacker padding the header with thousands of entries would
// otherwise force every middleware (logging, rate-limit, metrics) to re-parse it.
const maxXFFEntries = 10

// GetClientIPFunc returns a function that extracts the client IP considering trusted proxies.
//
// When the request originates from a trusted proxy, the function walks
// X-Forwarded-For right-to-left and returns the first IP that is itself NOT in
// trustedProxies — i.e., the closest hop to the client that the operator did not
// pre-authorise. Leftmost selection is unsafe: it is the client-supplied value
// and an attacker can spoof it freely (`X-Forwarded-For: 1.2.3.4` is appended-to
// by the proxy, never overwritten), which would let a single attacker bypass
// per-IP rate limiting by rotating the spoofed value.
//
// When TRUSTED_PROXIES is unset or the request comes from an untrusted source,
// X-Forwarded-For and X-Real-IP are ignored entirely.
func GetClientIPFunc(trustedProxies []*net.IPNet) func(*http.Request) string {
	return func(r *http.Request) string {
		remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			remoteIP = r.RemoteAddr
		}

		if len(trustedProxies) == 0 {
			return remoteIP
		}
		parsedRemote := net.ParseIP(remoteIP)
		if parsedRemote == nil || !containedIn(parsedRemote, trustedProxies) {
			return remoteIP
		}

		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			entries := strings.Split(xff, ",")
			if len(entries) > maxXFFEntries {
				entries = entries[len(entries)-maxXFFEntries:]
			}
			for i := len(entries) - 1; i >= 0; i-- {
				candidate := parseForwardedAddr(entries[i])
				if candidate == nil {
					continue
				}
				if !containedIn(candidate, trustedProxies) {
					return candidate.String()
				}
			}
		}

		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			if candidate := parseForwardedAddr(xri); candidate != nil {
				return candidate.String()
			}
		}

		return remoteIP
	}
}

// parseForwardedAddr extracts a net.IP from a single XFF / X-Real-IP entry,
// tolerating surrounding whitespace, IPv6 bracket notation, and an optional
// :port suffix that some proxies (HAProxy, AWS ALB) include. Returns nil for
// any unparseable input rather than letting garbage propagate into rate-limit
// keys or logs.
func parseForwardedAddr(s string) net.IP {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	// SplitHostPort handles "[::1]:443" and "1.2.3.4:5678"; fall back to the
	// raw string (and strip any lone surrounding brackets) when there is no port.
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	} else {
		s = strings.TrimPrefix(strings.TrimSuffix(s, "]"), "[")
	}
	return net.ParseIP(s)
}

func containedIn(ip net.IP, cidrs []*net.IPNet) bool {
	for _, cidr := range cidrs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}
