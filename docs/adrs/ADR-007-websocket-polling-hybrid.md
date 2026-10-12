# ADR-007: WebSocket-Primary with Polling Fallback for Console Updates

## Status
Accepted (2026-10-12)

## Context

The web console must display live service status without manual refresh. State
changes (service started, stopped, crashed) and log lines must appear in the
UI within seconds. Two real-time transport mechanisms are available:
- **WebSocket:** persistent connection, server-push, low latency
- **HTTP polling:** periodic GET requests, higher latency, simpler infrastructure

The console runs behind the Caddy gateway (`?XTransformPort=8800`) which
supports WebSocket upgrades, and directly from the daemon
(`http://localhost:8800`). The transport must be resilient to connection
drops, gateway restarts, and network instability.

## Decision

**WebSocket as the primary transport with 2-second polling as a fallback when
disconnected, and a 30-second watchdog for silent failures.**

The frontend opens a WebSocket to `/api/v1/events?XTransformPort=8800`. State
events patch the React Query cache directly (`queryClient.setQueryData`),
avoiding a full re-fetch. A `resync` event triggers
`queryClient.invalidateQueries()` to force a full re-fetch.

Reconnection uses exponential backoff (1s → 16s with jitter). While
disconnected, a 2-second polling interval keeps the UI updated. A 30-second
watchdog periodically invalidates queries to catch silent connection failures
(half-open TCP connections, gateway timeouts).

```typescript
// useEvents.ts
ws.onmessage = (e) => {
    const event = JSON.parse(e.data) as Event
    if (event.type === 'resync') {
        queryClient.invalidateQueries({ queryKey: ['services'] })
    }
    if (event.type === 'state' && event.name) {
        queryClient.invalidateQueries({ queryKey: ['service', event.name] })
    }
}
// Polling fallback: 2s while disconnected
// Watchdog: 30s periodic invalidate
```

## Alternatives Considered

### Polling-only (no WebSocket)
- **Pros:** Simplest implementation, no connection state management.
- **Cons:** 2-second polling at 100 services = 50 req/s against the API.
  Higher latency (up to 2s for state changes). The "no manual refresh"
  gate requirement is met but with unnecessary API load.
- **Rejected:** WebSocket reduces API load and latency for the common case.
  Polling is retained as a fallback.

### SSE (Server-Sent Events)
- **Pros:** Simpler than WebSocket (HTTP-only, no upgrade handshake).
- **Cons:** Unidirectional (server → client only). The frozen gate-4 evidence
  names a WebSocket endpoint. Switching would soften an immutable gate.
- **Rejected:** Gate constraint + WebSocket is already proven through Caddy.

### socket.io
- **Pros:** Automatic reconnection, room-based multiplexing.
- **Cons:** Extra client+server protocol layer. Caddy query routing is already
  proven for plain WebSocket. Adds a dependency for features not needed at
  ≤3 concurrent console clients.
- **Rejected:** Unnecessary dependency for the scale.

## Consequences

- **Positive:** Sub-second state updates when WebSocket is connected. Polling
  fallback ensures the UI never goes stale. Watchdog catches silent
  failures. The non-blocking event bus (ADR-004) ensures the supervisor is
  never affected by slow WebSocket writes.
- **Negative:** Two transport paths to maintain (WebSocket + polling). The
  server-side WebSocket handler for `/api/v1/events` is not yet implemented —
  the frontend client is wired but falls through to the polling fallback.
  Implementing the handler (gorilla/websocket or nhooyr.io/websocket) is the
  next step; until then, the 2-second polling interval carries the live-update
  load.
- **Neutral:** The WebSocket connection carries both state events and log
  lines. Per-service log multiplexing (subscribing to only one service's
  logs) is deferred to v2 — v1 broadcasts all events with client-side
  filtering.

## References
- Frontend hook: `launch-daemon/frontend/src/hooks/useEvents.ts`
- API client: `launch-daemon/frontend/src/api/client.ts`
- EventBus: `launch-daemon/daemon/internal/eventbus/eventbus.go` (ADR-004)
- Plan: `1/plan.md` §0 (polling strategy), §9 items 6-7 (rejected SSE, socket.io)
