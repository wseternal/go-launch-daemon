// Package main implements an HTTP echo service supervised by launchd.
// It listens on port 8901 with a /healthz endpoint.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"launchd/pkg/launchsdk"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Initialize launchd SDK.
	client, err := launchsdk.New("echo-service", ctx)
	if err == nil {
		client.Start()
		defer client.Close()
		// Use the SDK context for graceful shutdown.
		ctx = client.Context()
	} else if err != launchsdk.ErrNotSupervised {
		log.Fatalf("SDK init: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "echo: %s %s\n", r.Method, r.URL.Path)
	})

	srv := &http.Server{
		Addr:    "127.0.0.1:8901",
		Handler: mux,
	}

	// Start server in background.
	go func() {
		log.Printf("echo-service listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	// Wait for shutdown signal.
	<-ctx.Done()
	log.Println("echo-service shutting down")
	srv.Shutdown(context.Background())
}

// Suppress unused import warning for os.
var _ = os.Getenv
