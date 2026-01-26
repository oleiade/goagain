// Package server provides a reusable HTTP server with graceful shutdown.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/oleiade/goagain/internal/observability"
)

// Server is a reusable HTTP server.
type Server struct {
	*http.Server
	logger *slog.Logger
	name   string
}

// New creates a new Server.
func New(name string, port int, logger *slog.Logger, router http.Handler) *Server {
	addr := fmt.Sprintf(":%d", port)

	return &Server{
		Server: &http.Server{
			Addr:         addr,
			Handler:      router,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		logger: logger,
		name:   name,
	}
}

// Run starts the server and handles graceful shutdown.
func (s *Server) Run() {
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		observability.LogStartup(s.logger, s.name, s.Addr)
		if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("Server error", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	<-shutdown
	observability.LogShutdown(s.logger, s.name)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.Shutdown(ctx); err != nil {
		s.logger.Error("Server forced to shutdown", slog.String("error", err.Error()))
		os.Exit(1)
	}

	s.logger.Info("Server stopped")
}
