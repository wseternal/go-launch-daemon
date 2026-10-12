# Iteration 1 Plan — go-launch-daemon

**Pipeline:** as-goal · Phase 4 · Step 1 (Architect, via `multi-agent-planning`)
**Date:** 2026-10-12 · **Status:** awaiting user review (user-planned stop after this artifact)
**Inputs:** goal file · 5 frozen gates · 3 parallel read-only designs (Task 5-a Simplicity, 5-b Performance, 5-c Minimal-Change — full reports in each agent's worklog record)

---

## 0. Synthesis provenance (critical review summary)

| Aspect | Decision | Source & rationale |
|---|---|---|
| Base plan | **Agent A (Simplicity)** as backbone | matches the goal's "lite" positioning; fewest concepts, stdlib-first |
| Concurrency model | mutex-guarded registry (RWMutex) + one watcher goroutine per service | B's analysis: state transitions ≪1k/s even at 100 services; actor model adds deadlock surface for zero gain; A and C agree |
| Adoption from B | non-blocking eventbus + resync marker · exactly-once `cmd.Wait` pattern · snapshot-outside-lock · gzip middleware · log-file rotation · WS-first console data flow · `go test -race` + `goleak` | correctness insurance at bounded complexity; B itself marked the 100-service soak as non-gating |
| Adoption from C | **spike-first ordering** (WS-through-Caddy proven before console code) · dual-mode portal (gateway vs direct :3000) · offline-fallback portal · eslint containment line · PID file + `dev-clean.sh` · hermetic `testsvc` binary · `Setpgid`/`Pdeathsig` child containment | de-risks the two live systems we must not break: the user-visible Next.js origin and the shared sandbox |
| WS library | **`coder/websocket`** (ex-nhooyr) | ctx-native, actively maintained, auto ping handling — less deadline boilerplate; B explicitly neutral (both fine at ≤3 console clients) |
| SDK command transport | **long-poll `?wait=5s`** | frozen gate-2 evidence names "command long-poll"; wait 5s < global WriteTimeout 15s → B's `ResponseController` deadline complexity is unnecessary; C's "defer long-poll" would soften a gate |
| Static console serving | **disk `-static ../frontend/dist`** + B's gzip middleware | NOT `go:embed` (B's variant): embed forces a Go rebuild per frontend change; disk + `vite build --watch` keeps the dev loop tight; gzip still cuts the ~1 MB antd bundle to ~300 KB on the wire |
| Router | **HashRouter** | zero SPA-fallback server logic, no gateway deep-link edge cases; hash invisible inside the iframe (A) |
| Polling strategy | WS-primary (`setQueryData` patch, `resync`→`invalidateQueries`), poll 2s **only while disconnected**, 30s WS watchdog | B's design, hardened with a watchdog so gate-4's "no manual refresh" never rides on one transport; C's polling-only console is the pre-designed contingency if the T2 spike fails |
| Frontend lane | runs **alongside** the Go mainline in Step 2 | bench table: Frontend Engineer "plugs into Step 2 alongside Engineer"; each role keeps WIP-1 |
| Git baseline | **new task T1b: initial commit of the uncommitted scaffold** | verified: repo has `.git` but the entire tree is uncommitted — conventional commits as gate evidence need a baseline |

---

## 1. Architecture overview

```
                       ┌────────────────────────────────────────────────┐
 Browser (user)        │  Sandbox, single external origin via Caddy :81 │
   │                  │                                                │
   │  https://ext/    │   no query  ─────────────►  Next.js dev :3000  │
   │                  │   ?XTransformPort=P ─────►  localhost:P        │
   ▼                  │   (pages, REST, and WS upgrades all route)     │
┌──────────────┐      └────────────────────────────────────────────────┘
│ Next.js `/`  │           iframe src: /?XTransformPort=8800        (gateway mode)
│  portal      │─────────► direct mode (localhost:3000): iframe → http://localhost:8800/
│ (page.tsx)   │
└──────────────┘
                     ┌─────────────────────────────────────────────────┐
                     │  launchd daemon — Go, 127.0.0.1:8800            │
                     │  ┌──────────┐ ┌───────────┐ ┌────────────────┐  │
                     │  │ registry │ │ supervisor│ │ api (net/http) │  │
                     │  │ RWMutex  │ │ watcher/  │ │ REST /api/v1   │  │
                     │  │ state +  │ │ svc +     │ │ agent /agent/v1│  │
                     │  │ state.json│ │ restart   │ │ WS /api/v1/    │  │
                     │  └────┬─────┘ │ engine    │ │ events         │  │
                     │       │       └─────┬─────┘ │ + static dist + │  │
                     │  eventbus ◄──────────┘      │ gzip            │  │
                     │  (non-blocking, resync)     └────────────────┘  │
                     └───────────────┬─────────────────────────────────┘
                                     │ exec.Command(argv, Setpgid, Pdeathsig)
                          env: LAUNCHD_API_ADDR, LAUNCHD_SERVICE_NAME,
                               LAUNCHD_SESSION_TOKEN (per-spawn, 32-hex)
                    ┌────────────────┴────────────────┐
                    ▼                                 ▼
          launch-daemon/examples/           launch-daemon/daemon/internal/testsvc
          echo-service (:8901, /healthz)    (hermetic env-driven test binary:
          ticker-service (CRASH_AFTER)       sleep/crash/ignore-SIGTERM modes)
                    │
                    │ both import  launchd/pkg/launchsdk  (SOURCE-LEVEL INTEGRATION)
                    │ self-register → heartbeat 5s → long-poll commands → graceful exit
                    └─ 15s daemon-loss → SDK self-exits (orphan containment)
```

**Supervision model (lite launchd):** states `registered → starting → running → stopping → stopped | failed → restarting → backoff`. KeepAlive `always | on-failure | never`; backoff 500 ms ×2 cap 30 s; 30 s stable-run window resets failure count; max 5 consecutive failures → `failed`. RunAtLoad starts specs at daemon boot. Graceful stop: SIGTERM → `StopTimeoutSecs` (default 10) → SIGKILL to process group. Per-service logs: 1000-line ring + file (10 MB rotation, keep `.1`). Orphan registrations (unknown session token) → 403 (ADR-001).

## 2. Repo layout (all new files under `launch-daemon/` + one host-app file)

```
launch-daemon/
├── daemon/                      # Go module `launchd`
│   ├── go.mod                   # deps: coder/websocket; test: go.uber.org/goleak
│   ├── cmd/launchd/main.go      # daemon: flags -addr -state -logs -static -token; PID file
│   ├── cmd/launchdctl/main.go   # CLI: list|status|start|stop|restart|register|unregister|log|shutdown
│   ├── internal/registry/       # state.go registry.go persist.go + tests
│   ├── internal/eventbus/       # eventbus.go + tests (non-blocking, resync)
│   ├── internal/supervisor/     # supervisor.go spawn.go watcher.go restart.go logs.go + tests
│   ├── internal/api/            # server.go services.go agent.go auth.go ws.go static.go gzip.go + tests
│   ├── internal/client/         # Go API client shared by CLI + tests
│   ├── internal/testsvc/main.go # hermetic test service (env-driven modes)
│   ├── pkg/launchsdk/           # PUBLIC SDK: sdk.go client.go example_test.go
│   └── e2e/                     # lifecycle_test.go sdk_test.go crash_test.go helpers_test.go
├── examples/                    # Go module; require launchd v0.0.0 + replace => ../daemon
│   ├── go.mod
│   ├── echo-service/main.go     # HTTP :8901 + /healthz + SDK
│   └── ticker-service/main.go   # 1s ticks + CRASH_AFTER + SDK
├── frontend/                    # Vite + React + antd (latest, exact pins, bun.lock committed)
│   ├── package.json vite.config.ts tsconfig.json index.html
│   └── src/ (main.tsx App.tsx api/client.ts hooks/useEvents.ts
│             pages/Services.tsx pages/ServiceDetail.tsx
│             components/LogViewer.tsx components/StatusBadge.tsx)
├── scripts/ bootstrap-go.sh dev-clean.sh wsprobe.html
├── docs/adr/ADR-001-source-level-integration.md
├── run/                         # gitignored: state.json launchd.pid logs/
├── Makefile README.md .gitignore
src/app/page.tsx                 # HOST APP — only file touched (portal swap)
eslint.config.mjs                # HOST APP — +1 ignore line "launch-daemon/**"
docs/as-goals/go-launch-daemon/1/evidence/   # browser transcripts + screenshots
```

## 3. Ports (fixed — no PORT env)

| Component | Port | Bind | Note |
|---|---|---|---|
| launchd control API | **8800** | 127.0.0.1 | REST + WS + console static (verified free) |
| echo-service | **8901** | 127.0.0.1 | example service HTTP |
| vite dev | 5173 | localhost | direct local dev ONLY (never through gateway; no HMR via gateway) |
| Next.js host | 3000 | — | stays running, untouched; Turbopack hot-reloads the portal page |
| e2e tests | `127.0.0.1:0` | ephemeral | tests never bind 8800 |

## 4. Frozen API contract (consumed by console, CLI, SDK, portal)

**Operator REST** (Bearer token when `-token`/`LAUNCHD_TOKEN` set; else open on loopback):
- `GET /api/v1/health` → `{version, uptimeSec, services:{running,failed,total}, goroutines}`
- `GET /api/v1/services` → snapshot list
- `POST /api/v1/services` → register `{name, argv[], env[], keepAlive, runAtLoad, stopTimeoutSecs, maxRetries}` (201; name `^[a-z0-9-]{1,63}$` else 400; body >1MB → 413; env `KEY=VALUE`)
- `GET /api/v1/services/{name}` → spec + status + log tail
- `DELETE /api/v1/services/{name}` (409 if running)
- `POST /api/v1/services/{name}/start|stop|restart` → 202
- `GET /api/v1/services/{name}/logs?tail=200`

**Agent REST** (session token from spawn env):
- `POST /agent/v1/register` `{pid, version, meta}` → 200 / **403 orphan**
- `POST /agent/v1/heartbeat` `{status: ok|degraded, detail}`
- `GET /agent/v1/commands?wait=5` → `{commands:[{type:"shutdown"}]}` (long-poll ≤5s)

**Events WS:** `GET /api/v1/events` (upgrade; token via `?token=` when auth on). Frames: `{"type":"state",...}` `{"type":"log","name","line"}` `{"type":"resync"}`.

**SDK env contract:** `LAUNCHD_API_ADDR`, `LAUNCHD_SERVICE_NAME`, `LAUNCHD_SESSION_TOKEN` — injected by the supervisor at spawn. SDK absent-env → `ErrNotSupervised` (binary still runnable standalone).

---

## 5. Ordered tasks

> Each task: files (≤5) · acceptance criteria (AC) · verification (V) · gates fed · rollback. Engineer works the Go mainline T0→T11 one task at a time (WIP-1); Frontend Engineer runs the T12→T13 lane alongside (WIP-1 per role). Evidence dir: `docs/as-goals/go-launch-daemon/1/evidence/`.

### T0 — Go toolchain bootstrap + pre-flight
**Files:** `launch-daemon/scripts/bootstrap-go.sh` (only repo artifact; installs to `/usr/local/go`, PATH in `~/.bashrc`)
- **AC:** idempotent (re-run = no-op); `go version` ≥1.24 (latest stable tarball w/ sha256 from `go.dev/dl/?mode=json`; fallback `apt-get install golang-go`); pre-flight asserts 8800/8901/5173 free (verified free this session) and fails loudly otherwise
- **V:** run script twice; `go version && go build` a /tmp hello-world
- **Gates:** prerequisite for all · **Rollback:** `rm -rf /usr/local/go` + one PATH line

### T1 — Module skeletons + daemon health skeleton + git baseline
**Files:** `daemon/go.mod`, `daemon/cmd/launchd/main.go`, `daemon/internal/api/server.go` (health only), `launch-daemon/.gitignore` (run/, frontend/node_modules, dist)
- **AC:** module `launchd` builds (`go build ./...`, `go vet` clean); daemon binds `127.0.0.1:8800` (`-addr` override), `GET /api/v1/health` → 200 JSON; writes `run/launchd.pid`; SIGTERM → clean exit + pid removed; second start fails fast on bind; **T1b: initial commit of the whole uncommitted scaffold** (`chore: baseline scaffold before go-launch-daemon`) so later commits are diffable evidence
- **V:** curl health; `kill -TERM $(cat run/launchd.pid)`; httptest health test; `git log --oneline` shows baseline
- **Gates:** gate-5 artifact 1 · **Rollback:** delete subtree; `git checkout` baseline

### T2 — SPIKE: prove REST + WS through Caddy BEFORE any console code
**Files:** `daemon/internal/api/ws.go` (echo WS at `/api/v1/events`), `launch-daemon/scripts/wsprobe.html`
- **AC:** agent-browser opens probe page **through Caddy** (`http://localhost:81/?XTransformPort=8800`): in-page `fetch('/api/v1/health?XTransformPort=8800')` returns JSON; `new WebSocket((protocol)+'//'+host+'/api/v1/events?XTransformPort=8800')` connects and echo round-trips <2s; transcript → `1/evidence/ws-spike.md`; **verdict recorded: WS-via-gateway or polling-only contingency**
- **V:** agent-browser evaluate + console transcript; also verify `http://localhost:81` without outer TLS (isolates Caddy from outer TLS layer)
- **Gates:** de-risks gate-4 · **Contingency:** if WS fails → console ships polling-only (2s; "no manual refresh" still holds); escalate to synthesis for gate wording. **Rollback:** delete ws.go — nothing depends on it yet

### T3 — Registry: state machine + persistence
**Files:** `daemon/internal/registry/state.go`, `registry.go`, `persist.go`, `registry_test.go`
- **AC:** `Spec{Name, Argv, Env, KeepAlive, RunAtLoad, StopTimeoutSecs, MaxRetries}`; `Status{State, Pid, StartedAt, Restarts, LastExitCode, LastExitAt, HeartbeatAt, HeartbeatStatus, LastError}`; name regex enforced; all mutations via narrow methods (`Transition`, `TouchHeartbeat`); `Snapshot()` copies under RLock, JSON encode outside lock; persistence = single-writer goroutine, 200 ms debounce, tmp+rename atomic, monotonic `seq`; corrupt load → quarantine `.bad` + cold start + log line; runtime status ephemeral by design; RunAtLoad specs flagged for boot
- **V:** `go test -race ./internal/registry/` — 8 goroutines × 1000 transitions + concurrent snapshots, no illegal transition, no torn snapshot
- **Gates:** gate-1, gate-5 (name validation) · **Rollback:** package-local

### T4 — Eventbus (foundation for logs + WS)
**Files:** `daemon/internal/eventbus/eventbus.go`, `eventbus_test.go`
- **AC:** `Publish` non-blocking (select/default), event built once (shared immutable); subscriber = buffered chan 128 + drop counter; after drops, next send preceded by synthetic `resync`; topic filter (state events vs per-service log events); `Close()` releases subscribers, no leaks
- **V:** `go test -race`: 10k publishes vs never-draining subscriber → publisher returns <100 ms, drop count exact, drained subscriber sees resync
- **Gates:** gate-1, gate-4 · **Rollback:** package-local

### T5 — Supervisor core + log capture
**Files:** `daemon/internal/supervisor/supervisor.go`, `spawn.go`, `watcher.go`, `logs.go`, `daemon/internal/testsvc/main.go`
- **AC:** spawn `exec.Command(argv...)` (argv-only, no shell — grep-able) + `Setpgid` + best-effort `Pdeathsig` + env injection (API addr, service name, 32-hex session token, in-memory only); **one watcher goroutine per service; exactly-once `cmd.Wait`** (inner goroutine → buffered `waitCh` cap 1); single `select` over waitCh/stop/ctx — never polls; graceful stop = SIGTERM → timeout → **SIGKILL to process group**; `cmd.Stdout/Stderr` = `lineWriter`s (line split, 4 KB/line cap) → preallocated 1000-line ring + file via 64 KB bufio flush ≤500 ms + **10 MB rotation keep `.1`** + non-blocking publish to bus; state transitions published after releasing registry lock; daemon `Shutdown` cancels root ctx, stops all, WaitGroup returns — `goleak` clean; testsvc modes: `sleep`, `crash_after`, `ignore_sigterm`, `sdk` (hermetic e2e foundation)
- **V:** `go test -race ./internal/supervisor/` — transitions, kill escalation, log capture via testsvc, 100 start/stop cycles, goleak.VerifyTestMain
- **Gates:** gate-1 artifact 1, gate-3 artifacts 3/4, gate-5 artifact 4 · **Rollback:** package-local

### T6 — Restart-policy engine
**Files:** `daemon/internal/supervisor/restart.go`, `restart_test.go`
- **AC:** backoff `min(500ms·2^n, 30s)` via interruptible Timer+select (daemon shutdown cuts pending backoff); 30 s stable window resets failure count; max 5 consecutive → `failed`; policies always/on-failure/never (manual-stop intent suppresses keepalive); injectable constants (tests shrink them); monotonic clock
- **V:** unit tests: delay sequence 500ms/1s/2s/4s/8s→failed; stable-reset case; never-policy case
- **Gates:** gate-3 artifact 1 · **Rollback:** package-local

### T7 — Operator REST API + validation + auth
**Files:** `daemon/internal/api/services.go`, `auth.go`, `validation_test.go` (+ `auth_test.go`)
- **AC:** Go ≥1.22 `ServeMux` method patterns for all §4 operator routes; `http.Server{ReadHeaderTimeout:5s, IdleTimeout:60s, WriteTimeout:15s}`; handlers encode from `Snapshot()` copies (lock never held during encode); `MaxBytesReader` 1MB → 413; name/env validation → 400 (traversal `../x`, uppercase, spaces, >63, empty; env must be `KEY=VALUE`); bearer middleware default OFF, on → 401 wrong/missing, 200 valid, **WS handshake checks `?token=`**; RunAtLoad specs started at boot; 409 unregister-while-running
- **V:** `go test -race ./internal/api/` — httptest full register→start→status→stop→restart sequence with PID/exit/restart asserts; 401/200 matrix; 400/413 validation matrix
- **Gates:** gate-1 artifact 2, gate-5 artifacts 2/3 · **Rollback:** file-local

### T8 — Agent API + public SDK `launchsdk`
**Files:** `daemon/internal/api/agent.go`, `daemon/pkg/launchsdk/sdk.go`, `client.go`, `example_test.go`
- **AC:** agent routes per §4; session-token verification (unknown → 403, ADR-001 orphan rejection); long-poll parks ≤5s, released early on command, context-cancelled on client drop; SDK: `launchsdk.New(name, service)` reads env (absent → `ErrNotSupervised`, binary runnable standalone); **one shared `http.Client`** (keep-alive, dial 2s/req 10s/long-poll 35s); heartbeat 5s ticker `{status, detail}`; command long-poll `?wait=5`; `shutdown` → cancel service ctx → graceful return → exit 0; daemon unreachable → retry backoff, **self-exit after 15 s consecutive loss** (orphan containment); `Close()` one ctx + WaitGroup, goleak clean; godoc + runnable `Example()`
- **V:** `go test -race ./pkg/launchsdk/ ./internal/api/` vs httptest agent server; assert single-connection reuse; graceful-exit-within-window test
- **Gates:** gate-2 artifacts 1/2 · **Rollback:** pkg/ removal (no internal consumers yet besides examples)

### T9 — Example services + ADR-001
**Files:** `examples/go.mod` (`require launchd v0.0.0` + `replace launchd => ../daemon`), `examples/echo-service/main.go`, `examples/ticker-service/main.go`, `launch-daemon/docs/adr/ADR-001-source-level-integration.md`
- **AC:** both import `launchd/pkg/launchsdk` (module-import proof of source-level integration); echo-service: HTTP `127.0.0.1:8901` + `/healthz`, graceful `http.Server.Shutdown` on SDK ctx; ticker-service: 1 s tick logs, `CRASH_AFTER=<sec>` → `os.Exit(1)`; ADR-001 records: SDK-over-config model, session handshake, **orphan rejection**, SDK self-exit, rejected alternatives (config files, auto-adopt, SSE)
- **V:** `cd examples && go build ./...`; register both via curl → SDK-reported health visible
- **Gates:** gate-2 artifacts 3/4/6 · **Rollback:** subtree-local

### T10 — e2e suite (real processes, hermetic)
**Files:** `daemon/e2e/lifecycle_test.go`, `sdk_test.go`, `crash_test.go`, `helpers_test.go`
- **AC:** build testsvc + example binaries to `t.TempDir()`; daemon at `127.0.0.1:0`; **lifecycle:** register→start→`running`+PID→stop→`stopped`+exit code→restart (gate-1 artifact 3); **sdk:** heartbeat observed → shutdown → exit 0 in window (gate-2 artifact 5); **crash:** `CRASH_AFTER=1` keepalive=always → SIGKILL exit → backoff restart visible → cap 5 → `failed`; never-policy: no respawn (gate-3 artifact 2); poll-based assertions (200 ms polls, 10–30 s budgets, **no bare sleeps**), serial `-p 1`, `-race`, goleak at exit
- **V:** `cd launch-daemon/daemon && go test ./... -count=1` green
- **Gates:** gate-1/2/3 closing evidence · **Rollback:** test-local

### T11 — `launchdctl` CLI
**Files:** `daemon/cmd/launchdctl/main.go`, `daemon/internal/client/client.go`
- **AC:** stdlib `flag` + switch (no cobra): `list|status|start|stop|restart|register|unregister|log -n|shutdown` against `LAUNCHD_ADDR` (default 127.0.0.1:8800) + `LAUNCHD_TOKEN`; `list` table via `text/tabwriter` (PID · Status · LastExit · Name — launchctl-style); non-zero exit on API error; `shutdown` = stop services THEN daemon (PID file removed)
- **V:** `go vet ./cmd/...`; CLI smoke vs live daemon (commands recorded in README)
- **Gates:** goal success criterion 6; feeds gate-1/2/3 operator evidence · **Rollback:** standalone binary

### T12 — Frontend console (Vite + React + antd latest)
**Files:** `frontend/package.json` (+ `vite.config.ts`, `index.html`, `tsconfig.json`), `frontend/src/api/client.ts`, `frontend/src/hooks/useEvents.ts`, `frontend/src/pages/Services.tsx`, `frontend/src/pages/ServiceDetail.tsx` (+ `main.tsx`, `App.tsx`, `components/LogViewer.tsx`, `StatusBadge.tsx`)
- **AC:** deps exact-pinned at install: `react`, `react-dom`, `antd`, `react-router`, `@tanstack/react-query`, `dayjs` + **`@ant-design/v5-patch-for-react-19` iff antd@5 is the latest major** (implementer checks `bun pm view antd version` first); dev: `vite`, `@vitejs/plugin-react`, `typescript`, `@types/*`; **bun install ONLY inside `launch-daemon/frontend/`** (root package.json untouched); `bun.lock` committed; actual versions recorded in README; **every request via one `api()` helper appending `XTransformPort=8800` — zero absolute URLs (grep-able)**; WS = `${location.protocol}//${location.host}/api/v1/events?XTransformPort=8800` (works in both iframe contexts: gateway-routed and direct :8800); state events patch cache via `queryClient.setQueryData`, `resync` → `invalidateQueries`, reconnect backoff 1s→16s + jitter, **poll 2 s only while disconnected**, 30 s WS watchdog → reconnect+invalidate; **HashRouter** routes `/` + `/services/:name`; Services page: antd Table (status Badge, PID, uptime, restarts, last exit, actions with Popconfirm + message feedback); Detail page: Descriptions (spec/policy) + LogViewer (150 ms batched flush, 500-row cap + "N skipped", autoscroll/pause); daemon health in header; token input (localStorage) when auth on; loading Skeletons, error message feedback, long lists `max-h-* overflow-y-auto`; responsive 375px→desktop
- **V:** `bun run build` in frontend dir → dist/ zero errors; agent-browser through `http://localhost:81/?XTransformPort=8800`: table renders live services (direct daemon-served static once T13 wires it — until then vite preview for dev)
- **Gates:** gate-4 artifacts 1/2/3 · **Rollback:** delete `frontend/`; root untouched

### T13 — Daemon static serving + gzip + Next.js portal
**Files:** `daemon/internal/api/static.go`, `gzip.go`, `src/app/page.tsx`, `eslint.config.mjs` (+1 ignore line `"launch-daemon/**"`)
- **AC:** static: disk serving from `-static ../frontend/dist` (missing → JSON 404, API-only mode); `Cache-Control: no-store` on `index.html`; hashed assets `public, max-age=31536000, immutable`; gzip middleware (stdlib `compress/gzip` + `sync.Pool`, ≥1 KB only, respects Accept-Encoding, skips WS/hijacked) — antd bundle ≥60% smaller on the wire; **portal:** dual-mode — gateway mode (port ≠3000) iframe `/?XTransformPort=8800` + relative health probe; direct mode (`:3000`) iframe `http://localhost:8800/` + `no-cors` probe (resolves = reachable); 401 counts as online; slim 48px header (zinc/emerald-amber status dot — **no blue/indigo**); iframe `calc(100dvh - 48px)`, no body scroll, no footer by design (full-viewport embedded app); daemon-down → offline panel with start command + retry + 5 s auto re-probe; **recursion guard** (`window.self !== window.top` → notice); `bun run lint` CLEAN at root; dev server NOT restarted (Turbopack hot-reload — confirm `GET / 200` in dev.log after edit)
- **V:** `curl -H 'Accept-Encoding: gzip'` via `:81` → `Content-Encoding: gzip`; agent-browser at `http://localhost:81/` → portal → console; stop daemon → offline panel; restart → console returns; `bun run lint` → 0 errors
- **Gates:** gate-4 artifacts 4/6 · **Rollback:** `git checkout src/app/page.tsx eslint.config.mjs`; delete static.go

### T14 — Browser e2e evidence + docs + final green sweep
**Files:** `docs/as-goals/go-launch-daemon/1/evidence/*` (transcripts, screenshots), `launch-daemon/README.md`, `launch-daemon/scripts/dev-clean.sh`, `launch-daemon/Makefile`
- **AC:** agent-browser golden path at **`http://localhost:81/`** (real Caddy path) desktop ≥1280px + mobile 375px: portal loads console → live table → **stop service via UI → status changes WITHOUT manual refresh** → logs stream live → restart service; plus portal check at `http://localhost:3000/` (direct mode); dev.log cross-checked (no API/WS/hydration errors); final captures: `go test ./...` (daemon) green, `go build ./examples/...`, `bun run lint` green, working tree clean; README: quickstart, ports table, run commands, actual dependency versions, dev loop (`vite build --watch` + refresh); dev-clean.sh kills stale daemon/services by PID file + name pattern; Makefile: build/run/test/clean
- **V:** Test Engineer per gate-4 method; evidence files linked in manifest
- **Gates:** all 5 closing evidence · **Rollback:** docs/evidence only

### T15 — STRETCH (non-gating, `-short`-skippable): resilience soak
**Files:** `daemon/e2e/stress_test.go`, `daemon/e2e/testdata/{sleeper,spammer}/main.go`
- **AC:** 100 sleeper services → all running, daemon RSS <~100 MB, list p95 <100 ms; spammer 2000 lines/s ×5 s → ring holds cap, rotation works, slow WS client evicted, supervision never starved; shutdown → zero residual goroutines (goleak) + zero residual children
- **V:** `go test -race ./e2e/ -run Stress -timeout 300s` — recorded as headroom evidence, explicitly non-gating
- **Gates:** robustness evidence only · **Defer without blocking DONE** — flagged to user as stretch

---

## 6. Dependency graph

```
T0 ─► T1 ─► T2 (spike verdict ────► gates T12 WS-or-polling variant)
        └► T3 ─► T4 ─► T5 ─► T6 ─► T7 ─► T8 ─► T9 ─► T10 ─► T11   [Engineer lane, WIP-1]
                     (T2 verdict + T7 API contract) ─► T12 ─► T13 ─► T14   [Frontend lane, WIP-1]
T15 stretch, after T10, never blocks
External: go.dev tarball · npm registry · Caddy :81 frozen · Next :3000 stays up · agent-browser
```

## 7. Risk register (merged, top 12)

| # | Risk | Mitigation |
|---|---|---|
| R1 | Go bootstrap fails (tarball+apt) | dual-source idempotent script; fail fast; escalate (network-failure exception) |
| R2 | WS through Caddy fails first try | **T2 spike before console code**; polling-only contingency pre-designed; verdict artifact |
| R3 | Portal edit breaks live user-visible app | single-file swap + recursion guard + offline fallback + lint clean + Turbopack hot-reload (no restart); rollback = 2 files |
| R4 | antd v5 × React 19 incompat | conditional `@ant-design/v5-patch-for-react-19`, exact pins, lockfile committed, early render smoke |
| R5 | e2e flakiness | poll-based asserts 10–30 s, ephemeral `:0` ports, `CRASH_AFTER` determinism, serial `-p 1`, injectable backoff constants |
| R6 | zombies/orphans on dev restarts | exactly-once Wait · Setpgid + group SIGKILL · Pdeathsig best-effort · SDK 15 s self-exit · PID file · `launchdctl shutdown` · dev-clean.sh |
| R7 | state.json corruption | atomic tmp+rename; `.bad` quarantine + cold start; runtime state ephemeral by design |
| R8 | log disk exhaustion (shared sandbox) | 10 MB rotation keep `.1` + 1000-line ring + 4 KB line cap |
| R9 | slow WS client backpressures supervision | non-blocking bus publish (select/default) + WS write deadlines + resync marker |
| R10 | registry lock held during encode/IO | Snapshot copies under RLock, encode outside; single-writer persistence goroutine |
| R11 | "latest npm" drift | install-time resolution, exact pins, lockfile, versions recorded in README |
| R12 | ambient eslint/lint contamination | `launch-daemon/**` ignore line; root package.json never modified; bun install only in frontend dir |

## 8. Critical files

1. `launch-daemon/daemon/internal/supervisor/supervisor.go` (+ `watcher.go`) — every gate-1/3 behavior; exactly-once Wait; leak-free shutdown
2. `launch-daemon/daemon/internal/api/server.go` — frozen §4 contract shared by console, CLI, SDK, portal
3. `launch-daemon/daemon/pkg/launchsdk/sdk.go` — the source-level integration contract (the goal's core claim)
4. `launch-daemon/frontend/src/api/client.ts` (+ `hooks/useEvents.ts`) — gateway-aware networking, WS + fallback
5. `src/app/page.tsx` — the only host-app touch; user-visible entry

## 9. Rejected alternatives (decision record)

1. **Single-binary embedding** (services compiled into the daemon as goroutines) — loses process isolation and crash semantics; contradicts "manage the lifecycle of other services".
2. **Config-file integration** (generating launchd plists / systemd units) — contradicts the source-level-integration requirement; parsing is explicitly out of scope.
3. **Console inside the host Next.js app** — user fixed the console stack to Vite+React+antd; Next.js stays a thin portal.
4. **`go:embed` of the console dist** — forces a Go rebuild per frontend change; disk `-static` + `vite build --watch` keeps the loop tight (B's gzip middleware retained).
5. **Actor/channel registry** — mutex + per-service watchers: transitions are per-lifetime events; channels add deadlock surface for zero measurable gain (B's analysis).
6. **SSE / EventSource** — arguably simpler than WS, but frozen gate-4 evidence names a WebSocket endpoint; switching would soften an immutable gate.
7. **socket.io for events** — extra client+server protocol; Caddy query routing is already proven for plain WS (T2 spike + gateway's own socket.io examples).
8. **Orphan auto-adoption** — without spawn ownership KeepAlive cannot be guaranteed; rejected (ADR-001, open question Q3).
9. **Deferred long-poll (v1 short-poll)** — C's cut; frozen gate-2 evidence names "command long-poll"; `wait=5s` satisfies it without ResponseController complexity.

## 10. Task → gate mapping

| Gate | Tasks | Closing evidence |
|---|---|---|
| gate-1 lifecycle-control | T3, T5, T7, T10, T11 | api tests + e2e lifecycle + green `go test ./...` |
| gate-2 sdk-integration | T8, T9, T10 | SDK + examples + sdk e2e + ADR-001 |
| gate-3 supervision-resilience | T5, T6, T10 | restart engine tests + crash e2e + logs tests |
| gate-4 web-dashboard | T2, T12, T13, T14 | console source + portal + agent-browser transcripts (desktop+mobile) |
| gate-5 secure-api | T1, T7, T9(no-token-leak), T14 | auth/validation tests + loopback default + argv-only spawn proof |

## 11. Deferred to iteration 2+ (explicit non-goals for iteration 1)

Long-poll >5 s with ResponseController · log search/filter, multi-service streaming UI · restart-on-unhealthy (v1 restart is exit-driven; heartbeat is informational) · auto-resume of non-RunAtLoad services after daemon crash (predictable `registered`) · force-unregister running services · antd theming beyond one ConfigProvider token · WS log subscription multiplexing per service (v1 broadcasts with client-side filter) · T15 soak if budget pressure hits.

## 12. Open questions for user review (beyond goal-file Q1–Q3)

- **Q4:** T15 soak is stretch/non-gating — acceptable to defer if iteration budget tightens?
- **Q5:** HashRouter (hash URLs inside the iframe) — acceptable UX trade for zero server fallback logic?
- **Q6:** antd v5 + React 19 via `@ant-design/v5-patch-for-react-19` (conditional) — confirm approach, or pin React 18 instead? *(plan assumes latest of everything per the goal)*

---

*Self-contained per the multi-agent-planning Output Contract: a downstream executor can implement from this document + the frozen gates without re-exploring the repo. Plan-approval gates are auto-approved by the as-goal autonomous caller context — except this run, where the user explicitly reserved the approval decision at this stop point.*
