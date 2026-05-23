package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
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
		for _, cidr := range strings.Split(proxies, ",") {
			cidr = strings.TrimSpace(cidr)
			_, ipNet, err := net.ParseCIDR(cidr)
			if err != nil {
				sanitized := strings.ReplaceAll(strings.ReplaceAll(cidr, "\n", ""), "\r", "")
				slog.Warn("Invalid CIDR in TRUSTED_PROXIES", slog.String("cidr", sanitized))
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

	// Apply CORS first (innermost)
	handler = corsMiddleware(handler, config)

	// Rate limiting (uses clientIP for trusted-proxy-aware key)
	handler = rateLimitMiddleware(ctx, handler, config, metrics, clientIP)

	// Metrics middleware
	if metrics != nil {
		handler = metrics.MetricsMiddleware(observability.PathNormalizer())(handler)
	}

	// Logging middleware
	handler = observability.LoggingMiddleware(logger, clientIP)(handler)

	// Request ID middleware (outermost)
	handler = observability.RequestIDMiddleware(handler)

	return handler
}

func serveOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
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
	w.Header().Set("Content-Type", "text/html")
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

		if allowAll {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else if allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
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
			w.Header().Set("Content-Type", "application/json")
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
