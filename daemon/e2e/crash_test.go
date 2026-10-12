package e2e

import (
	"testing"
	"time"
)

// TestCrashRestartE2E tests automatic restart after crash with keepAlive=always.
func TestCrashRestartE2E(t *testing.T) {
	env := setup(t)

	// Register a crasher service with keepAlive=always.
	regBody := map[string]any{
		"name":            "crasher",
		"argv":            []string{env.ticker},
		"env":             []string{"MODE=ticker", "CRASH_AFTER=2"},
		"keepAlive":       "always",
		"stopTimeoutSecs": 5,
		"maxRetries":      5,
	}

	resp := env.apiPost("/api/v1/services", regBody)
	if resp.StatusCode != 201 {
		t.Fatalf("register: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Start.
	resp = env.apiPost("/api/v1/services/crasher/start", nil)
	if resp.StatusCode != 202 {
		t.Fatalf("start: expected 202, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Wait for running.
	env.waitForState("crasher", "running", 10*time.Second)

	// The service will crash after 2 seconds and be restarted.
	// Wait for it to go through crash → restart → running cycle.
	time.Sleep(5 * time.Second)

	// It should have restarted at least once.
	env.waitForState("crasher", "running", 10*time.Second)

	// Clean up — stop it.
	env.apiPost("/api/v1/services/crasher/stop", nil)
	env.waitForState("crasher", "stopped", 10*time.Second)
}

// TestNeverPolicyE2E tests that keepAlive=never does not auto-restart.
func TestNeverPolicyE2E(t *testing.T) {
	env := setup(t)

	// Register with keepAlive=never and CRASH_AFTER=1.
	regBody := map[string]any{
		"name":            "no-restart",
		"argv":            []string{env.ticker},
		"env":             []string{"MODE=ticker", "CRASH_AFTER=1"},
		"keepAlive":       "never",
		"stopTimeoutSecs": 5,
	}

	resp := env.apiPost("/api/v1/services", regBody)
	if resp.StatusCode != 201 {
		t.Fatalf("register: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Start.
	env.apiPost("/api/v1/services/no-restart/start", nil)
	env.waitForState("no-restart", "running", 10*time.Second)

	// Wait for crash.
	time.Sleep(3 * time.Second)

	// Should be in failed state (not restarted).
	env.waitForState("no-restart", "failed", 5*time.Second)
}
