package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"launchd/internal/registry"
	"launchd/internal/supervisor"
	"launchd/internal/eventbus"
)

func setupTestServer(t *testing.T, token string) (*Server, *registry.Registry) {
	t.Helper()
	reg := registry.New()
	bus := eventbus.New()
	sup := supervisor.New(reg, bus, t.TempDir(), "127.0.0.1:0")

	srv := NewServer(ServerConfig{
		Addr:    "127.0.0.1:0",
		Version: "test",
		Token:   token,
	})
	srv.Wire(reg, sup)
	return srv, reg
}

func TestRegisterService(t *testing.T) {
	srv, _ := setupTestServer(t, "")

	body := `{"name":"echo-svc","argv":["./echo"],"keepAlive":"always"}`
	req := httptest.NewRequest("POST", "/api/v1/services", strings.NewReader(body))
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp serviceResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Spec.Name != "echo-svc" {
		t.Fatalf("expected echo-svc, got %q", resp.Spec.Name)
	}
}

func TestRegisterServiceInvalidName(t *testing.T) {
	srv, _ := setupTestServer(t, "")

	tests := []struct {
		name string
		body string
	}{
		{"uppercase", `{"name":"My-Service","argv":["./svc"]}`},
		{"traversal", `{"name":"../etc","argv":["./svc"]}`},
		{"too long", fmt.Sprintf(`{"name":"%s","argv":["./svc"]}`, strings.Repeat("a", 64))},
		{"spaces", `{"name":"my service","argv":["./svc"]}`},
		{"empty", `{"name":"","argv":["./svc"]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/v1/services", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			srv.mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRegisterServiceInvalidEnv(t *testing.T) {
	srv, _ := setupTestServer(t, "")

	body := `{"name":"svc","argv":["./svc"],"env":["INVALID_NO_EQUALS"]}`
	req := httptest.NewRequest("POST", "/api/v1/services", strings.NewReader(body))
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid env, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRegisterServiceBodyTooLarge(t *testing.T) {
	srv, _ := setupTestServer(t, "")

	// Create a body larger than 1MB.
	body := strings.Repeat("x", maxRequestBody+1)
	req := httptest.NewRequest("POST", "/api/v1/services", strings.NewReader(body))
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", rec.Code)
	}
}

func TestListServices(t *testing.T) {
	srv, reg := setupTestServer(t, "")
	reg.Register(registry.Spec{Name: "a", Argv: []string{"./a"}})
	reg.Register(registry.Spec{Name: "b", Argv: []string{"./b"}})

	req := httptest.NewRequest("GET", "/api/v1/services", nil)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var services []serviceResponse
	json.Unmarshal(rec.Body.Bytes(), &services)
	if len(services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(services))
	}
}

func TestGetService(t *testing.T) {
	srv, reg := setupTestServer(t, "")
	reg.Register(registry.Spec{Name: "svc", Argv: []string{"./svc"}})

	req := httptest.NewRequest("GET", "/api/v1/services/svc", nil)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestGetServiceNotFound(t *testing.T) {
	srv, _ := setupTestServer(t, "")

	req := httptest.NewRequest("GET", "/api/v1/services/nonexistent", nil)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestUnregisterService(t *testing.T) {
	srv, reg := setupTestServer(t, "")
	reg.Register(registry.Spec{Name: "svc", Argv: []string{"./svc"}})

	req := httptest.NewRequest("DELETE", "/api/v1/services/svc", nil)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify removed.
	if _, ok := reg.Get("svc"); ok {
		t.Fatal("service should be removed")
	}
}

func TestUnregisterRunningServiceConflict(t *testing.T) {
	srv, reg := setupTestServer(t, "")
	reg.Register(registry.Spec{Name: "svc", Argv: []string{"./svc"}})
	reg.Transition("svc", registry.StateStarting)
	reg.Transition("svc", registry.StateRunning)

	req := httptest.NewRequest("DELETE", "/api/v1/services/svc", nil)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthMiddleware_NoToken(t *testing.T) {
	srv, _ := setupTestServer(t, "secret-token")

	// Request without token.
	req := httptest.NewRequest("GET", "/api/v1/services", nil)
	rec := httptest.NewRecorder()

	handler := srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
}

func TestAuthMiddleware_WrongToken(t *testing.T) {
	srv, _ := setupTestServer(t, "secret-token")

	req := httptest.NewRequest("GET", "/api/v1/services", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()

	handler := srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong token, got %d", rec.Code)
	}
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	srv, _ := setupTestServer(t, "secret-token")

	req := httptest.NewRequest("GET", "/api/v1/services", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()

	handler := srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid token, got %d", rec.Code)
	}
}

func TestAuthMiddleware_QueryToken(t *testing.T) {
	srv, _ := setupTestServer(t, "secret-token")

	req := httptest.NewRequest("GET", "/api/v1/services?token=secret-token", nil)
	rec := httptest.NewRecorder()

	handler := srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with query token, got %d", rec.Code)
	}
}

func TestAuthDisabled(t *testing.T) {
	srv, _ := setupTestServer(t, "") // empty token = auth disabled

	req := httptest.NewRequest("GET", "/api/v1/services", nil)
	rec := httptest.NewRecorder()

	handler := srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when auth disabled, got %d", rec.Code)
	}
}

// Test helper for suppressing unused import warnings.
var _ = bytes.NewBuffer
