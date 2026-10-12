# ADR-003: Disk-Based Static Serving over go:embed

## Status
Accepted (2026-10-12)

## Context

The daemon must serve the web console (Vite-built React app) to browsers. The
console is a separate frontend project that rebuilds independently of the Go
daemon. The serving mechanism must support:
- SPA routing (BrowserRouter deep links like `/services/echo`)
- Cache control (no-store for `index.html`, immutable for hashed assets)
- gzip compression (the antd bundle is ~1.1 MB uncompressed)
- Tight dev loop (frontend changes visible without Go rebuild)

## Decision

**Serve the built console from disk via `-static ../frontend/dist` flag with
SPA fallback and gzip middleware.**

The daemon accepts a `-static` directory path. When set, non-API/non-agent GET
requests are served from that directory. Unknown paths outside `/api/` and
`/agent/` prefixes fall back to `index.html` (SPA routing). When the flag is
empty, the daemon runs in API-only mode (JSON 404 for non-API routes).

A gzip middleware wraps all responses using `compress/gzip` with a `sync.Pool`
for writer reuse. It skips WebSocket upgrades and hijacked connections. The
antd bundle compresses from ~1.1 MB to ~352 KB on the wire.

```go
// static.go: SPA fallback
if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/agent/") {
    http.NotFound(w, r) // API routes stay JSON 404
    return
}
// ... try file, fallback to index.html for GET
```

## Alternatives Considered

### go:embed (compile-time embedding)
- **Pros:** Single-binary deployment, no filesystem dependency.
- **Cons:** Forces a Go rebuild for every frontend change (CSS tweak → full Go
  compile + link). Breaks the `vite build --watch` dev loop. The console
  iteration cycle becomes minutes instead of seconds.
- **Rejected:** Dev experience is unacceptable for a project where frontend and
  backend iterate independently.

### Reverse proxy to Vite dev server
- **Pros:** HMR in development.
- **Cons:** Requires the Vite dev server to be running in production. Not
  suitable for the daemon's standalone deployment model. Adds a process
  dependency.
- **Rejected:** Production serving must be self-contained within the daemon.

### Separate nginx/Caddy for static files
- **Pros:** Battle-tested static serving.
- **Cons:** Adds an external dependency. The daemon already runs an HTTP
  server; adding another process for static files contradicts the "lite"
  positioning.
- **Rejected:** Unnecessary process dependency.

## Consequences

- **Positive:** Frontend rebuilds without touching Go code. `vite build
  --watch` + browser refresh is the dev loop. gzip cuts the bundle by ~68%.
  SPA fallback enables BrowserRouter deep links through the Caddy gateway.
  API-only mode available when no frontend is built.
- **Negative:** Deployment requires the `dist/` directory alongside the daemon
  binary (mitigated: single `frontend/` subtree, standard build artifact).
- **Neutral:** Cache headers follow standard practice — `no-store` for
  `index.html` (always revalidate), `max-age=31536000, immutable` for hashed
  assets.

## References
- Implementation: `daemon/internal/api/static.go`, `gzip.go`
- Server integration: `daemon/internal/api/server.go`
- Plan: `1/plan.md` §0 (static serving), §9 item 4 (rejected go:embed)
