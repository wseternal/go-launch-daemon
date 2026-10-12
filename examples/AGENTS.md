# Examples Module

The `examples/` directory contains reference services that demonstrate the `launchsdk` integration pattern. Module name: `launchd-examples`. A `replace` directive references `../daemon`.

## Services

* `echo-service` — HTTP echo on `127.0.0.1:8901` with a `/healthz` endpoint.
* `ticker-service` — prints a tick every second. Set `CRASH_AFTER=<seconds>` to test crash restart.

## Integration Pattern

1. Create a signal context with `signal.NotifyContext`.
2. Call `launchsdk.New(name, ctx)`.
3. On success: call `client.Start()`, defer `client.Close()`, and use `client.Context()`.
4. On `ErrNotSupervised`: run standalone with the signal context.
5. On any other error: exit with `log.Fatalf`.

## Run an Example

```bash
cd examples
go build -o /tmp/echo-service ./echo-service
launchdctl register -name echo-service --keep-alive always /tmp/echo-service
launchdctl start echo-service
```

## Module Rules

* Every example compiles with `go build ./...` in `examples/`.
* Every example runs standalone and under supervision.
* Each example demonstrates one behavior. Keep the examples small.
