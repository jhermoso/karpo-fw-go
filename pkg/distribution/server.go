package distribution

import (
	"context"
	"net/http"
	"time"
)

// Server wraps http.Server with graceful shutdown support.
type Server struct {
	httpServer *http.Server
}

// NewServer creates a new HTTP Server configured with sensible timeout defaults.
func NewServer(addr string, handler http.Handler) *Server {
	return &Server{
		httpServer: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
		},
	}
}

// Start launches the HTTP server in a background goroutine.
func (s *Server) Start() <-chan error {
	errCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()
	return errCh
}

// Shutdown gracefully stops the server, waiting for active connections to drain.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
