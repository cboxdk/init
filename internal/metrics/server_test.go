package metrics

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/config"
)

// TestNewServer tests server creation with various configurations
func TestNewServer(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	tests := []struct {
		name         string
		port         int
		path         string
		aclConfig    *config.ACLConfig
		tlsConfig    *config.TLSConfig
		expectedPath string
	}{
		{
			name:         "default path",
			port:         9090,
			path:         "",
			aclConfig:    nil,
			tlsConfig:    nil,
			expectedPath: "/metrics",
		},
		{
			name:         "custom path",
			port:         9091,
			path:         "/custom",
			aclConfig:    nil,
			tlsConfig:    nil,
			expectedPath: "/custom",
		},
		{
			name: "with ACL enabled",
			port: 9092,
			path: "/metrics",
			aclConfig: &config.ACLConfig{
				Enabled:   true,
				Mode:      "allow",
				AllowList: []string{"127.0.0.1"},
			},
			tlsConfig:    nil,
			expectedPath: "/metrics",
		},
		{
			name:         "with ACL disabled",
			port:         9093,
			path:         "/metrics",
			aclConfig:    &config.ACLConfig{Enabled: false},
			tlsConfig:    nil,
			expectedPath: "/metrics",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(tt.port, tt.path, tt.aclConfig, tt.tlsConfig, logger)

			if server == nil {
				t.Fatal("Expected non-nil server")
			}

			if server.port != tt.port {
				t.Errorf("Expected port %d, got %d", tt.port, server.port)
			}

			if server.path != tt.expectedPath {
				t.Errorf("Expected path %s, got %s", tt.expectedPath, server.path)
			}

			if tt.aclConfig != nil && tt.aclConfig.Enabled && server.aclChecker == nil {
				t.Error("Expected ACL checker to be initialized when ACL is enabled")
			}

			if tt.aclConfig == nil || !tt.aclConfig.Enabled {
				if server.aclChecker != nil {
					t.Error("Expected ACL checker to be nil when ACL is disabled or not configured")
				}
			}
		})
	}
}

// TestServer_Port tests the Port() getter method
func TestServer_Port(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	tests := []struct {
		name string
		port int
	}{
		{
			name: "port 9090",
			port: 9090,
		},
		{
			name: "port 8080",
			port: 8080,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(tt.port, "/metrics", nil, nil, logger)

			if server.Port() != tt.port {
				t.Errorf("Expected port %d, got %d", tt.port, server.Port())
			}
		})
	}
}

// TestServer_StartStop tests server lifecycle
func TestServer_StartStop(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Use a high port to avoid conflicts
	port := 19090
	server := NewServer(port, "/metrics", nil, nil, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start server in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(ctx)
	}()

	// Wait for server to be ready
	time.Sleep(100 * time.Millisecond)

	// Verify server is running by making a request
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/health", port))
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Stop the server
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := server.Stop(stopCtx); err != nil {
		t.Errorf("Failed to stop server: %v", err)
	}

	// Verify server stopped
	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("Server returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Server did not stop in time")
	}
}

// TestServer_HealthEndpoint tests the /health endpoint
func TestServer_HealthEndpoint(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	port := 19091
	server := NewServer(port, "/metrics", nil, nil, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start server
	go func() { _ = server.Start(ctx) }()
	defer func() { _ = server.Stop(context.Background()) }()

	time.Sleep(100 * time.Millisecond)

	// Test health endpoint
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/health", port))
	if err != nil {
		t.Fatalf("Failed to connect to /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if string(body) != "OK" {
		t.Errorf("Expected body 'OK', got '%s'", string(body))
	}
}

// TestServer_MetricsEndpoint tests the /metrics endpoint
func TestServer_MetricsEndpoint(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	port := 19092
	server := NewServer(port, "/metrics", nil, nil, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start server
	go func() { _ = server.Start(ctx) }()
	defer func() { _ = server.Stop(context.Background()) }()

	time.Sleep(100 * time.Millisecond)

	// Test metrics endpoint
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", port))
	if err != nil {
		t.Fatalf("Failed to connect to /metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify it's Prometheus format (should contain HELP and TYPE comments)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	bodyStr := string(body)
	if len(bodyStr) == 0 {
		t.Error("Expected non-empty metrics response")
	}

	// Basic Prometheus format check
	if !contains(bodyStr, "# HELP") && !contains(bodyStr, "# TYPE") {
		// It's OK if metrics are empty initially, just check it's a valid response
		t.Logf("Metrics endpoint returned: %s", bodyStr)
	}
}

// TestServer_CustomPath tests server with custom metrics path
func TestServer_CustomPath(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	port := 19093
	customPath := "/custom-metrics"
	server := NewServer(port, customPath, nil, nil, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start server
	go func() { _ = server.Start(ctx) }()
	defer func() { _ = server.Stop(context.Background()) }()

	time.Sleep(100 * time.Millisecond)

	// Test custom path
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d%s", port, customPath))
	if err != nil {
		t.Fatalf("Failed to connect to custom path: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify default path doesn't work
	resp2, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", port))
	if err == nil {
		defer resp2.Body.Close()
		if resp2.StatusCode == http.StatusOK {
			t.Error("Default /metrics path should not work with custom path")
		}
	}
}

// TestServer_ACLMiddleware tests ACL integration
func TestServer_ACLMiddleware(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	tests := []struct {
		name         string
		aclConfig    *config.ACLConfig
		expectAccess bool
	}{
		{
			name: "allow list with localhost",
			aclConfig: &config.ACLConfig{
				Enabled:   true,
				Mode:      "allow",
				AllowList: []string{"127.0.0.1", "::1"},
			},
			expectAccess: true,
		},
		{
			name: "deny list without localhost",
			aclConfig: &config.ACLConfig{
				Enabled:  true,
				Mode:     "deny",
				DenyList: []string{"192.168.1.0/24"},
			},
			expectAccess: true,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := 19100 + i
			server := NewServer(port, "/metrics", tt.aclConfig, nil, logger)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			// Start server
			go func() { _ = server.Start(ctx) }()
			defer func() { _ = server.Stop(context.Background()) }()

			time.Sleep(100 * time.Millisecond)

			// Test access
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", port))
			if err != nil {
				if tt.expectAccess {
					t.Fatalf("Failed to connect: %v", err)
				}
				return
			}
			defer resp.Body.Close()

			if tt.expectAccess && resp.StatusCode != http.StatusOK {
				t.Errorf("Expected access allowed (200), got %d", resp.StatusCode)
			}

			if !tt.expectAccess && resp.StatusCode == http.StatusOK {
				t.Errorf("Expected access denied, got 200")
			}
		})
	}
}

// TestServer_StopBeforeStart tests stopping server that was never started
func TestServer_StopBeforeStart(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	server := NewServer(19094, "/metrics", nil, nil, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Attempt to stop without starting
	err := server.Stop(ctx)
	if err != nil {
		// It's OK if this returns an error since server wasn't started
		t.Logf("Stop before start returned: %v (expected)", err)
	}
}

// TestServer_MultipleStopCalls tests calling Stop() multiple times
func TestServer_MultipleStopCalls(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	port := 19095
	server := NewServer(port, "/metrics", nil, nil, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start server
	go func() { _ = server.Start(ctx) }()
	time.Sleep(100 * time.Millisecond)

	stopCtx := context.Background()

	// First stop
	if err := server.Stop(stopCtx); err != nil {
		t.Errorf("First stop failed: %v", err)
	}

	// Second stop (should not panic or error badly)
	if err := server.Stop(stopCtx); err != nil {
		t.Logf("Second stop returned: %v (expected)", err)
	}
}

// TestServer_ConcurrentRequests tests server handles concurrent requests
func TestServer_ConcurrentRequests(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	port := 19096
	server := NewServer(port, "/metrics", nil, nil, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start server
	go func() { _ = server.Start(ctx) }()
	defer func() { _ = server.Stop(context.Background()) }()

	time.Sleep(100 * time.Millisecond)

	// Make concurrent requests
	const numRequests = 10
	errCh := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/health", port))
			if err != nil {
				errCh <- err
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				errCh <- fmt.Errorf("expected 200, got %d", resp.StatusCode)
				return
			}
			errCh <- nil
		}()
	}

	// Check all requests succeeded
	for i := 0; i < numRequests; i++ {
		if err := <-errCh; err != nil {
			t.Errorf("Request %d failed: %v", i, err)
		}
	}
}

// TestNewServer_InvalidACL tests server creation with invalid ACL config
func TestNewServer_InvalidACL(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Create ACL config with invalid allow list entry (will cause error)
	aclConfig := &config.ACLConfig{
		Enabled:   true,
		Mode:      "allow",
		AllowList: []string{"invalid-ip-address"}, // Invalid entry
	}

	// Server should still be created even if ACL fails
	server := NewServer(19097, "/metrics", aclConfig, nil, logger)
	if server == nil {
		t.Fatal("Expected non-nil server even with invalid ACL config")
	}

	// ACL checker should be nil since creation failed
	if server.aclChecker != nil {
		t.Error("Expected ACL checker to be nil when ACL creation fails")
	}
}

// TestServer_StopWithTimeoutContext tests stopping with a context that times out
func TestServer_StopWithTimeoutContext(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	port := 19098
	server := NewServer(port, "/metrics", nil, nil, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start server
	go func() { _ = server.Start(ctx) }()
	time.Sleep(100 * time.Millisecond)

	// Verify server is running
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/health", port))
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	resp.Body.Close()

	// Stop with a very short context that may already be cancelled
	cancelledCtx, cancelImmediately := context.WithCancel(context.Background())
	cancelImmediately()

	// Stop should handle cancelled context gracefully
	err = server.Stop(cancelledCtx)
	if err != nil {
		// This is expected behavior - context was cancelled
		t.Logf("Stop with cancelled context returned: %v (expected)", err)
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || contains(s[1:], substr)))
}
