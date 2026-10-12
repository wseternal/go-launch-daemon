# go-launch-daemon

This guide describes the system architecture, the module layout, and the core development rules for this repository.

The project is a lite process supervisor for Go services. The daemon spawns, monitors, and restarts local service processes. Services integrate at source level through a public Go SDK (ADR-001).

---

## Global System Architecture

```
launchd daemon
  ├── Operator REST API (/api/v1)   ← launchdctl CLI, web console
  ├── Agent API (/agent/v1)         ← supervised services through launchsdk
  ├── Static file server            ← serves frontend/dist (-static flag)
  └── Supervisor
       ├── Spawns each service in its own process group
       ├── Captures stdout and stderr to a ring buffer and log files
       ├── Restarts services per KeepAlive policy with exponential backoff
       └── Publishes state and log events on the EventBus

Supervised service
  └── launchsdk client
       ├── Registers with the Agent API (per-spawn session token)
       ├── Sends a heartbeat every 5 seconds
       └── Long-polls for the shutdown command (5 s server-side wait)
```

For the package-level detail of the daemon, see the [Daemon Module Guide](daemon/AGENTS.md).

---

## Sub-Module Architecture & Directory Guides

Responsibilities are strictly separated across modules. Refer to each module guide for precise specifications:

### 1. [Daemon Module Guide](daemon/AGENTS.md)
* **Responsibility**: Supervisor core, HTTP APIs, operator CLI, and the public SDK. Own Go module `launchd`.
* **Path**: `daemon/`

### 2. [Frontend Console Guide](frontend/AGENTS.md)
* **Responsibility**: Operator web console. Standalone Vite + React + antd app (ADR-010). The daemon serves the build output.
* **Path**: `frontend/`

### 3. [Examples Guide](examples/AGENTS.md)
* **Responsibility**: Reference services that demonstrate the SDK integration pattern.
* **Path**: `examples/`

### 4. [Docs Guide](docs/AGENTS.md)
* **Responsibility**: Architecture Decision Records (ADRs) and their conventions.
* **Path**: `docs/`

---

## Core Development & Operation Rules

### 1. Build and Test
* Run `go build ./...` and `go test ./...` in `daemon/` before a commit. The test suite includes e2e tests.
* Run `bun run build` in `frontend/` before a commit. The build runs `tsc` first.
* Run `go build ./...` in `examples/` after SDK changes.

### 2. Module Layout
* Two Go modules exist: `daemon/` (module `launchd`) and `examples/` (module `launchd-examples`).
* The `examples/` module uses a `replace` directive for `../daemon`.
* The repository does not use a Go workspace.
* The frontend uses Bun. The lockfile is `frontend/bun.lock`.

### 3. API and Environment Contract
* The operator API (`/api/v1`), the agent API (`/agent/v1`), and the SDK environment variables form one contract.
* The contract has three consumers: `launchdctl`, the web console, and `launchsdk`.
* Change the contract and all consumers in the same commit.
* The daemon injects three variables at spawn: `LAUNCHD_API_ADDR`, `LAUNCHD_SERVICE_NAME`, `LAUNCHD_SESSION_TOKEN`.

### 4. Decision Records
* Record every significant technical decision in an ADR under `docs/adrs/`.
* Follow the conventions in the [Docs Guide](docs/AGENTS.md).

### 5. Auth and Network Model
* Bearer token auth is opt-in and off by default (ADR-008).
* The daemon binds to `127.0.0.1:8800` by default.
* Do not bind to a non-loopback address without a token.

### 6. Runtime Artifacts
* The daemon writes the state file, the logs, and the PID file under `run/`.
* The `run/` directory is gitignored. Never commit it.
