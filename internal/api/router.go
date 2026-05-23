package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oleiade/goagain/internal/data"
	"github.com/oleiade/goagain/internal/observability"
	"golang.org/x/time/rate"
)

//go:embed openapi.yaml
var openAPISpec []byte

//go:embed landing.html
var landingPage []byte

//go:embed landing.md
var landingMarkdown []byte

//go:embed static/tailwind.min.css
var tailwindCSS []byte

// Config holds configuration for the API server.
type Config struct {
	CORSOrigins    []string
	RateLimitRPS   int
	TrustedProxies []*net.IPNet
	APIBaseURL     string
	MCPBaseURL     string
}

// LoadConfig loads configuration from environment variables.
func LoadConfig() Config {
	config := Config{
		CORSOrigins:  []string{"*"},
		RateLimitRPS: 100,
		APIBaseURL:   "https://api.goagain.dev",
		MCPBaseURL:   "https://mcp.goagain.dev",
	}

	if origins := os.Getenv("CORS_ORIGINS"); origins != "" {
		config.CORSOrigins = strings.Split(origins, ",")
		for i := range config.CORSOrigins {
			config.CORSOrigins[i] = strings.TrimSpace(config.CORSOrigins[i])
		}
	}

	if rps := os.Getenv("RATE_LIMIT_RPS"); rps != "" {
		if rate, err := strconv.Atoi(rps); err == nil && rate > 0 {
			config.RateLimitRPS = rate
		}
	}

	if proxies := os.Getenv("TRUSTED_PROXIES"); proxies != "" {
		for cidr := range strings.SplitSeq(proxies, ",") {
			cidr = strings.TrimSpace(cidr)
			_, ipNet, err := net.ParseCIDR(cidr)
			if err != nil {
				sanitized := strings.ReplaceAll(strings.ReplaceAll(cidr, "\n", ""), "\r", "")
				slog.Warn("Invalid CIDR in TRUSTED_PROXIES", slog.String("cidr", sanitized))
				continue
			}
			// A /0 mask trusts every source IP, turning XFF spoofing into a
			// one-line global authentication bypass. Always reject.
			if ones, _ := ipNet.Mask.Size(); ones == 0 {
				slog.Warn("Refusing /0 CIDR in TRUSTED_PROXIES", slog.String("cidr", ipNet.String()))
				continue
			}
			config.TrustedProxies = append(config.TrustedProxies, ipNet)
		}
	}

	if apiURL := os.Getenv("API_BASE_URL"); apiURL != "" {
		config.APIBaseURL = strings.TrimSuffix(apiURL, "/")
	}

	if mcpURL := os.Getenv("MCP_BASE_URL"); mcpURL != "" {
		config.MCPBaseURL = strings.TrimSuffix(mcpURL, "/")
	}

	return config
}

// NewRouter creates a new HTTP router with all API routes registered.
// The provided ctx governs the lifetime of background goroutines (e.g. rate-limit cleanup);
// cancelling it stops them.
func NewRouter(ctx context.Context, store *data.Store, logger *slog.Logger, metrics *observability.Metrics, _ observability.Config) http.Handler {
	config := LoadConfig()

	mux := http.NewServeMux()
	h := NewHandler(store, config.APIBaseURL, config.MCPBaseURL)

	// Root - Landing page / API info (unversioned)
	mux.HandleFunc("GET /", h.Index)

	// Agent discovery endpoints
	mux.HandleFunc("GET /robots.txt", h.RobotsTxt)
	mux.HandleFunc("GET /sitemap.xml", h.SitemapXML)
	mux.HandleFunc("GET /.well-known/api-catalog", h.APICatalog)
	mux.HandleFunc("GET /.well-known/mcp/server-card.json", h.MCPServerCard)
	mux.HandleFunc("GET /.well-known/agent-skills/index.json", h.AgentSkillsIndex)

	// Operational endpoints (unversioned)
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("GET /openapi.yaml", serveOpenAPI)
	mux.HandleFunc("GET /openapi", serveOpenAPI)
	mux.HandleFunc("GET /docs", serveDocs)
	mux.HandleFunc("GET /static/tailwind.min.css", serveTailwindCSS)

	// API v1 endpoints
	mux.HandleFunc("GET /v1/cards", h.ListCards)
	mux.HandleFunc("GET /v1/cards/{id}", h.GetCard)
	mux.HandleFunc("GET /v1/cards/{id}/legality", h.GetCardLegality)
	mux.HandleFunc("GET /v1/sets", h.ListSets)
	mux.HandleFunc("GET /v1/sets/{id}", h.GetSet)
	mux.HandleFunc("GET /v1/keywords", h.ListKeywords)
	mux.HandleFunc("GET /v1/keywords/{name}", h.GetKeyword)
	mux.HandleFunc("GET /v1/abilities", h.ListAbilities)

	// Build middleware chain (applied in reverse order).
	// Single source of truth for client-IP extraction: observability.GetClientIPFunc.
	clientIP := observability.GetClientIPFunc(config.TrustedProxies)

	handler := http.Handler(mux)

	// Security headers sit innermost so every response — including 429 from the
	// rate limiter and 204 from CORS preflight — carries nosniff et al.
	handler = securityHeadersMiddleware(handler)

	// CORS
	handler = corsMiddleware(handler, config)

	// Rate limiting (uses clientIP for trusted-proxy-aware key)
	handler = rateLimitMiddleware(ctx, handler, config, metrics, clientIP)

	// Metrics middleware
	if metrics != nil {
		handler = metrics.MetricsMiddleware(observability.PathNormalizer())(handler)
	}

	// Logging middleware
	handler = observability.LoggingMiddleware(logger, clientIP)(handler)

	// Request ID middleware (sets X-Request-ID on the response writer before any
	// inner handler runs; recoverMiddleware reads it back from that header).
	handler = observability.RequestIDMiddleware(handler)

	// recoverMiddleware sits outermost so a panic anywhere downstream — including
	// in RequestIDMiddleware itself — still produces a clean 500 instead of
	// crashing the goroutine and corrupting the HTTP/1.1 connection.
	handler = recoverMiddleware(handler, logger)

	return handler
}

func serveOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(openAPISpec)
}

// scalarAPIReferenceVersion pins the Scalar API reference bundle loaded by the docs
// page. Bumping this version is a docs-only change but should be done deliberately;
// an unpinned `@latest` from a CDN turns any CDN-side compromise into RCE against
// every viewer of /docs. Last bumped from upstream npm latest.
const scalarAPIReferenceVersion = "1.57.5"

func serveDocs(w http.ResponseWriter, _ *http.Request) {
	// Serve Scalar API documentation
	html := `<!DOCTYPE html>
<html>
<head>
  <title>Flesh and Blood Cards API - Documentation</title>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
</head>
<body>
  <script id="api-reference" data-url="/openapi.yaml"></script>
  <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference@` + scalarAPIReferenceVersion + `"></script>
</body>
</html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

func serveTailwindCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(tailwindCSS)
}

func corsMiddleware(next http.Handler, config Config) http.Handler {
	allowAll := len(config.CORSOrigins) == 1 && config.CORSOrigins[0] == "*"
	allowedOrigins := make(map[string]bool)
	for _, origin := range config.CORSOrigins {
		allowedOrigins[origin] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Vary: Origin is required even when allowAll is true — a CDN keyed
		// purely on URL would otherwise serve a stale ACAO if an operator later
		// tightens CORS_ORIGINS without flushing the cache.
		w.Header().Add("Vary", "Origin")

		if allowAll {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else if allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			// Cache preflight for 10 minutes so each real request does not
			// double-fire as preflight + real, doubling rate-limit cost.
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// securityHeadersMiddleware sets the small set of always-safe response headers.
// We intentionally do NOT set Content-Security-Policy globally: /docs loads
// the Scalar bundle from cdn.jsdelivr.net (version-pinned) and a strict CSP
// would break it. For a JSON-only API there is no XSS sink to protect anyway;
// nosniff is the load-bearing header here.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// recoverMiddleware catches panics from downstream handlers, logs them with
// the request_id (read from the response header set by RequestIDMiddleware),
// and writes a generic 500 if the inner handler did not already commit a
// response. Without this, otelhttp does not recover panics and an HTTP/1.1
// connection can be left in a corrupted state.
func recoverMiddleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &recoverResponseWriter{ResponseWriter: w}
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			requestID := w.Header().Get("X-Request-ID")
			logger.LogAttrs(r.Context(), slog.LevelError, "HTTP handler panic",
				slog.Any("panic", rec),
				slog.String("request_id", requestID),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("stack", string(debug.Stack())),
			)
			if !rw.wroteHeader {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"internal server error"}`))
			}
		}()
		next.ServeHTTP(rw, r)
	})
}

// recoverResponseWriter is a minimal wrapper that tracks whether WriteHeader
// (or Write — which implicitly calls WriteHeader(200)) has been called, so
// recoverMiddleware knows whether it is safe to write its own 500 body.
type recoverResponseWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (rw *recoverResponseWriter) WriteHeader(code int) {
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *recoverResponseWriter) Write(b []byte) (int, error) {
	rw.wroteHeader = true
	return rw.ResponseWriter.Write(b)
}

func (rw *recoverResponseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// rateLimitMiddleware enforces a per-IP token-bucket limit and reaps idle entries in a
// background goroutine. The goroutine exits when ctx is cancelled.
func rateLimitMiddleware(ctx context.Context, next http.Handler, config Config, metrics *observability.Metrics, clientIP func(*http.Request) string) http.Handler {
	type client struct {
		limiter  *rate.Limiter
		lastSeen time.Time
	}

	var (
		mu      sync.Mutex
		clients = make(map[string]*client)
	)

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

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)

		// Resolve/create the limiter under lock, then release it before the rate check.
		// rate.Limiter has its own internal locking; holding mu across Allow() serializes
		// all checks through one mutex.
		mu.Lock()
		c, found := clients[ip]
		if !found {
			c = &client{
				limiter: rate.NewLimiter(rate.Limit(config.RateLimitRPS), config.RateLimitRPS*2),
			}
			clients[ip] = c
		}
		c.lastSeen = time.Now()
		mu.Unlock()

		if !c.limiter.Allow() {
			if metrics != nil {
				metrics.RecordRateLimitRejection()
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "rate limit exceeded",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}
