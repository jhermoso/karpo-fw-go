package distribution

import (
	"context"
	"net/http"
	"time"
)

// Server envuelve http.Server con apagado ordenado.
type Server struct {
	httpServer *http.Server
}

// NewServer crea un Server HTTP nuevo con plazos de espera por defecto razonables.
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

// Start arranca el servidor HTTP en una goroutine en segundo plano.
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

// Shutdown detiene el servidor de forma ordenada, esperando a que terminen las conexiones activas.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
