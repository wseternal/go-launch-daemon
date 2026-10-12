# ADR-009: Long-Poll Command Transport over SSE

## Status
Accepted (2026-10-12)

## Context

Supervised services need to receive commands from the daemon — primarily the
`shutdown` command for graceful exit. The command transport must:
- Deliver commands with low latency (seconds, not minutes)
- Not require the service to maintain a persistent connection
- Work through standard HTTP infrastructure (proxies, load balancers)
- Be implementable in Go without exotic dependencies

The SDK runs inside the supervised service process. It must be lightweight
(one goroutine for command polling, one for heartbeat) and must not block
the service's main logic.

## Decision

**HTTP long-poll with a 5-second server-side wait (`GET
/agent/v1/commands?wait=5`).**

The SDK polls for commands with a `wait` query parameter. The server holds
the request open for up to 5 seconds, returning immediately if a command is
queued or after the timeout with an empty list. This gives sub-5-second
command delivery latency while using standard HTTP — no persistent
connection, no upgrade handshake.

```go
// agent.go — long-poll handler
select {
case <-cmdCh:
    // Return queued commands immediately
case <-time.After(wait * time.Second):
    // Return empty list after timeout
case <-r.Context().Done():
    // Client disconnected
}
```

The SDK uses a single shared `http.Client` with connection keep-alive
(request timeout 10s, `MaxIdleConns` 1, `IdleConnTimeout` 30s). For the
long-poll request, a separate client with `Timeout = waitSec + 10` seconds
(default 15s) avoids cutting off the server-side wait.

The server's global `WriteTimeout` is 15 seconds — safely above the 5-second
long-poll wait, so no per-handler `ResponseController` deadline extension is
needed.

## Alternatives Considered

### SSE (Server-Sent Events)
- **Pros:** Persistent connection, server-push, lower latency.
- **Cons:** Requires a persistent connection per service. At 100 services,
  that's 100 open connections to the daemon. The frozen gate-2 evidence
  names "command long-poll" — switching to SSE would soften an immutable
  gate.
- **Rejected:** Gate constraint + long-poll is simpler and sufficient.

### WebSocket (per-service command channel)
- **Pros:** Bidirectional, lowest latency.
- **Cons:** Persistent connection per service. Protocol complexity (upgrade,
  ping/pong, framing). Overkill for a unidirectional command channel
  (daemon → service).
- **Rejected:** Unnecessary complexity for the use case.

### Short-poll (no server-side wait)
- **Pros:** Simplest server implementation.
- **Cons:** 1-second polling interval = 1 req/s per service. At 100
  services, 100 req/s just for command polling. Command latency up to 1
  second.
- **Rejected:** Long-poll reduces request rate by ~5x while maintaining low
  latency.

### gRPC streaming
- **Pros:** Bidirectional streaming, protobuf efficiency.
- **Cons:** Adds gRPC dependency. Requires protobuf code generation. The
  daemon's HTTP API is JSON/REST — introducing gRPC for one channel is
  inconsistent.
- **Rejected:** Inconsistent with the REST API architecture.

## Consequences

- **Positive:** Standard HTTP, no persistent connections, works through any
  HTTP proxy. 5-second server wait gives sub-5s command latency. Single
  shared `http.Client` with keep-alive minimizes overhead. The server
  implementation is a simple `select` over command channel + timeout.
- **Negative:** Each service maintains a pending HTTP request (one goroutine
  on the server side). At 100 services, 100 pending goroutines (negligible
  memory). Command latency is bounded by the wait timeout (5s worst case).
- **Neutral:** The `ResponseController` approach (extending per-handler
  deadlines) would be needed if the wait exceeded the global `WriteTimeout`
  — deferred to v2 if longer waits are required.

## References
- Server handler: `launch-daemon/daemon/internal/api/agent.go`
- SDK client: `launch-daemon/daemon/pkg/launchsdk/sdk.go`
- Plan: `1/plan.md` §0 (SDK command transport), §9 item 9 (rejected short-poll)
