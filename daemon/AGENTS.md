# Daemon Module

The `daemon/` directory contains the Go module `launchd`: the supervisor daemon, the operator CLI, and the public SDK.

## Architecture

```
cmd/
  launchd        — daemon binary and process entry point
  launchdctl     — operator CLI (launchctl style)
internal/
  registry       — service specs, runtime status, state machine, persistence
  supervisor     — process lifecycle, restart engine, log capture
  eventbus       — non-blocking pub/sub for state and log events
  api            — HTTP server: operator API, agent API, static files
  client         — Go client for the operator API
  testsvc        — test service binaries
pkg/
  launchsdk      — public SDK for supervised services
e2e/             — end-to-end tests
```

## Package Details

### cmd/launchd
* Flags: `-addr` (default `127.0.0.1:8800`), `-state`, `-logs`, `-static`, `-token`.
* Startup order: registry, eventbus, supervisor, API server. Then the RunAtLoad services start.
* Shutdown order: supervisor first, then the HTTP server with a 15 s timeout.
* The daemon writes a PID file at `run/launchd.pid`.

### cmd/launchdctl
* Commands: `list`, `status`, `start`, `stop`, `restart`, `register`, `unregister`.
* Environment: `LAUNCHD_ADDR` (default `127.0.0.1:8800`), `LAUNCHD_TOKEN`.
* The CLI uses `internal/client` with a 10 s HTTP timeout.

### internal/registry
* One `sync.RWMutex` guards all entries (ADR-002).
* States: `registered`, `starting`, `running`, `stopping`, `stopped`, `failed`, `restarting`, `backoff`.
* Name rules: 1–63 characters, lowercase alphanumeric and hyphens.
* Register defaults: KeepAlive `never`, StopTimeoutSecs 10, MaxRetries 5.
* Persistence: JSON file, 200 ms debounce, tmp file plus rename. The daemon quarantines a corrupt state file and starts cold.

### internal/supervisor
* Each service runs in its own process group (`Setpgid`, ADR-005).
* Spawn injects `LAUNCHD_API_ADDR`, `LAUNCHD_SERVICE_NAME`, and `LAUNCHD_SESSION_TOKEN` (32-byte hex, memory only).
* Stop: send SIGTERM to the process group. Escalate to SIGKILL after `StopTimeoutSecs`.
* Restart engine: backoff from 500 ms to 30 s. A stable run of 30 s resets the failure count. The service enters `failed` after 5 consecutive failures.
* Logs: 1000-line ring buffer, 4 KB line limit. Files rotate at 10 MB and keep one backup.

### internal/eventbus
* Publish never blocks (ADR-004). A full subscriber channel drops the event.
* The subscriber drop counter increments on each drop.
* The next successful send prepends a synthetic `resync` event.
* Topics: `state` and `log`. Subscriber buffer size: 128.

### internal/api

Operator routes:

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/v1/health` | Version, uptime, service counts, goroutines |
| GET | `/api/v1/services` | List all services |
| POST | `/api/v1/services` | Register a service |
| GET | `/api/v1/services/{name}` | Service detail plus log tail |
| DELETE | `/api/v1/services/{name}` | Unregister a service |
| POST | `/api/v1/services/{name}/start` | Start a service |
| POST | `/api/v1/services/{name}/stop` | Stop a service |
| POST | `/api/v1/services/{name}/restart` | Restart a service |
| GET | `/api/v1/services/{name}/logs` | Log tail |

Agent routes:

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/agent/v1/register` | Session handshake (ADR-001) |
| POST | `/agent/v1/heartbeat` | Heartbeat with status |
| GET | `/agent/v1/commands` | Long-poll for commands (ADR-009) |

* Go 1.22 pattern routing on `http.ServeMux`.
* Middleware: gzip compression and optional bearer auth (ADR-008). Gzip skips WebSocket upgrades.
* Static files: disk-based with SPA fallback (ADR-003, ADR-006). `index.html` uses `no-store`. Hashed assets use `immutable`.
* The request body limit is 1 MB.

### internal/client
* Small Go client for the operator API. `launchdctl` uses it.

### pkg/launchsdk
* The only public package of the module (ADR-001).
* `New()` reads the environment contract. It returns `ErrNotSupervised` when the variables are absent.
* `Start()` registers with the agent API. It then starts the heartbeat loop (5 s) and the command long-poll loop.
* `Context()` returns a context. The SDK cancels it when the daemon sends a shutdown command.
* `Close()` stops the goroutines.

### e2e/
* The tests build real binaries and test full lifecycles on `127.0.0.1:0`.
* The suite builds `internal/testsvc` and `examples/echo-service`.

## Known Gaps

* The WebSocket endpoint `/api/v1/events` (ADR-007) is not implemented. The console falls back to polling.
* `Server.SendShutdown` has no caller yet. The stop path uses SIGTERM directly. The SDK shutdown command is never delivered.
* `authMiddleware` is defined but not applied to the operator routes. The `-token` flag has no effect today (ADR-008).

## Module Rules

1. Keep `internal/` private. New public API belongs in `pkg/launchsdk` and needs an ADR.
2. Access the registry only through `Registry` methods. Never touch the entry map directly.
3. Keep EventBus publish non-blocking. Never add a blocking send on the supervisor path.
4. Signal the process group (negative PID), not the child PID alone.
5. A route change needs updates to `internal/client`, `launchdctl`, and the console client in the same commit.
6. Run `go test ./...` in `daemon/` before a commit.
