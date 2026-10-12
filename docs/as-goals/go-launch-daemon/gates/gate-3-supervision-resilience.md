# Gate: supervision-resilience

## Condition
KeepAlive semantics match the lite-launchd model: a service that crashes (killed with SIGKILL) is automatically restarted per its policy (`always` / `on-failure` / `never`) with exponential backoff (base 500ms, doubling, capped 30s), backoff resets after a stable-run window, and max-retry protection transitions the service to `failed` instead of infinite restarting. Per-service stdout/stderr is captured both in-memory (ring buffer, capped) and on disk (log file). Graceful stop sends SIGTERM, waits the configured timeout, then SIGKILL.

## Evidence Required
- [ ] Artifact 1: restart-policy engine with backoff/reset/max-retry logic → `launch-daemon/daemon/internal/supervisor/restart.go` (+ `restart_test.go`)
- [ ] Artifact 2: crash-restart e2e: spawn example service with `CRASH_AFTER`, observe SIGKILL exit, observe automatic restart with backoff, observe `failed` after retry cap → `launch-daemon/daemon/e2e/crash_test.go`
- [ ] Artifact 3: log capture (ring buffer + file, per service) → `launch-daemon/daemon/internal/supervisor/logs.go` (+ `logs_test.go`) and runtime logs under `launch-daemon/run/logs/`
- [ ] Artifact 4: graceful-stop test: SIGTERM → clean exit 0 within window; unresponsive process → SIGKILL after timeout → `launch-daemon/daemon/internal/supervisor/supervisor_test.go`
- [ ] Artifact 5: `never`-policy test: crash → no restart, state `failed`/`stopped` per exit code semantics

## Verification Method
Test Engineer: `go test ./...` green including e2e crash tests (poll-based assertions with generous timeouts to avoid flakes); manually kill a supervised process (`kill -9 <pid>`) against the running daemon and confirm restart via API; confirm log file grows and `GET /api/v1/services/{name}/logs` returns captured lines.

## Owner
Senior Software Engineer (implementation evidence); System Test Engineer (verdict)
