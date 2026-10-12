// Package main implements the launchd daemon — a lite process supervisor
// for Go services with source-level SDK integration.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"launchd/internal/api"
)

const version = "0.1.0"

func main() {
	addr := flag.String("addr", "127.0.0.1:8800", "listen address (host:port)")
	stateFile := flag.String("state", "", "state file path (default: run/state.json relative to working dir)")
	logDir := flag.String("logs", "", "log directory (default: run/logs relative to working dir)")
	staticDir := flag.String("static", "", "static console directory (default: empty, API-only mode)")
	token := flag.String("token", "", "bearer token for API auth (default: empty, auth disabled)")
	flag.Parse()

	// Resolve state file path.
	sf := *stateFile
	if sf == "" {
		sf = filepath.Join("run", "state.json")
	}

	// Resolve log directory.
	ld := *logDir
	if ld == "" {
		ld = filepath.Join("run", "logs")
	}

	// Write PID file.
	pidFile := filepath.Join("run", "launchd.pid")
	if err := writePIDFile(pidFile); err != nil {
		log.Fatalf("failed to write PID file: %v", err)
	}

	srv := api.NewServer(api.ServerConfig{
		Addr:      *addr,
		StateFile: sf,
		LogDir:    ld,
		StaticDir: *staticDir,
		Token:     *token,
		Version:   version,
	})

	if err := srv.Start(); err != nil {
		os.Remove(pidFile)
		log.Fatalf("server start failed: %v", err)
	}

	log.Printf("launchd %s listening on %s", version, *addr)

	// Wait for shutdown signal.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}

	os.Remove(pidFile)
	log.Println("stopped")
}

func writePIDFile(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create pid dir: %w", err)
	}
	return os.WriteFile(path, []byte(fmt.Sprintf("%d", os.Getpid())), 0o644)
}
