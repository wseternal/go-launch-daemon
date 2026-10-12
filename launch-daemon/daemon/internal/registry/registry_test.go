package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"echo-service", false},
		{"a", false},
		{"abc123", false},
		{"my-service-1", false},
		{"", true},
		{"a-b-c-d-e-f-g-h-i-j-k-l-m-n-o-p-q-r-s-t-u-v-w-x-y-z-0-1-2-3-4-5", false}, // 63 chars
		{"a-b-c-d-e-f-g-h-i-j-k-l-m-n-o-p-q-r-s-t-u-v-w-x-y-z-0-1-2-3-4-56", true}, // 64 chars
		{"My-Service", true},  // uppercase
		{"my service", true},  // space
		{"my_service", true},  // underscore
		{"../escape", true},   // traversal
		{"-leading", true},    // leading hyphen
		{"trailing-", true},   // trailing hyphen
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.name)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateName(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
		})
	}
}

func TestStateTransitions(t *testing.T) {
	// Valid transitions.
	valid := []struct {
		from, to ServiceState
	}{
		{StateRegistered, StateStarting},
		{StateStarting, StateRunning},
		{StateStarting, StateFailed},
		{StateRunning, StateStopping},
		{StateRunning, StateFailed},
		{StateStopping, StateStopped},
		{StateStopped, StateRestarting},
		{StateStopped, StateStarting},
		{StateFailed, StateRestarting},
		{StateFailed, StateStarting},
		{StateRestarting, StateBackoff},
		{StateRestarting, StateStarting},
		{StateBackoff, StateStarting},
		{StateBackoff, StateStopped},
		{StateBackoff, StateFailed},
	}
	for _, tt := range valid {
		if !CanTransition(tt.from, tt.to) {
			t.Errorf("expected %s → %s to be valid", tt.from, tt.to)
		}
	}

	// Invalid transitions.
	invalid := []struct {
		from, to ServiceState
	}{
		{StateRegistered, StateRunning},
		{StateRegistered, StateStopped},
		{StateRunning, StateRegistered},
		{StateRunning, StateStarting},
		{StateStopped, StateRunning},
		{StateFailed, StateRunning},
		{StateStarting, StateStopped},
	}
	for _, tt := range invalid {
		if CanTransition(tt.from, tt.to) {
			t.Errorf("expected %s → %s to be invalid", tt.from, tt.to)
		}
	}
}

func TestRegistryRegisterAndGet(t *testing.T) {
	reg := New()

	spec := Spec{
		Name:      "test-svc",
		Argv:      []string{"./test"},
		KeepAlive: KeepAliveAlways,
	}

	if err := reg.Register(spec); err != nil {
		t.Fatalf("register: %v", err)
	}

	entry, ok := reg.Get("test-svc")
	if !ok {
		t.Fatal("expected entry to exist")
	}
	if entry.Spec.Name != "test-svc" {
		t.Fatalf("expected name test-svc, got %q", entry.Spec.Name)
	}
	if entry.Status.State != StateRegistered {
		t.Fatalf("expected state registered, got %s", entry.Status.State)
	}

	// Duplicate registration fails.
	if err := reg.Register(spec); err == nil {
		t.Fatal("expected duplicate register to fail")
	}
}

func TestRegistryInvalidName(t *testing.T) {
	reg := New()
	if err := reg.Register(Spec{Name: "INVALID"}); err == nil {
		t.Fatal("expected invalid name to fail")
	}
}

func TestRegistryTransition(t *testing.T) {
	reg := New()
	reg.Register(Spec{Name: "svc", Argv: []string{"./svc"}})

	if err := reg.Transition("svc", StateStarting); err != nil {
		t.Fatalf("transition: %v", err)
	}

	entry, _ := reg.Get("svc")
	if entry.Status.State != StateStarting {
		t.Fatalf("expected starting, got %s", entry.Status.State)
	}

	// Invalid transition fails.
	if err := reg.Transition("svc", StateStopped); err == nil {
		t.Fatal("expected invalid transition to fail")
	}
}

func TestRegistryUnregister(t *testing.T) {
	reg := New()
	reg.Register(Spec{Name: "svc", Argv: []string{"./svc"}})

	if err := reg.Unregister("svc"); err != nil {
		t.Fatalf("unregister: %v", err)
	}

	_, ok := reg.Get("svc")
	if ok {
		t.Fatal("expected entry to be removed")
	}
}

func TestRegistryUnregisterRunningFails(t *testing.T) {
	reg := New()
	reg.Register(Spec{Name: "svc", Argv: []string{"./svc"}})
	reg.Transition("svc", StateStarting)
	reg.Transition("svc", StateRunning)

	if err := reg.Unregister("svc"); err == nil {
		t.Fatal("expected unregister of running service to fail")
	}
}

func TestRegistrySnapshot(t *testing.T) {
	reg := New()
	reg.Register(Spec{Name: "a", Argv: []string{"./a"}})
	reg.Register(Spec{Name: "b", Argv: []string{"./b"}})

	snap := reg.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(snap))
	}

	// Modifying snapshot doesn't affect registry.
	delete(snap, "a")
	if _, ok := reg.Get("a"); !ok {
		t.Fatal("snapshot modification affected registry")
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	reg := New()

	// Register 10 services.
	for i := range 10 {
		name := string(rune('a' + i))
		reg.Register(Spec{Name: name, Argv: []string{"./" + name}})
	}

	var wg sync.WaitGroup
	errs := make(chan error, 100)

	// 8 goroutines doing transitions.
	for g := range 8 {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := range 100 {
				name := string(rune('a' + (i % 10)))
				// Try valid transitions; some may fail due to state.
				reg.Transition(name, StateStarting)
				reg.Transition(name, StateRunning)
				reg.Transition(name, StateStopping)
				reg.Transition(name, StateStopped)
				reg.Transition(name, StateStarting)
			}
		}(g)
	}

	// 2 goroutines taking snapshots.
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				snap := reg.Snapshot()
				if len(snap) != 10 {
					errs <- &snapshotError{got: len(snap)}
				}
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent error: %v", err)
	}
}

type snapshotError struct {
	got int
}

func (e *snapshotError) Error() string {
	return "snapshot had " + string(rune('0'+e.got)) + " entries"
}

func TestRegistryHeartbeat(t *testing.T) {
	reg := New()
	reg.Register(Spec{Name: "svc", Argv: []string{"./svc"}})

	if err := reg.TouchHeartbeat("svc", "ok"); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	entry, _ := reg.Get("svc")
	if entry.Status.HeartbeatStatus != "ok" {
		t.Fatalf("expected ok, got %q", entry.Status.HeartbeatStatus)
	}
	if entry.Status.HeartbeatAt == nil {
		t.Fatal("expected heartbeat timestamp")
	}
}

func TestRegistryRunAtLoad(t *testing.T) {
	reg := New()
	reg.Register(Spec{Name: "a", Argv: []string{"./a"}, RunAtLoad: true})
	reg.Register(Spec{Name: "b", Argv: []string{"./b"}, RunAtLoad: false})
	reg.Register(Spec{Name: "c", Argv: []string{"./c"}, RunAtLoad: true})

	names := reg.RunAtLoadSpecs()
	if len(names) != 2 {
		t.Fatalf("expected 2 RunAtLoad specs, got %d", len(names))
	}
}

func TestPersistWriteAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Write.
	reg := New()
	reg.Register(Spec{Name: "svc-a", Argv: []string{"./a"}, KeepAlive: KeepAliveAlways})
	reg.Register(Spec{Name: "svc-b", Argv: []string{"./b"}, KeepAlive: KeepAliveNever})

	p := NewPersist(reg, path)
	if err := p.write(); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Verify file exists and is valid JSON.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var file stateFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(file.Specs) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(file.Specs))
	}

	// Load into a new registry.
	reg2 := New()
	p2 := NewPersist(reg2, path)
	if err := p2.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}

	snap := reg2.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 entries after load, got %d", len(snap))
	}
}

func TestPersistCorruptFileQuarantine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Write corrupt data.
	os.WriteFile(path, []byte("not json"), 0o644)

	reg := New()
	p := NewPersist(reg, path)
	if err := p.Load(); err != nil {
		t.Fatalf("load should not error on corrupt file: %v", err)
	}

	// Registry should be empty (cold start).
	if len(reg.Snapshot()) != 0 {
		t.Fatal("expected empty registry after corrupt file")
	}

	// Corrupt file should be quarantined.
	if _, err := os.Stat(path + ".bad"); os.IsNotExist(err) {
		t.Fatal("expected .bad quarantine file")
	}
}

func TestPersistAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "state.json")

	reg := New()
	reg.Register(Spec{Name: "svc", Argv: []string{"./svc"}})

	p := NewPersist(reg, path)
	if err := p.write(); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Verify no .tmp file left behind.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("tmp file should not exist after successful write")
	}
}

func TestRegistryDefaults(t *testing.T) {
	reg := New()
	reg.Register(Spec{Name: "svc", Argv: []string{"./svc"}})

	entry, _ := reg.Get("svc")
	if entry.Spec.KeepAlive != KeepAliveNever {
		t.Fatalf("expected default KeepAlive=never, got %s", entry.Spec.KeepAlive)
	}
	if entry.Spec.StopTimeoutSecs != 10 {
		t.Fatalf("expected default StopTimeoutSecs=10, got %d", entry.Spec.StopTimeoutSecs)
	}
	if entry.Spec.MaxRetries != 5 {
		t.Fatalf("expected default MaxRetries=5, got %d", entry.Spec.MaxRetries)
	}
}
