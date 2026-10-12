package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"

	"launchd/internal/registry"
	"launchd/internal/supervisor"
)

const maxRequestBody = 1 << 20 // 1 MB

var serviceNameRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// registerRequest is the JSON body for service registration.
type registerRequest struct {
	Name            string   `json:"name"`
	Argv            []string `json:"argv"`
	Env             []string `json:"env,omitempty"`
	KeepAlive       string   `json:"keepAlive,omitempty"`
	RunAtLoad       bool     `json:"runAtLoad,omitempty"`
	StopTimeoutSecs int      `json:"stopTimeoutSecs,omitempty"`
	MaxRetries      int      `json:"maxRetries,omitempty"`
}

// serviceResponse is the JSON shape for a single service.
type serviceResponse struct {
	Spec   registry.Spec   `json:"spec"`
	Status registry.Status `json:"status"`
}

// Wire sets up the registry and supervisor for the API server.
func (s *Server) Wire(reg *registry.Registry, sup *supervisor.Supervisor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reg = reg
	s.sup = sup
	s.registerOperatorRoutes()
}

func (s *Server) registerOperatorRoutes() {
	// Service CRUD.
	s.mux.HandleFunc("GET /api/v1/services", s.handleListServices)
	s.mux.HandleFunc("POST /api/v1/services", s.handleRegisterService)
	s.mux.HandleFunc("GET /api/v1/services/{name}", s.handleGetService)
	s.mux.HandleFunc("DELETE /api/v1/services/{name}", s.handleUnregisterService)

	// Service actions.
	s.mux.HandleFunc("POST /api/v1/services/{name}/start", s.handleStartService)
	s.mux.HandleFunc("POST /api/v1/services/{name}/stop", s.handleStopService)
	s.mux.HandleFunc("POST /api/v1/services/{name}/restart", s.handleRestartService)

	// Logs.
	s.mux.HandleFunc("GET /api/v1/services/{name}/logs", s.handleGetLogs)
}

func (s *Server) handleListServices(w http.ResponseWriter, r *http.Request) {
	snap := s.reg.Snapshot()
	services := make([]serviceResponse, 0, len(snap))
	for _, entry := range snap {
		services = append(services, serviceResponse{Spec: entry.Spec, Status: entry.Status})
	}
	writeJSON(w, http.StatusOK, services)
}

func (s *Server) handleRegisterService(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	data, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}

	var req registerRequest
	if err := json.Unmarshal(data, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	// Validate.
	if err := validateServiceRequest(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	spec := registry.Spec{
		Name:            req.Name,
		Argv:            req.Argv,
		Env:             req.Env,
		KeepAlive:       registry.KeepAlive(req.KeepAlive),
		RunAtLoad:       req.RunAtLoad,
		StopTimeoutSecs: req.StopTimeoutSecs,
		MaxRetries:      req.MaxRetries,
	}

	if err := s.reg.Register(spec); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	// Auto-start if RunAtLoad.
	if spec.RunAtLoad && s.sup != nil {
		go func() {
			if err := s.sup.StartService(req.Name); err != nil {
				log.Printf("RunAtLoad start %s: %v", req.Name, err)
			}
		}()
	}

	entry, _ := s.reg.Get(req.Name)
	writeJSON(w, http.StatusCreated, serviceResponse{Spec: entry.Spec, Status: entry.Status})
}

func (s *Server) handleGetService(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	entry, ok := s.reg.Get(name)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("service %q not found", name))
		return
	}

	resp := serviceResponse{Spec: entry.Spec, Status: entry.Status}

	// Include log tail if available.
	if s.sup != nil {
		if lines, err := s.sup.LogTail(name, 50); err == nil {
			resp.Status.LastError = strings.Join(lines, "\n")
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleUnregisterService(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	if err := s.reg.Unregister(name); err != nil {
		if strings.Contains(err.Error(), "running") || strings.Contains(err.Error(), "starting") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStartService(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if s.sup == nil {
		writeError(w, http.StatusServiceUnavailable, "supervisor not initialized")
		return
	}
	if err := s.sup.StartService(name); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleStopService(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if s.sup == nil {
		writeError(w, http.StatusServiceUnavailable, "supervisor not initialized")
		return
	}
	if err := s.sup.StopService(name); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleRestartService(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if s.sup == nil {
		writeError(w, http.StatusServiceUnavailable, "supervisor not initialized")
		return
	}
	if err := s.sup.RestartService(name); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if s.sup == nil {
		writeError(w, http.StatusServiceUnavailable, "supervisor not initialized")
		return
	}

	tail := 200
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, err := fmt.Sscanf(v, "%d", &tail); n != 1 || err != nil {
			tail = 200
		}
	}

	lines, err := s.sup.LogTail(name, tail)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"name": name, "lines": lines})
}

func validateServiceRequest(req registerRequest) error {
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}
	if len(req.Name) > 63 {
		return fmt.Errorf("name must be 1-63 characters")
	}
	if !serviceNameRegex.MatchString(req.Name) {
		return fmt.Errorf("name must match ^[a-z0-9-]{1,63}$")
	}
	if strings.Contains(req.Name, "..") {
		return fmt.Errorf("name must not contain path traversal")
	}
	if len(req.Argv) == 0 {
		return fmt.Errorf("argv is required")
	}
	for _, e := range req.Env {
		if !strings.Contains(e, "=") {
			return fmt.Errorf("env entry must be KEY=VALUE: %q", e)
		}
	}
	if req.KeepAlive != "" {
		switch registry.KeepAlive(req.KeepAlive) {
		case registry.KeepAliveAlways, registry.KeepAliveOnFailure, registry.KeepAliveNever:
		default:
			return fmt.Errorf("keepAlive must be always, on-failure, or never")
		}
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// Drain reads and discards the request body (for error responses).
func drain(r *http.Request) {
	io.Copy(io.Discard, r.Body)
}
