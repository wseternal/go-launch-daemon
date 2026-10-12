# Goal: go-launch-daemon

## Goal

Ship a Go-based launch daemon — a lite `launchctl` (macOS) / `systemctl` (Linux) — that supervises the full lifecycle of other Go services (register → start → monitor → restart → stop), integrates with those services **at source-code level** via an importable Go SDK (no plist/unit config files), and is operable through a Vite + React + Ant Design web console (all latest versions from npmjs.com).

## Interview Extraction (Phase 1 — interview-me)

**HYPOTHESIS:** You want a single-host Go process supervisor where Go services embed an SDK at source level to be supervised (instead of being described by external config files like `.plist`/`.unit`), with a modern web console for day-to-day operation, modeled on launchctl/systemctl semantics but deliberately "lite".

**CONFIDENCE:** ~90% — missing: (a) whether a `launchdctl` CLI belongs in v1, (b) preferred default for API auth, (c) whether auto-adopt of self-announced (externally started) services is wanted. These are listed under *Open Questions* below and resolved at the document review stop.

The user's stop instruction ("stop after phase 4's plan step, I need review all documents") is the explicit confirmation gate for this goal file — per `interview-me` Step 4/5, the restate below is structured for line-by-line confirm/correct.

### Restate for confirmation (confirm at review)

- **Outcome:**      A Go daemon + Go SDK + web console that start/stop/monitor/restart Go services with launchd-like semantics (KeepAlive, RunAtLoad, backoff, logs)
- **User:**         Developers/operators running several Go services on one Linux host
- **Why now:**      A lite, source-integrated supervisor is lighter than systemd and more controllable than process managers; the sandbox needs a visible console
- **Success:**      Services under supervision survive crashes (auto-restart), stop gracefully on command, and all of it is verifiable from a browser console + tests
- **Constraint:**   Go for daemon/SDK/examples; Vite+React+antd (latest) for the console; sandbox exposes one origin — everything routes through Caddy `?XTransformPort=<port>`
- **Out of scope:** Multi-host clustering, cgroups, Windows services, plist/unit parsers, user/role auth

**Yes / no / refine?** → *To be answered at the review stop after Phase 4 Step 1.*

## Context

- **What exists:** a Next.js 16 sandbox at `/home/z/my-project` (dev server on port 3000, the only user-visible origin behind Caddy `:81`); Node v24.21.0; Bun 1.3.14; no Go toolchain installed yet (apt candidate `golang-go 1.24`, official tarballs from go.dev reachable over the network).
- **What's missing:** everything for the goal — the daemon, the SDK, example supervised services, the console, the CLI, and the sandbox wiring that makes the console visible through the Next.js origin.
- **Sandbox gateway fact (verified in Caddyfile):** any request carrying the query param `XTransformPort=<port>` — page load, REST call, or WebSocket upgrade — is reverse-proxied by Caddy to `localhost:<port>`; all other traffic goes to the Next.js dev server on 3000. From the browser's point of view everything is same-origin (no CORS).
- **Integration model (the core architectural decision):** supervised services are spawned by the daemon and **import the SDK** (`launchd/pkg/launchsdk`) to self-register, heartbeat health, and receive control commands — i.e., integration happens in Go source code, not in external config files. This is the "lite launchctl/systemctl, source-code-level integration" the user asked for. Full rationale and rejected alternatives → ADR-001 (planned) + `1/plan.md`.

## Success Criteria

1. **Lifecycle control** — services can be registered, started, stopped, restarted; the daemon reports accurate state (`registered/starting/running/stopping/stopped/failed/restarting/backoff`) with PID, uptime, restart count, last exit; controllable via HTTP API.
2. **Source-level integration SDK** — a public Go package (`launchsdk`) that services import to self-register, send health heartbeats, and shut down gracefully on daemon command; proven by ≥2 example services.
3. **Supervision resilience (KeepAlive semantics)** — crashed services auto-restart per policy (always / on-failure / never) with exponential backoff and max-retry protection; per-service stdout/stderr is captured (ring buffer + log file).
4. **Web console** — a Vite + React + antd dashboard: live service table, start/stop/restart actions, log viewer with live streaming; verified end-to-end in a real browser.
5. **Secure by default** — control API binds loopback by default, optional bearer-token auth, validated inputs (no command injection via registration).
6. **`launchdctl` CLI** — launchctl-style operator CLI (`list`, `start`, `stop`, `restart`, `status`, `log`, `register`) talking to the same API. *(confirmed in v1 at review, 2026-10-12)*

## Constraints

- **Stacks are fixed by the user:** Go for daemon/SDK/examples; Vite + React + antd at latest npm versions for the console. The ambient Next.js/Prisma stack is NOT used inside the goal project; the Next.js app is only a thin portal shell (see below).
- **Sandbox network topology (hard):** single external origin; backend ports reachable only via `?XTransformPort=<port>`; all URLs must be relative; WebSocket connects with the same query-param mechanism (Caddy passes WS upgrades — confirmed by the gateway's socket.io examples).
- **Fixed ports (no PORT env):** daemon control API `127.0.0.1:8800`; vite dev server `5173` (dev only); example services `8901+`; Next.js stays on `3000`.
- **User visibility:** the user sees only `/` of the Next.js app — so `src/app/page.tsx` becomes a console shell embedding the daemon-served console via iframe `/?XTransformPort=8800`. The existing Next.js dev server must keep running on 3000; never `bun run build`.
- **Tests are gate evidence:** the as-goal pipeline requires passing test runs as evidence (its contract overrides the ambient "no test code" rule for this project). Browser gates are verified with agent-browser.
- Go toolchain must be bootstrapped first (official tarball preferred for the latest stable version; apt 1.24 as fallback).

## Out of Scope

- Multi-host / clustered supervision (single host only)
- cgroups / namespaces / resource limits (no systemd resource accounting — "lite")
- Windows service integration (Linux/macOS focus; the sandbox is Linux)
- plist / unit file parsing (registration happens via API, CLI, or SDK — the point of source-level integration)
- launchd "domains" (system/user/session distinction) — single supervision domain
- Auth beyond an optional static bearer token (no users, roles, OAuth, mTLS)
- Replacing or rewriting the host Next.js application (it stays a portal shell)
- Real HMR through the sandbox gateway (documented dev loop: `vite build --watch` + browser refresh; direct `vite dev` remains available for non-sandbox local development)

## Open Questions (RESOLVED at review, 2026-10-12)

1. **CLI scope:** is `launchdctl` (success criterion 6) in v1, or should the console be the only operator surface for iteration 1? *Plan currently includes it — it is cheap once the API exists and mirrors launchctl UX.* → **ANSWERED: yes, `launchdctl` is required in v1 (plan task T11).**
2. **Auth default:** token auth OFF by default (loopback bind only, as planned) or ON with a generated dev token printed at daemon boot? *Plan: OFF by default, `-token` flag / `LAUNCHD_TOKEN` env to enable; console supports entering a token.* → **ANSWERED: OFF by default confirmed.**
3. **Auto-adopt:** should a service that self-registers via SDK but was started *outside* the daemon (orphan) be adopted (supervised without respawn guarantee) or rejected? *Plan: rejected in v1 — adoption without spawn ownership cannot guarantee KeepAlive; documented in ADR-001.* → **ANSWERED: rejected confirmed (403 on unknown session token; ADR-001).**

*Plan questions Q4–Q6 (stretch soak, router, antd version) were resolved in the same review — decisions recorded in `1/plan.md` §12.*

## Created

2026-10-12 10:36 UTC
