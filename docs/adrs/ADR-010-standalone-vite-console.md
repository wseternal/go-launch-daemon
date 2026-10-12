# ADR-010: Standalone Vite Console over Host Next.js Embedding

## Status
Accepted (2026-10-12)

## Context

The project exists alongside a Next.js host application. The web console must
be accessible to operators. Two architectural options exist for the console's
frontend:
- **Standalone Vite + React app:** separate build, separate dev server,
  served by the daemon
- **Pages within the host Next.js app:** integrated into the existing SSR
  framework

The constraints are:
- The console stack is fixed by the user: Vite + React + antd (latest)
- The host Next.js app must remain running and untouched
- The console must be visible through the Caddy gateway
  (`?XTransformPort=8800`)

## Decision

**Build the console as a standalone Vite + React application served by the
daemon, not as pages within the host Next.js app.**

The console lives under `frontend/` with its own `package.json`,
`vite.config.ts`, and build pipeline. The daemon serves the built output
(`frontend/dist/`) via the `-static` flag (ADR-003). The host Next.js app
remains a thin portal shell — it does not contain console logic.

The separation provides:
- Independent build cycles (frontend changes don't trigger Go compilation)
- Independent dev servers (`vite dev` on 5173, daemon on 8800)
- Clean dependency isolation (console deps don't pollute the host app)
- The user's explicit stack requirement (Vite + React + antd, not Next.js)

```
frontend/
├── package.json          # isolated deps
├── vite.config.ts        # standalone build
├── src/
│   ├── main.tsx          # React entry point
│   ├── App.tsx           # Router + layout
│   ├── api/client.ts     # API client (XTransformPort=8800)
│   ├── hooks/useEvents.ts # WebSocket + polling
│   ├── pages/            # Services, ServiceDetail
│   └── components/       # LogViewer, StatusBadge
└── dist/                 # built output → served by daemon
```

## Alternatives Considered

### Console pages inside the host Next.js app
- **Pros:** Single application, shared layout, SSR for initial load.
- **Cons:** Violates the user's stack requirement (Vite + React + antd, not
  Next.js). Couples the console to the host app's build and deploy cycle.
  The host app's existing dependencies (Prisma, etc.) would mix with the
  console's dependencies.
- **Rejected:** User fixed the console stack. Host app stays a thin portal.

### Micro-frontend (Module Federation)
- **Pros:** Runtime integration without build coupling.
- **Cons:** Significant infrastructure complexity (webpack module federation
  or import maps). The daemon already serves static files — module
  federation adds a runtime dependency loader for no measurable benefit at
  this scale.
- **Rejected:** Over-engineered. Static serving (ADR-003) is sufficient.

### iframe embedding of the Vite dev server
- **Pros:** HMR in development through the gateway.
- **Cons:** HMR through the Caddy gateway is unreliable (WebSocket
  reconnection, CORS). The production model (daemon serves built dist)
  differs from the dev model (Vite dev server), creating a dev/prod parity
  gap.
- **Rejected:** Dev loop uses `vite build --watch` + browser refresh for
  gateway testing; direct `vite dev` on 5173 for local development.

## Consequences

- **Positive:** Clean separation of concerns. Frontend and backend iterate
  independently. `bun install` runs only inside `frontend/` —
  the root `package.json` is untouched. The host Next.js app is not affected.
  The console can be developed, built, and deployed independently.
- **Negative:** Two separate frontend projects to maintain (host app +
  console). No shared components between them (mitigated: they serve
  different purposes — portal vs. operator console).
- **Neutral:** The `eslint.config.mjs` includes a `"frontend/**"` ignore
  line to prevent the host app's linter from scanning the console's code.

## References
- Console: `frontend/`
- Static serving: `daemon/internal/api/static.go` (ADR-003)
- Plan: `1/plan.md` §9 item 3 (rejected Next.js console), T12
