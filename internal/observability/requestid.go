package observability

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// contextKey is an unexported type used for context keys to avoid collisions.
type contextKey string

const (
	// RequestIDKey is the context key for the request ID.
	RequestIDKey contextKey = "request_id"
)

// requestIDCounter provides additional uniqueness within the same millisecond.
var requestIDCounter uint32

// GenerateRequestID generates a ULID-like request ID.
// Format: 10 chars timestamp (ms) + 6 chars random/counter
func GenerateRequestID() string {
	// Timestamp in milliseconds (base32 encoded)
	ts := uint64(time.Now().UnixMilli())

	// Counter + random for uniqueness
	counter := atomic.AddUint32(&requestIDCounter, 1)
	var randomBytes [2]byte
	_, _ = rand.Read(randomBytes[:])
	randomPart := uint16(randomBytes[0])<<8 | uint16(randomBytes[1])

	// Combine counter and random
	suffix := uint32(counter&0xFFFF)<<16 | uint32(randomPart)

	// Base32 Crockford encoding (similar to ULID)
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	result := make([]byte, 16)

	// Encode timestamp (10 chars)
	for i := 9; i >= 0; i-- {
		result[i] = alphabet[ts&0x1F]
		ts >>= 5
	}

	// Encode suffix (6 chars)
	for i := 15; i >= 10; i-- {
		result[i] = alphabet[suffix&0x1F]
		suffix >>= 5
	}

	return string(result)
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
