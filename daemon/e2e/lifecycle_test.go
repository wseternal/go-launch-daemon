package e2e

import (
	"encoding/json"
	"testing"
	"time"
)

// TestLifecycleE2E tests the full lifecycle: register → start → running → stop → stopped.
func TestLifecycleE2E(t *testing.T) {
	env := setup(t)

	// Register a service.
	regBody := map[string]any{
		"name":            "lifecycle-test",
		"argv":            []string{env.ticker},
		"env":             []string{"MODE=ticker"},
		"keepAlive":       "never",
		"stopTimeoutSecs": 5,
	}

	resp := env.apiPost("/api/v1/services", regBody)
	if resp.StatusCode != 201 {
		t.Fatalf("register: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Start the service.
	resp = env.apiPost("/api/v1/services/lifecycle-test/start", nil)
	if resp.StatusCode != 202 {
		t.Fatalf("start: expected 202, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Wait for running state.
	env.waitForState("lifecycle-test", "running", 10*time.Second)

	// Verify PID is present.
	resp = env.apiGet("/api/v1/services/lifecycle-test")
	var info struct {
		Status struct {
			State string `json:"state"`
			Pid   int    `json:"pid"`
		} `json:"status"`
	}
	json.NewDecoder(resp.Body).Decode(&info)
	resp.Body.Close()

	if info.Status.Pid == 0 {
		t.Fatal("expected non-zero PID when running")
	}

	// Stop the service.
	resp = env.apiPost("/api/v1/services/lifecycle-test/stop", nil)
	if resp.StatusCode != 202 {
		t.Fatalf("stop: expected 202, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Wait for stopped state.
	env.waitForState("lifecycle-test", "stopped", 10*time.Second)

	// Verify exit code is recorded.
	resp = env.apiGet("/api/v1/services/lifecycle-test")
	var stopInfo struct {
		Status struct {
			State        string `json:"state"`
			LastExitCode *int   `json:"lastExitCode"`
		} `json:"status"`
	}
	json.NewDecoder(resp.Body).Decode(&stopInfo)
	resp.Body.Close()

	if stopInfo.Status.LastExitCode == nil {
		t.Fatal("expected exit code to be recorded")
	}

	// Restart the service.
	resp = env.apiPost("/api/v1/services/lifecycle-test/restart", nil)
	if resp.StatusCode != 202 {
		t.Fatalf("restart: expected 202, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	env.waitForState("lifecycle-test", "running", 10*time.Second)

	// Clean up.
	env.apiPost("/api/v1/services/lifecycle-test/stop", nil)
	env.waitForState("lifecycle-test", "stopped", 10*time.Second)
}
