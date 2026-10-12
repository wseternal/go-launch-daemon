package registry

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// persistInterval is the debounce delay for writes.
const persistInterval = 200 * time.Millisecond

// Persist handles atomic file persistence of registry specs.
// It runs as a single goroutine, debouncing writes and using tmp+rename.
type Persist struct {
	path     string
	registry *Registry
	mu       sync.Mutex
	seq      uint64
	trigger  chan struct{}
	done     chan struct{}
}

// NewPersist creates a persistence handler for the given registry and file path.
func NewPersist(reg *Registry, path string) *Persist {
	p := &Persist{
		path:     path,
		registry: reg,
		trigger:  make(chan struct{}, 1),
		done:     make(chan struct{}),
	}

	reg.SetChangeCallback(func() {
		select {
		case p.trigger <- struct{}{}:
		default:
		}
	})

	return p
}

// Load reads the state file and loads specs into the registry.
// If the file is corrupt, it quarantines the file and starts cold.
func (p *Persist) Load() error {
	data, err := os.ReadFile(p.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // cold start
		}
		return fmt.Errorf("read state: %w", err)
	}

	var file stateFile
	if err := json.Unmarshal(data, &file); err != nil {
		// Quarantine corrupt file.
		badPath := p.path + ".bad"
		log.Printf("corrupt state file, quarantining to %s: %v", badPath, err)
		if renameErr := os.Rename(p.path, badPath); renameErr != nil {
			log.Printf("quarantine rename failed: %v", renameErr)
		}
		return nil // cold start
	}

	p.seq = file.Seq
	p.registry.Load(file.Specs)
	return nil
}

// Run starts the persistence loop. It blocks until Close is called.
func (p *Persist) Run() {
	defer close(p.done)

	for {
		select {
		case <-p.trigger:
			time.Sleep(persistInterval)
			if err := p.write(); err != nil {
				log.Printf("persist error: %v", err)
			}
		}
	}
}

// Close stops the persistence loop and does a final write.
func (p *Persist) Close() error {
	close(p.trigger)
	// Wait for the run loop to exit.
	<-p.done
	// Final write.
	return p.write()
}

// stateFile is the on-disk JSON format.
type stateFile struct {
	Seq   uint64 `json:"seq"`
	Specs []Spec `json:"specs"`
}

func (p *Persist) write() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	snap := p.registry.Snapshot()
	specs := make([]Spec, 0, len(snap))
	for _, entry := range snap {
		specs = append(specs, entry.Spec)
	}

	p.seq++
	file := stateFile{
		Seq:   p.seq,
		Specs: specs,
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	// Ensure directory exists.
	dir := filepath.Dir(p.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	// Atomic write: write to tmp, then rename.
	tmp := p.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, p.path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}

	p.registry.ClearDirty()
	return nil
}
