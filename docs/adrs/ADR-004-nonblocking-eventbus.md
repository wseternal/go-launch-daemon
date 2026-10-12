# ADR-004: Non-Blocking EventBus with Drop Detection and Resync

## Status
Accepted (2026-10-12)

## Context

The daemon produces two types of events: service state changes (transitions,
heartbeat updates) and log lines (stdout/stderr from supervised processes).
These events flow to multiple consumers: the WebSocket broadcaster (console
clients), future log aggregators, and potentially external integrations.

The event distribution must satisfy:
- **Non-blocking publish:** a slow or disconnected consumer must never stall
  the supervisor's process lifecycle (the critical path)
- **Drop detection:** consumers must know when they missed events
- **Recovery:** after drops, consumers must be able to resynchronize
- **No goroutine leaks:** `Close()` must release all subscribers cleanly

## Decision

**Non-blocking publish via `select`/`default` with per-subscriber buffered
channels (128), drop counters, and synthetic resync markers.**

`Publish` attempts a non-blocking send to each subscriber's buffered channel.
If the channel is full, the event is silently dropped and the subscriber's
drop counter increments. On the next successful send to that subscriber, a
synthetic `resync` event precedes the real event — signaling the consumer to
invalidate its cache and re-fetch from the API.

```go
func (b *Bus) Publish(event Event) {
    // ...
    for sub := range b.subscribers {
        select {
        case sub.ch <- event:
            // If drops happened before, inject resync first.
        default:
            sub.mu.Lock()
            sub.drops++
            sub.mu.Unlock()
        }
    }
}
```

Events are built once and shared immutably across subscribers (no per-subscriber
copy). Topic filtering (state vs. log) is handled at the subscriber level.

## Alternatives Considered

### Blocking publish (unbuffered channels)
- **Pros:** Guaranteed delivery.
- **Cons:** A single slow WebSocket client can stall the entire supervisor.
  The process lifecycle (start/stop/crash handling) must never wait for event
  consumers.
- **Rejected:** Supervision correctness must not depend on event delivery.

### Ring buffer per subscriber (bounded, overwrites oldest)
- **Pros:** Bounded memory, newest events always available.
- **Cons:** Consumer cannot distinguish "no events" from "events overwritten."
  No explicit resync signal — consumer must poll blindly.
- **Rejected:** Drop counter + resync marker gives the consumer explicit
  recovery information.

### External message broker (Redis pub/sub, NATS)
- **Pros:** Persistence, replay, multi-host fan-out.
- **Cons:** External dependency contradicts "lite" positioning. Single-host
  supervisor does not need distributed messaging.
- **Rejected:** Over-engineered for the scope.

## Consequences

- **Positive:** Supervisor never blocks on event delivery. Consumers get
  explicit drop notification (resync marker) and can recover by re-fetching
  state from the API. 128-slot buffer absorbs short bursts without drops.
  `go test -race` with 10k publishes against a never-draining subscriber
  confirms the publisher returns in <100 ms.
- **Negative:** Events can be lost under sustained high throughput (mitigated:
  at ≤100 services, state transitions are infrequent; log lines are the main
  volume and are also captured in ring buffers and files).
- **Neutral:** The WebSocket handler uses the resync marker to trigger
  `queryClient.invalidateQueries()` on the frontend, ensuring the console
  recovers from any missed state events.

## References
- Implementation: `daemon/internal/eventbus/eventbus.go`
- Frontend consumer: `frontend/src/hooks/useEvents.ts`
- Plan: `1/plan.md` §0 (adoption from Agent B)
