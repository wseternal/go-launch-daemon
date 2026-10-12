package supervisor

import (
	"context"
	"fmt"
	"log"
	"sync"

	"launchd/internal/eventbus"
	"launchd/internal/registry"
)

// Supervisor manages the lifecycle of supervised services.
type Supervisor struct {
	reg     *registry.Registry
	bus     *eventbus.Bus
	logDir  string
	apiAddr string

	mu       sync.RWMutex
	watchers map[string]*serviceWatcher

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a new Supervisor.
func New(reg *registry.Registry, bus *eventbus.Bus, logDir, apiAddr string) *Supervisor {
	ctx, cancel := context.WithCancel(context.Background())
	return &Supervisor{
		reg:      reg,
		bus:      bus,
		logDir:   logDir,
		apiAddr:  apiAddr,
		watchers: make(map[string]*serviceWatcher),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start begins the supervisor and starts all RunAtLoad services.
func (s *Supervisor) Start() error {
	// Start RunAtLoad services.
	for _, name := range s.reg.RunAtLoadSpecs() {
		if err := s.StartService(name); err != nil {
			log.Printf("RunAtLoad start %s: %v", name, err)
		}
	}
	return nil
}

// Shutdown stops all services and waits for goroutines to finish.
func (s *Supervisor) Shutdown() {
	s.cancel()

	s.mu.RLock()
	names := make([]string, 0, len(s.watchers))
	for name := range s.watchers {
		names = append(names, name)
	}
	s.mu.RUnlock()

	// Stop all services.
	for _, name := range names {
		s.StopService(name)
	}

	s.wg.Wait()
}

// StartService starts a registered service.
func (s *Supervisor) StartService(name string) error {
	entry, ok := s.reg.Get(name)
	if !ok {
		return fmt.Errorf("service %q not found", name)
	}

	if entry.Status.State == registry.StateRunning || entry.Status.State == registry.StateStarting {
		return fmt.Errorf("service %q is already %s", name, entry.Status.State)
	}

	s.mu.Lock()
	w := newServiceWatcher(name, entry.Spec, s.reg, s.bus, s.logDir, s.apiAddr)
	s.watchers[name] = w
	s.mu.Unlock()

	// Start is synchronous (spawns process + launches watcher goroutine).
	if err := w.start(s.ctx); err != nil {
		s.mu.Lock()
		delete(s.watchers, name)
		s.mu.Unlock()
		return err
	}

	return nil
}

// StopService stops a running service gracefully.
func (s *Supervisor) StopService(name string) error {
	s.mu.RLock()
	w, ok := s.watchers[name]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("service %q not running", name)
	}

	w.stop()
	w.wait()

	s.mu.Lock()
	delete(s.watchers, name)
	s.mu.Unlock()

	return nil
}

// RestartService stops and then starts a service.
func (s *Supervisor) RestartService(name string) error {
	entry, ok := s.reg.Get(name)
	if !ok {
		return fmt.Errorf("service %q not found", name)
	}

	if entry.Status.State == registry.StateRunning || entry.Status.State == registry.StateStarting {
		if err := s.StopService(name); err != nil {
			return err
		}
	}

	// Transition through restarting → backoff → starting.
	s.reg.Transition(name, registry.StateRestarting)
	s.reg.Transition(name, registry.StateBackoff)

	return s.StartService(name)
}

// LogTail returns the last n log lines for a service.
func (s *Supervisor) LogTail(name string, n int) ([]string, error) {
	s.mu.RLock()
	w, ok := s.watchers[name]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("service %q not running", name)
	}

	return w.LogTail(n), nil
}

// SessionToken returns the session token for a running service.
func (s *Supervisor) SessionToken(name string) (string, error) {
	s.mu.RLock()
	w, ok := s.watchers[name]
	s.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("service %q not running", name)
	}

	return w.token, nil
}
