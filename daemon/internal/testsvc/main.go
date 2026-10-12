// Package main implements a hermetic test service for supervisor testing.
// Modes are controlled via environment variables:
//   - MODE=sleep: sleep indefinitely (default)
//   - MODE=crash_after: crash after CRASH_AFTER seconds
//   - MODE=ignore_sigterm: ignore SIGTERM and sleep
//   - MODE=echo: start HTTP server on PORT with /healthz
//   - MODE=ticker: print a tick line every second
package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func main() {
	mode := os.Getenv("MODE")
	if mode == "" {
		mode = "sleep"
	}

	switch mode {
	case "sleep":
		sleepMode()
	case "crash_after":
		crashMode()
	case "ignore_sigterm":
		ignoreSigtermMode()
	case "echo":
		echoMode()
	case "ticker":
		tickerMode()
	default:
		fmt.Fprintf(os.Stderr, "unknown mode: %s\n", mode)
		os.Exit(1)
	}
}

func sleepMode() {
	fmt.Println("testsvc: sleeping")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	fmt.Println("testsvc: received signal, exiting")
}

func crashMode() {
	after := 1
	if v := os.Getenv("CRASH_AFTER"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			after = n
		}
	}
	fmt.Printf("testsvc: crashing in %d seconds\n", after)
	time.Sleep(time.Duration(after) * time.Second)
	fmt.Println("testsvc: crashing now")
	os.Exit(1)
}

func ignoreSigtermMode() {
	fmt.Println("testsvc: ignoring SIGTERM")
	// Ignore SIGTERM but not SIGKILL.
	signal.Ignore(syscall.SIGTERM)
	// Still handle SIGINT for clean test teardown.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT)
	<-sig
	fmt.Println("testsvc: received SIGINT, exiting")
}

func echoMode() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8901"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "echo: %s %s\n", r.Method, r.URL.Path)
	})

	addr := "127.0.0.1:" + port
	fmt.Printf("testsvc: echo listening on %s\n", addr)

	srv := &http.Server{Addr: addr, Handler: mux}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		fmt.Println("testsvc: echo shutting down")
		srv.Close()
	}()

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "echo: %v\n", err)
		os.Exit(1)
	}
}

func tickerMode() {
	fmt.Println("testsvc: ticking every second")

	// Check for crash directive.
	crashAfter := 0
	if v := os.Getenv("CRASH_AFTER"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			crashAfter = n
		}
	}

	tick := 0
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	timer := time.NewTicker(time.Second)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			tick++
			fmt.Printf("testsvc: tick %d\n", tick)
			if crashAfter > 0 && tick >= crashAfter {
				fmt.Println("testsvc: CRASH_AFTER reached")
				os.Exit(1)
			}
		case <-sig:
			fmt.Println("testsvc: ticker exiting")
			return
		}
	}
}
