package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/whookdev/relay/internal/config"
)

type Server struct {
	cfg    *config.Config
	server *http.Server
	logger *slog.Logger
}

func New(cfg *config.Config, logger *slog.Logger) (*Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	logger = logger.With("component", "server")

	s := &Server{
		cfg:    cfg,
		logger: logger,
	}

	s.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:      s.routes(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s, nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.handleRequest)

	return mux
}
func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		s.logger.Info("received request",
			"headers", r.Header,
			"body", string(body),
		)
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) Start(ctx context.Context) error {
	go func() {
		s.logger.Info("starting server", "address", s.server.Addr)
		if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
			s.logger.Error("server error", "error", err)
		}
	}()

	<-ctx.Done()
	return s.Shutdown()
}

func (s *Server) Shutdown() error {
	s.logger.Info("shutting down server")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return s.server.Shutdown(ctx)
}
