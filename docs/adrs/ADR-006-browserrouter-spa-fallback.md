# ADR-006: BrowserRouter with SPA Fallback over HashRouter

## Status
Accepted (2026-10-12)

## Context

The web console uses React Router for client-side navigation. Two routing
strategies are available:
- **BrowserRouter:** clean URLs (`/services/echo`), requires server-side SPA
  fallback for deep links
- **HashRouter:** hash-based URLs (`/#/services/echo`), works without server
  configuration but produces non-standard URLs

The console is served through the Caddy gateway
(`/?XTransformPort=8800/services/echo`) and directly from the daemon
(`http://localhost:8800/services/echo`). Deep links must work in both
contexts.

## Decision

**Use BrowserRouter with a SPA fallback in the daemon's static handler.**

The static handler serves `index.html` for any GET request whose path does not
start with `/api/` or `/agent/`. This means `/services/echo`, `/`, and any
future routes all resolve to the SPA entry point. The React Router client
handles the actual route matching.

The clean `/api` + `/agent` prefix separation makes the fallback a single
prefix check — no route table duplication between the daemon and the frontend.

```go
// static.go
if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/agent/") {
    http.NotFound(w, r)
    return
}
// ... SPA fallback to index.html
```

This decision was made by the user at review (Q5: "No — avoid HashRouter").

## Alternatives Considered

### HashRouter
- **Pros:** No server-side configuration needed. Deep links work out of the
  box because the hash fragment is never sent to the server.
- **Cons:** Non-standard URLs (`/#/services/echo`). Deep links through the
  Caddy gateway require the hash to survive the proxy (it does, but the URL
  is less clean). The user explicitly rejected this approach.
- **Rejected:** User decision (Q5).

### Server-side route table mirroring React Router
- **Pros:** Server can return proper 404s for non-existent routes.
- **Cons:** Dual maintenance — every new frontend route requires a server
  change. The SPA fallback approach is simpler and the 404 can be handled
  client-side (a catch-all route).
- **Rejected:** Unnecessary coupling between server and client routing.

## Consequences

- **Positive:** Clean URLs. Deep links work through the Caddy gateway
  (`?XTransformPort=8800/services/echo` routes correctly). No route table
  duplication. The user's explicit preference.
- **Negative:** The SPA fallback means any typo in a URL loads the app and
  shows a client-side 404 rather than a server-side one (acceptable for an
  operator console).
- **Neutral:** Cache-Control for `index.html` is `no-store` to ensure the
  latest version is always fetched for deep links.

## References
- Implementation: `daemon/internal/api/static.go`
- Frontend router: `frontend/src/App.tsx`
- Plan: `1/plan.md` §0 (router), §9 item 10 (rejected HashRouter), §12 Q5
