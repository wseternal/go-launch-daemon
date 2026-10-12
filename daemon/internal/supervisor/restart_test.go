package supervisor

import (
	"testing"
	"time"

	"launchd/internal/eventbus"
	"launchd/internal/registry"
)

func TestBackoffDelay(t *testing.T) {
	cfg := BackoffConfig{
		BaseDelay: 500 * time.Millisecond,
		MaxDelay:  30 * time.Second,
	}
	re := NewRestartEngine(registry.New(), nil, cfg)

	tests := []struct {
		failures int
		want     time.Duration
	}{
		{0, 500 * time.Millisecond},
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 16 * time.Second},
		{6, 30 * time.Second}, // capped
		{10, 30 * time.Second}, // capped
	}

	for _, tt := range tests {
		got := re.backoffDelay(tt.failures)
		if got != tt.want {
			t.Errorf("backoffDelay(%d) = %v, want %v", tt.failures, got, tt.want)
		}
	}
}

func TestRestartEngineNeverPolicy(t *testing.T) {
	reg := registry.New()
	reg.Register(registry.Spec{
		Name:      "svc",
		Argv:      []string{"./svc"},
		KeepAlive: registry.KeepAliveNever,
	})
	reg.Transition("svc", registry.StateStarting)
	reg.Transition("svc", registry.StateRunning)
	reg.Transition("svc", registry.StateStopping)
	reg.Transition("svc", registry.StateStopped)

	re := NewRestartEngine(reg, nil, DefaultBackoff())
	re.HandleExit("svc")

	// State should remain stopped (no restart).
	entry, _ := reg.Get("svc")
	if entry.Status.State != registry.StateStopped {
		t.Fatalf("expected stopped, got %s", entry.Status.State)
	}
}

func TestRestartEngineAlwaysPolicy(t *testing.T) {
	reg := registry.New()
	reg.Register(registry.Spec{
		Name:       "svc",
		Argv:       []string{"./svc"},
		KeepAlive:  registry.KeepAliveAlways,
		MaxRetries: 3,
	})
	reg.Transition("svc", registry.StateStarting)
	reg.Transition("svc", registry.StateRunning)
	reg.Transition("svc", registry.StateStopping)
	reg.Transition("svc", registry.StateStopped)

	// Use a mock supervisor that doesn't actually start anything.
	re := NewRestartEngine(reg, nil, BackoffConfig{
		BaseDelay:      10 * time.Millisecond,
		MaxDelay:       100 * time.Millisecond,
		StableRunWindow: 1 * time.Second,
		MaxRetries:     3,
	})

	re.HandleExit("svc")

	// State should transition to restarting → backoff.
	entry, _ := reg.Get("svc")
	if entry.Status.State != registry.StateBackoff {
		t.Fatalf("expected backoff, got %s", entry.Status.State)
	}
}

func TestRestartEngineMaxRetries(t *testing.T) {
	reg := registry.New()
	reg.Register(registry.Spec{
		Name:       "svc",
		Argv:       []string{"./svc"},
		KeepAlive:  registry.KeepAliveAlways,
		MaxRetries: 3,
	})

	// Move to stopped state (simulating multiple crash/restart cycles).
	reg.Transition("svc", registry.StateStarting)
	reg.Transition("svc", registry.StateRunning)
	reg.Transition("svc", registry.StateStopping)
	reg.Transition("svc", registry.StateStopped)

	// Simulate 3 previous failures.
	for range 3 {
		reg.IncrementRestarts("svc")
	}

	re := NewRestartEngine(reg, nil, DefaultBackoff())
	re.HandleExit("svc")

	// Should be marked as failed (max retries reached).
	entry, _ := reg.Get("svc")
	if entry.Status.State != registry.StateFailed {
		t.Fatalf("expected failed, got %s", entry.Status.State)
	}
}

func TestRestartEngineOnFailurePolicy(t *testing.T) {
	reg := registry.New()
	reg.Register(registry.Spec{
		Name:       "svc",
		Argv:       []string{"./svc"},
		KeepAlive:  registry.KeepAliveOnFailure,
		MaxRetries: 3,
	})
	reg.Transition("svc", registry.StateStarting)
	reg.Transition("svc", registry.StateRunning)
	reg.Transition("svc", registry.StateStopping)
	reg.Transition("svc", registry.StateStopped)

	// Clean exit (exit code 0) — should not restart.
	re := NewRestartEngine(reg, nil, DefaultBackoff())
	exitCode := 0
	reg.RecordExit("svc", exitCode)
	re.HandleExit("svc")

	entry, _ := reg.Get("svc")
	if entry.Status.State != registry.StateStopped {
		t.Fatalf("expected stopped after clean exit, got %s", entry.Status.State)
	}
}

func TestRestartEngineCancelPending(t *testing.T) {
	reg := registry.New()
	bus := eventbus.New()
	defer bus.Close()

	reg.Register(registry.Spec{
		Name:       "svc",
		Argv:       []string{"./svc"},
		KeepAlive:  registry.KeepAliveAlways,
		MaxRetries: 3,
	})
	reg.Transition("svc", registry.StateStarting)
	reg.Transition("svc", registry.StateRunning)
	reg.Transition("svc", registry.StateStopping)
	reg.Transition("svc", registry.StateStopped)

	re := NewRestartEngine(reg, nil, BackoffConfig{
		BaseDelay:      1 * time.Second, // long delay so we can cancel
		MaxDelay:       30 * time.Second,
		StableRunWindow: 30 * time.Second,
		MaxRetries:     3,
	})

	re.HandleExit("svc")

	// Cancel the pending restart.
	re.CancelPending("svc")

	// State should remain in backoff (not restarted).
	entry, _ := reg.Get("svc")
	if entry.Status.State != registry.StateBackoff {
		t.Fatalf("expected backoff, got %s", entry.Status.State)
	}
}

func TestRestartEngineStableTimer(t *testing.T) {
	reg := registry.New()
	reg.Register(registry.Spec{
		Name:       "svc",
		Argv:       []string{"./svc"},
		KeepAlive:  registry.KeepAliveAlways,
		MaxRetries: 3,
	})

	// Simulate some restarts.
	reg.IncrementRestarts("svc")
	reg.IncrementRestarts("svc")

	entry, _ := reg.Get("svc")
	if entry.Status.Restarts != 2 {
		t.Fatalf("expected 2 restarts, got %d", entry.Status.Restarts)
	}

	re := NewRestartEngine(reg, nil, BackoffConfig{
		BaseDelay:      500 * time.Millisecond,
		MaxDelay:       30 * time.Second,
		StableRunWindow: 100 * time.Millisecond, // short for test
		MaxRetries:     3,
	})

	// Start stable timer — should reset restarts after 100ms.
	re.StartStableTimer("svc")
	time.Sleep(200 * time.Millisecond)

	entry, _ = reg.Get("svc")
	if entry.Status.Restarts != 0 {
		t.Fatalf("expected 0 restarts after stable window, got %d", entry.Status.Restarts)
	}

	re.Shutdown()
}
