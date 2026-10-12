# ADR-005: Process Group Isolation and SIGKILL Escalation

## Status
Accepted (2026-10-12)

## Context

The daemon spawns child processes (supervised services) that may themselves
spawn grandchildren. When the daemon stops a service or shuts down, all
descendant processes must be terminated — no zombies, no orphans. On macOS
and Linux, the default signal behavior is:
- `SIGTERM` to a process does not propagate to its children
- A child that ignores `SIGTERM` continues running indefinitely
- The daemon's own exit leaves orphaned children adopted by PID 1

## Decision

**Use `Setpgid` for process group isolation, SIGTERM-to-process-group for
graceful stop, and SIGKILL-to-process-group after timeout.**

Each spawned process gets its own process group via `cmd.SysProcAttr.Setpgid =
true`. Signals are sent to the negative PID (the process group) to reach all
descendants. Graceful stop follows a two-phase protocol:

1. **SIGTERM to process group** (`syscall.Kill(-pid, SIGTERM)`)
2. Wait up to `StopTimeoutSecs` (default 10s) for the process to exit
3. **SIGKILL to process group** (`syscall.Kill(-pid, SIGKILL)`) if still alive

The watcher goroutine uses a two-phase select to avoid a tight loop on closed
stop channels:
- Phase 1: wait for stop signal or process exit
- Phase 2 (after SIGTERM): wait for process exit with periodic log flush

```go
// spawn.go
cmd.SysProcAttr = &syscall.SysProcAttr{
    Setpgid: true,
}

// watcher.go — SIGTERM to process group
syscall.Kill(-w.cmd.Process.Pid, syscall.SIGTERM)
```

`cmd.Wait()` is called exactly once via a buffered channel (`waitCh` cap 1) to
prevent double-wait panics.

## Alternatives Considered

### Signal only the direct child (no process group)
- **Pros:** Simpler signal logic.
- **Cons:** Grandchild processes survive the stop. A service that forks
  workers leaves orphaned processes.
- **Rejected:** Process group is necessary for complete cleanup.

### cgroups / namespaces (Linux-only containment)
- **Pros:** Stronger isolation, resource limits.
- **Cons:** Linux-only (the daemon also runs on macOS). Requires elevated
  privileges. Contradicts "lite" positioning.
- **Rejected:** Out of scope for v1 (explicit non-goal).

### SIGKILL immediately (no grace period)
- **Pros:** Guaranteed fast termination.
- **Cons:** No opportunity for graceful shutdown (open files, connections,
  in-flight requests). Data loss risk.
- **Rejected:** SIGTERM grace period is essential for well-behaved services.

## Consequences

- **Positive:** All descendants are cleaned up on stop/shutdown. The
  `stubborn` test binary (which absorbs SIGTERM) validates the SIGKILL
  escalation path. `READY_FILE` synchronization makes the escalation test
  deterministic. The SDK's 15-second daemon-loss self-exit (ADR-001) provides
  a second layer of orphan containment.
- **Negative:** `Setpgid` creates a new process group per service — the child
  cannot receive signals from the daemon's terminal (acceptable: the daemon
  is not an interactive terminal).
- **Neutral:** `Pdeathsig` (Linux-only, best-effort) could provide an additional
  safeguard — if the daemon crashes, children receive `SIGHUP` automatically.
  This is not yet implemented; deferred pending Linux deployment testing.

## References
- Implementation: `daemon/internal/supervisor/spawn.go`
- Watcher: `daemon/internal/supervisor/watcher.go`
- Stubborn test binary: `daemon/internal/testsvc/stubborn/main.go`
- Plan: `1/plan.md` §0 (adoption from Agent C), T5
