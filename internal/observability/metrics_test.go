package observability

import (
	"sync"
	"testing"
)

// TestMetrics_RaceFreeStats exercises the concurrent path that triggered the High
// finding in the review: async-gauge callbacks read mutable counter and map fields
// from the SDK's collection goroutine, while SetDataStats / SetIndexStats write them
// from the loader goroutine. Pre-fix the scalars were plain int64 (data race) and
// SetIndexStats mutated the map in place (concurrent map write / iteration would
// panic). With the atomic.Int64 / atomic.Pointer fix this test runs cleanly under
// `go test -race`.
func TestMetrics_RaceFreeStats(t *testing.T) {
	m := NewMetrics("test-race")

	const iterations = 2000
	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Simulate the gauge callback's reads. The real callback runs on an OTel SDK
	// goroutine; we hit the same atomic getters directly.
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = m.dataCardsTotal.Load()
			_ = m.dataSetsTotal.Load()
			_ = m.dataKeywordsTotal.Load()
			_ = m.dataAbilitiesTotal.Load()
			if snap := m.dataIndexEntries.Load(); snap != nil {
				for k, v := range *snap {
					_, _ = k, v
				}
			}
		}
	})

	for i := range iterations {
		m.SetDataStats(map[string]int{
			"cards":     i,
			"sets":      i,
			"keywords":  i,
			"abilities": i,
		})
		m.SetIndexStats(map[string]int{
			"cards_by_id":   i,
			"cards_by_name": i,
			"cards_by_type": i,
		})
	}

	close(stop)
	wg.Wait()

	// Sanity check: last write is visible.
	if got := m.dataCardsTotal.Load(); got != int64(iterations-1) {
		t.Errorf("dataCardsTotal = %d, want %d", got, iterations-1)
	}
	if snap := m.dataIndexEntries.Load(); snap == nil || (*snap)["cards_by_id"] != int64(iterations-1) {
		t.Errorf("dataIndexEntries missing latest write, snap=%v", snap)
	}
}
