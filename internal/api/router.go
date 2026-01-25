package api

import (
	_ "embed"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/oleiade/goagain/internal/data"
	"github.com/oleiade/goagain/internal/observability"
)

//go:embed openapi.yaml
var openAPISpec []byte

//go:embed landing.html
var landingPage []byte

// Config holds configuration for the API server.
type Config struct {
	CORSOrigins    []string
	RateLimitRPS   int
	TrustedProxies []*net.IPNet
}

// LoadConfig loads configuration from environment variables.
func LoadConfig() Config {
	config := Config{
		CORSOrigins:  []string{"*"},
		RateLimitRPS: 100,
	}

	if origins := os.Getenv("CORS_ORIGINS"); origins != "" {
		config.CORSOrigins = strings.Split(origins, ",")
		for i := range config.CORSOrigins {
			config.CORSOrigins[i] = strings.TrimSpace(config.CORSOrigins[i])
		}
	}

	if rps := os.Getenv("RATE_LIMIT_RPS"); rps != "" {
		var rate int
		if _, err := parseEnvInt(rps, &rate); err == nil && rate > 0 {
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

	return config
}

func parseEnvInt(s string, out *int) (int, error) {
	var v int
	_, err := parseEnvIntValue(s, &v)
	if err == nil {
		*out = v
	}
	return v, err
}

func parseEnvIntValue(s string, out *int) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, nil
		}
		n = n*10 + int(c-'0')
	}
	*out = n
	return n, nil
}

// NewRouter creates a new HTTP router with all API routes registered.
func NewRouter(store *data.Store, logger *slog.Logger, metrics *observability.Metrics, obsConfig observability.Config) http.Handler {
	config := LoadConfig()

	mux := http.NewServeMux()
	h := NewHandler(store)

	// Root - API info
	mux.HandleFunc("GET /", h.Index)

	// Health check
	mux.HandleFunc("GET /health", h.Health)

	// Cards
	mux.HandleFunc("GET /cards", h.ListCards)
	mux.HandleFunc("GET /cards/{id}", h.GetCard)
	mux.HandleFunc("GET /cards/{id}/legality", h.GetCardLegality)

	// Sets
	mux.HandleFunc("GET /sets", h.ListSets)
	mux.HandleFunc("GET /sets/{id}", h.GetSet)

	// Keywords
	mux.HandleFunc("GET /keywords", h.ListKeywords)
	mux.HandleFunc("GET /keywords/{name}", h.GetKeyword)

	// Abilities
	mux.HandleFunc("GET /abilities", h.ListAbilities)

	// OpenAPI spec
	mux.HandleFunc("GET /openapi.yaml", serveOpenAPI)
	mux.HandleFunc("GET /docs", serveDocs)

	// Metrics endpoint
	if metrics != nil && obsConfig.MetricsEnabled {
		mux.Handle("GET "+obsConfig.MetricsPath, metrics.Handler())
	}

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
	// Serve Swagger UI via CDN
	html := `<!DOCTYPE html>
<html>
<head>
  <title>Flesh and Blood Cards API - Documentation</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    SwaggerUIBundle({
      url: '/openapi.yaml',
      dom_id: '#swagger-ui',
      presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
      layout: "BaseLayout"
    });
  </script>
</body>
</html>`
	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write([]byte(html))
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

// rateLimiter implements a simple token bucket rate limiter per IP.
type rateLimiter struct {
	mu      sync.Mutex
	clients map[string]*clientBucket
	rps     int
}

type clientBucket struct {
	tokens   float64
	lastSeen time.Time
}

func newRateLimiter(rps int) *rateLimiter {
	rl := &rateLimiter{
		clients: make(map[string]*clientBucket),
		rps:     rps,
	}
	go rl.cleanup()
	return rl
}

func (rl *rateLimiter) cleanup() {
	ticker := time.NewTicker(time.Minute)
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, bucket := range rl.clients {
			if now.Sub(bucket.lastSeen) > 5*time.Minute {
				delete(rl.clients, ip)
			}
		}
		rl.mu.Unlock()
	}
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	bucket, exists := rl.clients[ip]

	if !exists {
		rl.clients[ip] = &clientBucket{
			tokens:   float64(rl.rps) - 1,
			lastSeen: now,
		}
		return true
	}

	// Refill tokens based on time elapsed
	elapsed := now.Sub(bucket.lastSeen).Seconds()
	bucket.tokens += elapsed * float64(rl.rps)
	if bucket.tokens > float64(rl.rps) {
		bucket.tokens = float64(rl.rps)
	}
	bucket.lastSeen = now

	if bucket.tokens >= 1 {
		bucket.tokens--
		return true
	}

	return false
}

func rateLimitMiddleware(next http.Handler, config Config, metrics *observability.Metrics) http.Handler {
	limiter := newRateLimiter(config.RateLimitRPS)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := getClientIP(r, config)

		if !limiter.allow(ip) {
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
