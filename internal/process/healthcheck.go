package process

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"time"

	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/metrics"
)

// HealthChecker defines the interface for health checks.
// Implementations probe service health and return nil on success
// or an error describing the failure.
//
// Health checks are used for:
//   - Readiness probes: Determine when a service is ready to accept traffic
//   - Liveness probes: Detect when a service needs to be restarted
//   - Dependency ordering: Wait for upstream services before starting dependents
//
// The Check method should respect context cancellation and timeouts.
type HealthChecker interface {
	Check(ctx context.Context) error
}

// NewHealthChecker creates the appropriate health checker based on configuration.
// Returns a HealthChecker implementation based on the configured type:
//   - "tcp": TCPHealthChecker that verifies TCP port connectivity
//   - "http": HTTPHealthChecker that performs HTTP GET and validates status code
//   - "exec": ExecHealthChecker that runs a command and checks exit code
//   - nil config: NoOpHealthChecker that always succeeds
//
// Returns an error for unknown health check types.
func NewHealthChecker(cfg *config.HealthCheck) (HealthChecker, error) {
	if cfg == nil {
		return &NoOpHealthChecker{}, nil
	}

	switch cfg.Type {
	case "tcp":
		return &TCPHealthChecker{address: cfg.Address}, nil
	case "http":
		return &HTTPHealthChecker{
			url:            cfg.URL,
			expectedStatus: cfg.ExpectedStatus,
		}, nil
	case "exec":
		return &ExecHealthChecker{command: cfg.Command}, nil
	default:
		return nil, fmt.Errorf("unknown health check type: %s", cfg.Type)
	}
}

// NoOpHealthChecker always succeeds (for processes without health checks)
type NoOpHealthChecker struct{}

func (n *NoOpHealthChecker) Check(ctx context.Context) error {
	return nil
}

// TCPHealthChecker checks if TCP port is accepting connections
type TCPHealthChecker struct {
	address string
}

func (t *TCPHealthChecker) Check(ctx context.Context) error {
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", t.address)
	if err != nil {
		return fmt.Errorf("tcp connection failed: %w", err)
	}
	conn.Close()
	return nil
}

// HTTPHealthChecker performs HTTP GET and validates status code
type HTTPHealthChecker struct {
	url            string
	expectedStatus int
}

func (h *HTTPHealthChecker) Check(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", h.url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != h.expectedStatus {
		return fmt.Errorf("unexpected status code: got %d, want %d", resp.StatusCode, h.expectedStatus)
	}

	return nil
}

// ExecHealthChecker runs a command and checks exit code
type ExecHealthChecker struct {
	command []string
}

func (e *ExecHealthChecker) Check(ctx context.Context) error {
	if len(e.command) == 0 {
		return fmt.Errorf("no command specified")
	}

	cmd := exec.CommandContext(ctx, e.command[0], e.command[1:]...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("health check command failed: %w", err)
	}

	return nil
}

// HealthMonitor continuously monitors process health using the configured checker.
// It runs health checks at regular intervals and tracks consecutive successes/failures
// to determine overall health status with hysteresis (avoiding flapping).
//
// HealthMonitor supports two modes:
//   - Readiness: Used during startup to determine when service is ready
//   - Liveness: Used during runtime to detect unhealthy services
//
// The monitor emits health status updates through a channel that the Supervisor
// uses to trigger restarts or mark services as ready.
type HealthMonitor struct {
	processName        string
	checker            HealthChecker
	config             *config.HealthCheck
	consecutiveFails   int
	consecutiveSuccess int
	currentlyHealthy   bool
	logger             *slog.Logger
}

// NewHealthMonitor creates a new health monitor
func NewHealthMonitor(processName string, cfg *config.HealthCheck, log *slog.Logger) (*HealthMonitor, error) {
	checker, err := NewHealthChecker(cfg)
	if err != nil {
		return nil, err
	}

	return &HealthMonitor{
		processName:      processName,
		checker:          checker,
		config:           cfg,
		currentlyHealthy: true, // Start optimistic
		logger:           log,
	}, nil
}

// Start begins health check monitoring
func (hm *HealthMonitor) Start(ctx context.Context) <-chan HealthStatus {
	statusCh := make(chan HealthStatus, 1)

	go func() {
		defer close(statusCh)

		// Wait for initial delay
		if hm.config != nil && hm.config.InitialDelay > 0 {
			select {
			case <-time.After(time.Duration(hm.config.InitialDelay) * time.Second):
			case <-ctx.Done():
				return
			}
		}

		status := hm.performCheck(ctx)
		statusCh <- status

		ticker := time.NewTicker(time.Duration(hm.config.Period) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				status := hm.performCheck(ctx)
				statusCh <- status
			case <-ctx.Done():
				return
			}
		}
	}()

	return statusCh
}

func (hm *HealthMonitor) performCheck(ctx context.Context) HealthStatus {
	checkCtx, cancel := context.WithTimeout(ctx, time.Duration(hm.config.Timeout)*time.Second)
	defer cancel()

	// Measure health check duration
	startTime := time.Now()
	err := hm.checker.Check(checkCtx)
	duration := time.Since(startTime).Seconds()

	// Determine success threshold (default 1 if not configured)
	successThreshold := hm.config.SuccessThreshold
	if successThreshold <= 0 {
		successThreshold = 1
	}

	if err != nil {
		// Health check failed
		hm.consecutiveFails++
		hm.consecutiveSuccess = 0 // Reset success counter on failure

		// Record metrics for failure
		metrics.RecordHealthCheck(hm.processName, hm.config.Type, duration, false)
		metrics.RecordHealthCheckFailures(hm.processName, hm.consecutiveFails)

		hm.logger.Warn("Health check failed",
			"consecutive_fails", hm.consecutiveFails,
			"failure_threshold", hm.config.FailureThreshold,
			"error", err,
		)

		// Mark as unhealthy only if we've exceeded failure threshold
		if hm.consecutiveFails >= hm.config.FailureThreshold {
			if hm.currentlyHealthy {
				hm.logger.Error("Process marked as unhealthy",
					"consecutive_fails", hm.consecutiveFails,
				)
				hm.currentlyHealthy = false
			}
			return HealthStatus{Healthy: false, LastCheckSucceeded: false, Error: err}
		}
		// Still considered healthy until threshold reached (for liveness)
		// But LastCheckSucceeded=false indicates this check failed (for readiness)
		return HealthStatus{Healthy: true, LastCheckSucceeded: false, Error: nil}
	}

	// Health check succeeded
	hm.consecutiveSuccess++
	hm.consecutiveFails = 0 // Reset failure counter on success

	// Record metrics for success
	metrics.RecordHealthCheck(hm.processName, hm.config.Type, duration, true)
	metrics.RecordHealthCheckFailures(hm.processName, 0)

	// If currently unhealthy, require success threshold to be met before recovering
	if !hm.currentlyHealthy {
		if hm.consecutiveSuccess >= successThreshold {
			hm.logger.Info("Health check recovered",
				"consecutive_successes", hm.consecutiveSuccess,
				"success_threshold", successThreshold,
			)
			hm.currentlyHealthy = true
			return HealthStatus{Healthy: true, LastCheckSucceeded: true, Error: nil}
		}
		// Still unhealthy, waiting for more successes
		// But the check DID succeed, so LastCheckSucceeded=true
		hm.logger.Debug("Health check succeeded but waiting for threshold",
			"consecutive_successes", hm.consecutiveSuccess,
			"success_threshold", successThreshold,
		)
		return HealthStatus{Healthy: false, LastCheckSucceeded: true, Error: nil}
	}

	// Already healthy and check succeeded
	return HealthStatus{Healthy: true, LastCheckSucceeded: true, Error: nil}
}

// HealthStatus represents the result of a health check
type HealthStatus struct {
	Healthy            bool  // Whether process should be considered healthy (for liveness/restart decisions)
	LastCheckSucceeded bool  // Whether the most recent health check actually succeeded (for readiness)
	Error              error // Error from the health check, if any
}
