package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/logger"
	"github.com/cboxdk/init/internal/process"
)

// TestNewAPIClient tests client creation
func TestNewAPIClient(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		auth    string
	}{
		{
			name:    "with base URL and auth",
			baseURL: "http://localhost:9180",
			auth:    "test-token",
		},
		{
			name:    "with base URL no auth",
			baseURL: "http://localhost:9180",
			auth:    "",
		},
		{
			name:    "empty base URL",
			baseURL: "",
			auth:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewAPIClient(tt.baseURL, tt.auth)

			if client == nil {
				t.Fatal("Expected non-nil client")
			}

			if client.baseURL != tt.baseURL {
				t.Errorf("Expected baseURL %s, got %s", tt.baseURL, client.baseURL)
			}

			if client.auth != tt.auth {
				t.Errorf("Expected auth %s, got %s", tt.auth, client.auth)
			}

			if client.client == nil {
				t.Error("Expected non-nil HTTP client")
			}

			// Should fall back to TCP since no socket exists
			if client.socketPath != "" {
				t.Errorf("Expected empty socketPath for non-existent socket, got %s", client.socketPath)
			}
		})
	}
}

// TestAPIClient_getURL tests URL construction
func TestAPIClient_getURL(t *testing.T) {
	tests := []struct {
		name       string
		baseURL    string
		socketPath string
		path       string
		expected   string
	}{
		{
			name:       "TCP URL",
			baseURL:    "http://localhost:9180",
			socketPath: "",
			path:       "/api/v1/health",
			expected:   "http://localhost:9180/api/v1/health",
		},
		{
			name:       "socket URL",
			baseURL:    "",
			socketPath: "/tmp/cbox.sock",
			path:       "/api/v1/processes",
			expected:   "http://unix/api/v1/processes",
		},
		{
			name:       "root path",
			baseURL:    "http://api:9000",
			socketPath: "",
			path:       "/",
			expected:   "http://api:9000/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &APIClient{
				baseURL:    tt.baseURL,
				socketPath: tt.socketPath,
			}

			url := client.getURL(tt.path)
			if url != tt.expected {
				t.Errorf("Expected URL %s, got %s", tt.expected, url)
			}
		})
	}
}

// TestAPIClient_ListProcesses tests fetching process list
func TestAPIClient_ListProcesses(t *testing.T) {
	tests := []struct {
		name           string
		serverResponse interface{}
		serverStatus   int
		auth           string
		wantErr        bool
		expectedCount  int
	}{
		{
			name: "successful list",
			serverResponse: map[string]interface{}{
				"processes": []interface{}{
					map[string]interface{}{
						"name":     "php-fpm",
						"status":   "running",
						"pid":      1234,
						"uptime":   120,
						"restarts": 0,
					},
					map[string]interface{}{
						"name":     "nginx",
						"status":   "running",
						"pid":      5678,
						"uptime":   120,
						"restarts": 0,
					},
				},
			},
			serverStatus:  http.StatusOK,
			wantErr:       false,
			expectedCount: 2,
		},
		{
			name:           "empty list",
			serverResponse: map[string]interface{}{"processes": []interface{}{}},
			serverStatus:   http.StatusOK,
			wantErr:        false,
			expectedCount:  0,
		},
		{
			name:           "server error",
			serverResponse: map[string]interface{}{"error": "internal error"},
			serverStatus:   http.StatusInternalServerError,
			wantErr:        true,
		},
		{
			name:           "unauthorized",
			serverResponse: map[string]interface{}{"error": "unauthorized"},
			serverStatus:   http.StatusUnauthorized,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/processes" {
					t.Errorf("Expected path /api/v1/processes, got %s", r.URL.Path)
				}

				if r.Method != "GET" {
					t.Errorf("Expected GET method, got %s", r.Method)
				}

				// Check auth header if provided
				if tt.auth != "" {
					auth := r.Header.Get("Authorization")
					expected := "Bearer " + tt.auth
					if auth != expected {
						t.Errorf("Expected Authorization header %s, got %s", expected, auth)
					}
				}

				w.WriteHeader(tt.serverStatus)
				_ = json.NewEncoder(w).Encode(tt.serverResponse)
			}))
			defer server.Close()

			client := NewAPIClient(server.URL, tt.auth)
			processes, err := client.ListProcesses()

			if (err != nil) != tt.wantErr {
				t.Errorf("ListProcesses() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(processes) != tt.expectedCount {
					t.Errorf("Expected %d processes, got %d", tt.expectedCount, len(processes))
				}
			}
		})
	}
}

func TestAPIClient_GetLogs(t *testing.T) {
	expectedLogs := []logger.LogEntry{
		{
			ProcessName: "app",
			InstanceID:  "app-0",
			Stream:      "stdout",
			Message:     "hello",
			Level:       "info",
			Timestamp:   time.Unix(1700000000, 0),
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/processes/app/logs" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "50" {
			t.Fatalf("expected limit=50, got %s", r.URL.Query().Get("limit"))
		}
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("expected auth header")
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"process": "app",
			"logs":    expectedLogs,
		})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "token")
	logs, err := client.GetLogs("app", 50)
	if err != nil {
		t.Fatalf("GetLogs returned error: %v", err)
	}
	if len(logs) != 1 || logs[0].Message != "hello" || logs[0].ProcessName != "app" {
		t.Fatalf("unexpected logs response: %#v", logs)
	}
}

func TestAPIClient_GetStackLogs(t *testing.T) {
	expectedLogs := []logger.LogEntry{
		{
			ProcessName: "stack",
			InstanceID:  "stack-1",
			Stream:      "stderr",
			Message:     "world",
			Level:       "error",
			Timestamp:   time.Unix(1700000100, 0),
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/logs" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"scope": "stack",
			"logs":  expectedLogs,
		})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	logs, err := client.GetStackLogs(0)
	if err != nil {
		t.Fatalf("GetStackLogs returned error: %v", err)
	}
	if len(logs) != 1 || logs[0].Message != "world" {
		t.Fatalf("unexpected stack logs: %#v", logs)
	}
}

func TestAPIClient_DeleteProcess(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/processes/app" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	if err := client.DeleteProcess("app"); err != nil {
		t.Fatalf("DeleteProcess returned error: %v", err)
	}
	if !called {
		t.Fatal("server was not called")
	}
}

func TestAPIClient_UpdateProcess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("expected PUT, got %s", r.Method)
		}
		var payload struct {
			Process config.Process `json:"process"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if payload.Process.Scale != 2 {
			t.Fatalf("unexpected payload: %+v", payload.Process)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	proc := &config.Process{
		Enabled: true,
		Command: []string{"php"},
		Scale:   2,
		Restart: "always",
	}
	if err := client.UpdateProcess("app", proc); err != nil {
		t.Fatalf("UpdateProcess returned error: %v", err)
	}
}

func TestAPIClient_GetProcessConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/processes/app" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"process": "app",
			"config": map[string]interface{}{
				"enabled": true,
				"command": []string{"php"},
				"scale":   3,
				"restart": "on-failure",
			},
		})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	cfg, err := client.GetProcessConfig("app")
	if err != nil {
		t.Fatalf("GetProcessConfig returned error: %v", err)
	}
	if cfg.Scale != 3 || cfg.Restart != "on-failure" || len(cfg.Command) != 1 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

// TestAPIClient_StartProcess tests starting a process
func TestAPIClient_StartProcess(t *testing.T) {
	tests := []struct {
		name         string
		processName  string
		serverStatus int
		wantErr      bool
	}{
		{
			name:         "successful start",
			processName:  "php-fpm",
			serverStatus: http.StatusOK,
			wantErr:      false,
		},
		{
			name:         "accepted status",
			processName:  "nginx",
			serverStatus: http.StatusAccepted,
			wantErr:      false,
		},
		{
			name:         "process not found",
			processName:  "unknown",
			serverStatus: http.StatusNotFound,
			wantErr:      true,
		},
		{
			name:         "already running",
			processName:  "php-fpm",
			serverStatus: http.StatusBadRequest,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := "/api/v1/processes/" + tt.processName + "/start"
				if r.URL.Path != expectedPath {
					t.Errorf("Expected path %s, got %s", expectedPath, r.URL.Path)
				}

				if r.Method != "POST" {
					t.Errorf("Expected POST method, got %s", r.Method)
				}

				w.WriteHeader(tt.serverStatus)
				if tt.wantErr {
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "operation failed"})
				}
			}))
			defer server.Close()

			client := NewAPIClient(server.URL, "")
			err := client.StartProcess(tt.processName)

			if (err != nil) != tt.wantErr {
				t.Errorf("StartProcess() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestAPIClient_StopProcess tests stopping a process
func TestAPIClient_StopProcess(t *testing.T) {
	tests := []struct {
		name         string
		processName  string
		serverStatus int
		wantErr      bool
	}{
		{
			name:         "successful stop",
			processName:  "php-fpm",
			serverStatus: http.StatusOK,
			wantErr:      false,
		},
		{
			name:         "process not found",
			processName:  "unknown",
			serverStatus: http.StatusNotFound,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := "/api/v1/processes/" + tt.processName + "/stop"
				if r.URL.Path != expectedPath {
					t.Errorf("Expected path %s, got %s", expectedPath, r.URL.Path)
				}

				w.WriteHeader(tt.serverStatus)
			}))
			defer server.Close()

			client := NewAPIClient(server.URL, "")
			err := client.StopProcess(tt.processName)

			if (err != nil) != tt.wantErr {
				t.Errorf("StopProcess() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestAPIClient_RestartProcess tests restarting a process
func TestAPIClient_RestartProcess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/processes/test-proc/restart" {
			t.Errorf("Expected path /api/v1/processes/test-proc/restart, got %s", r.URL.Path)
		}

		if r.Method != "POST" {
			t.Errorf("Expected POST method, got %s", r.Method)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	err := client.RestartProcess("test-proc")

	if err != nil {
		t.Errorf("RestartProcess() unexpected error: %v", err)
	}
}

// TestAPIClient_ScaleProcess tests scaling a process
func TestAPIClient_ScaleProcess(t *testing.T) {
	tests := []struct {
		name         string
		processName  string
		desired      int
		serverStatus int
		wantErr      bool
	}{
		{
			name:         "successful scale",
			processName:  "worker",
			desired:      5,
			serverStatus: http.StatusOK,
			wantErr:      false,
		},
		{
			name:         "invalid scale count",
			processName:  "worker",
			desired:      -1,
			serverStatus: http.StatusBadRequest,
			wantErr:      true,
		},
		{
			name:         "process not found",
			processName:  "unknown",
			desired:      3,
			serverStatus: http.StatusNotFound,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := "/api/v1/processes/" + tt.processName + "/scale"
				if r.URL.Path != expectedPath {
					t.Errorf("Expected path %s, got %s", expectedPath, r.URL.Path)
				}

				if r.Method != "POST" {
					t.Errorf("Expected POST method, got %s", r.Method)
				}

				// Check Content-Type
				if ct := r.Header.Get("Content-Type"); ct != "application/json" {
					t.Errorf("Expected Content-Type application/json, got %s", ct)
				}

				// Decode and check body
				var body map[string]int
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("Failed to decode request body: %v", err)
				}

				if body["desired"] != tt.desired {
					t.Errorf("Expected desired %d, got %d", tt.desired, body["desired"])
				}

				w.WriteHeader(tt.serverStatus)
				if tt.wantErr {
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "scale failed"})
				}
			}))
			defer server.Close()

			client := NewAPIClient(server.URL, "")
			err := client.ScaleProcess(tt.processName, tt.desired)

			if (err != nil) != tt.wantErr {
				t.Errorf("ScaleProcess() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestAPIClient_HealthCheck tests health check endpoint
func TestAPIClient_HealthCheck(t *testing.T) {
	tests := []struct {
		name         string
		serverStatus int
		timeout      time.Duration
		wantErr      bool
	}{
		{
			name:         "healthy",
			serverStatus: http.StatusOK,
			timeout:      5 * time.Second,
			wantErr:      false,
		},
		{
			name:         "unhealthy",
			serverStatus: http.StatusServiceUnavailable,
			timeout:      5 * time.Second,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/health" {
					t.Errorf("Expected path /api/v1/health, got %s", r.URL.Path)
				}

				if r.Method != "GET" {
					t.Errorf("Expected GET method, got %s", r.Method)
				}

				w.WriteHeader(tt.serverStatus)
			}))
			defer server.Close()

			client := NewAPIClient(server.URL, "")

			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()

			err := client.HealthCheck(ctx)

			if (err != nil) != tt.wantErr {
				t.Errorf("HealthCheck() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestAPIClient_AddProcess tests adding a new process
func TestAPIClient_AddProcess(t *testing.T) {
	tests := []struct {
		name         string
		processName  string
		command      []string
		scale        int
		restart      string
		enabled      bool
		serverStatus int
		wantErr      bool
	}{
		{
			name:         "successful add",
			processName:  "new-worker",
			command:      []string{"php", "artisan", "queue:work"},
			scale:        2,
			restart:      "always",
			enabled:      true,
			serverStatus: http.StatusCreated,
			wantErr:      false,
		},
		{
			name:         "ok status accepted",
			processName:  "test-proc",
			command:      []string{"echo", "test"},
			scale:        1,
			restart:      "never",
			enabled:      false,
			serverStatus: http.StatusOK,
			wantErr:      false,
		},
		{
			name:         "process already exists",
			processName:  "php-fpm",
			command:      []string{"php-fpm", "-F"},
			scale:        1,
			restart:      "always",
			enabled:      true,
			serverStatus: http.StatusConflict,
			wantErr:      true,
		},
		{
			name:         "invalid request",
			processName:  "",
			command:      []string{},
			scale:        0,
			restart:      "invalid",
			enabled:      true,
			serverStatus: http.StatusBadRequest,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/processes" {
					t.Errorf("Expected path /api/v1/processes, got %s", r.URL.Path)
				}

				if r.Method != "POST" {
					t.Errorf("Expected POST method, got %s", r.Method)
				}

				// Check Content-Type
				if ct := r.Header.Get("Content-Type"); ct != "application/json" {
					t.Errorf("Expected Content-Type application/json, got %s", ct)
				}

				// Decode and validate body
				var body map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("Failed to decode request body: %v", err)
				}

				if body["name"] != tt.processName {
					t.Errorf("Expected name %s, got %v", tt.processName, body["name"])
				}

				w.WriteHeader(tt.serverStatus)
				if tt.wantErr {
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "operation failed"})
				} else {
					_ = json.NewEncoder(w).Encode(map[string]string{"message": "success"})
				}
			}))
			defer server.Close()

			client := NewAPIClient(server.URL, "")

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			err := client.AddProcess(ctx, tt.processName, tt.command, tt.scale, tt.restart, tt.enabled)

			if (err != nil) != tt.wantErr {
				t.Errorf("AddProcess() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestAPIClient_ListProcesses_WithAuth tests authentication header
func TestAPIClient_ListProcesses_WithAuth(t *testing.T) {
	expectedAuth := "test-token-123"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+expectedAuth {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"processes": []process.ProcessInfo{},
		})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, expectedAuth)
	_, err := client.ListProcesses()

	if err != nil {
		t.Errorf("ListProcesses() with auth failed: %v", err)
	}
}

// TestAPIClient_ScaleProcessDelta tests delta scaling
func TestAPIClient_ScaleProcessDelta(t *testing.T) {
	tests := []struct {
		name         string
		processName  string
		delta        int
		currentScale int
		wantErr      bool
	}{
		{
			name:         "scale up by 2",
			processName:  "worker",
			delta:        2,
			currentScale: 3,
			wantErr:      false,
		},
		{
			name:         "scale down by 1",
			processName:  "worker",
			delta:        -1,
			currentScale: 3,
			wantErr:      false,
		},
		{
			name:         "scale down too much",
			processName:  "worker",
			delta:        -5,
			currentScale: 3,
			wantErr:      true,
		},
		{
			name:         "no change",
			processName:  "worker",
			delta:        0,
			currentScale: 3,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/processes" && r.Method == "GET" {
					// Mock ListProcesses
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"processes": []interface{}{
							map[string]interface{}{
								"name":     tt.processName,
								"scale":    tt.currentScale,
								"desired":  tt.currentScale,
								"status":   "running",
								"pid":      1234,
								"uptime":   100,
								"restarts": 0,
							},
						},
					})
				} else if strings.HasSuffix(r.URL.Path, "/scale") && r.Method == "POST" {
					// Mock ScaleProcess - return error for negative desired scale
					newScale := tt.currentScale + tt.delta
					if newScale < 1 {
						w.WriteHeader(http.StatusBadRequest)
						_ = json.NewEncoder(w).Encode(map[string]string{"error": "scale must be >= 1"})
					} else {
						w.WriteHeader(http.StatusOK)
					}
				}
			}))
			defer server.Close()

			client := NewAPIClient(server.URL, "")
			err := client.ScaleProcessDelta(tt.processName, tt.delta)

			if (err != nil) != tt.wantErr {
				t.Errorf("ScaleProcessDelta() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestAPIClient_trySocket tests socket detection logic
func TestAPIClient_trySocket(t *testing.T) {
	tests := []struct {
		name       string
		socketPath string
		wantResult bool
	}{
		{
			name:       "non-existent socket",
			socketPath: "/tmp/does-not-exist.sock",
			wantResult: false,
		},
		{
			name:       "empty path",
			socketPath: "",
			wantResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &APIClient{}
			result := client.trySocket(tt.socketPath)

			if result != tt.wantResult {
				t.Errorf("trySocket(%s) = %v, want %v", tt.socketPath, result, tt.wantResult)
			}
		})
	}

	// Test successful socket connection
	t.Run("successful socket connection", func(t *testing.T) {
		// Create a temporary Unix socket server
		socketPath := "/tmp/test-cbox-" + time.Now().Format("20060102150405") + ".sock"
		defer func() {
			// Clean up socket file
			_ = os.Remove(socketPath)
		}()

		// Start a Unix socket listener
		listener, err := net.Listen("unix", socketPath)
		if err != nil {
			t.Fatalf("Failed to create socket listener: %v", err)
		}
		defer listener.Close()

		// Test that trySocket succeeds with a real socket
		client := &APIClient{}
		result := client.trySocket(socketPath)

		if !result {
			t.Errorf("trySocket(%s) = false, want true for valid socket", socketPath)
		}
	})
}

// TestAPIClient_createSocketClient tests socket client creation
func TestAPIClient_createSocketClient(t *testing.T) {
	client := &APIClient{}
	socketPath := "/tmp/test.sock"

	httpClient := client.createSocketClient(socketPath)

	if httpClient == nil {
		t.Fatal("createSocketClient returned nil")
	}

	if httpClient.Timeout != 10*time.Second {
		t.Errorf("Expected timeout 10s, got %v", httpClient.Timeout)
	}

	if httpClient.Transport == nil {
		t.Fatal("Expected non-nil Transport")
	}

	// Test that the socket client can make actual requests
	t.Run("socket client with real server", func(t *testing.T) {
		socketPath := "/tmp/test-cbox-client-" + time.Now().Format("20060102150405") + ".sock"
		defer os.Remove(socketPath)

		// Create Unix socket HTTP server
		listener, err := net.Listen("unix", socketPath)
		if err != nil {
			t.Fatalf("Failed to create socket listener: %v", err)
		}
		defer listener.Close()

		// Start simple HTTP server on socket
		server := &http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			}),
		}
		go func() { _ = server.Serve(listener) }()
		defer server.Close()

		// Create client with socket
		apiClient := &APIClient{}
		httpClient := apiClient.createSocketClient(socketPath)

		// Make request through socket client
		req, err := http.NewRequest("GET", "http://unix/test", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			t.Fatalf("Request through socket client failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
	})
}

// TestAPIClient_DeleteProcess_ErrorPaths tests error handling in DeleteProcess
func TestAPIClient_DeleteProcess_ErrorPaths(t *testing.T) {
	tests := []struct {
		name           string
		serverResponse string
		serverStatus   int
		wantErr        bool
		errContains    string
	}{
		{
			name:           "not found error",
			serverResponse: `{"error": "process not found"}`,
			serverStatus:   http.StatusNotFound,
			wantErr:        true,
			errContains:    "delete failed",
		},
		{
			name:           "internal server error",
			serverResponse: `{"error": "internal error"}`,
			serverStatus:   http.StatusInternalServerError,
			wantErr:        true,
			errContains:    "delete failed",
		},
		{
			name:           "bad request",
			serverResponse: `{"error": "invalid process name"}`,
			serverStatus:   http.StatusBadRequest,
			wantErr:        true,
			errContains:    "delete failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.serverStatus)
				_, _ = w.Write([]byte(tt.serverResponse))
			}))
			defer server.Close()

			client := NewAPIClient(server.URL, "")
			err := client.DeleteProcess("test-process")

			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteProcess() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err != nil && tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("Expected error containing %q, got %v", tt.errContains, err)
			}
		})
	}
}

// TestAPIClient_DeleteProcess_NetworkError tests network failure handling
func TestAPIClient_DeleteProcess_NetworkError(t *testing.T) {
	client := &APIClient{
		baseURL: "http://localhost:0", // Invalid port
		client:  &http.Client{Timeout: 100 * time.Millisecond},
	}

	err := client.DeleteProcess("test-process")
	if err == nil {
		t.Fatal("Expected error for network failure, got nil")
	}

	if !strings.Contains(err.Error(), "failed to send request") {
		t.Errorf("Expected 'failed to send request' error, got %v", err)
	}
}

// TestAPIClient_UpdateProcess_ErrorPaths tests error handling in UpdateProcess
func TestAPIClient_UpdateProcess_ErrorPaths(t *testing.T) {
	tests := []struct {
		name           string
		processConfig  *config.Process
		serverStatus   int
		serverResponse string
		wantErr        bool
		errContains    string
	}{
		{
			name:          "nil process config",
			processConfig: nil,
			wantErr:       true,
			errContains:   "process configuration is required",
		},
		{
			name: "not found error",
			processConfig: &config.Process{
				Enabled: true,
				Command: []string{"test"},
				Scale:   1,
			},
			serverStatus:   http.StatusNotFound,
			serverResponse: `{"error": "process not found"}`,
			wantErr:        true,
			errContains:    "update failed",
		},
		{
			name: "bad request error",
			processConfig: &config.Process{
				Enabled: true,
				Command: []string{},
				Scale:   -1,
			},
			serverStatus:   http.StatusBadRequest,
			serverResponse: `{"error": "invalid config"}`,
			wantErr:        true,
			errContains:    "update failed",
		},
		{
			name: "internal server error",
			processConfig: &config.Process{
				Enabled: true,
				Command: []string{"test"},
				Scale:   1,
			},
			serverStatus:   http.StatusInternalServerError,
			serverResponse: `{"error": "server error"}`,
			wantErr:        true,
			errContains:    "update failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var server *httptest.Server
			if tt.processConfig != nil {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.serverStatus)
					_, _ = w.Write([]byte(tt.serverResponse))
				}))
				defer server.Close()
			}

			var client *APIClient
			if server != nil {
				client = NewAPIClient(server.URL, "")
			} else {
				client = NewAPIClient("http://localhost:9999", "")
			}

			err := client.UpdateProcess("test-process", tt.processConfig)

			if (err != nil) != tt.wantErr {
				t.Errorf("UpdateProcess() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err != nil && tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("Expected error containing %q, got %v", tt.errContains, err)
			}
		})
	}
}

// TestAPIClient_UpdateProcess_NetworkError tests network failure
func TestAPIClient_UpdateProcess_NetworkError(t *testing.T) {
	client := &APIClient{
		baseURL: "http://localhost:0",
		client:  &http.Client{Timeout: 100 * time.Millisecond},
	}

	proc := &config.Process{
		Enabled: true,
		Command: []string{"test"},
		Scale:   1,
	}

	err := client.UpdateProcess("test", proc)
	if err == nil {
		t.Fatal("Expected error for network failure, got nil")
	}

	if !strings.Contains(err.Error(), "failed to send request") {
		t.Errorf("Expected 'failed to send request' error, got %v", err)
	}
}

// TestAPIClient_fetchLogs_ErrorPaths tests error handling in fetchLogs
func TestAPIClient_fetchLogs_ErrorPaths(t *testing.T) {
	tests := []struct {
		name           string
		clientSetup    func() *APIClient
		path           string
		serverStatus   int
		serverResponse string
		wantErr        bool
		errContains    string
	}{
		{
			name: "nil HTTP client",
			clientSetup: func() *APIClient {
				return &APIClient{client: nil}
			},
			path:        "/api/v1/logs",
			wantErr:     true,
			errContains: "API client not initialized",
		},
		{
			name: "HTTP error status",
			clientSetup: func() *APIClient {
				return nil // Will be set by test
			},
			path:           "/api/v1/logs",
			serverStatus:   http.StatusNotFound,
			serverResponse: `{"error": "not found"}`,
			wantErr:        true,
			errContains:    "logs request failed",
		},
		{
			name: "invalid JSON response",
			clientSetup: func() *APIClient {
				return nil // Will be set by test
			},
			path:           "/api/v1/logs",
			serverStatus:   http.StatusOK,
			serverResponse: `invalid json{`,
			wantErr:        true,
			errContains:    "failed to decode logs response",
		},
		{
			name: "internal server error",
			clientSetup: func() *APIClient {
				return nil
			},
			path:           "/api/v1/logs",
			serverStatus:   http.StatusInternalServerError,
			serverResponse: `{"error": "internal error"}`,
			wantErr:        true,
			errContains:    "logs request failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var client *APIClient

			if tt.clientSetup != nil && tt.clientSetup() != nil {
				client = tt.clientSetup()
			} else {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.serverStatus)
					_, _ = w.Write([]byte(tt.serverResponse))
				}))
				defer server.Close()
				client = NewAPIClient(server.URL, "")
			}

			_, err := client.fetchLogs(tt.path)

			if (err != nil) != tt.wantErr {
				t.Errorf("fetchLogs() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err != nil && tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("Expected error containing %q, got %v", tt.errContains, err)
			}
		})
	}
}

// TestAPIClient_fetchLogs_NetworkError tests network failure
func TestAPIClient_fetchLogs_NetworkError(t *testing.T) {
	client := &APIClient{
		baseURL: "http://localhost:0",
		client:  &http.Client{Timeout: 100 * time.Millisecond},
	}

	_, err := client.fetchLogs("/api/v1/logs")
	if err == nil {
		t.Fatal("Expected error for network failure, got nil")
	}

	if !strings.Contains(err.Error(), "failed to fetch logs") {
		t.Errorf("Expected 'failed to fetch logs' error, got %v", err)
	}
}

// TestAPIClient_GetLogs_EmptyProcessName tests empty process name validation
func TestAPIClient_GetLogs_EmptyProcessName(t *testing.T) {
	client := NewAPIClient("http://localhost:9999", "")

	_, err := client.GetLogs("", 10)
	if err == nil {
		t.Fatal("Expected error for empty process name, got nil")
	}

	if !strings.Contains(err.Error(), "process name is required") {
		t.Errorf("Expected 'process name is required' error, got %v", err)
	}
}

// TestAPIClient_GetProcessConfig_ErrorPaths tests error handling in GetProcessConfig
func TestAPIClient_GetProcessConfig_ErrorPaths(t *testing.T) {
	tests := []struct {
		name           string
		processName    string
		serverStatus   int
		serverResponse string
		wantErr        bool
		errContains    string
	}{
		{
			name:        "empty process name",
			processName: "",
			wantErr:     true,
			errContains: "process name is required",
		},
		{
			name:           "not found error",
			processName:    "unknown",
			serverStatus:   http.StatusNotFound,
			serverResponse: `{"error": "not found"}`,
			wantErr:        true,
			errContains:    "process request failed",
		},
		{
			name:           "invalid JSON response",
			processName:    "test",
			serverStatus:   http.StatusOK,
			serverResponse: `invalid json{`,
			wantErr:        true,
			errContains:    "failed to decode process response",
		},
		{
			name:           "missing config in response",
			processName:    "test",
			serverStatus:   http.StatusOK,
			serverResponse: `{"config": null}`,
			wantErr:        true,
			errContains:    "process configuration missing in response",
		},
		{
			name:           "internal server error",
			processName:    "test",
			serverStatus:   http.StatusInternalServerError,
			serverResponse: `{"error": "server error"}`,
			wantErr:        true,
			errContains:    "process request failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var server *httptest.Server
			if tt.processName != "" {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.serverStatus)
					_, _ = w.Write([]byte(tt.serverResponse))
				}))
				defer server.Close()
			}

			var client *APIClient
			if server != nil {
				client = NewAPIClient(server.URL, "")
			} else {
				client = NewAPIClient("http://localhost:9999", "")
			}

			_, err := client.GetProcessConfig(tt.processName)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetProcessConfig() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err != nil && tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("Expected error containing %q, got %v", tt.errContains, err)
			}
		})
	}
}

// TestAPIClient_GetProcessConfig_NetworkError tests network failure
func TestAPIClient_GetProcessConfig_NetworkError(t *testing.T) {
	client := &APIClient{
		baseURL: "http://localhost:0",
		client:  &http.Client{Timeout: 100 * time.Millisecond},
	}

	_, err := client.GetProcessConfig("test")
	if err == nil {
		t.Fatal("Expected error for network failure, got nil")
	}

	if !strings.Contains(err.Error(), "failed to fetch process") {
		t.Errorf("Expected 'failed to fetch process' error, got %v", err)
	}
}

// TestAPIClient_ListProcesses_InvalidJSON tests invalid JSON response handling
func TestAPIClient_ListProcesses_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`invalid json{`))
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	_, err := client.ListProcesses()

	if err == nil {
		t.Fatal("Expected error for invalid JSON, got nil")
	}

	if !strings.Contains(err.Error(), "failed to decode response") {
		t.Errorf("Expected 'failed to decode response' error, got %v", err)
	}
}

// TestAPIClient_ReloadConfig tests configuration reload via API
func TestAPIClient_ReloadConfig(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   string
		wantErr    bool
	}{
		{
			name:       "successful reload",
			statusCode: http.StatusOK,
			response:   `{"message": "Configuration reloaded"}`,
			wantErr:    false,
		},
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			response:   `{"error": "Failed to reload"}`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("Expected POST, got %s", r.Method)
				}
				if r.URL.Path != "/api/v1/config/reload" {
					t.Errorf("Expected /api/v1/config/reload, got %s", r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewAPIClient(server.URL, "")
			err := client.ReloadConfig()

			if (err != nil) != tt.wantErr {
				t.Errorf("ReloadConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestAPIClient_SaveConfig tests configuration save via API
func TestAPIClient_SaveConfig(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   string
		wantErr    bool
	}{
		{
			name:       "successful save",
			statusCode: http.StatusOK,
			response:   `{"message": "Configuration saved"}`,
			wantErr:    false,
		},
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			response:   `{"error": "Failed to save"}`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("Expected POST, got %s", r.Method)
				}
				if r.URL.Path != "/api/v1/config/save" {
					t.Errorf("Expected /api/v1/config/save, got %s", r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewAPIClient(server.URL, "")
			err := client.SaveConfig()

			if (err != nil) != tt.wantErr {
				t.Errorf("SaveConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestAPIClient_PauseSchedule tests pausing scheduled jobs via API
func TestAPIClient_PauseSchedule(t *testing.T) {
	tests := []struct {
		name         string
		processName  string
		serverStatus int
		wantErr      bool
	}{
		{
			name:         "successful pause",
			processName:  "test-cron",
			serverStatus: http.StatusOK,
			wantErr:      false,
		},
		{
			name:         "accepted status",
			processName:  "scheduled-task",
			serverStatus: http.StatusAccepted,
			wantErr:      false,
		},
		{
			name:         "not found",
			processName:  "unknown",
			serverStatus: http.StatusNotFound,
			wantErr:      true,
		},
		{
			name:         "already paused",
			processName:  "paused-task",
			serverStatus: http.StatusBadRequest,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := "/api/v1/processes/" + tt.processName + "/schedule/pause"
				if r.URL.Path != expectedPath {
					t.Errorf("Expected path %s, got %s", expectedPath, r.URL.Path)
				}

				if r.Method != "POST" {
					t.Errorf("Expected POST method, got %s", r.Method)
				}

				w.WriteHeader(tt.serverStatus)
				if tt.wantErr {
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "operation failed"})
				}
			}))
			defer server.Close()

			client := NewAPIClient(server.URL, "")
			err := client.PauseSchedule(tt.processName)

			if (err != nil) != tt.wantErr {
				t.Errorf("PauseSchedule() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestAPIClient_ResumeSchedule tests resuming paused scheduled jobs via API
func TestAPIClient_ResumeSchedule(t *testing.T) {
	tests := []struct {
		name         string
		processName  string
		serverStatus int
		wantErr      bool
	}{
		{
			name:         "successful resume",
			processName:  "test-cron",
			serverStatus: http.StatusOK,
			wantErr:      false,
		},
		{
			name:         "accepted status",
			processName:  "scheduled-task",
			serverStatus: http.StatusAccepted,
			wantErr:      false,
		},
		{
			name:         "not found",
			processName:  "unknown",
			serverStatus: http.StatusNotFound,
			wantErr:      true,
		},
		{
			name:         "not paused",
			processName:  "running-task",
			serverStatus: http.StatusBadRequest,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := "/api/v1/processes/" + tt.processName + "/schedule/resume"
				if r.URL.Path != expectedPath {
					t.Errorf("Expected path %s, got %s", expectedPath, r.URL.Path)
				}

				if r.Method != "POST" {
					t.Errorf("Expected POST method, got %s", r.Method)
				}

				w.WriteHeader(tt.serverStatus)
				if tt.wantErr {
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "operation failed"})
				}
			}))
			defer server.Close()

			client := NewAPIClient(server.URL, "")
			err := client.ResumeSchedule(tt.processName)

			if (err != nil) != tt.wantErr {
				t.Errorf("ResumeSchedule() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestAPIClient_TriggerSchedule tests manually triggering scheduled jobs via API
func TestAPIClient_TriggerSchedule(t *testing.T) {
	tests := []struct {
		name         string
		processName  string
		serverStatus int
		wantErr      bool
	}{
		{
			name:         "successful trigger",
			processName:  "test-cron",
			serverStatus: http.StatusOK,
			wantErr:      false,
		},
		{
			name:         "accepted status",
			processName:  "scheduled-task",
			serverStatus: http.StatusAccepted,
			wantErr:      false,
		},
		{
			name:         "not found",
			processName:  "unknown",
			serverStatus: http.StatusNotFound,
			wantErr:      true,
		},
		{
			name:         "server error",
			processName:  "failing-task",
			serverStatus: http.StatusInternalServerError,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := "/api/v1/processes/" + tt.processName + "/schedule/trigger"
				if r.URL.Path != expectedPath {
					t.Errorf("Expected path %s, got %s", expectedPath, r.URL.Path)
				}

				if r.Method != "POST" {
					t.Errorf("Expected POST method, got %s", r.Method)
				}

				w.WriteHeader(tt.serverStatus)
				if tt.wantErr {
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "operation failed"})
				}
			}))
			defer server.Close()

			client := NewAPIClient(server.URL, "")
			err := client.TriggerSchedule(tt.processName)

			if (err != nil) != tt.wantErr {
				t.Errorf("TriggerSchedule() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestAPIClient_ScheduleActions_NetworkError tests network failure handling for schedule actions
func TestAPIClient_ScheduleActions_NetworkError(t *testing.T) {
	client := &APIClient{
		baseURL: "http://localhost:0", // Invalid port
		client:  &http.Client{Timeout: 100 * time.Millisecond},
	}

	// Test PauseSchedule
	err := client.PauseSchedule("test")
	if err == nil {
		t.Error("Expected error for PauseSchedule network failure, got nil")
	}

	// Test ResumeSchedule
	err = client.ResumeSchedule("test")
	if err == nil {
		t.Error("Expected error for ResumeSchedule network failure, got nil")
	}

	// Test TriggerSchedule
	err = client.TriggerSchedule("test")
	if err == nil {
		t.Error("Expected error for TriggerSchedule network failure, got nil")
	}
}

// TestAPIClient_ScheduleActions_WithAuth tests schedule actions with authentication
func TestAPIClient_ScheduleActions_WithAuth(t *testing.T) {
	expectedAuth := "schedule-token-123"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+expectedAuth {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, expectedAuth)

	// Test PauseSchedule with auth
	err := client.PauseSchedule("test-cron")
	if err != nil {
		t.Errorf("PauseSchedule() with auth failed: %v", err)
	}

	// Test ResumeSchedule with auth
	err = client.ResumeSchedule("test-cron")
	if err != nil {
		t.Errorf("ResumeSchedule() with auth failed: %v", err)
	}

	// Test TriggerSchedule with auth
	err = client.TriggerSchedule("test-cron")
	if err != nil {
		t.Errorf("TriggerSchedule() with auth failed: %v", err)
	}
}

// TestAPIClient_GetOneshotHistory tests oneshot history retrieval via API
func TestAPIClient_GetOneshotHistory(t *testing.T) {
	tests := []struct {
		name       string
		limit      int
		statusCode int
		response   string
		wantCount  int
		wantErr    bool
	}{
		{
			name:       "successful with results",
			limit:      10,
			statusCode: http.StatusOK,
			response: `{
				"executions": [
					{"process_name": "test-oneshot", "exit_code": 0, "started_at": "2025-01-01T00:00:00Z"}
				],
				"count": 1
			}`,
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:       "empty results",
			limit:      10,
			statusCode: http.StatusOK,
			response:   `{"executions": [], "count": 0}`,
			wantCount:  0,
			wantErr:    false,
		},
		{
			name:       "no limit specified",
			limit:      0,
			statusCode: http.StatusOK,
			response:   `{"executions": [], "count": 0}`,
			wantCount:  0,
			wantErr:    false,
		},
		{
			name:       "server error",
			limit:      10,
			statusCode: http.StatusInternalServerError,
			response:   `{"error": "Server error"}`,
			wantCount:  0,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("Expected GET, got %s", r.Method)
				}
				if !strings.HasPrefix(r.URL.Path, "/api/v1/oneshot/history") {
					t.Errorf("Expected /api/v1/oneshot/history, got %s", r.URL.Path)
				}
				if tt.limit > 0 {
					limitParam := r.URL.Query().Get("limit")
					if limitParam != fmt.Sprintf("%d", tt.limit) {
						t.Errorf("Expected limit=%d, got %s", tt.limit, limitParam)
					}
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewAPIClient(server.URL, "")
			results, err := client.GetOneshotHistory(tt.limit)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetOneshotHistory() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && len(results) != tt.wantCount {
				t.Errorf("GetOneshotHistory() got %d results, want %d", len(results), tt.wantCount)
			}
		})
	}
}
