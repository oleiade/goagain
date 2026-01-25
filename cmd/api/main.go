// Package main provides the REST API server for Flesh and Blood card data.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/oleiade/goagain/internal/api"
	"github.com/oleiade/goagain/internal/data"
	"github.com/oleiade/goagain/internal/observability"
)

func main() {
	port := flag.Int("port", 8080, "Port to listen on")
	flag.Parse()

	// Check for PORT environment variable (common in container environments)
	if envPort := os.Getenv("PORT"); envPort != "" {
		_, _ = fmt.Sscanf(envPort, "%d", port)
	}

	// Initialize observability
	obsConfig := observability.LoadConfig("goagain-api")
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

	router := api.NewRouter(store, logger, metrics, obsConfig)
	addr := fmt.Sprintf(":%d", *port)

	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Channel to listen for shutdown signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	// Start server in goroutine
	go func() {
		observability.LogStartup(logger, "api", addr,
			slog.Bool("metrics_enabled", obsConfig.MetricsEnabled),
			slog.String("metrics_path", obsConfig.MetricsPath))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Server error", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal
	<-shutdown
	observability.LogShutdown(logger, "api")

	// Create context with timeout for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown", slog.String("error", err.Error()))
		os.Exit(1)
	}

	logger.Info("Server stopped")
}
