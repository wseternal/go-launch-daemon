// Package main implements a ticker service supervised by launchd.
// It prints a tick every second. Set CRASH_AFTER=<seconds> to test crash restart.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"launchd/pkg/launchsdk"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Initialize launchd SDK.
	client, err := launchsdk.New("ticker-service", ctx)
	if err == nil {
		client.Start()
		defer client.Close()
		ctx = client.Context()
	} else if err != launchsdk.ErrNotSupervised {
		log.Fatalf("SDK init: %v", err)
	}

	crashAfter := 0
	if v := os.Getenv("CRASH_AFTER"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			crashAfter = n
		}
	}

	tick := 0
	timer := time.NewTicker(time.Second)
	defer timer.Stop()

	log.Println("ticker-service started")

	for {
		select {
		case <-timer.C:
			tick++
			fmt.Printf("tick %d\n", tick)
			if crashAfter > 0 && tick >= crashAfter {
				log.Println("CRASH_AFTER reached — exiting with code 1")
				os.Exit(1)
			}
		case <-ctx.Done():
			log.Println("ticker-service exiting")
			return
		}
	}
}
