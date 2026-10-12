// Package launchsdk provides source-level integration for Go services
// to be supervised by the launchd daemon.
//
// A supervised service imports this package, creates a Client, and
// passes the returned context to its main loop. When the daemon sends
// a shutdown command, the context is cancelled and the service should
// exit gracefully.
//
// Environment contract (injected by the daemon at spawn):
//   - LAUNCHD_API_ADDR: daemon control API address (e.g., "127.0.0.1:8800")
//   - LAUNCHD_SERVICE_NAME: the service name registered with the daemon
//   - LAUNCHD_SESSION_TOKEN: per-spawn session token for agent API auth
//
// When these env vars are absent, New() returns ErrNotSupervised and the
// binary can still run standalone.
package launchsdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

// ErrNotSupervised is returned by New() when the LAUNCHD_* env vars are absent.
var ErrNotSupervised = errors.New("launchsdk: not running under launchd supervision")

// Client communicates with the launchd daemon's agent API.
type Client struct {
	apiAddr     string
	serviceName string
	token       string
	httpClient  *http.Client

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a new SDK client from environment variables.
// Returns ErrNotSupervised if the required env vars are absent.
// The service parameter is the user's main context (e.g., from signal.NotifyContext).
func New(name string, service context.Context) (*Client, error) {
	apiAddr := os.Getenv("LAUNCHD_API_ADDR")
	serviceName := os.Getenv("LAUNCHD_SERVICE_NAME")
	token := os.Getenv("LAUNCHD_SESSION_TOKEN")

	if apiAddr == "" || serviceName == "" || token == "" {
		return nil, ErrNotSupervised
	}

	ctx, cancel := context.WithCancel(service)
	c := &Client{
		apiAddr:     apiAddr,
		serviceName: serviceName,
		token:       token,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        1,
				IdleConnTimeout:     30 * time.Second,
				DisableCompression:  true,
			},
		},
		ctx:    ctx,
		cancel: cancel,
	}

	return c, nil
}

// Start begins the heartbeat and command long-poll goroutines.
// The caller should defer Close().
func (c *Client) Start() {
	// Register with the daemon.
	c.register()

	// Start heartbeat goroutine.
	c.wg.Add(1)
	go c.heartbeatLoop()

	// Start command long-poll goroutine.
	c.wg.Add(1)
	go c.commandLoop()
}

// Close shuts down the SDK client and waits for goroutines to finish.
func (c *Client) Close() {
	c.cancel()
	c.wg.Wait()
}

// Context returns the SDK's context, which is cancelled on shutdown command.
func (c *Client) Context() context.Context {
	return c.ctx
}

func (c *Client) register() {
	body := map[string]any{
		"pid":     os.Getpid(),
		"version": "0.1.0",
	}
	data, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST",
		fmt.Sprintf("http://%s/agent/v1/register", c.apiAddr),
		bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return // registration failure is non-fatal
	}
	resp.Body.Close()
}

func (c *Client) heartbeatLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.sendHeartbeat("ok", "")
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *Client) sendHeartbeat(status, detail string) {
	body := map[string]string{
		"status": status,
		"detail": detail,
	}
	data, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST",
		fmt.Sprintf("http://%s/agent/v1/heartbeat", c.apiAddr),
		bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

func (c *Client) commandLoop() {
	defer c.wg.Done()

	// Self-exit timer: if daemon is unreachable for 15s, exit.
	lossStart := time.Time{}
	const lossTimeout = 15 * time.Second

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		cmds, err := c.pollCommands(5)
		if err != nil {
			if lossStart.IsZero() {
				lossStart = time.Now()
			} else if time.Since(lossStart) > lossTimeout {
				// Daemon unreachable — self-exit.
				os.Exit(0)
			}
			time.Sleep(time.Second)
			continue
		}

		lossStart = time.Time{} // reset loss timer on success

		for _, cmd := range cmds {
			if cmd.Type == "shutdown" {
				c.cancel() // signal the service to exit
				return
			}
		}
	}
}

type agentCommand struct {
	Type string `json:"type"`
}

func (c *Client) pollCommands(waitSec int) ([]agentCommand, error) {
	url := fmt.Sprintf("http://%s/agent/v1/commands?wait=%d", c.apiAddr, waitSec)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)

	// Use a longer timeout for long-poll.
	client := &http.Client{Timeout: time.Duration(waitSec+10) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		c.cancel() // orphan — daemon rejected our token
		return nil, fmt.Errorf("orphan: session rejected")
	}

	var result struct {
		Commands []agentCommand `json:"commands"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Commands, nil
}
