// Package registry manages service specifications and runtime status.
package registry

import (
	"fmt"
	"regexp"
	"time"
)

// ServiceState represents the lifecycle state of a service.
type ServiceState string

const (
	StateRegistered ServiceState = "registered"
	StateStarting   ServiceState = "starting"
	StateRunning    ServiceState = "running"
	StateStopping   ServiceState = "stopping"
	StateStopped    ServiceState = "stopped"
	StateFailed     ServiceState = "failed"
	StateRestarting ServiceState = "restarting"
	StateBackoff    ServiceState = "backoff"
)

// KeepAlive policy for automatic restart behavior.
type KeepAlive string

const (
	KeepAliveAlways    KeepAlive = "always"
	KeepAliveOnFailure KeepAlive = "on-failure"
	KeepAliveNever     KeepAlive = "never"
)

// nameRegex enforces valid service names: lowercase alphanumeric + hyphens, 1-63 chars.
var nameRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// ValidateName checks that a service name is valid.
func ValidateName(name string) error {
	if len(name) == 0 || len(name) > 63 {
		return fmt.Errorf("name must be 1-63 characters, got %d", len(name))
	}
	if !nameRegex.MatchString(name) {
		return fmt.Errorf("name %q must match ^[a-z0-9-]{1,63}$", name)
	}
	return nil
}

// Spec is the persistent service specification.
type Spec struct {
	Name            string    `json:"name"`
	Argv            []string  `json:"argv"`
	Env             []string  `json:"env,omitempty"`
	KeepAlive       KeepAlive `json:"keepAlive"`
	RunAtLoad       bool      `json:"runAtLoad"`
	StopTimeoutSecs int       `json:"stopTimeoutSecs"`
	MaxRetries      int       `json:"maxRetries"`
}

// Status is the runtime status of a service (not persisted).
type Status struct {
	State           ServiceState `json:"state"`
	Pid             int          `json:"pid,omitempty"`
	StartedAt       *time.Time   `json:"startedAt,omitempty"`
	Restarts        int          `json:"restarts"`
	LastExitCode    *int         `json:"lastExitCode,omitempty"`
	LastExitAt      *time.Time   `json:"lastExitAt,omitempty"`
	HeartbeatAt     *time.Time   `json:"heartbeatAt,omitempty"`
	HeartbeatStatus string       `json:"heartbeatStatus,omitempty"`
	LastError       string       `json:"lastError,omitempty"`
}

// Entry combines spec and status for a registered service.
type Entry struct {
	Spec   Spec   `json:"spec"`
	Status Status `json:"status"`
}

// validTransitions defines the legal state machine transitions.
var validTransitions = map[ServiceState][]ServiceState{
	StateRegistered: {StateStarting},
	StateStarting:   {StateRunning, StateFailed, StateStopping},
	StateRunning:    {StateStopping, StateFailed},
	StateStopping:   {StateStopped, StateFailed},
	StateStopped:    {StateRestarting, StateStarting, StateFailed},
	StateFailed:     {StateRestarting, StateStarting},
	StateRestarting: {StateBackoff, StateStarting},
	StateBackoff:    {StateStarting, StateStopped, StateFailed},
}

// CanTransition checks whether a state transition is valid.
func CanTransition(from, to ServiceState) bool {
	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}
