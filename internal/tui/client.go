package tui

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

// APIClient connects to a running Cbox Init daemon via API
type APIClient struct {
	baseURL    string
	socketPath string
	auth       string
	client     *http.Client
}

// NewAPIClient creates a new API client with auto-detection
// Tries Unix socket first, falls back to TCP
func NewAPIClient(baseURL, auth string) *APIClient {
	client := &APIClient{
		baseURL: baseURL,
		auth:    auth,
	}

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

	// Fall back to TCP
	client.client = &http.Client{
		Timeout: 10 * time.Second,
	}

	return client
}

// trySocket tests if a socket path is accessible
func (c *APIClient) trySocket(socketPath string) bool {
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
func (c *APIClient) createSocketClient(socketPath string) *http.Client {
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
func (c *APIClient) getURL(path string) string {
	if c.socketPath != "" {
		// Use dummy hostname for socket connections
		return fmt.Sprintf("http://unix%s", path)
	}
	return fmt.Sprintf("%s%s", c.baseURL, path)
}

// ListProcesses fetches process list from API
func (c *APIClient) ListProcesses() ([]process.ProcessInfo, error) {
	url := c.getURL("/api/v1/processes")

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.auth != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.auth))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Processes []process.ProcessInfo `json:"processes"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return response.Processes, nil
}

// StartProcess starts a stopped process
func (c *APIClient) StartProcess(name string) error {
	return c.processAction(name, "start")
}

// StopProcess stops a running process
func (c *APIClient) StopProcess(name string) error {
	return c.processAction(name, "stop")
}

// RestartProcess restarts a process
func (c *APIClient) RestartProcess(name string) error {
	return c.processAction(name, "restart")
}

// ScaleProcess scales a process
func (c *APIClient) ScaleProcess(name string, desired int) error {
	url := c.getURL(fmt.Sprintf("/api/v1/processes/%s/scale", name))

	body := fmt.Sprintf(`{"desired":%d}`, desired)

	req, err := http.NewRequest("POST", url, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.auth != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.auth))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("scale failed: %s", string(respBody))
	}

	return nil
}

// ScaleProcessDelta adjusts process scale by delta
func (c *APIClient) ScaleProcessDelta(name string, delta int) error {
	url := c.getURL(fmt.Sprintf("/api/v1/processes/%s/scale", name))

	body := fmt.Sprintf(`{"delta":%d}`, delta)

	req, err := http.NewRequest("POST", url, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.auth != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.auth))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("scale failed: %s", string(respBody))
	}

	return nil
}

// processAction performs a process action (start/stop/restart)
func (c *APIClient) processAction(name, action string) error {
	url := c.getURL(fmt.Sprintf("/api/v1/processes/%s/%s", name, action))

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if c.auth != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.auth))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s failed: %s", action, string(body))
	}

	return nil
}

// DeleteProcess removes a process via API
func (c *APIClient) DeleteProcess(name string) error {
	url := c.getURL(fmt.Sprintf("/api/v1/processes/%s", name))

	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if c.auth != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.auth))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete failed: %s", string(body))
	}

	return nil
}

// UpdateProcess updates an existing process definition
func (c *APIClient) UpdateProcess(name string, proc *config.Process) error {
	if proc == nil {
		return fmt.Errorf("process configuration is required")
	}

	url := c.getURL(fmt.Sprintf("/api/v1/processes/%s", name))
	body, err := json.Marshal(map[string]*config.Process{
		"process": proc,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal process config: %w", err)
	}

	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.auth != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.auth))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update failed: %s", string(respBody))
	}

	return nil
}

// HealthCheck checks if API is reachable
func (c *APIClient) HealthCheck(ctx context.Context) error {
	url := c.getURL("/api/v1/health")

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("API not reachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API unhealthy: status %d", resp.StatusCode)
	}

	return nil
}

// AddProcess creates a new process via API
func (c *APIClient) AddProcess(ctx context.Context, name string, command []string, scale int, restart string, enabled bool) error {
	url := c.getURL("/api/v1/processes")

	// Build request body matching API expectations
	reqBody := map[string]interface{}{
		"name": name,
		"process": map[string]interface{}{
			"enabled": enabled,
			"command": command,
			"scale":   scale,
			"restart": restart,
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(body)))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	if c.auth != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.auth))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// GetLogs retrieves logs for a specific process
func (c *APIClient) GetLogs(processName string, limit int) ([]logger.LogEntry, error) {
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
func (c *APIClient) GetStackLogs(limit int) ([]logger.LogEntry, error) {
	path := "/api/v1/logs"
	if limit > 0 {
		path = fmt.Sprintf("%s?limit=%d", path, limit)
	}
	return c.fetchLogs(path)
}

func (c *APIClient) fetchLogs(path string) ([]logger.LogEntry, error) {
	if c.client == nil {
		return nil, fmt.Errorf("API client not initialized")
	}

	req, err := http.NewRequest("GET", c.getURL(path), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.auth != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.auth))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch logs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("logs request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Logs []logger.LogEntry `json:"logs"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("failed to decode logs response: %w", err)
	}

	return payload.Logs, nil
}

// GetProcessConfig fetches full configuration for a process
func (c *APIClient) GetProcessConfig(name string) (*config.Process, error) {
	if name == "" {
		return nil, fmt.Errorf("process name is required")
	}

	url := c.getURL(fmt.Sprintf("/api/v1/processes/%s", name))
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if c.auth != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.auth))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch process: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("process request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Config *config.Process `json:"config"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("failed to decode process response: %w", err)
	}

	if payload.Config == nil {
		return nil, fmt.Errorf("process configuration missing in response")
	}

	return payload.Config, nil
}

// PauseSchedule pauses a scheduled job via API
func (c *APIClient) PauseSchedule(name string) error {
	return c.processAction(name, "schedule/pause")
}

// ResumeSchedule resumes a paused scheduled job via API
func (c *APIClient) ResumeSchedule(name string) error {
	return c.processAction(name, "schedule/resume")
}

// TriggerSchedule manually triggers a scheduled job via API
func (c *APIClient) TriggerSchedule(name string) error {
	return c.processAction(name, "schedule/trigger")
}

// ReloadConfig reloads configuration from disk via API
func (c *APIClient) ReloadConfig() error {
	url := c.getURL("/api/v1/config/reload")

	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if c.auth != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.auth))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("reload failed: %s", string(body))
	}

	return nil
}

// SaveConfig saves running configuration to file via API
func (c *APIClient) SaveConfig() error {
	url := c.getURL("/api/v1/config/save")

	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if c.auth != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.auth))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("save failed: %s", string(body))
	}

	return nil
}

// GetOneshotHistory fetches oneshot execution history from API
func (c *APIClient) GetOneshotHistory(limit int) ([]process.OneshotExecution, error) {
	path := "/api/v1/oneshot/history"
	if limit > 0 {
		path = fmt.Sprintf("%s?limit=%d", path, limit)
	}

	req, err := http.NewRequest(http.MethodGet, c.getURL(path), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.auth != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.auth))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get oneshot history failed: %s", string(body))
	}

	var response struct {
		Executions []process.OneshotExecution `json:"executions"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return response.Executions, nil
}
