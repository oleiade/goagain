package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestGenerateRequestID is a smoke test for the Low #15 fix that replaced the
// bespoke ULID-ish encoder with uuid.NewString.
func TestGenerateRequestID(t *testing.T) {
	id := GenerateRequestID()
	if _, err := uuid.Parse(id); err != nil {
		t.Fatalf("GenerateRequestID returned %q, not a valid UUID: %v", id, err)
	}
	if id2 := GenerateRequestID(); id == id2 {
		t.Errorf("two consecutive IDs collided: %q", id)
	}
}

// TestRequestIDMiddleware_RejectsUnsafeHeaders covers the log-injection /
// log-amplification vector closed by validating X-Request-ID at the boundary.
// A rejected value MUST be silently replaced with a fresh UUID — never logged,
// never echoed in the response header — otherwise the validation gives the
// attacker exactly the surface it was added to protect.
func TestRequestIDMiddleware_RejectsUnsafeHeaders(t *testing.T) {
	cases := []struct {
		name   string
		header string
	}{
		{"contains newline", "abc\ndef"},
		{"contains carriage return", "abc\rdef"},
		{"contains ANSI escape", "abc\x1b[31mred"},
		{"contains space", "ab cd"},
		{"contains quote", "ab\"cd"},
		{"contains semicolon", "ab;cd"},
		{"over length", strings.Repeat("a", maxRequestIDLen+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set("X-Request-ID", tc.header)
			rr := httptest.NewRecorder()
			var seen string
			RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen = RequestIDFromContext(r.Context())
			})).ServeHTTP(rr, r)

			if seen == tc.header {
				t.Fatalf("rejected header %q reached handler context — must be replaced", tc.header)
			}
			if got := rr.Header().Get("X-Request-ID"); got == tc.header {
				t.Fatalf("rejected header %q was echoed in response — must be replaced", tc.header)
			}
			if _, err := uuid.Parse(seen); err != nil {
				t.Fatalf("replacement ID %q is not a UUID: %v", seen, err)
			}
		})
	}
}

// TestRequestIDMiddleware_AcceptsCleanHeader confirms the validation does not
// reject the documented format (caller correlation IDs commonly look like
// `service.req-1234`).
func TestRequestIDMiddleware_AcceptsCleanHeader(t *testing.T) {
	const id = "service.req-1234_v2"
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Request-ID", id)
	rr := httptest.NewRecorder()
	var seen string
	RequestIDMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = RequestIDFromContext(r.Context())
	})).ServeHTTP(rr, r)
	if seen != id {
		t.Fatalf("clean header dropped: want %q got %q", id, seen)
	}
	if got := rr.Header().Get("X-Request-ID"); got != id {
		t.Fatalf("response header dropped: want %q got %q", id, got)
	}
}
