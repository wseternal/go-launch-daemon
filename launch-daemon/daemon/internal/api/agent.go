package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"
)

// agentRegisterRequest is the JSON body for agent registration.
type agentRegisterRequest struct {
	Pid     int               `json:"pid"`
	Version string            `json:"version,omitempty"`
	Meta    map[string]string `json:"meta,omitempty"`
}

// agentHeartbeatRequest is the JSON body for agent heartbeats.
type agentHeartbeatRequest struct {
	Status string `json:"status"` // "ok" or "degraded"
	Detail string `json:"detail,omitempty"`
}

// agentCommand is a command sent to an agent via long-poll.
type agentCommand struct {
	Type string `json:"type"` // "shutdown"
}

// pendingCommands tracks pending shutdown commands per service.
type pendingCommands map[string]chan struct{}

func (s *Server) registerAgentRoutes() {
	s.mux.HandleFunc("POST /agent/v1/register", s.handleAgentRegister)
	s.mux.HandleFunc("POST /agent/v1/heartbeat", s.handleAgentHeartbeat)
	s.mux.HandleFunc("GET /agent/v1/commands", s.handleAgentCommands)
}

// WireWithAgent sets up both operator and agent routes.
func (s *Server) WireWithAgent() {
	s.registerAgentRoutes()
}

func (s *Server) handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	// Verify session token from the Authorization header.
	token := extractBearerToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing session token")
		return
	}

	// Find the service with this session token.
	serviceName := s.findServiceByToken(token)
	if serviceName == "" {
		writeError(w, http.StatusForbidden, "orphan registration rejected")
		return
	}

	var req agentRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	log.Printf("agent[%s]: registered pid=%d version=%s", serviceName, req.Pid, req.Version)
	writeJSON(w, http.StatusOK, map[string]string{"status": "registered"})
}

func (s *Server) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	token := extractBearerToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing session token")
		return
	}

	serviceName := s.findServiceByToken(token)
	if serviceName == "" {
		writeError(w, http.StatusForbidden, "unknown session token")
		return
	}

	var req agentHeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	s.reg.TouchHeartbeat(serviceName, req.Status)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAgentCommands(w http.ResponseWriter, r *http.Request) {
	token := extractBearerToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing session token")
		return
	}

	serviceName := s.findServiceByToken(token)
	if serviceName == "" {
		writeError(w, http.StatusForbidden, "unknown session token")
		return
	}

	// Long-poll: wait up to 5 seconds for a command.
	waitStr := r.URL.Query().Get("wait")
	waitSec := 5
	if waitStr != "" {
		if n, err := strconv.Atoi(waitStr); err == nil && n > 0 && n <= 30 {
			waitSec = n
		}
	}

	// Check if there's a pending shutdown command.
	s.mu.RLock()
	cmdCh, ok := s.pendingCmds[serviceName]
	s.mu.RUnlock()

	if !ok {
		cmdCh = make(chan struct{}, 1)
		s.mu.Lock()
		if s.pendingCmds == nil {
			s.pendingCmds = make(pendingCommands)
		}
		s.pendingCmds[serviceName] = cmdCh
		s.mu.Unlock()
	}

	select {
	case <-cmdCh:
		// Shutdown command pending.
		writeJSON(w, http.StatusOK, map[string]any{
			"commands": []agentCommand{{Type: "shutdown"}},
		})
	case <-time.After(time.Duration(waitSec) * time.Second):
		// No commands within the wait period.
		writeJSON(w, http.StatusOK, map[string]any{
			"commands": []agentCommand{},
		})
	case <-r.Context().Done():
		// Client disconnected.
	}
}

// SendShutdown sends a shutdown command to a service's long-poll.
func (s *Server) SendShutdown(serviceName string) {
	s.mu.RLock()
	cmdCh, ok := s.pendingCmds[serviceName]
	s.mu.RUnlock()

	if ok {
		select {
		case cmdCh <- struct{}{}:
		default:
		}
	}
}

// findServiceByToken searches the supervisor for a service with the given session token.
func (s *Server) findServiceByToken(token string) string {
	if s.sup == nil {
		return ""
	}

	snap := s.reg.Snapshot()
	for name := range snap {
		t, err := s.sup.SessionToken(name)
		if err == nil && t == token {
			return name
		}
	}
	return ""
}
