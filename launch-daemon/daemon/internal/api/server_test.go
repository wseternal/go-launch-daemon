package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	srv := NewServer(ServerConfig{
		Addr:    "127.0.0.1:0",
		Version: "test",
	})

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected application/json, got %q", ct)
	}

	var resp healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Version != "test" {
		t.Fatalf("expected version test, got %q", resp.Version)
	}
	if resp.UptimeSec < 0 {
		t.Fatalf("expected non-negative uptime, got %f", resp.UptimeSec)
	}
	if resp.Goroutines < 1 {
		t.Fatalf("expected at least 1 goroutine, got %d", resp.Goroutines)
	}
}

func TestHealthEndpoint_MethodNotAllowed(t *testing.T) {
	srv := NewServer(ServerConfig{
		Addr:    "127.0.0.1:0",
		Version: "test",
	})

	req := httptest.NewRequest("POST", "/api/v1/health", nil)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	// Go 1.22+ ServeMux returns 405 for method mismatch.
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestServerStartAndShutdown(t *testing.T) {
	srv := NewServer(ServerConfig{
		Addr:    "127.0.0.1:0",
		Version: "test",
	})

	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	addr := srv.Addr()
	if addr == "" {
		t.Fatal("expected non-empty address")
	}

	// Verify health responds.
	resp, err := http.Get("http://" + addr + "/api/v1/health")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Shutdown.
	if err := srv.Shutdown(t.Context()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestServerBindFails(t *testing.T) {
	srv1 := NewServer(ServerConfig{
		Addr:    "127.0.0.1:0",
		Version: "test",
	})
	if err := srv1.Start(); err != nil {
		t.Fatalf("start srv1: %v", err)
	}
	defer srv1.Shutdown(t.Context())

	// Try to bind the same address.
	srv2 := NewServer(ServerConfig{
		Addr:    srv1.Addr(),
		Version: "test",
	})
	err := srv2.Start()
	if err == nil {
		srv2.Shutdown(t.Context())
		t.Fatal("expected bind error, got nil")
	}
}
