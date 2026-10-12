# ADR-001: Source-Level Integration via Go SDK

## Status
Accepted (2026-10-12)

## Context

The launchd daemon must supervise the lifecycle of other Go services.
The core architectural decision is how supervised services integrate with the daemon:
external configuration files (like launchd plists or systemd units) versus
source-code-level integration via an importable Go SDK.

## Decision

**Source-level integration via a public Go SDK package (`launchd/pkg/launchsdk`).**

A supervised service imports the SDK, creates a client, and receives lifecycle
commands through the SDK's context. The daemon spawns the service process and
injects connection details via environment variables:

- `LAUNCHD_API_ADDR` — daemon control API address
- `LAUNCHD_SERVICE_NAME` — registered service name
- `LAUNCHD_SESSION_TOKEN` — per-spawn session token (32-hex, in-memory only)

### Session Handshake

1. Daemon spawns the process with the env vars above.
2. Process imports SDK, reads env vars, calls `POST /agent/v1/register`.
3. Daemon verifies the session token matches the spawn record.
4. If valid: registration accepted (200).
5. If unknown token: **orphan rejected** (403) — the process was not spawned by this daemon.

### Heartbeat & Commands

- SDK sends heartbeat every 5 seconds: `POST /agent/v1/heartbeat {status, detail}`.
- SDK long-polls for commands: `GET /agent/v1/commands?wait=5`.
- On `shutdown` command: SDK cancels its context → service exits gracefully (exit 0).

### Orphan Containment

- If the daemon is unreachable for 15 consecutive seconds, the SDK self-exits.
- This prevents orphan processes from lingering after daemon crash.
- Orphan registrations (unknown session token) are rejected with 403.

### Standalone Mode

When the env vars are absent, `launchsdk.New()` returns `ErrNotSupervised`.
The binary remains runnable standalone — useful for local development and testing.

## Rejected Alternatives

1. **Config-file integration** (generating launchd plists / systemd units)
   - Contradicts the "source-level integration" requirement.
   - Adds file I/O and format parsing complexity.
   - No runtime handshake or heartbeat capability.

2. **Auto-adopt of orphan services**
   - A service that self-registers via SDK but was started outside the daemon
     cannot be supervised reliably — KeepAlive cannot be guaranteed without
     spawn ownership.
   - Rejected: daemon rejects unknown session tokens with 403.

3. **SSE (Server-Sent Events) instead of long-poll**
   - Frozen gate-2 evidence names "command long-poll".
   - SSE would soften an immutable gate.
   - Long-poll with 5s wait satisfies the contract without added complexity.

4. **Config-file generation + external process manager**
   - Adds systemd/launchd as a dependency — contradicts the "lite" positioning.
   - Loses the direct daemon ↔ service communication channel.

## Consequences

- **Positive:** Clean Go-native API, no config files, compile-time type safety,
  built-in heartbeat and graceful shutdown, orphan protection.
- **Negative:** Services must be written in Go (or use the HTTP agent API directly).
  Non-Go services require a sidecar or adapter.
- **Neutral:** The env-var contract is simple but requires the daemon to manage
  the spawn environment carefully (no env leakage, in-memory-only tokens).
