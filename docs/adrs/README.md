# Architecture Decision Records

This directory contains Architecture Decision Records (ADRs) for the
go-launch-daemon project. Each ADR captures the reasoning behind a
significant technical decision — the context, the alternatives considered,
and the consequences of the chosen approach.

## Index

| ADR | Title | Status | Date |
|-----|-------|--------|------|
| [ADR-001](ADR-001-source-level-integration.md) | Source-Level Integration via Go SDK | Accepted | 2026-10-12 |
| [ADR-002](ADR-002-rwmutex-registry.md) | RWMutex-Guarded Registry over Actor/Channel Model | Accepted | 2026-10-12 |
| [ADR-003](ADR-003-disk-static-serving.md) | Disk-Based Static Serving over go:embed | Accepted | 2026-10-12 |
| [ADR-004](ADR-004-nonblocking-eventbus.md) | Non-Blocking EventBus with Drop Detection and Resync | Accepted | 2026-10-12 |
| [ADR-005](ADR-005-process-group-isolation.md) | Process Group Isolation and SIGKILL Escalation | Accepted | 2026-10-12 |
| [ADR-006](ADR-006-browserrouter-spa-fallback.md) | BrowserRouter with SPA Fallback over HashRouter | Accepted | 2026-10-12 |
| [ADR-007](ADR-007-websocket-polling-hybrid.md) | WebSocket-Primary with Polling Fallback | Accepted | 2026-10-12 |
| [ADR-008](ADR-008-opt-in-bearer-auth.md) | Opt-In Bearer Token Auth (Default OFF on Loopback) | Accepted | 2026-10-12 |
| [ADR-009](ADR-009-longpoll-commands.md) | Long-Poll Command Transport over SSE | Accepted | 2026-10-12 |
| [ADR-010](ADR-010-standalone-vite-console.md) | Standalone Vite Console over Host Next.js Embedding | Accepted | 2026-10-12 |

## Convention

- **Location:** `docs/adrs/` (this directory)
- **Naming:** `ADR-NNN-short-title.md` (sequential numbering)
- **Format:** Status, Context, Decision, Alternatives Considered, Consequences, References
- **Lifecycle:** Proposed → Accepted → Superseded / Deprecated

ADR-001 was originally created during the implementation phase.
It has been moved here for consistency.
