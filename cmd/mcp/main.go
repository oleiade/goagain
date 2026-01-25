// Package main provides the MCP server for Flesh and Blood card data.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/server"
	fabmcp "github.com/oleiade/goagain/internal/mcp"

	"github.com/oleiade/goagain/internal/data"
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

	log.Println("Loading card data...")
	store, err := data.NewStore()
	if err != nil {
		log.Fatalf("Failed to load data: %v", err)
	}

	stats := store.Stats()
	log.Printf("Loaded %d cards, %d sets, %d keywords, %d abilities",
		stats["cards"], stats["sets"], stats["keywords"], stats["abilities"])

	mcpServer := fabmcp.NewServer(store)

	switch *mode {
	case "stdio":
		runStdio(mcpServer)
	case "http":
		runHTTP(mcpServer, *port)
	default:
		log.Fatalf("Unknown mode: %s (use 'stdio' or 'http')", *mode)
	}
}

func runStdio(mcpServer *fabmcp.Server) {
	log.Println("Starting MCP server on stdio...")
	if err := server.ServeStdio(mcpServer.MCPServer()); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func runHTTP(mcpServer *fabmcp.Server, port int) {
	httpServer := server.NewStreamableHTTPServer(mcpServer.MCPServer())

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

	addr := fmt.Sprintf(":%d", port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second, // Longer for SSE connections
	}

	// Channel to listen for shutdown signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	// Start server in goroutine
	go func() {
		log.Printf("Starting MCP HTTP server on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-shutdown
	log.Println("Shutting down server...")

	// Create context with timeout for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server stopped")
}
