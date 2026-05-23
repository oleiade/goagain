// Package server provides a reusable HTTP server with graceful shutdown.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/oleiade/goagain/internal/observability"
)

// shutdownTimeout caps how long graceful shutdown will wait for in-flight requests.
const shutdownTimeout = 30 * time.Second

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
			Addr:    addr,
			Handler: router,
			// ReadHeaderTimeout caps how long a client may take to send headers.
			// Without it, Slowloris-style attacks can hold goroutines open up to
			// the full ReadTimeout window. Keep this much tighter than ReadTimeout
			// so legitimate slow bodies still work while header-drip attacks are
			// killed quickly.
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
			MaxHeaderBytes:    64 << 10,
		},
		logger: logger,
		name:   name,
	}
}

// Run starts the server and blocks until ctx is cancelled or the server fails.
// On ctx cancellation it performs a graceful shutdown bounded by shutdownTimeout.
func (s *Server) Run(ctx context.Context) error {
	serveErr := make(chan error, 1)
	go func() {
		observability.LogStartup(s.logger, s.name, s.Addr)
		if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		return fmt.Errorf("%s server: %w", s.name, err)
	case <-ctx.Done():
	}

	observability.LogShutdown(s.logger, s.name)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := s.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("%s server forced shutdown: %w", s.name, err)
	}

	s.logger.Info("Server stopped", slog.String("name", s.name))
	return nil
}
