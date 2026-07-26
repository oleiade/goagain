// Package main provides the REST API server for Flesh and Blood card data.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/oleiade/goagain/internal/api"
	"github.com/oleiade/goagain/internal/data"
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
	port := flag.Int("port", 8080, "Port to listen on")
	flag.Parse()

	// PORT env var (common in container environments) overrides the flag default.
	if envPort := os.Getenv("PORT"); envPort != "" {
		if p, atoiErr := strconv.Atoi(envPort); atoiErr == nil {
			*port = p
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Set up OpenTelemetry first (before logger, so logs can flow to OTel).
	otelConfig := observability.LoadOTelConfig("goagain-api")
	otelShutdown, err := observability.SetupOTelSDK(ctx, otelConfig)
	if err != nil {
		return fmt.Errorf("setting up otel: %w", err)
	}
	// Flush exporters before exit. Runs on every return path because we
	// propagate errors instead of os.Exit. The timeout has to comfortably
	// exceed the HTTP graceful-shutdown window (30s) plus a safety margin
	// for the final OTLP batch, otherwise the last spans/metrics/logs are
	// dropped on shutdown.
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		err = errors.Join(err, otelShutdown(shutdownCtx))
	}()

	obsConfig := observability.LoadConfig("goagain-api")
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

	router := api.NewRouter(ctx, store, logger, metrics, obsConfig)

	handler := otelhttp.NewHandler(router, "goagain-api",
		otelhttp.WithMessageEvents(otelhttp.ReadEvents, otelhttp.WriteEvents),
		otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != "/health" }),
	)

	srv := server.New("api", *port, logger, handler)
	return srv.Run(ctx)
}
