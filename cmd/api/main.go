// Package main provides the REST API server for Flesh and Blood card data.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/oleiade/goagain/internal/api"
	"github.com/oleiade/goagain/internal/data"
)

func main() {
	port := flag.Int("port", 8080, "Port to listen on")
	flag.Parse()

	// Check for PORT environment variable (common in container environments)
	if envPort := os.Getenv("PORT"); envPort != "" {
		_, _ = fmt.Sscanf(envPort, "%d", port)
	}

	log.Println("Loading card data...")
	store, err := data.NewStore()
	if err != nil {
		log.Fatalf("Failed to load data: %v", err)
	}

	stats := store.Stats()
	log.Printf("Loaded %d cards, %d sets, %d keywords, %d abilities",
		stats["cards"], stats["sets"], stats["keywords"], stats["abilities"])

	router := api.NewRouter(store)
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
		log.Printf("Starting API server on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-shutdown
	log.Println("Shutting down server...")

	// Create context with timeout for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server stopped")
}
