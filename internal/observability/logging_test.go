package observability

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// failingHandler always reports Enabled and returns sentinel from Handle.
type failingHandler struct{ err error }

func (h *failingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *failingHandler) Handle(context.Context, slog.Record) error {
	return h.err
}
func (h *failingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *failingHandler) WithGroup(string) slog.Handler      { return h }

// TestMultiHandler_AggregatesErrorsAndFansOut covers the Medium #6 finding. The
// pre-fix implementation returned on the first handler error, so a failing OTel
// bridge could prevent stdout logs from being emitted. After the fix every enabled
// handler runs and the errors are joined.
func TestMultiHandler_AggregatesErrorsAndFansOut(t *testing.T) {
	sentinel := errors.New("primary handler failed")
	failing := &failingHandler{err: sentinel}

	var buf bytes.Buffer
	text := slog.NewTextHandler(&buf, nil)

	mh := &multiHandler{handlers: []slog.Handler{failing, text}}

	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "hello world", 0)
	err := mh.Handle(context.Background(), rec)

	if !errors.Is(err, sentinel) {
		t.Errorf("Handle should join the failing handler's error; got %v", err)
	}
	if !strings.Contains(buf.String(), "hello world") {
		t.Errorf("text handler should have received the record despite first handler failing; buf=%q", buf.String())
	}
}

// TestSanitizeLogField asserts the byte-stripping behaviour: control bytes (except
// tab) and 0x7f are dropped; the result is capped at max bytes. This is what
// prevents log injection in the text-handler code path, where slog does not
// escape these bytes before they reach a downstream shipper or terminal.
func TestSanitizeLogField(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"empty", "", 16, ""},
		{"plain ASCII passes through", "/v1/cards", 64, "/v1/cards"},
		{"newline stripped", "abc\ndef", 64, "abcdef"},
		{"CRLF stripped", "abc\r\ndef", 64, "abcdef"},
		{"NUL stripped", "abc\x00def", 64, "abcdef"},
		{"ANSI escape stripped", "abc\x1b[31mred\x1b[0m", 64, "abc[31mred[0m"},
		{"DEL stripped", "abc\x7fdef", 64, "abcdef"},
		{"tab preserved", "abc\tdef", 64, "abc\tdef"},
		{"truncates oversize input", strings.Repeat("a", 100), 16, strings.Repeat("a", 16)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeLogField(tc.in, tc.max)
			if got != tc.want {
				t.Errorf("sanitizeLogField(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
			}
		})
	}
}

// TestLoggingMiddleware_SanitizesPathAndQuery is the integration check: a
// request with control bytes in the URL must not produce a log line that
// contains those bytes. We use the slog text handler because that is where
// log injection actually bites — JSON escapes control bytes already.
func TestLoggingMiddleware_SanitizesPathAndQuery(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	// httptest.NewRequest URL-parses, so build the URL with already-percent-
	// encoded bytes that Go's parser leaves alone in r.URL.RawQuery and
	// inject the bytes directly into the Path field (which is what the
	// middleware logs).
	r := httptest.NewRequest(http.MethodGet, "/v1/cards", nil)
	r.URL.Path = "/v1/cards\x1b[31m"
	r.URL.RawQuery = "q=\nINJECT"

	rr := httptest.NewRecorder()
	LoggingMiddleware(logger, func(*http.Request) string { return "127.0.0.1" })(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, r)

	out := buf.String()
	if strings.ContainsAny(out, "\x00\x01\x1b\x7f") {
		t.Errorf("log line contains control bytes after sanitization: %q", out)
	}
	if strings.Count(out, "\n") != 1 {
		t.Errorf("log line should be exactly one line; got %d newlines in %q", strings.Count(out, "\n"), out)
	}
}

// TestResponseWriterWrapper_Flush covers the mcp-go SSE regression: mcp-go
// type-asserts w.(http.Flusher) directly, so responseWriterWrapper must
// implement Flush and forward it to the underlying ResponseWriter. It also
// checks that flushing before any WriteHeader call leaves status at its
// implicit-200 default, matching what Write already does in that case.
func TestResponseWriterWrapper_Flush(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriterWrapper{ResponseWriter: rec, status: http.StatusOK}

	var _ http.Flusher = rw
	rw.Flush()

	if !rec.Flushed {
		t.Error("Flush did not reach the underlying ResponseRecorder")
	}
	if rw.status != http.StatusOK {
		t.Errorf("status = %d, want %d (implicit 200 before any WriteHeader)", rw.status, http.StatusOK)
	}
}

// Log fields are request-derived and feed the OTLP log batch, where a single
// invalid byte fails the whole batch. Two ways bad UTF-8 gets in: raw hostile
// bytes (which sit above the control-byte filter's range), and the length cap
// splitting a multi-byte rune in otherwise valid input.
func TestSanitizeLogField_AlwaysValidUTF8(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
	}{
		{"hostile raw bytes", "/v1/cards/\xff\xfe", 256},
		{"hostile bytes with control chars", "/a\x00\xff/b", 256},
		{"truncation splits a rune", strings.Repeat("日", 10), 4},
		{"truncation splits an emoji", "aa🎴🎴", 3},
		{"clean ascii untouched", "/v1/cards", 256},
		{"clean unicode untouched", "/cards/日本", 256},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeLogField(tt.in, tt.max)
			if !utf8.ValidString(got) {
				t.Errorf("sanitizeLogField(%q, %d) = %q, which is not valid UTF-8", tt.in, tt.max, got)
			}
		})
	}
}
