package registry

import (
	"fmt"
	"sync"
	"time"
)

// Registry holds all registered services with thread-safe access.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]*Entry
	dirty   bool
	onChange func() // called when entries change (for persistence trigger)
}

// New creates an empty registry.
func New() *Registry {
	return &Registry{
		entries: make(map[string]*Entry),
	}
}

// SetChangeCallback sets a function called on every mutation (for persistence).
func (r *Registry) SetChangeCallback(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onChange = fn
}

// Register adds a new service spec. Fails if the name is invalid or already exists.
func (r *Registry) Register(spec Spec) error {
	if err := ValidateName(spec.Name); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.entries[spec.Name]; exists {
		return fmt.Errorf("service %q already registered", spec.Name)
	}

	// Apply defaults.
	if spec.KeepAlive == "" {
		spec.KeepAlive = KeepAliveNever
	}
	if spec.StopTimeoutSecs <= 0 {
		spec.StopTimeoutSecs = 10
	}
	if spec.MaxRetries <= 0 {
		spec.MaxRetries = 5
	}

	r.entries[spec.Name] = &Entry{
		Spec: spec,
		Status: Status{
			State: StateRegistered,
		},
	}
	r.dirty = true
	r.notifyChange()
	return nil
}

// Unregister removes a service. Fails if the service is running.
func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.entries[name]
	if !exists {
		return fmt.Errorf("service %q not found", name)
	}

	if entry.Status.State == StateRunning || entry.Status.State == StateStarting {
		return fmt.Errorf("service %q is %s, stop it first", name, entry.Status.State)
	}

	delete(r.entries, name)
	r.dirty = true
	r.notifyChange()
	return nil
}

// Transition changes the state of a service. Fails if the transition is invalid.
func (r *Registry) Transition(name string, to ServiceState) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.entries[name]
	if !exists {
		return fmt.Errorf("service %q not found", name)
	}

	if !CanTransition(entry.Status.State, to) {
		return fmt.Errorf("invalid transition %s → %s for %q", entry.Status.State, to, name)
	}

	entry.Status.State = to
	r.dirty = true
	r.notifyChange()
	return nil
}

// SetPid records the process ID for a running service.
func (r *Registry) SetPid(name string, pid int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.entries[name]
	if !exists {
		return fmt.Errorf("service %q not found", name)
	}

	entry.Status.Pid = pid
	now := time.Now()
	entry.Status.StartedAt = &now
	r.dirty = true
	r.notifyChange()
	return nil
}

// RecordExit records a process exit with the given exit code.
func (r *Registry) RecordExit(name string, exitCode int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.entries[name]
	if !exists {
		return fmt.Errorf("service %q not found", name)
	}

	entry.Status.Pid = 0
	entry.Status.LastExitCode = &exitCode
	now := time.Now()
	entry.Status.LastExitAt = &now
	r.dirty = true
	r.notifyChange()
	return nil
}

// IncrementRestarts bumps the restart counter.
func (r *Registry) IncrementRestarts(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.entries[name]
	if !exists {
		return fmt.Errorf("service %q not found", name)
	}

	entry.Status.Restarts++
	r.dirty = true
	r.notifyChange()
	return nil
}

// ResetRestarts sets the restart counter to zero (after stable run window).
func (r *Registry) ResetRestarts(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.entries[name]
	if !exists {
		return fmt.Errorf("service %q not found", name)
	}

	entry.Status.Restarts = 0
	r.dirty = true
	r.notifyChange()
	return nil
}

// SetError records the last error message.
func (r *Registry) SetError(name string, errMsg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.entries[name]
	if !exists {
		return fmt.Errorf("service %q not found", name)
	}

	entry.Status.LastError = errMsg
	r.dirty = true
	r.notifyChange()
	return nil
}

// TouchHeartbeat updates the heartbeat timestamp and status.
func (r *Registry) TouchHeartbeat(name string, status string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.entries[name]
	if !exists {
		return fmt.Errorf("service %q not found", name)
	}

	now := time.Now()
	entry.Status.HeartbeatAt = &now
	entry.Status.HeartbeatStatus = status
	r.dirty = true
	r.notifyChange()
	return nil
}

// Get returns a copy of the entry for the given service.
func (r *Registry) Get(name string) (Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.entries[name]
	if !exists {
		return Entry{}, false
	}
	return *entry, true
}

// Snapshot returns a deep copy of all entries (safe to use outside the lock).
func (r *Registry) Snapshot() map[string]Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	snap := make(map[string]Entry, len(r.entries))
	for k, v := range r.entries {
		snap[k] = *v
	}
	return snap
}

// RunAtLoadSpecs returns the names of all specs flagged for auto-start at boot.
func (r *Registry) RunAtLoadSpecs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var names []string
	for name, entry := range r.entries {
		if entry.Spec.RunAtLoad {
			names = append(names, name)
		}
	}
	return names
}

// IsDirty returns true if the registry has been modified since the last clear.
func (r *Registry) IsDirty() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.dirty
}

// ClearDirty resets the dirty flag.
func (r *Registry) ClearDirty() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dirty = false
}

// notifyChange calls the onChange callback (must be called with lock held).
func (r *Registry) notifyChange() {
	if r.onChange != nil {
		go r.onChange()
	}
}

// Load populates the registry from a list of persisted specs.
func (r *Registry) Load(specs []Spec) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, spec := range specs {
		r.entries[spec.Name] = &Entry{
			Spec: spec,
			Status: Status{
				State: StateRegistered,
			},
		}
	}
}
