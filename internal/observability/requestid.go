package observability

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	"github.com/google/uuid"
)

// contextKey is an unexported type used for context keys to avoid collisions.
type contextKey string

const (
	// RequestIDKey is the context key for the request ID.
	RequestIDKey contextKey = "request_id"

	// maxRequestIDLen caps client-supplied X-Request-ID values. Without a cap,
	// an attacker can send a 1 MiB header that lands in every log line for
	// that request, amplifying log-volume costs at zero attacker cost.
	maxRequestIDLen = 128
)

// requestIDPattern restricts client-supplied request IDs to characters that
// survive any log format (JSON, text, syslog) without escaping. Without this,
// a client sending control characters (CR, LF, ANSI escapes) can poison logs
// — slog's text handler emits them verbatim and many shippers split records
// on \n before parsing.
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// GenerateRequestID returns a fresh UUIDv4 string. UUIDs give 122 bits of entropy and
// well-understood collision properties; the previous bespoke ULID-ish encoding was
// truncating both counter and random bits and silently dropping crypto/rand errors.
func GenerateRequestID() string {
	return uuid.NewString()
}

// RequestIDFromContext extracts the request ID from context.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(RequestIDKey).(string); ok {
		return id
	}
	return ""
}

// ContextWithRequestID returns a new context with the request ID set.
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey, requestID)
}

// RequestIDMiddleware adds a unique request ID to each request's context.
// A client-supplied X-Request-ID is honoured only if it passes length and
// charset validation; otherwise it is silently discarded and replaced with a
// fresh UUIDv4. The rejected value is never logged or echoed (doing so would
// re-introduce the log-injection vector).
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" || len(requestID) > maxRequestIDLen || !requestIDPattern.MatchString(requestID) {
			requestID = GenerateRequestID()
		}

		// Add request ID to response header
		w.Header().Set("X-Request-ID", requestID)

		// Add to context
		ctx := ContextWithRequestID(r.Context(), requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDString returns a formatted request ID string for logging.
func RequestIDString(ctx context.Context) string {
	id := RequestIDFromContext(ctx)
	if id == "" {
		return ""
	}
	return fmt.Sprintf("[%s]", id)
}
