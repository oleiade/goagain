package observability

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
)

// contextKey is an unexported type used for context keys to avoid collisions.
type contextKey string

const (
	// RequestIDKey is the context key for the request ID.
	RequestIDKey contextKey = "request_id"
)

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
// If the request already has an X-Request-ID header, it uses that value.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
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
