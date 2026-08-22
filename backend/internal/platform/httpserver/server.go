package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

type Server struct {
	server          *http.Server
	logger          *slog.Logger
	shutdownTimeout time.Duration
}

func New(address string, handler http.Handler, logger *slog.Logger, shutdownTimeout time.Duration) *Server {
	return &Server{server: &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}, logger: logger, shutdownTimeout: shutdownTimeout}
}

func (s *Server) Run(ctx context.Context) error {
	errorsCh := make(chan error, 1)
	go func() {
		s.logger.Info("API listening", "operation", "http.listen", "address", s.server.Addr)
		errorsCh <- s.server.ListenAndServe()
	}()
	select {
	case err := <-errorsCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		s.logger.Info("API shutting down", "operation", "http.shutdown")
		return s.server.Shutdown(shutdownContext)
	}
}
