package observability

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
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
