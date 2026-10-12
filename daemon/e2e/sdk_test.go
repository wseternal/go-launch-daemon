package e2e

import (
	"encoding/json"
	"testing"
	"time"
)

// TestSDKIntegrationE2E tests the SDK handshake: register → heartbeat → shutdown → exit.
func TestSDKIntegrationE2E(t *testing.T) {
	env := setup(t)

	// Register an echo-service that uses the SDK.
	regBody := map[string]any{
		"name":            "echo-sdk",
		"argv":            []string{env.echoBin},
		"keepAlive":       "never",
		"stopTimeoutSecs": 10,
	}

	resp := env.apiPost("/api/v1/services", regBody)
	if resp.StatusCode != 201 {
		t.Fatalf("register: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Start the service.
	resp = env.apiPost("/api/v1/services/echo-sdk/start", nil)
	if resp.StatusCode != 202 {
		t.Fatalf("start: expected 202, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Wait for running.
	env.waitForState("echo-sdk", "running", 10*time.Second)

	// Wait for the SDK to register and send a heartbeat.
	env.waitForHeartbeat("echo-sdk", 15*time.Second)

	// Stop the service — SIGTERM cancels the signal context and the service exits gracefully.
	resp = env.apiPost("/api/v1/services/echo-sdk/stop", nil)
	if resp.StatusCode != 202 {
		t.Fatalf("stop: expected 202, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Wait for stopped (should exit 0 via SDK graceful shutdown).
	env.waitForState("echo-sdk", "stopped", 15*time.Second)

	// Verify exit code is 0.
	resp = env.apiGet("/api/v1/services/echo-sdk")
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
	if *stopInfo.Status.LastExitCode != 0 {
		t.Fatalf("expected exit code 0 (graceful SDK shutdown), got %d", *stopInfo.Status.LastExitCode)
	}
}
