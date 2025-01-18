package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/whookdev/relay/internal/config"
	"github.com/whookdev/relay/internal/tunnel"
)

type Server struct {
	cfg        *config.Config
	httpServer *http.Server
	wsServer   *http.Server
	upgrader   websocket.Upgrader
	tunnels    map[string]*tunnel.Connection
	tunnelsMux sync.RWMutex
	logger     *slog.Logger
}

func New(cfg *config.Config, logger *slog.Logger) (*Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	logger = logger.With("component", "server")

	s := &Server{
		cfg:     cfg,
		logger:  logger,
		tunnels: make(map[string]*tunnel.Connection),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				//TODO: implement proper origin checking
				return true
			},
		},
	}

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:      s.routes(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	s.wsServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.WSPort),
		Handler:      s.wsRoutes(),
		ReadTimeout:  120 * time.Second,
		WriteTimeout: 120 * time.Second,
	}

	return s, nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.handleRequest)

	return mux
}

func (s *Server) wsRoutes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/tunnel", s.handleTunnelConnection)

	return mux
}

func (s *Server) handleTunnelConnection(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project")
	if projectID == "" {
		http.Error(w, "Missing project ID", http.StatusBadRequest)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("failed to upgrade websocket connection", "error", err)
		return
	}
	defer conn.Close()

	tunnelConn := tunnel.NewConnection(projectID, conn)

	s.tunnelsMux.Lock()
	s.tunnels[projectID] = tunnelConn
	s.tunnelsMux.Unlock()

	defer func() {
		s.tunnelsMux.Lock()
		delete(s.tunnels, projectID)
		s.tunnelsMux.Unlock()
		conn.Close()
	}()

	if err := tunnelConn.Handle(); err != nil {
		s.logger.Error("tunnel connection error",
			"error", err,
			"project", projectID,
		)
	} else {
		s.logger.Info("new tunnel connection established")
	}
}

func (s *Server) handleProxyRequest(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project")
	if projectID == "" {
		http.Error(w, "no project id specified", http.StatusBadRequest)
		return
	}

	s.tunnelsMux.Lock()
	tunnel, exists := s.tunnels[projectID]
	s.tunnelsMux.Unlock()

	if !exists {
		http.Error(w, "No active tunnel for project", http.StatusServiceUnavailable)
		return
	}

	resp, err := tunnel.ForwardRequest(r)
	if err != nil {
		s.logger.Error("failed to forward request",
			"error", err,
			"project", projectID,
		)
		http.Error(w, "Failed to forward request", http.StatusBadGateway)
		return
	}

	if err := copyResponse(w, resp); err != nil {
		// We've already started writing headers, so error handling is limited
		// to logging at this point
		s.logger.Error("Failed to copy response", "error", err)
	}
}

func copyResponse(w http.ResponseWriter, r *http.Response) error {
	for k, v := range r.Header {
		w.Header()[k] = v
	}

	w.WriteHeader(r.StatusCode)

	if r.Body != nil {
		defer r.Body.Close()

		_, err := io.Copy(w, r.Body)
		if err != nil {
			return fmt.Errorf("copying response body: %w", err)
		}
	}

	return nil
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
		s.logger.Info("starting HTTP server", "address", s.httpServer.Addr)
		if err := s.httpServer.ListenAndServe(); err != http.ErrServerClosed {
			s.logger.Error("HTTP server error", "error", err)
		}
	}()

	go func() {
		s.logger.Info("starting WebSocket server", "address", s.httpServer.Addr)
		if err := s.wsServer.ListenAndServe(); err != http.ErrServerClosed {
			s.logger.Error("WebSocket server error", "error", err)
		}
	}()

	<-ctx.Done()
	return s.Shutdown()
}

func (s *Server) Shutdown() error {
	s.logger.Info("shutting down server")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("error shutting down HTTP server: %w", err)
	}

	if err := s.wsServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("error shutting down WebSocket server: %w", err)
	}

	return nil
}
