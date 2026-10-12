# Gate: sdk-integration

## Condition
Source-code-level integration works as designed: a Go service imports the public SDK package, and when spawned by the daemon it self-registers, sends health heartbeats, receives the shutdown command, and exits gracefully (exit 0) within the graceful window — no config files involved. At least 2 example Go services prove the contract, and the integration model is recorded in ADR-001.

## Evidence Required
- [ ] Artifact 1: public SDK package with godoc and a runnable example → `launch-daemon/daemon/pkg/launchsdk/` (incl. `example_test.go`)
- [ ] Artifact 2: agent-side endpoints (register / heartbeat / command long-poll) + session-token injection via env → `launch-daemon/daemon/internal/api/` (agent routes)
- [ ] Artifact 3: example service 1 (HTTP echo service with `/healthz`) → `launch-daemon/examples/echo-service/`
- [ ] Artifact 4: example service 2 (ticker worker with `CRASH_AFTER` env for crash testing) → `launch-daemon/examples/ticker-service/`
- [ ] Artifact 5: SDK integration test: register → heartbeat observed → shutdown command → process exits 0 within timeout → `launch-daemon/daemon/e2e/sdk_test.go`
- [ ] Artifact 6: ADR-001 (source-level integration model; orphan services rejected in v1) → `launch-daemon/docs/adr/ADR-001-source-level-integration.md`

## Verification Method
Test Engineer: `go test ./...` green (SDK + e2e tests); read ADR-001 and confirm the implemented handshake matches it; build both example services (`go build ./examples/...`), register them with the daemon, confirm both appear with SDK-reported health status. Orphan-policy behavior documented and consistent.

## Owner
Senior Software Engineer (SDK + examples); Documentation Engineer (ADR-001); System Test Engineer (verdict)
