// Package main provides the REST API server for Flesh and Blood card data.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"

	"github.com/oleiade/goagain/internal/api"
	"github.com/oleiade/goagain/internal/data"
	"github.com/oleiade/goagain/internal/observability"
	"github.com/oleiade/goagain/internal/server"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func main() {
	port := flag.Int("port", 8080, "Port to listen on")
	flag.Parse()

	// Check for PORT environment variable (common in container environments)
	if envPort := os.Getenv("PORT"); envPort != "" {
		_, _ = fmt.Sscanf(envPort, "%d", port)
	}

	// Handle SIGINT (CTRL+C) gracefully.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Set up OpenTelemetry first (before logger, so logs can flow to OTel).
	otelConfig := observability.LoadOTelConfig("goagain-api")
	otelShutdown, err := observability.SetupOTelSDK(ctx, otelConfig)
	if err != nil {
		log.Fatal(err)
	}

	// Handle shutdown properly so nothing leaks.
	defer func() {
		err = errors.Join(err, otelShutdown(context.Background()))
	}()

	// Initialize observability (logger and metrics use OTel now)
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

	// Wrap with OTel HTTP tracing
	handler := otelhttp.NewHandler(router, "goagain-api",
		otelhttp.WithMessageEvents(otelhttp.ReadEvents, otelhttp.WriteEvents),
	)

	srv := server.New("api", *port, logger, handler)
	srv.Run()
}
