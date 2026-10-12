package supervisor

import (
	"context"
	"log"
	"sync"
	"time"

	"launchd/internal/registry"
)

// BackoffConfig holds configurable backoff constants.
type BackoffConfig struct {
	BaseDelay      time.Duration // initial backoff (default 500ms)
	MaxDelay       time.Duration // max backoff cap (default 30s)
	StableRunWindow time.Duration // stable run resets failure count (default 30s)
	MaxRetries     int           // max consecutive failures before failed (default 5)
}

// DefaultBackoff returns the default backoff configuration.
func DefaultBackoff() BackoffConfig {
	return BackoffConfig{
		BaseDelay:      500 * time.Millisecond,
		MaxDelay:       30 * time.Second,
		StableRunWindow: 30 * time.Second,
		MaxRetries:     5,
	}
}

// RestartEngine manages automatic restarts for services with KeepAlive policies.
type RestartEngine struct {
	reg     *registry.Registry
	sup     *Supervisor
	cfg     BackoffConfig

	mu       sync.Mutex
	timers   map[string]context.CancelFunc // per-service backoff timers
	stable   map[string]context.CancelFunc // per-service stable-run timers
}

// NewRestartEngine creates a restart engine with the given config.
func NewRestartEngine(reg *registry.Registry, sup *Supervisor, cfg BackoffConfig) *RestartEngine {
	return &RestartEngine{
		reg:    reg,
		sup:    sup,
		cfg:    cfg,
		timers: make(map[string]context.CancelFunc),
		stable: make(map[string]context.CancelFunc),
	}
}

// HandleExit is called when a service exits. It decides whether to restart
// based on the KeepAlive policy and failure count.
func (re *RestartEngine) HandleExit(name string) {
	entry, ok := re.reg.Get(name)
	if !ok {
		return
	}

	policy := entry.Spec.KeepAlive
	switch policy {
	case registry.KeepAliveNever:
		return // no auto-restart
	case registry.KeepAliveOnFailure:
		if entry.Status.LastExitCode != nil && *entry.Status.LastExitCode == 0 {
			return // clean exit, no restart
		}
	case registry.KeepAliveAlways:
		// always restart
	}

	// Check failure count.
	restarts := entry.Status.Restarts
	maxRetries := entry.Spec.MaxRetries
	if maxRetries <= 0 {
		maxRetries = re.cfg.MaxRetries
	}
	if restarts >= maxRetries {
		log.Printf("restart[%s]: max retries (%d) reached, marking failed", name, maxRetries)
		re.reg.Transition(name, registry.StateFailed)
		re.reg.SetError(name, "max retries exceeded")
		return
	}

	// Calculate backoff delay.
	delay := re.backoffDelay(restarts)

	re.scheduleRestart(name, delay)
}

// StartStableTimer starts a timer that resets the failure count after a stable run.
func (re *RestartEngine) StartStableTimer(name string) {
	re.mu.Lock()
	defer re.mu.Unlock()

	// Cancel any existing stable timer.
	if cancel, ok := re.stable[name]; ok {
		cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	re.stable[name] = cancel

	go func() {
		select {
		case <-time.After(re.cfg.StableRunWindow):
			re.reg.ResetRestarts(name)
		case <-ctx.Done():
		}
	}()
}

// CancelStableTimer cancels the stable-run timer for a service.
func (re *RestartEngine) CancelStableTimer(name string) {
	re.mu.Lock()
	defer re.mu.Unlock()

	if cancel, ok := re.stable[name]; ok {
		cancel()
		delete(re.stable, name)
	}
}

// CancelPending cancels any pending restart for a service (e.g., manual stop).
func (re *RestartEngine) CancelPending(name string) {
	re.mu.Lock()
	defer re.mu.Unlock()

	if cancel, ok := re.timers[name]; ok {
		cancel()
		delete(re.timers, name)
	}
}

// Shutdown cancels all pending restarts and stable timers.
func (re *RestartEngine) Shutdown() {
	re.mu.Lock()
	defer re.mu.Unlock()

	for name, cancel := range re.timers {
		cancel()
		delete(re.timers, name)
	}
	for name, cancel := range re.stable {
		cancel()
		delete(re.stable, name)
	}
}

func (re *RestartEngine) backoffDelay(failures int) time.Duration {
	delay := re.cfg.BaseDelay
	for i := 0; i < failures; i++ {
		delay *= 2
		if delay > re.cfg.MaxDelay {
			delay = re.cfg.MaxDelay
			break
		}
	}
	return delay
}

func (re *RestartEngine) scheduleRestart(name string, delay time.Duration) {
	re.mu.Lock()

	// Cancel any existing restart timer.
	if cancel, ok := re.timers[name]; ok {
		cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	re.timers[name] = cancel
	re.mu.Unlock()

	// Transition to restarting → backoff.
	re.reg.Transition(name, registry.StateRestarting)
	re.reg.Transition(name, registry.StateBackoff)

	log.Printf("restart[%s]: scheduling restart in %v (failures=%d)", name, delay, 0)

	go func() {
		select {
		case <-time.After(delay):
			re.mu.Lock()
			delete(re.timers, name)
			re.mu.Unlock()

			re.reg.IncrementRestarts(name)
			if re.sup != nil {
				if err := re.sup.StartService(name); err != nil {
					log.Printf("restart[%s]: failed: %v", name, err)
				}
			}
		case <-ctx.Done():
			// Restart cancelled (manual stop or shutdown).
		}
	}()
}
