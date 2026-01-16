package process

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/config"
)

func TestNewHealthChecker(t *testing.T) {
	tests := []struct {
		name     string
		config   *config.HealthCheck
		wantErr  bool
		wantType string
	}{
		{
			name:     "nil config returns NoOpHealthChecker",
			config:   nil,
			wantErr:  false,
			wantType: "*process.NoOpHealthChecker",
		},
		{
			name: "tcp health check",
			config: &config.HealthCheck{
				Type:    "tcp",
				Address: "localhost:9000",
			},
			wantErr:  false,
			wantType: "*process.TCPHealthChecker",
		},
		{
			name: "http health check",
			config: &config.HealthCheck{
				Type:           "http",
				URL:            "http://localhost:9180/health",
				ExpectedStatus: 200,
			},
			wantErr:  false,
			wantType: "*process.HTTPHealthChecker",
		},
		{
			name: "exec health check",
			config: &config.HealthCheck{
				Type:    "exec",
				Command: []string{"echo", "healthy"},
			},
			wantErr:  false,
			wantType: "*process.ExecHealthChecker",
		},
		{
			name: "unknown health check type",
			config: &config.HealthCheck{
				Type: "unknown",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker, err := NewHealthChecker(tt.config)

			if tt.wantErr {
				if err == nil {
					t.Errorf("NewHealthChecker() expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("NewHealthChecker() unexpected error: %v", err)
				return
			}

			if checker == nil {
				t.Errorf("NewHealthChecker() returned nil checker")
			}
		})
	}
}

func TestNoOpHealthChecker(t *testing.T) {
	checker := &NoOpHealthChecker{}
	ctx := context.Background()

	err := checker.Check(ctx)
	if err != nil {
		t.Errorf("NoOpHealthChecker.Check() unexpected error: %v", err)
	}
}

func TestTCPHealthChecker(t *testing.T) {
	// Start a test TCP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start test server: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	tests := []struct {
		name    string
		address string
		wantErr bool
	}{
		{
			name:    "successful connection",
			address: addr,
			wantErr: false,
		},
		{
			name:    "failed connection - port not listening",
			address: "127.0.0.1:65535",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &TCPHealthChecker{address: tt.address}
			ctx := context.Background()

			err := checker.Check(ctx)

			if tt.wantErr && err == nil {
				t.Errorf("TCPHealthChecker.Check() expected error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("TCPHealthChecker.Check() unexpected error: %v", err)
			}
		})
	}
}

func TestHTTPHealthChecker(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthy" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		wantErr        bool
	}{
		{
			name:           "successful health check",
			url:            server.URL + "/healthy",
			expectedStatus: 200,
			wantErr:        false,
		},
		{
			name:           "wrong status code",
			url:            server.URL + "/unhealthy",
			expectedStatus: 200,
			wantErr:        true,
		},
		{
			name:           "invalid URL",
			url:            "http://localhost:99999/health",
			expectedStatus: 200,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &HTTPHealthChecker{
				url:            tt.url,
				expectedStatus: tt.expectedStatus,
			}
			ctx := context.Background()

			err := checker.Check(ctx)

			if tt.wantErr && err == nil {
				t.Errorf("HTTPHealthChecker.Check() expected error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("HTTPHealthChecker.Check() unexpected error: %v", err)
			}
		})
	}
}

func TestExecHealthChecker(t *testing.T) {
	tests := []struct {
		name    string
		command []string
		wantErr bool
	}{
		{
			name:    "successful command",
			command: []string{"echo", "healthy"},
			wantErr: false,
		},
		{
			name:    "failing command",
			command: []string{"false"},
			wantErr: true,
		},
		{
			name:    "non-existent command",
			command: []string{"nonexistent-command"},
			wantErr: true,
		},
		{
			name:    "empty command",
			command: []string{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &ExecHealthChecker{command: tt.command}
			ctx := context.Background()

			err := checker.Check(ctx)

			if tt.wantErr && err == nil {
				t.Errorf("ExecHealthChecker.Check() expected error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ExecHealthChecker.Check() unexpected error: %v", err)
			}
		})
	}
}

func TestHealthMonitor(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	t.Run("successful monitoring", func(t *testing.T) {
		cfg := &config.HealthCheck{
			Type:             "tcp",
			Address:          "127.0.0.1:0", // Will fail but good for testing
			InitialDelay:     0,
			Period:           1,
			Timeout:          1,
			FailureThreshold: 2,
		}

		monitor, err := NewHealthMonitor("test-process", cfg, logger)
		if err != nil {
			t.Fatalf("NewHealthMonitor() unexpected error: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		statusCh := monitor.Start(ctx)

		// Collect some status updates
		statusCount := 0
		for status := range statusCh {
			statusCount++
			if statusCount >= 2 {
				cancel()
				break
			}
			// We expect unhealthy status since we're connecting to a non-listening port
			_ = status
		}

		if statusCount < 2 {
			t.Errorf("Expected at least 2 status updates, got %d", statusCount)
		}
	})

	t.Run("context cancellation stops monitoring", func(t *testing.T) {
		cfg := &config.HealthCheck{
			Type:         "tcp",
			Address:      "127.0.0.1:0",
			InitialDelay: 0,
			Period:       1,
			Timeout:      1,
		}

		monitor, err := NewHealthMonitor("test-process", cfg, logger)
		if err != nil {
			t.Fatalf("NewHealthMonitor() unexpected error: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		statusCh := monitor.Start(ctx)

		// Cancel immediately
		cancel()

		// Channel should close quickly
		select {
		case <-statusCh:
		case <-time.After(2 * time.Second):
			t.Errorf("Channel did not close after context cancellation")
		}
	})
}

func TestHealthMonitor_FailureThreshold(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg := &config.HealthCheck{
		Type:             "tcp",
		Address:          "127.0.0.1:65535", // Non-listening port
		InitialDelay:     0,
		Period:           1,
		Timeout:          1,
		FailureThreshold: 3,
	}

	monitor, err := NewHealthMonitor("test-process", cfg, logger)
	if err != nil {
		t.Fatalf("NewHealthMonitor() unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	statusCh := monitor.Start(ctx)

	// Count failures until threshold is reached
	failureCount := 0
	var finalStatus HealthStatus
	for status := range statusCh {
		if !status.Healthy {
			finalStatus = status
			break
		}
		failureCount++
		if failureCount >= 5 {
			t.Errorf("Expected unhealthy status after failure threshold, but didn't get one")
			cancel()
			break
		}
	}

	if finalStatus.Healthy {
		t.Errorf("Expected unhealthy status after failure threshold")
	}
	if finalStatus.Error == nil {
		t.Errorf("Expected error in unhealthy status")
	}
}
