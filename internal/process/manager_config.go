package process

import (
	"context"
	"fmt"
	"strings"

	"github.com/cboxdk/init/internal/config"
)

// SetConfigPath sets the config file path for saving.
func (m *Manager) SetConfigPath(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configPath = path
}

// AddProcess adds a new process to the configuration and optionally starts it.
func (m *Manager) AddProcess(ctx context.Context, name string, procCfg *config.Process) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Validate process name
	if name == "" {
		return fmt.Errorf("process name cannot be empty")
	}

	// Check if process already exists
	if _, exists := m.config.Processes[name]; exists {
		return fmt.Errorf("process %s already exists", name)
	}

	// Basic validation
	if len(procCfg.Command) == 0 {
		return fmt.Errorf("process command cannot be empty")
	}
	if procCfg.Scale < 1 {
		return fmt.Errorf("process scale must be at least 1")
	}

	// Add to config
	m.config.Processes[name] = procCfg

	// If enabled, start the process
	if procCfg.Enabled {
		m.logger.Info("Starting new process", "name", name, "command", procCfg.Command, "scale", procCfg.Scale)

		supervisor := NewSupervisor(name, procCfg, &m.config.Global, m.logger, m.auditLogger, m.resourceCollector)
		supervisor.SetOneshotHistory(m.oneshotHistory)
		// Use background context for supervisor lifetime (independent of API request)
		if err := supervisor.Start(context.Background()); err != nil {
			// Remove from config on failure
			delete(m.config.Processes, name)
			return fmt.Errorf("failed to start process: %w", err)
		}

		m.processes[name] = supervisor
		m.logger.Info("Process added and started successfully", "name", name)

		// Audit log
		m.auditLogger.LogProcessAdded(name, procCfg.Command, procCfg.Scale)
	} else {
		m.logger.Info("Process added (disabled)", "name", name)
	}

	return nil
}

// RemoveProcess removes a process from the configuration and stops it if running.
func (m *Manager) RemoveProcess(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if process exists
	if _, exists := m.config.Processes[name]; !exists {
		return fmt.Errorf("process %s does not exist", name)
	}

	// Stop the process if running
	if supervisor, running := m.processes[name]; running {
		m.logger.Info("Stopping process before removal", "name", name)

		if err := supervisor.Stop(ctx); err != nil {
			return fmt.Errorf("failed to stop process: %w", err)
		}

		delete(m.processes, name)
	}

	// Remove from config
	delete(m.config.Processes, name)

	m.logger.Info("Process removed successfully", "name", name)

	// Audit log
	m.auditLogger.LogProcessRemoved(name)

	return nil
}

// UpdateProcess updates an existing process configuration.
func (m *Manager) UpdateProcess(ctx context.Context, name string, procCfg *config.Process) error {
	m.mu.Lock()
	err := m.updateProcessLocked(ctx, name, procCfg)
	m.mu.Unlock()
	if err != nil {
		return err
	}

	// Restart all running processes to ensure consistent state
	if err := m.restartAllProcesses(ctx); err != nil {
		return err
	}

	return nil
}

func (m *Manager) updateProcessLocked(ctx context.Context, name string, procCfg *config.Process) error {
	// Check if process exists
	oldCfg, exists := m.config.Processes[name]
	if !exists {
		return fmt.Errorf("process %s does not exist", name)
	}

	// Basic validation
	if len(procCfg.Command) == 0 {
		return fmt.Errorf("process command cannot be empty")
	}
	if procCfg.Scale < 1 {
		return fmt.Errorf("process scale must be at least 1")
	}

	// Update config
	m.config.Processes[name] = procCfg

	// If process is running, need to restart with new config
	if supervisor, running := m.processes[name]; running {
		m.logger.Info("Restarting process with new configuration", "name", name)

		// Stop old supervisor
		if err := supervisor.Stop(ctx); err != nil {
			// Rollback config change on error
			m.config.Processes[name] = oldCfg
			return fmt.Errorf("failed to stop process: %w", err)
		}

		// If new config is enabled, start with new config
		if procCfg.Enabled {
			newSupervisor := NewSupervisor(name, procCfg, &m.config.Global, m.logger, m.auditLogger, m.resourceCollector)
			newSupervisor.SetOneshotHistory(m.oneshotHistory)
			// Use background context for supervisor lifetime (independent of API request)
			if err := newSupervisor.Start(context.Background()); err != nil {
				// Rollback config change on error
				m.config.Processes[name] = oldCfg
				return fmt.Errorf("failed to start process with new config: %w", err)
			}

			m.processes[name] = newSupervisor
			m.logger.Info("Process updated and restarted", "name", name)
		} else {
			// New config is disabled, just remove from running processes
			delete(m.processes, name)
			m.logger.Info("Process updated and disabled", "name", name)
		}
	} else if procCfg.Enabled {
		// Process wasn't running but new config enables it
		m.logger.Info("Starting previously disabled process", "name", name)

		supervisor := NewSupervisor(name, procCfg, &m.config.Global, m.logger, m.auditLogger, m.resourceCollector)
		supervisor.SetOneshotHistory(m.oneshotHistory)
		// Use background context for supervisor lifetime (independent of API request)
		if err := supervisor.Start(context.Background()); err != nil {
			// Rollback config change on error
			m.config.Processes[name] = oldCfg
			return fmt.Errorf("failed to start process: %w", err)
		}

		m.processes[name] = supervisor
		m.logger.Info("Process updated and started", "name", name)
	}

	// Audit log
	m.auditLogger.LogProcessUpdated(name, procCfg.Command, procCfg.Scale)

	return nil
}

// restartAllProcesses restarts every running process sequentially.
func (m *Manager) restartAllProcesses(ctx context.Context) error {
	m.mu.RLock()
	names := make([]string, 0, len(m.processes))
	for name := range m.processes {
		names = append(names, name)
	}
	m.mu.RUnlock()

	var errs []string
	for _, name := range names {
		if err := m.RestartProcess(ctx, name); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to restart processes: %s", strings.Join(errs, "; "))
	}

	return nil
}

// SaveConfig saves the current configuration to the config file.
func (m *Manager) SaveConfig() error {
	m.mu.RLock()
	configPath := m.configPath
	cfg := m.config
	m.mu.RUnlock()

	if configPath == "" {
		return fmt.Errorf("config file path not set")
	}

	m.logger.Info("Saving configuration", "path", configPath)

	if err := config.Save(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	m.logger.Info("Configuration saved successfully", "path", configPath)

	// Audit log
	m.auditLogger.LogConfigSaved(configPath)

	return nil
}

// ReloadConfig reloads the configuration from the config file.
func (m *Manager) ReloadConfig(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.configPath == "" {
		return fmt.Errorf("config file path not set")
	}

	m.logger.Info("Reloading configuration", "path", m.configPath)

	// Load new config
	newCfg, err := config.LoadWithEnvExpansion(m.configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Determine what changed
	toStop := []string{}
	toStart := []string{}
	toUpdate := []string{}

	// Check for removed processes
	for name := range m.config.Processes {
		if _, exists := newCfg.Processes[name]; !exists {
			toStop = append(toStop, name)
		}
	}

	// Check for new or updated processes
	for name, newProc := range newCfg.Processes {
		if oldProc, exists := m.config.Processes[name]; exists {
			// Process exists, check if changed
			if !oldProc.Equal(newProc) {
				toUpdate = append(toUpdate, name)
			}
		} else {
			// New process
			toStart = append(toStart, name)
		}
	}

	m.logger.Info("Configuration reload plan",
		"to_stop", toStop,
		"to_start", toStart,
		"to_update", toUpdate,
	)

	// Stop removed processes
	m.stopRemovedProcesses(ctx, toStop)

	// Update config
	m.config = newCfg

	// Start new processes
	m.startNewProcesses(newCfg, toStart)

	// Update changed processes
	m.updateChangedProcesses(ctx, newCfg, toUpdate)

	m.logger.Info("Configuration reloaded successfully")

	// Audit log
	m.auditLogger.LogConfigReloaded(m.configPath)

	return nil
}

// stopRemovedProcesses stops processes that were removed from config
func (m *Manager) stopRemovedProcesses(ctx context.Context, names []string) {
	for _, name := range names {
		if supervisor, running := m.processes[name]; running {
			m.logger.Info("Stopping removed process", "name", name)
			if err := supervisor.Stop(ctx); err != nil {
				m.logger.Error("Failed to stop process during reload", "name", name, "error", err)
			}
			delete(m.processes, name)
		}
	}
}

// startNewProcesses starts newly added processes
func (m *Manager) startNewProcesses(cfg *config.Config, names []string) {
	for _, name := range names {
		procCfg := cfg.Processes[name]
		if procCfg.Enabled {
			m.logger.Info("Starting new process", "name", name)
			supervisor := NewSupervisor(name, procCfg, &cfg.Global, m.logger, m.auditLogger, m.resourceCollector)
			supervisor.SetOneshotHistory(m.oneshotHistory)
			// Use background context for supervisor lifetime (independent of reload request)
			if err := supervisor.Start(context.Background()); err != nil {
				m.logger.Error("Failed to start new process during reload", "name", name, "error", err)
				continue
			}
			m.processes[name] = supervisor
		}
	}
}

// updateChangedProcesses restarts processes whose config changed
func (m *Manager) updateChangedProcesses(ctx context.Context, cfg *config.Config, names []string) {
	for _, name := range names {
		procCfg := cfg.Processes[name]

		if supervisor, running := m.processes[name]; running {
			m.logger.Info("Restarting updated process", "name", name)

			if err := supervisor.Stop(ctx); err != nil {
				m.logger.Error("Failed to stop process during update", "name", name, "error", err)
				continue
			}

			if procCfg.Enabled {
				newSupervisor := NewSupervisor(name, procCfg, &cfg.Global, m.logger, m.auditLogger, m.resourceCollector)
				newSupervisor.SetOneshotHistory(m.oneshotHistory)
				// Use background context for supervisor lifetime (independent of reload request)
				if err := newSupervisor.Start(context.Background()); err != nil {
					m.logger.Error("Failed to start updated process", "name", name, "error", err)
					continue
				}
				m.processes[name] = newSupervisor
			} else {
				delete(m.processes, name)
			}
		} else if procCfg.Enabled {
			m.logger.Info("Starting previously disabled process", "name", name)
			supervisor := NewSupervisor(name, procCfg, &cfg.Global, m.logger, m.auditLogger, m.resourceCollector)
			supervisor.SetOneshotHistory(m.oneshotHistory)
			// Use background context for supervisor lifetime (independent of reload request)
			if err := supervisor.Start(context.Background()); err != nil {
				m.logger.Error("Failed to start process during reload", "name", name, "error", err)
				continue
			}
			m.processes[name] = supervisor
		}
	}
}

// GetConfig returns a copy of the current configuration.
func (m *Manager) GetConfig() *config.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy to prevent external modifications
	cfgCopy := *m.config
	cfgCopy.Processes = make(map[string]*config.Process, len(m.config.Processes))
	for k, v := range m.config.Processes {
		procCopy := *v
		cfgCopy.Processes[k] = &procCopy
	}

	return &cfgCopy
}

// GetProcessConfig returns a copy of a single process configuration.
func (m *Manager) GetProcessConfig(name string) (*config.Process, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	proc, exists := m.config.Processes[name]
	if !exists {
		return nil, fmt.Errorf("process %s not found", name)
	}

	procCopy := *proc
	if proc.Command != nil {
		procCopy.Command = append([]string{}, proc.Command...)
	}
	if proc.Env != nil {
		procCopy.Env = make(map[string]string, len(proc.Env))
		for k, v := range proc.Env {
			procCopy.Env[k] = v
		}
	}
	if proc.DependsOn != nil {
		procCopy.DependsOn = append([]string{}, proc.DependsOn...)
	}
	return &procCopy, nil
}
