package supervisor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"launchd/internal/eventbus"
	"launchd/internal/registry"
)

// buildTestSvc compiles the testsvc binary to a temp directory.
func buildTestSvc(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "testsvc")
	cmd := exec.Command("go", "build", "-o", bin, "./internal/testsvc/")
	cmd.Dir = filepath.Join(mustGetDaemonDir(t))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build testsvc: %v\n%s", err, out)
	}
	return bin
}

func mustGetDaemonDir(t *testing.T) string {
	t.Helper()
	// We're in daemon/internal/supervisor, go up to daemon.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "..", "..")
}

func TestSupervisorStartAndStop(t *testing.T) {
	bin := buildTestSvc(t)
	logDir := t.TempDir()

	reg := registry.New()
	bus := eventbus.New()
	defer bus.Close()

	reg.Register(registry.Spec{
		Name:            "test-svc",
		Argv:            []string{bin},
		Env:             []string{"MODE=sleep"},
		KeepAlive:       registry.KeepAliveNever,
		StopTimeoutSecs: 5,
		MaxRetries:      3,
	})

	sup := New(reg, bus, logDir, "127.0.0.1:0")
	defer sup.Shutdown()

	// Start the service.
	if err := sup.StartService("test-svc"); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Wait for running state.
	waitForState(t, reg, "test-svc", registry.StateRunning, 5*time.Second)

	entry, _ := reg.Get("test-svc")
	if entry.Status.Pid == 0 {
		t.Fatal("expected non-zero PID")
	}

	// Stop the service.
	if err := sup.StopService("test-svc"); err != nil {
		t.Fatalf("stop: %v", err)
	}

	waitForState(t, reg, "test-svc", registry.StateStopped, 5*time.Second)
}

func TestSupervisorSigkillEscalation(t *testing.T) {
	logDir := t.TempDir()

	// Build the stubborn binary that ignores SIGTERM.
	dir := t.TempDir()
	bin := filepath.Join(dir, "stubborn")
	cmd := exec.Command("go", "build", "-o", bin, "./internal/testsvc/stubborn/")
	cmd.Dir = mustGetDaemonDir(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build stubborn: %v\n%s", err, out)
	}

	// Ready file signals when child has set up signal handling.
	readyFile := filepath.Join(dir, "ready")

	reg := registry.New()
	bus := eventbus.New()
	defer bus.Close()

	reg.Register(registry.Spec{
		Name:            "stubborn",
		Argv:            []string{bin},
		Env:             []string{"READY_FILE=" + readyFile},
		KeepAlive:       registry.KeepAliveNever,
		StopTimeoutSecs: 1,
		MaxRetries:      3,
	})

	sup := New(reg, bus, logDir, "127.0.0.1:0")
	defer sup.Shutdown()

	if err := sup.StartService("stubborn"); err != nil {
		t.Fatalf("start: %v", err)
	}

	waitForState(t, reg, "stubborn", registry.StateRunning, 5*time.Second)

	// Wait for the child to signal readiness (signal handling set up).
	waitForFile(t, readyFile, 5*time.Second)

	// Stop should escalate to SIGKILL after 1 second.
	start := time.Now()
	if err := sup.StopService("stubborn"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	elapsed := time.Since(start)

	// Should take at least ~1 second (the stop timeout).
	if elapsed < 800*time.Millisecond {
		t.Fatalf("stop took %v, expected at least ~1s for SIGKILL escalation", elapsed)
	}

	waitForState(t, reg, "stubborn", registry.StateStopped, 2*time.Second)
}

// waitForFile polls for a file to exist.
func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for file %s", path)
}

func TestSupervisorLogCapture(t *testing.T) {
	bin := buildTestSvc(t)
	logDir := t.TempDir()

	reg := registry.New()
	bus := eventbus.New()
	defer bus.Close()

	reg.Register(registry.Spec{
		Name:            "ticker",
		Argv:            []string{bin},
		Env:             []string{"MODE=ticker"},
		KeepAlive:       registry.KeepAliveNever,
		StopTimeoutSecs: 5,
		MaxRetries:      3,
	})

	sup := New(reg, bus, logDir, "127.0.0.1:0")
	defer sup.Shutdown()

	if err := sup.StartService("ticker"); err != nil {
		t.Fatalf("start: %v", err)
	}

	waitForState(t, reg, "ticker", registry.StateRunning, 5*time.Second)

	// Wait for some ticks.
	time.Sleep(3 * time.Second)

	// Check log tail.
	lines, err := sup.LogTail("ticker", 100)
	if err != nil {
		t.Fatalf("log tail: %v", err)
	}

	hasTick := false
	for _, line := range lines {
		if strings.Contains(line, "tick") {
			hasTick = true
			break
		}
	}
	if !hasTick {
		t.Fatalf("expected tick lines in log, got: %v", lines)
	}

	sup.StopService("ticker")
}

func TestSupervisorMultipleStartStop(t *testing.T) {
	bin := buildTestSvc(t)
	logDir := t.TempDir()

	reg := registry.New()
	bus := eventbus.New()
	defer bus.Close()

	reg.Register(registry.Spec{
		Name:            "cycle",
		Argv:            []string{bin},
		Env:             []string{"MODE=sleep"},
		KeepAlive:       registry.KeepAliveNever,
		StopTimeoutSecs: 5,
		MaxRetries:      3,
	})

	sup := New(reg, bus, logDir, "127.0.0.1:0")
	defer sup.Shutdown()

	for i := range 5 {
		if err := sup.StartService("cycle"); err != nil {
			t.Fatalf("start #%d: %v", i, err)
		}
		waitForState(t, reg, "cycle", registry.StateRunning, 5*time.Second)

		if err := sup.StopService("cycle"); err != nil {
			t.Fatalf("stop #%d: %v", i, err)
		}
		waitForState(t, reg, "cycle", registry.StateStopped, 5*time.Second)
	}
}

func TestSupervisorSessionToken(t *testing.T) {
	bin := buildTestSvc(t)
	logDir := t.TempDir()

	reg := registry.New()
	bus := eventbus.New()
	defer bus.Close()

	reg.Register(registry.Spec{
		Name:            "token-test",
		Argv:            []string{bin},
		Env:             []string{"MODE=sleep"},
		KeepAlive:       registry.KeepAliveNever,
		StopTimeoutSecs: 5,
	})

	sup := New(reg, bus, logDir, "127.0.0.1:0")
	defer sup.Shutdown()

	sup.StartService("token-test")
	waitForState(t, reg, "token-test", registry.StateRunning, 5*time.Second)

	token, err := sup.SessionToken("token-test")
	if err != nil {
		t.Fatalf("session token: %v", err)
	}
	if len(token) != 64 { // 32 bytes = 64 hex chars
		t.Fatalf("expected 64-char token, got %d", len(token))
	}

	sup.StopService("token-test")
}

func TestRingBuffer(t *testing.T) {
	rb := NewRingBuffer(5)

	// Add 3 lines.
	rb.Add("a")
	rb.Add("b")
	rb.Add("c")

	lines := rb.Tail(2)
	if len(lines) != 2 || lines[0] != "b" || lines[1] != "c" {
		t.Fatalf("tail 2: %v", lines)
	}

	// Overflow.
	rb.Add("d")
	rb.Add("e")
	rb.Add("f") // evicts "a"
	rb.Add("g") // evicts "b"

	lines = rb.Tail(5)
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(lines))
	}
	// Should be c, d, e, f, g.
	expected := []string{"c", "d", "e", "f", "g"}
	for i, exp := range expected {
		if lines[i] != exp {
			t.Fatalf("line %d: expected %q, got %q", i, exp, lines[i])
		}
	}
}

func TestLineWriter(t *testing.T) {
	var lines []string
	lw := NewLineWriter(func(line string) {
		lines = append(lines, line)
	})

	lw.Write([]byte("hello\nworld\n"))
	if len(lines) != 2 || lines[0] != "hello" || lines[1] != "world" {
		t.Fatalf("lines: %v", lines)
	}

	// Partial write.
	lines = nil
	lw.Write([]byte("partial"))
	if len(lines) != 0 {
		t.Fatalf("expected 0 lines for partial write, got %d", len(lines))
	}
	lw.Write([]byte(" complete\n"))
	if len(lines) != 1 || lines[0] != "partial complete" {
		t.Fatalf("lines after completion: %v", lines)
	}
}

func waitForState(t *testing.T, reg *registry.Registry, name string, state registry.ServiceState, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		entry, ok := reg.Get(name)
		if ok && entry.Status.State == state {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	entry, _ := reg.Get(name)
	t.Fatalf("timeout waiting for %s to reach %s (current: %s)", name, state, entry.Status.State)
}
