package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/acdifran/go-tools/logger"
)

const (
	InternalServerError string = "Internal Server Error"
	Unauthorized        string = "Not Authorized"
)

type Server struct {
	addr            string
	handler         http.Handler
	logger          logger.Logger
	shutdownTimeout time.Duration
}

type ServerOption func(*Server)

func WithTimeout(timeout time.Duration) ServerOption {
	return func(s *Server) {
		s.shutdownTimeout = timeout
	}
}

func WithLogger(logger logger.Logger) ServerOption {
	return func(s *Server) {
		s.logger = logger
	}
}

func NewServer(
	addr string,
	handler http.Handler,
	opts ...ServerOption,
) *Server {
	server := &Server{
		addr:            addr,
		handler:         handler,
		logger:          *logger.Default(),
		shutdownTimeout: 30 * time.Second,
	}

	for _, opt := range opts {
		opt(server)
	}

	return server
}

func (s *Server) Start() error {
	logger := s.logger.WithGroup("server")

	srv := &http.Server{
		Addr:    s.addr,
		Handler: s.handler,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("Starting application", "addr", s.addr)
		errCh <- srv.ListenAndServe()
	}()

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	select {
	case err := <-errCh:
		switch {
		case err == nil:
			logger.Info("Server stopped")
			return nil
		case errors.Is(err, http.ErrServerClosed):
			logger.Info("Server closed")
			return nil
		default:
			return fmt.Errorf("server exited with error: %w", err)
		}

	case <-sigCtx.Done():
		logger.Info("Shutdown signal received, draining in-flight requests")
		srv.SetKeepAlivesEnabled(false)

		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			_ = srv.Close() // forced stop if graceful shutdown timed out/failed
			return fmt.Errorf("graceful shutdown failed: %w", err)
		}

		// Ensure server goroutine exits cleanly.
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server exited during shutdown: %w", err)
		}

		logger.Info("Server shutdown complete")
		return nil
	}
}
