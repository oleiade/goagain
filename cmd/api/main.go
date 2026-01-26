// Package main provides the REST API server for Flesh and Blood card data.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/oleiade/goagain/internal/api"
	"github.com/oleiade/goagain/internal/data"
	"github.com/oleiade/goagain/internal/observability"
	"github.com/oleiade/goagain/internal/server"
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

	router := api.NewRouter(store, logger, metrics, obsConfig)

	srv := server.New("api", *port, logger, router)
	srv.Run()
}