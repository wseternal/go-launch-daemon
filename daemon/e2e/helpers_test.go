package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"launchd/internal/api"
	"launchd/internal/eventbus"
	"launchd/internal/registry"
	"launchd/internal/supervisor"
)

// testEnv holds the shared test environment.
type testEnv struct {
	t       *testing.T
	dir     string
	reg     *registry.Registry
	bus     *eventbus.Bus
	sup     *supervisor.Supervisor
	srv     *api.Server
	addr    string
	ticker  string // path to ticker binary (testsvc)
	echoBin string // path to echo-service binary
}

func setup(t *testing.T) *testEnv {
	t.Helper()

	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	os.MkdirAll(logDir, 0o755)

	// Build test binaries.
	tickerBin := buildBinary(t, "./internal/testsvc/", "testsvc")
	echoBin := buildExampleBinary(t, "./echo-service/", "echo-service")

	reg := registry.New()
	bus := eventbus.New()

	srv := api.NewServer(api.ServerConfig{
		Addr:    "127.0.0.1:0",
		Version: "test",
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}

	// Create the supervisor after the server starts. It injects the real
	// listen address (not :0) into LAUNCHD_API_ADDR.
	sup := supervisor.New(reg, bus, logDir, srv.Addr())
	srv.Wire(reg, sup)

	t.Cleanup(func() {
		sup.Shutdown()
		srv.Shutdown(context.Background())
		bus.Close()
	})

	return &testEnv{
		t:       t,
		dir:     dir,
		reg:     reg,
		bus:     bus,
		sup:     sup,
		srv:     srv,
		addr:    srv.Addr(),
		ticker:  tickerBin,
		echoBin: echoBin,
	}
}

func buildBinary(t *testing.T, pkg, name string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, name)
	daemonDir := filepath.Join(mustGetDaemonDir(t))
	cmd := exec.Command("go", "build", "-o", bin, pkg)
	cmd.Dir = daemonDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build %s: %v\n%s", name, err, out)
	}
	return bin
}

func buildExampleBinary(t *testing.T, pkg, name string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, name)
	examplesDir := filepath.Join(mustGetDaemonDir(t), "..", "examples")
	cmd := exec.Command("go", "build", "-o", bin, pkg)
	cmd.Dir = examplesDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build %s: %v\n%s", name, err, out)
	}
	return bin
}

func mustGetDaemonDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "..")
}

// apiGet makes a GET request to the daemon API.
func (e *testEnv) apiGet(path string) *http.Response {
	e.t.Helper()
	resp, err := http.Get("http://" + e.addr + path)
	if err != nil {
		e.t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

// apiPost makes a POST request with JSON body.
func (e *testEnv) apiPost(path string, body any) *http.Response {
	e.t.Helper()
	data, _ := json.Marshal(body)
	resp, err := http.Post("http://"+e.addr+path, "application/json",
		bytes.NewReader(data))
	if err != nil {
		e.t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

// waitForState polls the API until the service reaches the expected state.
func (e *testEnv) waitForState(name, state string, timeout time.Duration) {
	e.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp := e.apiGet("/api/v1/services/" + name)
		var info struct {
			Status struct {
				State string `json:"state"`
			} `json:"status"`
		}
		json.NewDecoder(resp.Body).Decode(&info)
		resp.Body.Close()

		if info.Status.State == state {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	e.t.Fatalf("timeout: %s did not reach state %s within %v", name, state, timeout)
}

// waitForHeartbeat polls the API until the service reports a heartbeat.
func (e *testEnv) waitForHeartbeat(name string, timeout time.Duration) {
	e.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp := e.apiGet("/api/v1/services/" + name)
		var info struct {
			Status struct {
				HeartbeatAt *string `json:"heartbeatAt"`
			} `json:"status"`
		}
		json.NewDecoder(resp.Body).Decode(&info)
		resp.Body.Close()

		if info.Status.HeartbeatAt != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	e.t.Fatalf("timeout: %s did not send a heartbeat within %v", name, timeout)
}
