// Package middleware holds HTTP middleware shared between the REST API and
// the MCP HTTP server: rate-limit, panic-recover, security headers. Anything
// API-specific (CORS policy, OpenAPI bundling) stays in internal/api.
package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/oleiade/goagain/internal/observability"
	"golang.org/x/time/rate"
)

// MaxRateLimitClients caps the per-IP map. See RateLimit for the rationale.
const MaxRateLimitClients = 50_000

// RateLimitConfig configures the two-tier rate limiter. RPS sets the per-IP
// allowance; the global tier is fixed at RPS*100 (burst RPS*200), which absorbs
// rotating-IP floods so the per-IP map cannot grow without bound.
type RateLimitConfig struct {
	RPS int
}

// RateLimit returns a middleware that enforces a global rate limit followed
// by a per-IP token bucket. clientIP must be the project's trusted-proxy-aware
// extractor; passing nil falls back to RemoteAddr. The middleware spawns a
// background reaper goroutine that evicts entries idle >=5min; the goroutine
// exits when ctx is cancelled.
func RateLimit(ctx context.Context, cfg RateLimitConfig, metrics *observability.Metrics, clientIP func(*http.Request) string) func(http.Handler) http.Handler {
	if clientIP == nil {
		clientIP = func(r *http.Request) string { return r.RemoteAddr }
	}

	type client struct {
		limiter  *rate.Limiter
		lastSeen time.Time
	}

	var (
		mu      sync.Mutex
		clients = make(map[string]*client)
	)

	// Global tier: per-IP buckets alone are useless if an attacker rotates IPs
	// faster than the reaper runs. A second-tier global bucket absorbs that
	// pattern without forcing the per-IP map to grow.
	globalLimiter := rate.NewLimiter(rate.Limit(cfg.RPS*100), cfg.RPS*200)

	writeLimitExceeded := func(w http.ResponseWriter) {
		if metrics != nil {
			metrics.RecordRateLimitRejection()
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "rate limit exceeded",
		})
	}

	// Reap idle entries until the parent context is cancelled.
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				mu.Lock()
				for ip, c := range clients {
					if time.Since(c.lastSeen) > 5*time.Minute {
						delete(clients, ip)
					}
				}
				mu.Unlock()
			}
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Global tier first: cheap, and rejections here never grow the map.
			if !globalLimiter.Allow() {
				writeLimitExceeded(w)
				return
			}

			ip := clientIP(r)

			mu.Lock()
			c, found := clients[ip]
			if !found {
				// Cap enforcement: when the map is full, evict the entry with
				// the oldest lastSeen. The O(n) scan only fires on overflow,
				// by which point the global limiter is already throttling
				// and the scan is rare.
				if len(clients) >= MaxRateLimitClients {
					var (
						oldestIP   string
						oldestSeen time.Time
					)
					for k, v := range clients {
						if oldestIP == "" || v.lastSeen.Before(oldestSeen) {
							oldestIP = k
							oldestSeen = v.lastSeen
						}
					}
					delete(clients, oldestIP)
				}
				c = &client{
					limiter: rate.NewLimiter(rate.Limit(cfg.RPS), cfg.RPS*2),
				}
				clients[ip] = c
			}
			c.lastSeen = time.Now()
			mu.Unlock()

			if !c.limiter.Allow() {
				writeLimitExceeded(w)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
