// Package api implements the HTTP API server for the launchd daemon.
package api

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"runtime"
	"sync"
	"time"

	"launchd/internal/registry"
	"launchd/internal/supervisor"
)

// ServerConfig holds configuration for the API server.
type ServerConfig struct {
	Addr      string
	StateFile string
	LogDir    string
	StaticDir string
	Token     string
	Version   string
}

// Server is the launchd HTTP API server.
type Server struct {
	cfg         ServerConfig
	mux         *http.ServeMux
	httpSrv     *http.Server
	listener    net.Listener
	startTime   time.Time
	reg         *registry.Registry
	sup         *supervisor.Supervisor
	pendingCmds map[string]chan struct{}
	mu          sync.RWMutex
}

// NewServer creates a new API server with the given configuration.
func NewServer(cfg ServerConfig) *Server {
	s := &Server{
		cfg:         cfg,
		mux:         http.NewServeMux(),
		startTime:   time.Now(),
		pendingCmds: make(map[string]chan struct{}),
	}
	s.registerRoutes()
	return s
}

// Start begins listening and serving. It fails fast if the port is already in use.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return err
	}
	s.listener = ln

	s.httpSrv = &http.Server{
		Handler:           s.mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		WriteTimeout:      15 * time.Second,
	}

	go func() {
		if err := s.httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("serve error: %v", err)
		}
	}()

	return nil
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpSrv == nil {
		return nil
	}
	return s.httpSrv.Shutdown(ctx)
}

// Addr returns the actual listen address (useful when :0 is used).
func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /api/v1/health", s.handleHealth)
}

// healthResponse is the JSON shape of the health endpoint.
type healthResponse struct {
	Version    string         `json:"version"`
	UptimeSec  float64        `json:"uptimeSec"`
	Services   servicesStatus `json:"services"`
	Goroutines int            `json:"goroutines"`
}

type servicesStatus struct {
	Running int `json:"running"`
	Failed  int `json:"failed"`
	Total   int `json:"total"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	resp := healthResponse{
		Version:    s.cfg.Version,
		UptimeSec:  time.Since(s.startTime).Seconds(),
		Services:   servicesStatus{},
		Goroutines: runtime.NumGoroutine(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("health encode: %v", err)
	}
}
