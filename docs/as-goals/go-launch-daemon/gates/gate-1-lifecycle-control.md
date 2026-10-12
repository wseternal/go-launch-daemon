# Gate: lifecycle-control

## Condition
The daemon manages the full lifecycle of registered Go services: registration, start, stop, restart, and status reporting are all available over the control API, and reported state is accurate — the state machine (`registered → starting → running → stopping → stopped | failed → restarting → backoff`) reflects observable process reality (PID present when running, exit code recorded on stop/failure, restart count increments, uptime advances).

## Evidence Required
- [ ] Artifact 1: supervisor + registry implementation with state machine → `launch-daemon/daemon/internal/supervisor/`, `launch-daemon/daemon/internal/registry/`
- [ ] Artifact 2: API integration tests covering register → start → status → stop → restart with asserted state transitions, PID/exit-code/restart-count fields → `launch-daemon/daemon/internal/api/*_test.go`
- [ ] Artifact 3: end-to-end lifecycle test against a real spawned process (not a mock) → `launch-daemon/daemon/e2e/lifecycle_test.go`
- [ ] Artifact 4: green test-run output (`go test ./...` in `launch-daemon/daemon`) linked in the evidence manifest

## Verification Method
Test Engineer: check out the iteration's commits, run `go test ./...` inside `launch-daemon/daemon` — all lifecycle-related tests must pass; then exercise the running daemon over HTTP (register a service pointing at a built example binary, start it, observe `running` with a live PID, stop it, observe `stopped` with exit code, restart it). State mismatches = Fail.

## Owner
Senior Software Engineer (implementation evidence); System Test Engineer (verdict)
