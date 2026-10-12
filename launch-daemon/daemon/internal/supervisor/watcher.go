package supervisor

import (
	"context"
	"log"
	"os/exec"
	"syscall"
	"time"

	"launchd/internal/eventbus"
	"launchd/internal/registry"
)

// serviceWatcher manages the lifecycle of a single supervised service.
type serviceWatcher struct {
	name    string
	spec    registry.Spec
	cmd     *exec.Cmd
	token   string
	reg     *registry.Registry
	bus     *eventbus.Bus
	logDir  string
	apiAddr string

	ring *RingBuffer
	file *FileWriter

	restart *RestartEngine

	stopCh chan struct{} // closed to signal graceful stop
	done   chan struct{} // closed when watcher goroutine finishes
	ready  chan struct{} // closed when watcher enters its select loop
}

// newServiceWatcher creates a watcher for a service.
func newServiceWatcher(name string, spec registry.Spec, reg *registry.Registry, bus *eventbus.Bus, logDir, apiAddr string, restart *RestartEngine) *serviceWatcher {
	return &serviceWatcher{
		name:    name,
		spec:    spec,
		reg:     reg,
		bus:     bus,
		logDir:  logDir,
		apiAddr: apiAddr,
		ring:    NewRingBuffer(ringCap),
		restart: restart,
		stopCh:  make(chan struct{}),
		done:    make(chan struct{}),
		ready:   make(chan struct{}),
	}
}

// start spawns the service process and begins watching it.
// This is synchronous: it returns once the process is running (or failed to start).
func (w *serviceWatcher) start(ctx context.Context) error {
	cfg := spawnConfig{
		APIAddr: w.apiAddr,
		LogDir:  w.logDir,
	}

	cmd, token, err := spawn(w.spec, cfg)
	if err != nil {
		return err
	}

	w.cmd = cmd
	w.token = token

	// Set up log capture.
	fw, err := NewFileWriter(w.logDir, w.name)
	if err != nil {
		log.Printf("log file for %s: %v (continuing without file logging)", w.name, err)
	}
	w.file = fw

	logWriter := CombinedLogWriter(w.ring, w.file)
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	// Transition to starting.
	if err := w.reg.Transition(w.name, registry.StateStarting); err != nil {
		return err
	}
	w.publishState("starting")

	// Start the process.
	if err := cmd.Start(); err != nil {
		w.reg.Transition(w.name, registry.StateFailed)
		w.reg.SetError(w.name, err.Error())
		w.publishState("failed")
		return err
	}

	// Record PID.
	w.reg.SetPid(w.name, cmd.Process.Pid)
	w.reg.Transition(w.name, registry.StateRunning)
	w.publishState("running")

	// Launch the watcher goroutine.
	go w.watch(ctx)

	// Wait for the watcher to enter its select loop.
	<-w.ready

	return nil
}

// watch monitors the process and handles state transitions.
func (w *serviceWatcher) watch(ctx context.Context) {
	defer close(w.done)

	// Exactly-once Wait via buffered channel.
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- w.cmd.Wait()
	}()

	// Flush logs periodically.
	flushTicker := time.NewTicker(500 * time.Millisecond)
	defer flushTicker.Stop()

	// Signal that we're ready.
	close(w.ready)

	// Phase 1: wait for stop signal or process exit.
	select {
	case err := <-waitCh:
		w.handleExit(err)
		return
	case <-w.stopCh:
		// Graceful stop requested — send SIGTERM.
		w.sendSIGTERM()
	case <-ctx.Done():
		// Daemon shutdown — force kill.
		w.kill()
		<-waitCh
		w.handleExit(&exec.ExitError{})
		return
	}

	// Phase 2: after SIGTERM, wait for process exit or keep flushing.
	for {
		select {
		case err := <-waitCh:
			w.handleExit(err)
			return
		case <-ctx.Done():
			w.kill()
			<-waitCh
			w.handleExit(&exec.ExitError{})
			return
		case <-flushTicker.C:
			if w.file != nil {
				w.file.Flush()
			}
		}
	}
}

// handleExit processes the result of a process exit.
func (w *serviceWatcher) handleExit(err error) {
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	w.reg.RecordExit(w.name, exitCode)

	if w.file != nil {
		w.file.Flush()
		w.file.Close()
	}

	// Determine next state: clean exit → stopped, non-zero → failed.
	currentState, _ := w.reg.Get(w.name)
	if currentState.Status.State == registry.StateStopping {
		// We were in the process of stopping — always go to stopped.
		w.reg.Transition(w.name, registry.StateStopped)
		w.publishState("stopped")
	} else if err == nil {
		w.reg.Transition(w.name, registry.StateStopped)
		w.publishState("stopped")
		// Clean exit — check restart policy (keepAlive=always restarts even clean exits).
		if w.restart != nil {
			w.restart.HandleExit(w.name)
		}
	} else {
		w.reg.Transition(w.name, registry.StateFailed)
		w.publishState("failed")
		// Non-zero exit — check restart policy.
		if w.restart != nil {
			w.restart.HandleExit(w.name)
		}
	}
}

// stop sends SIGTERM and waits for the process to exit, escalating to SIGKILL.
func (w *serviceWatcher) stop() {
	if w.cmd == nil || w.cmd.Process == nil {
		return
	}

	w.reg.Transition(w.name, registry.StateStopping)
	w.publishState("stopping")

	// Signal the watcher goroutine to send SIGTERM.
	close(w.stopCh)

	// Wait for graceful exit.
	timeout := time.Duration(w.spec.StopTimeoutSecs) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	select {
	case <-w.done:
		// Process exited gracefully.
		return
	case <-time.After(timeout):
		// Force kill.
		w.kill()
		<-w.done
	}
}

// sendSIGTERM sends SIGTERM to the process group.
func (w *serviceWatcher) sendSIGTERM() {
	if w.cmd == nil || w.cmd.Process == nil {
		return
	}
	syscall.Kill(-w.cmd.Process.Pid, syscall.SIGTERM)
}

// kill sends SIGKILL to the process group.
func (w *serviceWatcher) kill() {
	if w.cmd == nil || w.cmd.Process == nil {
		return
	}
	syscall.Kill(-w.cmd.Process.Pid, syscall.SIGKILL)
}

// wait waits for the watcher goroutine to finish.
func (w *serviceWatcher) wait() {
	<-w.done
}

// publishState publishes a state event to the event bus.
func (w *serviceWatcher) publishState(state string) {
	w.bus.Publish(eventbus.Event{
		Topic:     eventbus.TopicState,
		Type:      "state",
		Name:      w.name,
		Data:      state,
		Timestamp: time.Now(),
	})
}

// LogTail returns the last n lines from the ring buffer.
func (w *serviceWatcher) LogTail(n int) []string {
	return w.ring.Tail(n)
}
