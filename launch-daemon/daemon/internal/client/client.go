// Package client provides a Go API client for the launchd daemon.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client communicates with the launchd daemon's operator API.
type Client struct {
	addr   string
	token  string
	http   *http.Client
}

// New creates a new client for the given daemon address.
func New(addr string, token string) *Client {
	return &Client{
		addr:  addr,
		token: token,
		http:  &http.Client{Timeout: 10 * time.Second},
	}
}

// ServiceSpec is the registration request body.
type ServiceSpec struct {
	Name            string   `json:"name"`
	Argv            []string `json:"argv"`
	Env             []string `json:"env,omitempty"`
	KeepAlive       string   `json:"keepAlive,omitempty"`
	RunAtLoad       bool     `json:"runAtLoad,omitempty"`
	StopTimeoutSecs int      `json:"stopTimeoutSecs,omitempty"`
	MaxRetries      int      `json:"maxRetries,omitempty"`
}

// ServiceInfo is the response for a single service.
type ServiceInfo struct {
	Spec   ServiceSpec `json:"spec"`
	Status struct {
		State        string     `json:"state"`
		Pid          int        `json:"pid,omitempty"`
		StartedAt    *time.Time `json:"startedAt,omitempty"`
		Restarts     int        `json:"restarts"`
		LastExitCode *int       `json:"lastExitCode,omitempty"`
	} `json:"status"`
}

// Register registers a new service.
func (c *Client) Register(spec ServiceSpec) (*ServiceInfo, error) {
	data, _ := json.Marshal(spec)
	resp, err := c.do("POST", "/api/v1/services", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, readError(resp)
	}

	var info ServiceInfo
	json.NewDecoder(resp.Body).Decode(&info)
	return &info, nil
}

// List returns all registered services.
func (c *Client) List() ([]ServiceInfo, error) {
	resp, err := c.do("GET", "/api/v1/services", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var services []ServiceInfo
	json.NewDecoder(resp.Body).Decode(&services)
	return services, nil
}

// Get returns info for a single service.
func (c *Client) Get(name string) (*ServiceInfo, error) {
	resp, err := c.do("GET", "/api/v1/services/"+name, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("service %q not found", name)
	}

	var info ServiceInfo
	json.NewDecoder(resp.Body).Decode(&info)
	return &info, nil
}

// Start starts a service.
func (c *Client) Start(name string) error {
	return c.action(name, "start")
}

// Stop stops a service.
func (c *Client) Stop(name string) error {
	return c.action(name, "stop")
}

// Restart restarts a service.
func (c *Client) Restart(name string) error {
	return c.action(name, "restart")
}

// Unregister removes a service.
func (c *Client) Unregister(name string) error {
	resp, err := c.do("DELETE", "/api/v1/services/"+name, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		return fmt.Errorf("service %q is running, stop it first", name)
	}
	return nil
}

func (c *Client) action(name, action string) error {
	resp, err := c.do("POST", fmt.Sprintf("/api/v1/services/%s/%s", name, action), nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted {
		return nil
	}
	return readError(resp)
}

func (c *Client) do(method, path string, body io.Reader) (*http.Response, error) {
	url := "http://" + c.addr + path
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return c.http.Do(req)
}

func readError(resp *http.Response) error {
	var e struct {
		Error string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&e)
	if e.Error != "" {
		return fmt.Errorf("%s", e.Error)
	}
	return fmt.Errorf("HTTP %d", resp.StatusCode)
}
