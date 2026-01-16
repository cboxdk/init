// Package readiness manages container readiness file for Kubernetes integration.
//
// The readiness manager creates a file (e.g., /tmp/cbox-ready) when all tracked
// processes are healthy/running, enabling Kubernetes readiness probes to determine
// if the container is ready to receive traffic.
//
// # Overview
//
// The manager supports two readiness modes:
//   - "all_healthy": All tracked processes must pass health checks (default)
//   - "all_running": All tracked processes just need to be running
//
// # Usage
//
// Configure readiness in your cbox-init.yaml:
//
//	global:
//	  readiness:
//	    enabled: true
//	    path: "/tmp/cbox-ready"
//	    mode: "all_healthy"
//	    processes:
//	      - php-fpm
//	      - nginx
//
// Then configure a Kubernetes readiness probe:
//
//	readinessProbe:
//	  exec:
//	    command: ["test", "-f", "/tmp/cbox-ready"]
//	  initialDelaySeconds: 5
//	  periodSeconds: 5
//
// # File Lifecycle
//
//   - Startup: File does not exist (not ready)
//   - All processes ready: File is created at configured path
//   - Any process unhealthy: File is removed
//   - Shutdown: File is removed during graceful shutdown
package readiness

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cboxdk/init/internal/config"
)

// ProcessState represents a process state for readiness evaluation.
// These states map to the internal process manager states but are
// simplified for readiness determination.
type ProcessState string

const (
	// StateRunning indicates the process is running but health status is unknown.
	StateRunning ProcessState = "running"

	// StateHealthy indicates the process is running and has passed health checks.
	StateHealthy ProcessState = "healthy"

	// StateUnhealthy indicates the process is running but has failed health checks.
	StateUnhealthy ProcessState = "unhealthy"

	// StateStopped indicates the process is not running (graceful stop).
	StateStopped ProcessState = "stopped"

	// StateFailed indicates the process has crashed or failed to start.
	StateFailed ProcessState = "failed"
)

// ProcessStatus provides process state information for readiness evaluation.
type ProcessStatus struct {
	// Name is the process name as defined in configuration.
	Name string

	// State is the current process state (running, healthy, unhealthy, stopped, failed).
	State ProcessState

	// Health is the health check result: "healthy", "unhealthy", or "unknown".
	Health string
}

// Manager manages the container readiness file lifecycle.
//
// The manager tracks process states and creates/removes a readiness file
// based on the configured mode. It is thread-safe and can be used
// concurrently from multiple goroutines.
//
// The zero value is not usable; use NewManager to create a Manager.
type Manager struct {
	config    *config.ReadinessConfig
	logger    *slog.Logger
	mu        sync.RWMutex
	isReady   bool
	processes map[string]ProcessStatus // tracked processes
	stopCh    chan struct{}
	stopped   bool
}

// NewManager creates a new readiness manager with the given configuration.
//
// The manager is initialized but not started. Call Start to begin readiness
// tracking. If cfg is nil or cfg.Enabled is false, the manager will be
// created but most operations will be no-ops.
//
// The logger is tagged with "component"="readiness" for structured logging.
func NewManager(cfg *config.ReadinessConfig, logger *slog.Logger) *Manager {
	return &Manager{
		config:    cfg,
		logger:    logger.With("component", "readiness"),
		processes: make(map[string]ProcessStatus),
		stopCh:    make(chan struct{}),
	}
}

// Start begins the readiness manager and prepares for process tracking.
//
// If readiness is disabled (config nil or Enabled=false), Start returns
// immediately without error. Otherwise, it:
//   - Creates the directory for the readiness file if it doesn't exist
//   - Removes any existing readiness file (container starts as not ready)
//
// The context is accepted for future use but is not currently used for
// cancellation.
//
// Returns an error if the readiness directory cannot be created.
func (m *Manager) Start(ctx context.Context) error {
	if m.config == nil || !m.config.Enabled {
		return nil
	}

	m.logger.Info("Starting readiness manager",
		"path", m.config.Path,
		"mode", m.config.Mode,
	)

	// Ensure the directory exists
	dir := filepath.Dir(m.config.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create readiness directory: %w", err)
	}

	// Initially remove readiness file (not ready yet)
	m.removeReadinessFile()

	return nil
}

// Stop gracefully shuts down the readiness manager.
//
// It removes the readiness file (marking container as not ready) and
// closes internal channels. Stop is idempotent - calling it multiple
// times is safe and subsequent calls return nil immediately.
//
// This method should be called during container shutdown to ensure
// Kubernetes removes the pod from service before processes terminate.
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stopped {
		return nil
	}
	m.stopped = true

	close(m.stopCh)

	// Remove readiness file on shutdown
	m.removeReadinessFile()

	m.logger.Info("Readiness manager stopped")
	return nil
}

// SetTrackedProcesses configures which processes to monitor for readiness.
//
// This method replaces any existing tracked processes with the provided list.
// All processes start in StateStopped with "unknown" health status.
//
// Call this method during initialization before processes start. The process
// manager typically calls this based on the configuration's process list
// or the readiness.processes filter.
//
// This method is thread-safe.
func (m *Manager) SetTrackedProcesses(processNames []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Clear existing and add new
	m.processes = make(map[string]ProcessStatus)
	for _, name := range processNames {
		m.processes[name] = ProcessStatus{
			Name:   name,
			State:  StateStopped,
			Health: "unknown",
		}
	}

	m.logger.Debug("Set tracked processes", "processes", processNames)
}

// UpdateProcessState updates the state and health of a tracked process.
//
// The method automatically re-evaluates overall readiness after each update,
// potentially creating or removing the readiness file.
//
// Process filtering logic:
//   - If process is in the tracked list, it is always updated
//   - If config.Processes is empty (track all), any process is added/updated
//   - If config.Processes is set, only those processes are tracked
//
// The health parameter should be one of: "healthy", "unhealthy", or "unknown".
//
// This method is thread-safe and is typically called by the process manager
// when process states change.
func (m *Manager) UpdateProcessState(name string, state ProcessState, health string) {
	if m.config == nil || !m.config.Enabled {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Only track if process is in our tracked list
	if _, tracked := m.processes[name]; !tracked {
		// If no specific processes configured, track all
		if len(m.config.Processes) > 0 {
			return
		}
	}

	prev := m.processes[name]
	m.processes[name] = ProcessStatus{
		Name:   name,
		State:  state,
		Health: health,
	}

	if prev.State != state || prev.Health != health {
		m.logger.Debug("Process state updated",
			"process", name,
			"prev_state", prev.State,
			"new_state", state,
			"prev_health", prev.Health,
			"new_health", health,
		)
	}

	// Re-evaluate readiness
	m.evaluateReadiness()
}

// RemoveProcess removes a process from readiness tracking.
//
// After removal, the process no longer affects readiness evaluation.
// This is typically called when a process is stopped intentionally
// or removed from the configuration.
//
// This method is thread-safe and triggers a readiness re-evaluation.
func (m *Manager) RemoveProcess(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.processes, name)
	m.evaluateReadiness()
}

// IsReady returns whether the container is currently ready.
//
// A container is ready when all tracked processes meet the readiness
// criteria defined by the configured mode (all_healthy or all_running).
//
// This method is thread-safe and can be called from any goroutine.
func (m *Manager) IsReady() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isReady
}

// evaluateReadiness checks if all tracked processes meet readiness criteria.
//
// This internal method iterates through all tracked processes and determines
// if the container should be marked as ready or not ready.
//
// Logic:
//   - If no processes are tracked, the container is not ready
//   - If any tracked process fails isProcessReady, the container is not ready
//   - Only when ALL tracked processes are ready is the container marked ready
//
// Must be called with the mutex lock held.
func (m *Manager) evaluateReadiness() {
	if len(m.processes) == 0 {
		// No processes to track, not ready
		m.setReady(false)
		return
	}

	ready := true
	for name, status := range m.processes {
		if !m.isProcessReady(status) {
			m.logger.Debug("Process not ready",
				"process", name,
				"state", status.State,
				"health", status.Health,
				"mode", m.config.Mode,
			)
			ready = false
			break
		}
	}

	m.setReady(ready)
}

// isProcessReady checks if a single process meets readiness criteria.
//
// The criteria depends on the configured mode:
//
// Mode "all_running":
//   - Process is ready if State is Running or Healthy
//   - Health check status is ignored
//   - Use when you want faster readiness or processes lack health endpoints
//
// Mode "all_healthy" (default):
//   - Process is ready if State is Healthy
//   - Or if State is Running AND Health is "healthy"
//   - Use for production deployments where health checks matter
func (m *Manager) isProcessReady(status ProcessStatus) bool {
	switch m.config.Mode {
	case "all_running":
		// Process just needs to be running (not stopped/failed)
		return status.State == StateRunning || status.State == StateHealthy
	case "all_healthy":
		fallthrough
	default:
		// Process must be healthy (passed health check or running without health check)
		return status.State == StateHealthy || (status.State == StateRunning && status.Health == "healthy")
	}
}

// setReady updates the ready state and manages the readiness file.
//
// If the ready state changes:
//   - true: Creates the readiness file at the configured path
//   - false: Removes the readiness file
//
// If the state hasn't changed, this method is a no-op to avoid unnecessary
// file system operations.
//
// Must be called with the mutex lock held.
func (m *Manager) setReady(ready bool) {
	if m.isReady == ready {
		return
	}

	m.isReady = ready

	if ready {
		m.createReadinessFile()
	} else {
		m.removeReadinessFile()
	}
}

// createReadinessFile creates the readiness indicator file.
//
// File content is either:
//   - Custom content from config.Content if specified
//   - Default format: "ready\ntimestamp=<unix_seconds>\n"
//
// The file is created with 0644 permissions. Errors are logged but do not
// cause the readiness state to revert.
func (m *Manager) createReadinessFile() {
	content := m.config.Content
	if content == "" {
		content = fmt.Sprintf("ready\ntimestamp=%d\n", time.Now().Unix())
	}

	if err := os.WriteFile(m.config.Path, []byte(content), 0644); err != nil {
		m.logger.Error("Failed to create readiness file",
			"path", m.config.Path,
			"error", err,
		)
		return
	}

	m.logger.Info("Container is ready",
		"path", m.config.Path,
		"processes", len(m.processes),
	)
}

// removeReadinessFile removes the readiness indicator file.
//
// If the file doesn't exist, this is not considered an error.
// Other errors (permission denied, etc.) are logged.
//
// Logs "Container is not ready" only if the container was previously ready,
// to avoid spurious messages during initialization.
func (m *Manager) removeReadinessFile() {
	if err := os.Remove(m.config.Path); err != nil && !os.IsNotExist(err) {
		m.logger.Error("Failed to remove readiness file",
			"path", m.config.Path,
			"error", err,
		)
		return
	}

	if m.isReady {
		m.logger.Info("Container is not ready", "path", m.config.Path)
	}
}

// GetStatus returns a snapshot of all tracked processes and their states.
//
// The returned map is a copy, so callers can safely read it without
// holding any locks. Modifications to the returned map do not affect
// the manager's internal state.
//
// This method is useful for debugging, monitoring, and API responses.
//
// This method is thread-safe.
func (m *Manager) GetStatus() map[string]ProcessStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]ProcessStatus)
	for k, v := range m.processes {
		result[k] = v
	}
	return result
}
