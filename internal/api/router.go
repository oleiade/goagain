package api

import (
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
				slog.Warn("Invalid CIDR in TRUSTED_PROXIES", slog.String("cidr", cidr))
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
func NewRouter(store *data.Store, logger *slog.Logger, metrics *observability.Metrics, obsConfig observability.Config) http.Handler {
	config := LoadConfig()

	mux := http.NewServeMux()
	h := NewHandler(store, config.APIBaseURL, config.MCPBaseURL)

	// Root - Landing page / API info (unversioned)
	mux.HandleFunc("GET /", h.Index)

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

	// Build middleware chain (applied in reverse order)
	handler := http.Handler(mux)

	// Apply CORS first (innermost)
	handler = corsMiddleware(handler, config)

	// Rate limiting
	handler = rateLimitMiddleware(handler, config, metrics)

	// Metrics middleware
	if metrics != nil {
		handler = metrics.MetricsMiddleware(observability.PathNormalizer())(handler)
	}

	// Logging middleware
	getClientIP := observability.GetClientIPFunc(config.TrustedProxies)
	handler = observability.LoggingMiddleware(logger, getClientIP)(handler)

	// Request ID middleware (outermost)
	handler = observability.RequestIDMiddleware(handler)

	return handler
}

func serveOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(openAPISpec)
}

func serveDocs(w http.ResponseWriter, r *http.Request) {
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
  <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
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

func rateLimitMiddleware(next http.Handler, config Config, metrics *observability.Metrics) http.Handler {
	type client struct {
		limiter  *rate.Limiter
		lastSeen time.Time
	}

	var (
		mu      sync.Mutex
		clients = make(map[string]*client)
	)

	// Background goroutine to remove old entries from the clients map.
	go func() {
		for {
			time.Sleep(time.Minute)
			mu.Lock()
			for ip, client := range clients {
				if time.Since(client.lastSeen) > 5*time.Minute {
					delete(clients, ip)
				}
			}
			mu.Unlock()
		}
	}()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := getClientIP(r, config)

		mu.Lock()
		if _, found := clients[ip]; !found {
			clients[ip] = &client{
				limiter: rate.NewLimiter(rate.Limit(config.RateLimitRPS), config.RateLimitRPS*2),
			}
		}
		clients[ip].lastSeen = time.Now()
		if !clients[ip].limiter.Allow() {
			mu.Unlock()
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
		mu.Unlock()

		next.ServeHTTP(w, r)
	})
}

func getClientIP(r *http.Request, config Config) string {
	// Check if request is from a trusted proxy
	remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteIP = r.RemoteAddr
	}

	trusted := false
	if len(config.TrustedProxies) > 0 {
		ip := net.ParseIP(remoteIP)
		for _, cidr := range config.TrustedProxies {
			if cidr.Contains(ip) {
				trusted = true
				break
			}
		}
	}

	// Only trust X-Forwarded-For from trusted proxies
	if trusted {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// Take the first IP in the chain (original client)
			if idx := strings.Index(xff, ","); idx != -1 {
				return strings.TrimSpace(xff[:idx])
			}
			return strings.TrimSpace(xff)
		}

		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return strings.TrimSpace(xri)
		}
	}

	return remoteIP
}
