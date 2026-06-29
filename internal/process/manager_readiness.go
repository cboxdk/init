package process

import (
	"context"
	"time"

	"github.com/cboxdk/init/internal/readiness"
)

// GetReadinessManager returns the readiness manager (may be nil).
func (m *Manager) GetReadinessManager() *readiness.Manager {
	return m.readinessManager
}

// StartReadinessMonitor starts the readiness state monitoring.
// Should be called after all processes are started.
func (m *Manager) StartReadinessMonitor(ctx context.Context) {
	if m.readinessManager == nil {
		return
	}

	// Start the readiness manager
	if err := m.readinessManager.Start(ctx); err != nil {
		m.logger.Error("Failed to start readiness manager", "error", err)
		return
	}

	// Determine which processes to track
	m.mu.RLock()
	var trackedProcesses []string
	cfg := m.config.Global.Readiness

	if len(cfg.Processes) > 0 {
		// Track specific processes
		trackedProcesses = cfg.Processes
	} else {
		// Track all enabled longrun processes
		for name, proc := range m.config.Processes {
			if proc.Enabled && proc.Type == "longrun" && proc.Schedule == "" {
				trackedProcesses = append(trackedProcesses, name)
			}
		}
	}
	m.mu.RUnlock()

	m.readinessManager.SetTrackedProcesses(trackedProcesses)
	m.logger.Info("Readiness monitoring started", "tracked_processes", trackedProcesses)

	// Start a goroutine to periodically update process states
	go m.monitorReadinessStates(ctx)
}

// monitorReadinessStates periodically updates the readiness manager with process states.
func (m *Manager) monitorReadinessStates(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.shutdownCh:
			return
		case <-ticker.C:
			m.updateReadinessStates()
		}
	}
}

// updateReadinessStates updates the readiness manager with current process states.
func (m *Manager) updateReadinessStates() {
	if m.readinessManager == nil {
		return
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	for name, sup := range m.processes {
		supState := sup.GetState()
		instances := sup.GetInstances()
		healthStatus, lastCheckSucceeded, hasHealthCheck := sup.HealthSnapshot()

		var processState readiness.ProcessState
		var health string

		switch supState {
		case StateRunning:
			if hasHealthCheck {
				health = healthStatus
				if lastCheckSucceeded {
					processState = readiness.StateHealthy
				} else {
					processState = readiness.StateRunning
				}
			} else {
				processState = readiness.StateHealthy
				health = "healthy"
			}
		case StateStopped:
			processState = readiness.StateStopped
			health = "unknown"
		case StateCompleted:
			// Oneshot completed successfully
			processState = readiness.StateStopped
			health = "unknown"
		case StateFailed:
			processState = readiness.StateFailed
			health = "unhealthy"
		case StateStarting, StateStopping:
			// Transitional states
			if len(instances) > 0 {
				processState = readiness.StateRunning
				health = "unknown"
			} else {
				processState = readiness.StateStopped
				health = "unknown"
			}
		default:
			processState = readiness.StateStopped
			health = "unknown"
		}

		m.readinessManager.UpdateProcessState(name, processState, health)
	}
}

// StopReadinessManager stops the readiness manager and removes the readiness file.
func (m *Manager) StopReadinessManager() error {
	if m.readinessManager == nil {
		return nil
	}
	return m.readinessManager.Stop()
}
