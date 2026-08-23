package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

type ServerOptions struct {
	ShutdownTimeout, ReadHeaderTimeout, IdleTimeout time.Duration
	MaxHeaderBytes                                  int
	Readiness                                       *Readiness
}
type Server struct {
	server  *http.Server
	logger  *slog.Logger
	options ServerOptions
}

func New(address string, handler http.Handler, logger *slog.Logger, options ServerOptions) *Server {
	return &Server{server: &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: options.ReadHeaderTimeout, IdleTimeout: options.IdleTimeout, MaxHeaderBytes: options.MaxHeaderBytes}, logger: logger, options: options}
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
		if s.options.Readiness != nil {
			s.options.Readiness.BeginDrain()
		}
		s.logger.Info("API draining", "operation", "http.shutdown", "grace_period", s.options.ShutdownTimeout)
		shutdownContext, cancel := context.WithTimeout(context.Background(), s.options.ShutdownTimeout)
		defer cancel()
		if err := s.server.Shutdown(shutdownContext); err != nil {
			s.logger.Warn("API graceful shutdown deadline exceeded", "operation", "http.shutdown", "error", err)
			_ = s.server.Close()
			return err
		}
		return nil
	}
}
