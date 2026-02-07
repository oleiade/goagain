// Package main provides the MCP server for Flesh and Blood card data.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"

	mcp "github.com/mark3labs/mcp-go/server"
	"github.com/oleiade/goagain/internal/data"
	fabmcp "github.com/oleiade/goagain/internal/mcp"
	"github.com/oleiade/goagain/internal/observability"
	"github.com/oleiade/goagain/internal/server"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
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

	// Handle SIGINT (CTRL+C) gracefully.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Set up OpenTelemetry first (before logger, so logs can flow to OTel).
	otelConfig := observability.LoadOTelConfig("goagain-mcp")
	otelShutdown, err := observability.SetupOTelSDK(ctx, otelConfig)
	if err != nil {
		log.Fatal(err)
	}

	// Handle shutdown properly so nothing leaks.
	defer func() {
		err = errors.Join(err, otelShutdown(context.Background()))
	}()

	// Initialize observability (logger and metrics use OTel now)
	obsConfig := observability.LoadConfig("goagain-mcp")
	logger := observability.SetupLogger(obsConfig)

	var metrics *observability.Metrics
	if obsConfig.MetricsEnabled {
		metrics = observability.NewMetrics(obsConfig.ServiceName)
	}

	logger.Info("Loading card data...")
	store, err := data.NewStore(metrics)
	if err != nil {
		logger.Error("Failed to load data", slog.String("error", err.Error()))
		os.Exit(1)
	}

	dataStats, indexStats := store.Stats()
	observability.LogDataLoaded(logger, dataStats)

	// Set data metrics
	if metrics != nil {
		metrics.SetDataStats(dataStats)
		metrics.SetIndexStats(indexStats)
	}

	mcpServer := fabmcp.NewServer(store, logger, metrics)

	switch *mode {
	case "stdio":
		runStdio(mcpServer, logger)
	case "http":
		runHTTP(mcpServer, *port, logger, metrics)
	default:
		logger.Error("Unknown mode", slog.String("mode", *mode))
		os.Exit(1)
	}
}

func runStdio(mcpServer *fabmcp.Server, logger *slog.Logger) {
	observability.LogStartup(logger, "mcp-stdio", "stdio")
	if err := mcp.ServeStdio(mcpServer.MCPServer()); err != nil {
		logger.Error("Server error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func runHTTP(mcpServer *fabmcp.Server, port int, logger *slog.Logger, metrics *observability.Metrics) {
	httpServer := mcp.NewStreamableHTTPServer(mcpServer.MCPServer())

	// Create a mux to add health endpoint
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
		})
	})

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

	// Wrap with OTel HTTP tracing
	handler = otelhttp.NewHandler(handler, "goagain-mcp",
		otelhttp.WithMessageEvents(otelhttp.ReadEvents, otelhttp.WriteEvents),
	)

	srv := server.New("mcp-http", port, logger, handler)
	srv.Run()
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
