# Gate: secure-api

## Condition
The control plane is secure by default and hardened against the realistic threat surface of a process supervisor: the daemon binds loopback only by default; when token auth is configured every `/api/v1/*` route (including the WebSocket handshake and agent endpoints) rejects unauthenticated requests with 401; all registration inputs are validated (service name restricted to `[a-z0-9-]{1,63}` — used in file paths and log names, so traversal/injection must be impossible; argv-based spawn with NO shell invocation; payload size limits); logs and state files do not leak the token; stop timeouts and WS/log caps bound resource abuse.

## Evidence Required
- [ ] Artifact 1: default loopback bind (`127.0.0.1:8800`) with explicit `-addr` override documented → `launch-daemon/daemon/cmd/launchd/main.go` + README
- [ ] Artifact 2: bearer-token middleware + tests: 401 without token, 401 with wrong token, 200 with valid token, WS handshake rejected without token → `launch-daemon/daemon/internal/api/auth.go` (+ `auth_test.go`)
- [ ] Artifact 3: input-validation tests: invalid service names (traversal `../x`, spaces, uppercase, >63 chars, empty) rejected 400; oversized payloads rejected 413; env entries must be `KEY=VALUE` → `launch-daemon/daemon/internal/api/validation_test.go`
- [ ] Artifact 4: spawn safety: process creation uses `exec.Command` with argv array only — grep-able absence of `sh -c` / shell strings in the spawn path → `launch-daemon/daemon/internal/supervisor/spawn.go`
- [ ] Artifact 5: security review section in the iteration's `review.md` (Security Engineer sign-off with findings list)

## Verification Method
Test Engineer + Security Engineer: `go test ./...` green for auth/validation tests; start daemon with `-token secret` and confirm curl without/with header behaves 401/200; code inspection of spawn path for shell usage; confirm state file `launch-daemon/run/state.json` and log files contain no plaintext token beyond the auth-compare field if unavoidable (then flagged as finding).

## Owner
Senior Software Engineer (implementation evidence); Security Engineer (review + proposal owner); System Test Engineer (verdict)
