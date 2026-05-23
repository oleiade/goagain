// Package main provides the MCP server for Flesh and Blood card data.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	mcp "github.com/mark3labs/mcp-go/server"
	"github.com/oleiade/goagain/internal/data"
	fabmcp "github.com/oleiade/goagain/internal/mcp"
	"github.com/oleiade/goagain/internal/observability"
	"github.com/oleiade/goagain/internal/server"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (err error) {
	mode := flag.String("mode", "stdio", "Transport mode: stdio or http")
	port := flag.Int("port", 8081, "HTTP port (only used in http mode)")
	flag.Parse()

	if envMode := os.Getenv("MCP_MODE"); envMode != "" {
		*mode = envMode
	}
	if envPort := os.Getenv("MCP_PORT"); envPort != "" {
		if p, atoiErr := strconv.Atoi(envPort); atoiErr == nil {
			*port = p
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	otelConfig := observability.LoadOTelConfig("goagain-mcp")
	otelShutdown, err := observability.SetupOTelSDK(ctx, otelConfig)
	if err != nil {
		return fmt.Errorf("setting up otel: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err = errors.Join(err, otelShutdown(shutdownCtx))
	}()

	obsConfig := observability.LoadConfig("goagain-mcp")
	logger := observability.SetupLogger(obsConfig)

	var metrics *observability.Metrics
	if obsConfig.MetricsEnabled {
		metrics = observability.NewMetrics(obsConfig.ServiceName)
	}

	logger.Info("Loading card data...")
	store, err := data.NewStore(metrics)
	if err != nil {
		return fmt.Errorf("loading data: %w", err)
	}

	dataStats, _ := store.Stats()
	observability.LogDataLoaded(logger, dataStats)

	mcpServer := fabmcp.NewServer(store, logger, metrics)

	switch *mode {
	case "stdio":
		return runStdio(mcpServer, logger)
	case "http":
		return runHTTP(ctx, mcpServer, *port, logger, metrics)
	default:
		return fmt.Errorf("unknown mode %q (want stdio or http)", *mode)
	}
}

func runStdio(mcpServer *fabmcp.Server, logger *slog.Logger) error {
	observability.LogStartup(logger, "mcp-stdio", "stdio")
	if err := mcp.ServeStdio(mcpServer.MCPServer()); err != nil {
		return fmt.Errorf("stdio server: %w", err)
	}
	return nil
}

func runHTTP(ctx context.Context, mcpServer *fabmcp.Server, port int, logger *slog.Logger, metrics *observability.Metrics) error {
	httpServer := mcp.NewStreamableHTTPServer(mcpServer.MCPServer())

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
		})
	})

	// MCP endpoint (handles /mcp by default)
	mux.Handle("/", httpServer)

	var handler http.Handler = mux

	if metrics != nil {
		handler = metrics.MetricsMiddleware(mcpPathNormalizer())(handler)
	}

	handler = observability.LoggingMiddleware(logger, nil)(handler)
	handler = observability.RequestIDMiddleware(handler)
	handler = otelhttp.NewHandler(handler, "goagain-mcp",
		otelhttp.WithMessageEvents(otelhttp.ReadEvents, otelhttp.WriteEvents),
	)

	srv := server.New("mcp-http", port, logger, handler)
	return srv.Run(ctx)
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
