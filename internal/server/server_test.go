package server_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/oleiade/goagain/internal/observability"
	"github.com/oleiade/goagain/internal/server"
)

// TestServer_ShutsDownOnContextCancel covers the High #1 + Medium #10 lifecycle
// refactor. Pre-fix Server.Run installed its own signal handler and called os.Exit
// on errors; after the fix it blocks on the caller's context and returns nil on a
// clean ctx-driven shutdown.
func TestServer_ShutsDownOnContextCancel(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Port 0 lets the kernel assign an ephemeral port; we don't actually hit the server.
	srv := server.New("test", 0, observability.DiscardLogger(), handler)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- srv.Run(ctx)
	}()

	// Give Run a moment to call ListenAndServe before we cancel — otherwise we may
	// race ahead of the goroutine and the test becomes flaky.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Server.Run returned error after clean shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Server.Run did not return within 5s after ctx cancel")
	}
}
