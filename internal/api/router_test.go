package api_test

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/oleiade/goagain/internal/api"
	"github.com/oleiade/goagain/internal/data"
	"github.com/oleiade/goagain/internal/observability"
)

// TestRateLimit_GoroutineExitsOnContextCancel exercises the High #2 fix. Pre-fix
// NewRouter spawned an unstoppable `for { time.Sleep(...) ... }` reaper goroutine;
// after the fix it selects on ctx.Done() and exits when the parent context is
// cancelled.
func TestRateLimit_GoroutineExitsOnContextCancel(t *testing.T) {
	store, err := data.NewStore(nil)
	if err != nil {
		t.Fatalf("data.NewStore: %v", err)
	}

	// Baseline: measure after a GC to flush any test-runner transients.
	runtime.GC()
	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	_ = api.NewRouter(ctx, store, observability.DiscardLogger(), nil, observability.Config{})

	// Give the reaper a moment to spin up.
	time.Sleep(50 * time.Millisecond)
	spawned := runtime.NumGoroutine()
	if spawned <= before {
		// Either Go scheduled it on the same thread or we measured before it got going.
		// Not a failure, but log so a regression where the goroutine never starts
		// would at least be visible.
		t.Logf("goroutine count did not increase after NewRouter: before=%d after=%d", before, spawned)
	}

	cancel()

	// The select inside the reaper observes ctx.Done() immediately.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runtime.Gosched()
		if runtime.NumGoroutine() <= before {
			return // success: reaper exited
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("reaper goroutine did not exit after ctx cancel: before=%d after=%d", before, runtime.NumGoroutine())
}
