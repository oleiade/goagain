// Package main provides the MCP server for Flesh and Blood card data.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"github.com/oleiade/goagain/internal/data"
	fabmcp "github.com/oleiade/goagain/internal/mcp"
	"github.com/oleiade/goagain/internal/observability"
)

func main() {
	mode := flag.String("mode", "stdio", "Transport mode: stdio or http")
	port := flag.Int("port", 8081, "HTTP port (only used in http mode)")
	flag.Parse()

	// Check for environment variables
	if envMode := os.Getenv("MCP_MODE"); envMode != "" {
		*mode = envMode
	}
	if envPort := os.Getenv("MCP_PORT"); envPort != "" {
		_, _ = fmt.Sscanf(envPort, "%d", port)
	}

	// Initialize observability
	obsConfig := observability.LoadConfig("goagain-mcp")
	logger := observability.SetupLogger(obsConfig)

	var metrics *observability.Metrics
	if obsConfig.MetricsEnabled {
		metrics = observability.NewMetrics(obsConfig.ServiceName)
	}

	logger.Info("Loading card data...")
	store, err := data.NewStore()
	if err != nil {
		logger.Error("Failed to load data", slog.String("error", err.Error()))
		os.Exit(1)
	}

	stats := store.Stats()
	observability.LogDataLoaded(logger, stats)

	// Set data metrics
	if metrics != nil {
		metrics.SetDataStats(stats)
	}

	mcpServer := fabmcp.NewServer(store, logger, metrics)

	switch *mode {
	case "stdio":
		runStdio(mcpServer, logger)
	case "http":
		runHTTP(mcpServer, *port, logger, metrics, obsConfig)
	default:
		logger.Error("Unknown mode", slog.String("mode", *mode))
		os.Exit(1)
	}
}

func runStdio(mcpServer *fabmcp.Server, logger *slog.Logger) {
	observability.LogStartup(logger, "mcp-stdio", "stdio")
	if err := server.ServeStdio(mcpServer.MCPServer()); err != nil {
		logger.Error("Server error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func runHTTP(mcpServer *fabmcp.Server, port int, logger *slog.Logger, metrics *observability.Metrics, obsConfig observability.Config) {
	httpServer := server.NewStreamableHTTPServer(mcpServer.MCPServer())

	// Create a mux to add health and metrics endpoints
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
		})
	})

	// Metrics endpoint
	if metrics != nil && obsConfig.MetricsEnabled {
		mux.Handle("GET "+obsConfig.MetricsPath, metrics.Handler())
	}

	// MCP endpoint (handles /mcp by default)
	mux.Handle("/", httpServer)

	// Apply middleware
	var handler http.Handler = mux

	// Metrics middleware for HTTP requests
	if metrics != nil {
		handler = metrics.MetricsMiddleware(mcpPathNormalizer())(handler)
	}

	// Logging middleware
	handler = observability.LoggingMiddleware(logger, nil)(handler)

	// Request ID middleware
	handler = observability.RequestIDMiddleware(handler)

	addr := fmt.Sprintf(":%d", port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second, // Longer for SSE connections
	}

	// Channel to listen for shutdown signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	// Start server in goroutine
	go func() {
		observability.LogStartup(logger, "mcp-http", addr,
			slog.Bool("metrics_enabled", obsConfig.MetricsEnabled),
			slog.String("metrics_path", obsConfig.MetricsPath))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Server error", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal
	<-shutdown
	observability.LogShutdown(logger, "mcp-http")

	// Create context with timeout for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown", slog.String("error", err.Error()))
		os.Exit(1)
	}

	logger.Info("Server stopped")
}

// mcpPathNormalizer returns a path normalizer for MCP HTTP endpoints.
func mcpPathNormalizer() func(string) string {
	return func(path string) string {
		// Normalize MCP paths - they typically use /mcp for SSE and POST
		if path == "/mcp" || path == "/mcp/message" {
			return path
		}
		return path
	}
}
