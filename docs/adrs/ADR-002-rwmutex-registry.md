# ADR-002: RWMutex-Guarded Registry over Actor/Channel Model

## Status
Accepted (2026-10-12)

## Context

The daemon must maintain a concurrent registry of service specifications and
runtime status (state, PID, exit codes, restart counts). Multiple goroutines
read and mutate this state: the HTTP API handler reads snapshots, the
supervisor transitions states on process exits, and the persistence goroutine
serializes to disk.

The concurrency model must satisfy:
- Thread-safe reads from API handlers (no lock held during JSON encoding)
- Atomic state transitions on process events
- No deadlock surface at ≤100 services
- Snapshot isolation for API responses

## Decision

**Use a `sync.RWMutex`-guarded in-memory map with snapshot-outside-lock.**

The `Registry` struct wraps `sync.RWMutex` + `map[string]*Entry`. All mutations
(`Register`, `Transition`, `RecordExit`, `SetPid`) acquire a write lock. Reads
(`Get`, `List`) acquire a read lock. The `Snapshot()` method copies entries
under `RLock`, then JSON encoding happens outside the lock — the lock is never
held during I/O.

```go
type Registry struct {
    mu      sync.RWMutex
    entries map[string]*Entry
}
```

One watcher goroutine per service manages the process lifecycle. The watcher
publishes state transitions through narrow registry methods, not through
channels.

## Alternatives Considered

### Actor model (per-service goroutines with message passing)
- **Pros:** No shared mutable state, natural isolation per service.
- **Cons:** Adds deadlock surface (cross-actor coordination for list/snapshot
  operations), more complex shutdown sequencing, channel fan-in for the API
  layer. State transitions are per-lifetime events (≪1k/s even at 100
  services) — the throughput that justifies actors does not exist here.
- **Rejected:** Mutex is simpler and sufficient for the actual concurrency
  profile.

### Channel-based event log (append-only log + materialized view)
- **Pros:** Full audit trail, replay capability.
- **Cons:** Significant complexity for a "lite" supervisor; the daemon already
  writes structured logs and persists state to disk. The event bus (ADR-004)
  provides the pub/sub layer without coupling it to the registry's data model.
- **Rejected:** Over-engineered for v1 scope.

### sync.Map
- **Pros:** Optimized for read-heavy workloads.
- **Cons:** No atomic multi-field updates (state + PID + exit code must change
  together); `Range` iteration is not snapshot-isolated.
- **Rejected:** `RWMutex` gives stronger consistency guarantees for compound
  mutations.

## Consequences

- **Positive:** Simple, well-understood concurrency primitive. Snapshot copies
  under `RLock` ensure API responses are consistent. No deadlock risk from
  channel cycles. `go test -race` with 8 goroutines × 100 iterations
  (5 transitions each) plus 2 concurrent snapshot readers validates the model.
- **Negative:** Write lock contention under extreme burst (theoretical at
  >10k transitions/s — not a realistic load for a single-host supervisor).
- **Neutral:** The persistence goroutine (single-writer, 200ms debounce,
  tmp+rename atomic) runs independently of the registry lock.

## References
- Implementation: `launch-daemon/daemon/internal/registry/registry.go`
- State machine: `launch-daemon/daemon/internal/registry/state.go`
- Plan: `1/plan.md` §0 (concurrency model), §9 item 5 (rejected actor model)
