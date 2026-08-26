package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/logger"
	"github.com/cboxdk/init/internal/process"
)

// Client connects to a running Cbox Init daemon via API
type Client struct {
	baseURL    string
	socketPath string
	auth       string
	client     *http.Client
}

// BaseURL returns the API base URL this client is configured to use.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// New creates a client for an explicit API endpoint.
// When baseURL is non-empty, no socket auto-discovery is attempted.
func New(baseURL, auth string) *Client {
	client := &Client{
		baseURL: baseURL,
		auth:    auth,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	return client
}

// NewWithAutoDiscover creates a client that prefers known local Unix sockets
// and falls back to the provided TCP baseURL when none are reachable.
func NewWithAutoDiscover(baseURL, auth string) *Client {
	client := New(baseURL, auth)

	// Auto-detect socket paths (priority order)
	socketPaths := []string{
		"/var/run/cbox-init.sock",
		"/tmp/cbox-init.sock",
		"/run/cbox-init.sock",
	}

	// Try each socket path
	for _, socketPath := range socketPaths {
		if client.trySocket(socketPath) {
			client.socketPath = socketPath
			client.client = client.createSocketClient(socketPath)
			return client
		}
	}

	return client
}

// trySocket tests if a socket path is accessible
func (c *Client) trySocket(socketPath string) bool {
	// Check if socket file exists
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		return false
	}

	// Try connecting
	conn, err := net.DialTimeout("unix", socketPath, 100*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()

	return true
}

// createSocketClient creates an HTTP client that uses Unix socket
func (c *Client) createSocketClient(socketPath string) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
	}
}

// getURL constructs the URL for API requests
func (c *Client) getURL(path string) string {
	if c.socketPath != "" {
		// Use dummy hostname for socket connections
		return fmt.Sprintf("http://unix%s", path)
	}
	return fmt.Sprintf("%s%s", c.baseURL, path)
}

// APIError is returned when the API responds with a non-2xx status. It carries
// the HTTP status and the server's error message (extracted from the JSON
// `{"error": …}` body), so callers get a clean message instead of a raw,
// nested JSON dump. Callers can type-assert with errors.As to branch on
// StatusCode.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("API error: HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("API error (HTTP %d): %s", e.StatusCode, e.Message)
}

// parseAPIError builds an *APIError from a non-2xx response, preferring the
// server's JSON error message over the raw body.
func parseAPIError(resp *http.Response) *APIError {
	body, _ := io.ReadAll(resp.Body)
	msg := strings.TrimSpace(string(body))
	var payload struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.Error != "" {
		msg = payload.Error
	}
	return &APIError{StatusCode: resp.StatusCode, Message: msg}
}

// do performs a single API request: it marshals reqBody to JSON (when non-nil),
// sets the auth header, executes, and — on any 2xx — decodes the response into
// out (when non-nil). A non-2xx response is returned as an *APIError. This is
// the single request path every client method goes through.
func (c *Client) do(ctx context.Context, method, path string, reqBody, out any) error {
	if c.client == nil {
		return fmt.Errorf("API client not initialized")
	}

	var body io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("failed to encode request: %w", err)
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.getURL(path), body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.auth != "" {
		req.Header.Set("Authorization", "Bearer "+c.auth)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseAPIError(resp)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}
	return nil
}

// ListProcesses fetches process list from API
func (c *Client) ListProcesses() ([]process.ProcessInfo, error) {
	var response struct {
		Processes []process.ProcessInfo `json:"processes"`
	}
	if err := c.do(context.Background(), http.MethodGet, "/api/v1/processes", nil, &response); err != nil {
		return nil, err
	}
	return response.Processes, nil
}

// StartProcess starts a stopped process
func (c *Client) StartProcess(name string) error {
	return c.processAction(name, "start")
}

// StopProcess stops a running process
func (c *Client) StopProcess(name string) error {
	return c.processAction(name, "stop")
}

// RestartProcess restarts a process
func (c *Client) RestartProcess(name string) error {
	return c.processAction(name, "restart")
}

// SignalProcess delivers an operational signal (e.g. SIGHUP, SIGUSR1, SIGUSR2)
// to a single process's group.
func (c *Client) SignalProcess(name, signal string) error {
	return c.do(context.Background(), http.MethodPost,
		fmt.Sprintf("/api/v1/processes/%s/signal", name),
		map[string]string{"signal": signal}, nil)
}

// ScaleProcess scales a process to an absolute count.
func (c *Client) ScaleProcess(name string, desired int) error {
	return c.do(context.Background(), http.MethodPost,
		fmt.Sprintf("/api/v1/processes/%s/scale", name),
		map[string]int{"desired": desired}, nil)
}

// ScaleProcessDelta adjusts process scale by delta
func (c *Client) ScaleProcessDelta(name string, delta int) error {
	return c.do(context.Background(), http.MethodPost,
		fmt.Sprintf("/api/v1/processes/%s/scale", name),
		map[string]int{"delta": delta}, nil)
}

// processAction performs a process action (start/stop/restart, schedule/*)
func (c *Client) processAction(name, action string) error {
	return c.do(context.Background(), http.MethodPost,
		fmt.Sprintf("/api/v1/processes/%s/%s", name, action), nil, nil)
}

// DeleteProcess removes a process via API
func (c *Client) DeleteProcess(name string) error {
	return c.do(context.Background(), http.MethodDelete,
		fmt.Sprintf("/api/v1/processes/%s", name), nil, nil)
}

// UpdateProcess updates an existing process definition
func (c *Client) UpdateProcess(name string, proc *config.Process) error {
	if proc == nil {
		return fmt.Errorf("process configuration is required")
	}
	return c.do(context.Background(), http.MethodPut,
		fmt.Sprintf("/api/v1/processes/%s", name),
		map[string]*config.Process{"process": proc}, nil)
}

// HealthCheck checks if API is reachable
func (c *Client) HealthCheck(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/api/v1/health", nil, nil)
}

// AddProcess creates a new process via API
func (c *Client) AddProcess(ctx context.Context, name string, command []string, scale int, restart string, enabled bool) error {
	reqBody := map[string]any{
		"name": name,
		"process": map[string]any{
			"enabled": enabled,
			"command": command,
			"scale":   scale,
			"restart": restart,
		},
	}
	return c.do(ctx, http.MethodPost, "/api/v1/processes", reqBody, nil)
}

// GetLogs retrieves logs for a specific process
func (c *Client) GetLogs(processName string, limit int) ([]logger.LogEntry, error) {
	if processName == "" {
		return nil, fmt.Errorf("process name is required")
	}

	path := fmt.Sprintf("/api/v1/processes/%s/logs", url.PathEscape(processName))
	if limit > 0 {
		path = fmt.Sprintf("%s?limit=%d", path, limit)
	}

	return c.fetchLogs(path)
}

// GetStackLogs retrieves aggregated logs for all processes
func (c *Client) GetStackLogs(limit int) ([]logger.LogEntry, error) {
	path := "/api/v1/logs"
	if limit > 0 {
		path = fmt.Sprintf("%s?limit=%d", path, limit)
	}
	return c.fetchLogs(path)
}

func (c *Client) fetchLogs(path string) ([]logger.LogEntry, error) {
	var payload struct {
		Logs []logger.LogEntry `json:"logs"`
	}
	if err := c.do(context.Background(), http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Logs, nil
}

// GetProcessConfig fetches full configuration for a process
func (c *Client) GetProcessConfig(name string) (*config.Process, error) {
	if name == "" {
		return nil, fmt.Errorf("process name is required")
	}
	var payload struct {
		Config *config.Process `json:"config"`
	}
	if err := c.do(context.Background(), http.MethodGet, fmt.Sprintf("/api/v1/processes/%s", name), nil, &payload); err != nil {
		return nil, err
	}
	if payload.Config == nil {
		return nil, fmt.Errorf("process configuration missing in response")
	}
	return payload.Config, nil
}

// PauseSchedule pauses a scheduled job via API
func (c *Client) PauseSchedule(name string) error {
	return c.processAction(name, "schedule/pause")
}

// ResumeSchedule resumes a paused scheduled job via API
func (c *Client) ResumeSchedule(name string) error {
	return c.processAction(name, "schedule/resume")
}

// TriggerSchedule manually triggers a scheduled job via API
func (c *Client) TriggerSchedule(name string) error {
	return c.processAction(name, "schedule/trigger")
}

// ReloadConfig reloads configuration from disk via API
func (c *Client) ReloadConfig() error {
	return c.do(context.Background(), http.MethodPost, "/api/v1/config/reload", nil, nil)
}

// SaveConfig saves running configuration to file via API
func (c *Client) SaveConfig() error {
	return c.do(context.Background(), http.MethodPost, "/api/v1/config/save", nil, nil)
}

// GetOneshotHistory fetches oneshot execution history from API
func (c *Client) GetOneshotHistory(limit int) ([]process.OneshotExecution, error) {
	path := "/api/v1/oneshot/history"
	if limit > 0 {
		path = fmt.Sprintf("%s?limit=%d", path, limit)
	}
	var response struct {
		Executions []process.OneshotExecution `json:"executions"`
	}
	if err := c.do(context.Background(), http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return response.Executions, nil
}
