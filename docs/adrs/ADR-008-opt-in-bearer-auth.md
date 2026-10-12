# ADR-008: Opt-In Bearer Token Auth (Default OFF on Loopback)

## Status
Accepted (2026-10-12)

## Context

The daemon exposes a control API that can start, stop, and unregister
services. This API must be protected against unauthorized access, but the
daemon binds loopback (`127.0.0.1:8800`) by default — only local processes
can reach it. The threat model includes:
- A compromised local process accessing the daemon API
- Remote access if the bind address is overridden to `0.0.0.0`
- Cross-site request forgery from a browser on the same machine

The authentication mechanism must be:
- Simple (no users, roles, OAuth, or mTLS — explicit non-goals)
- Zero-configuration for the default local-only use case
- Available when the daemon is exposed beyond loopback

## Decision

**Bearer token authentication, OFF by default, enabled via `-token` flag.**

When no token is configured, the API is open on loopback. When a token is
set, every `/api/v1/*` and `/agent/v1/*` request must include an
`Authorization: Bearer <token>` header. The WebSocket handshake accepts the
token via `?token=` query parameter. Token comparison uses constant-time
comparison (`crypto/subtle.ConstantTimeCompare`) to prevent timing attacks.

```go
// auth.go
func bearerAuth(token string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if token == "" {
                next.ServeHTTP(w, r) // auth disabled
                return
            }
            // ... verify Authorization header or ?token= for WS
        })
    }
}
```

The default loopback bind (`127.0.0.1:8800`) provides the first layer of
security — the API is not reachable from the network. Token auth adds a
second layer for environments where the bind address is overridden.

This decision was confirmed by the user at review (Q2: "Default OFF
confirmed").

## Alternatives Considered

### Auth always ON with generated dev token
- **Pros:** No accidental exposure if bind address changes.
- **Cons:** Adds friction for the common local-only case. Every daemon start
  requires reading and passing a token. The `launchdctl` CLI needs the token
  in its environment.
- **Rejected:** User decision (Q2). The loopback bind is sufficient for the
  default case.

### mTLS (mutual TLS)
- **Pros:** Strong mutual authentication, no shared secret.
- **Cons:** Certificate management complexity. Overkill for a single-host
  supervisor. Contradicts "lite" positioning.
- **Rejected:** Out of scope (explicit non-goal: "no auth beyond an optional
  static bearer token").

### IP-based allowlist
- **Pros:** Simple network-level filtering.
- **Cons:** Does not protect against compromised local processes. Less
  granular than per-request authentication.
- **Rejected:** Bearer token provides request-level auth; loopback bind
  provides network-level protection.

## Consequences

- **Positive:** Zero configuration for local development. Token auth available
  when needed. Constant-time comparison prevents timing attacks. The console
  UI includes a token input field (stored in localStorage) for authenticated
  environments.
- **Negative:** If the bind address is changed to `0.0.0.0` without setting a
  token, the API is exposed to the network (mitigated: the default bind is
  documented; changing the bind address is an explicit opt-in).
- **Neutral:** The agent API (session tokens) uses a separate authentication
  mechanism (ADR-001) — the bearer token applies only to the operator API.

## References
- Implementation: `daemon/internal/api/auth.go`
- Tests: `daemon/internal/api/services_test.go` (auth matrix)
- Default bind: `daemon/cmd/launchd/main.go`
- Plan: `1/plan.md` §12 Q2, T7
