# Frontend Console Module

The `frontend/` directory contains the operator web console: a standalone Vite + React single-page app (ADR-010). The daemon serves the build output `frontend/dist/` through the `-static` flag.

## Stack

* React 19, TypeScript, Vite, antd, react-router-dom 7, TanStack Query 5, dayjs.
* Bun is the package manager. The lockfile is `bun.lock`.

## Structure

```
src/
  api/client.ts           — REST and WebSocket client
  hooks/useEvents.ts      — event stream hook with polling fallback
  pages/Services.tsx      — service list page
  pages/ServiceDetail.tsx — detail page: status, actions, logs
  components/             — LogViewer, StatusBadge
  App.tsx                 — routes and layout
  main.tsx                — providers: QueryClient, BrowserRouter, antd theme
```

## Conventions

1. **Gateway parameter**: every API and WebSocket URL carries `?XTransformPort=8800` for the Caddy gateway. Use the `apiUrl` and `wsUrl` helpers in `api/client.ts`. Never hard-code URLs.
2. **Routing**: BrowserRouter with SPA fallback (ADR-006). Routes: `/` and `/services/:name`.
3. **Data flow**: TanStack Query holds the server state. A `state` event invalidates the service queries. A `resync` event invalidates all queries. A `log` event appends to the logs cache with a 500-line cap.
4. **Realtime**: WebSocket is primary and polling is the fallback (ADR-007). Reconnect backoff grows from 1 s to 16 s.
5. **Theme**: the antd `ConfigProvider` lives in `main.tsx`. Primary color: `#10b981`.

## Build and Develop

* `bun install` — install the dependencies.
* `bun run dev` — start the dev server on port 5173 (strict port).
* `bun run build` — run `tsc` and `vite build`. Type errors fail the build.
* Serve `dist/` through the daemon `-static` flag for the full setup.

## Module Rules

* Keep the strict TypeScript flags. Do not weaken `noUnusedLocals`, `noUnusedParameters`, or `noUncheckedIndexedAccess`.
* Keep the API types in `api/client.ts` in sync with `daemon/internal/api`.
* Do not add a state library. Server state stays in TanStack Query.
