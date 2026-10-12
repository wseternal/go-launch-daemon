// Package main is a stubborn test service that ignores SIGTERM.
// Used only by supervisor tests to verify SIGKILL escalation.
// It writes its PID to READY_FILE when signal handling is set up.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
)

func main() {
	ch := make(chan os.Signal, 10)
	signal.Notify(ch, syscall.SIGTERM)
	go func() {
		for range ch {
			fmt.Println("stubborn: absorbed SIGTERM")
		}
	}()

	// Write readiness file.
	if f := os.Getenv("READY_FILE"); f != "" {
		os.WriteFile(f, []byte(strconv.Itoa(os.Getpid())), 0o644)
	}

	fmt.Fprintf(os.Stderr, "stubborn: ignoring SIGTERM, pid=%d\n", os.Getpid())
	// Block forever; only SIGKILL can terminate us.
	select {}
}
