package process

import (
	"context"
	"fmt"
	"os"

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
		return fmt.Errorf("process name cannot be empty: %w", ErrInvalidArgument)
	}

	// Check if process already exists
	if _, exists := m.config.Processes[name]; exists {
		return fmt.Errorf("process %q: %w", name, ErrProcessExists)
	}

	// Apply defaults and validate the full definition — command, scale, and the
	// enum fields (type, initial_state, restart). Previously only command and
	// scale were checked, so a typo'd restart policy was accepted and silently
	// degraded to "never".
	if err := m.config.ValidateProcessDefinition(name, procCfg); err != nil {
		return fmt.Errorf("invalid process %q: %w: %w", name, err, ErrInvalidArgument)
	}

	// Add to config
	m.config.Processes[name] = procCfg

	// If enabled, start the process
	if procCfg.Enabled {
		m.logger.Info("Starting new process", "name", name, "command", procCfg.Command, "scale", procCfg.Scale)

		supervisor := m.newConfiguredSupervisor(name, procCfg)
		// Use background context for supervisor lifetime (independent of API request)
		if err := m.startSupervisor(ctx, supervisor); err != nil {
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
		return fmt.Errorf("process %q: %w", name, ErrProcessNotFound)
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
	defer m.mu.Unlock()
	// updateProcessLocked already stops the target and restarts it with the new
	// config. It must NOT be followed by a restart of every other process: that
	// bounced the whole stack (nginx, php-fpm, every sibling) in map-iteration
	// order — ignoring the dependency DAG — for a one-process edit, and restarted
	// the just-updated target a second time.
	return m.updateProcessLocked(ctx, name, procCfg)
}

func (m *Manager) updateProcessLocked(ctx context.Context, name string, procCfg *config.Process) error {
	// Check if process exists
	oldCfg, exists := m.config.Processes[name]
	if !exists {
		return fmt.Errorf("process %q: %w", name, ErrProcessNotFound)
	}

	// Apply defaults and validate the full definition, including the enum fields
	// (type, initial_state, restart) that the ad-hoc check here used to skip.
	if err := m.config.ValidateProcessDefinition(name, procCfg); err != nil {
		return fmt.Errorf("invalid process %q: %w: %w", name, err, ErrInvalidArgument)
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
			newSupervisor := m.newConfiguredSupervisor(name, procCfg)
			// Use background context for supervisor lifetime (independent of API request)
			if err := m.startSupervisor(ctx, newSupervisor); err != nil {
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

		supervisor := m.newConfiguredSupervisor(name, procCfg)
		// Use background context for supervisor lifetime (independent of API request)
		if err := m.startSupervisor(ctx, supervisor); err != nil {
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

// SaveConfig saves the current configuration to the config file.
func (m *Manager) SaveConfig() error {
	m.mu.RLock()
	configPath := m.configPath
	cfg := m.config
	m.mu.RUnlock()

	if configPath == "" {
		return fmt.Errorf("config file path not set")
	}

	// Refuse to save when the effective config was assembled from the
	// environment: the in-memory config holds ${VAR} placeholders already
	// resolved to their (often secret) values, and CBOX_INIT_* overrides that
	// live only in the process environment. Marshalling that back to disk would
	// write those secrets in cleartext and clobber the placeholders — a silent
	// data-at-rest leak. Point the operator at the file instead. (SEC-2)
	if envOverrides := config.EnvOverridesPresent(); envOverrides {
		return fmt.Errorf("refusing to save %s: configuration is assembled from CBOX_INIT_* environment overrides; saving would write their resolved values to disk. Edit the file directly instead", configPath)
	}
	if content, err := os.ReadFile(configPath); err == nil && config.HasEnvReferences(string(content)) {
		return fmt.Errorf("refusing to save %s: it uses ${VAR} environment-variable references; saving would replace them with their resolved (possibly secret) values and destroy the templates. Edit the file directly instead", configPath)
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

	// Validate BEFORE touching any running process. Without this an invalid
	// config (bad settings, or a dependency cycle that would make the startup
	// order unresolvable) would stop the removed/changed services first and
	// only then fail — leaving them down. Validating up front makes a failed
	// reload a no-op: the running configuration is left exactly as it was.
	if err := newCfg.Validate(); err != nil {
		return fmt.Errorf("refusing to reload invalid config (running config unchanged): %w", err)
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

	stopSet := namesToSet(append(toStop, toUpdate...))
	startSet := namesToSet(append(toStart, toUpdate...))
	// Every process the reload touches — used to roll back to the old state if a
	// start fails partway through.
	touched := namesToSet(append(append(append([]string{}, toStop...), toStart...), toUpdate...))

	// Snapshot the running configuration so a partial failure can be undone
	// rather than leaving changed/removed services stopped. (CDX-10)
	oldCfg := m.config

	// Stop removed and changed processes in old shutdown order.
	m.stopReloadProcesses(ctx, stopSet)

	// Update config
	m.config = newCfg

	if err := m.startReloadProcesses(ctx, newCfg, startSet); err != nil {
		// The new config could not be brought up (e.g. a new/changed process has
		// a bad command). Roll back to the previous configuration so a failed
		// reload does not leave services down. Best-effort.
		m.logger.Error("Reload failed to start the new configuration; rolling back", "error", err)
		if failures := m.rollbackReload(ctx, oldCfg, touched); failures > 0 {
			// Say so plainly rather than claiming a clean rollback: some services
			// are down and need operator attention.
			return fmt.Errorf("reload failed and the rollback could not restore %d process(es) — they are stopped: %w", failures, err)
		}
		return fmt.Errorf("reload failed and was rolled back to the previous configuration: %w", err)
	}

	m.logger.Info("Configuration reloaded successfully")

	// Audit log
	m.auditLogger.LogConfigReloaded(m.configPath)

	return nil
}

// rollbackReload restores the previous configuration after a reload failed
// partway through. It stops whatever the failed reload left running for the
// touched processes, then restores the old config and restarts the processes
// that were running before (removed and changed ones); processes that the
// reload had only newly added are left absent. It returns how many processes it
// could NOT bring back, so the caller can report honestly instead of claiming a
// clean rollback.
//
// The rollback deliberately does NOT reuse the reload's context: the very
// failures that trigger a rollback (a dependency wait timing out, an operator
// cancelling the request) are the ones that exhaust it, and a dead context would
// force-kill the old processes and then refuse to start any of them — turning a
// failed reload into a total outage. It runs on a detached context with its own
// bounded deadline.
func (m *Manager) rollbackReload(ctx context.Context, oldCfg *config.Config, touched map[string]bool) int {
	budget := m.processStopTimeout + m.processStartTimeout
	if budget <= 0 {
		budget = DefaultProcessStopTimeout + DefaultProcessStartTimeout
	}
	rbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), budget)
	defer cancel()

	// Tear down whatever is currently running for the touched processes, in
	// reverse dependency order so dependents stop before what they depend on.
	stopOrder := m.getShutdownOrder()
	for _, name := range stopOrder {
		if !touched[name] {
			continue
		}
		m.unregisterScheduledProcess(name)
		if sup, ok := m.processes[name]; ok {
			if err := sup.Stop(rbCtx); err != nil {
				m.logger.Error("Rollback: failed to stop process", "name", name, "error", err)
			}
			delete(m.processes, name)
		}
	}
	// Anything touched but not in the shutdown order (e.g. only present in the
	// new config) still needs its scheduler registration cleared.
	for name := range touched {
		if sup, ok := m.processes[name]; ok {
			m.unregisterScheduledProcess(name)
			if err := sup.Stop(rbCtx); err != nil {
				m.logger.Error("Rollback: failed to stop process", "name", name, "error", err)
			}
			delete(m.processes, name)
		}
	}

	// Restore the previous configuration and bring the touched processes that
	// existed before back up on their old definitions, in dependency order.
	m.config = oldCfg
	failures := 0
	order, err := m.getStartupOrder()
	if err != nil {
		m.logger.Error("Rollback: could not determine startup order", "error", err)
		return len(touched)
	}
	for _, name := range order {
		if !touched[name] {
			continue
		}
		procCfg, ok := oldCfg.Processes[name]
		if !ok || !procCfg.Enabled {
			continue // newly-added process — nothing to restore
		}
		if procCfg.Schedule != "" {
			if err := m.registerScheduledProcess(name, procCfg); err != nil {
				m.logger.Error("Rollback: failed to re-register scheduled process", "name", name, "error", err)
				failures++
			}
			continue
		}
		if err := m.startRegularProcess(rbCtx, name, procCfg); err != nil {
			m.logger.Error("Rollback: failed to restart process", "name", name, "error", err)
			failures++
		}
	}
	if failures > 0 {
		m.logger.Error("Reload rollback finished with processes still down", "failed", failures)
	} else {
		m.logger.Warn("Reload rolled back to the previous configuration")
	}
	return failures
}

func namesToSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

// stopReloadProcesses stops removed and changed processes in reverse dependency order.
func (m *Manager) stopReloadProcesses(ctx context.Context, names map[string]bool) {
	for _, name := range m.getShutdownOrder() {
		if !names[name] {
			continue
		}
		m.unregisterScheduledProcess(name)
		if supervisor, running := m.processes[name]; running {
			m.logger.Info("Stopping process during reload", "name", name)
			if err := supervisor.Stop(ctx); err != nil {
				m.logger.Error("Failed to stop process during reload", "name", name, "error", err)
				continue
			}
			delete(m.processes, name)
		}
	}
}

func (m *Manager) unregisterScheduledProcess(name string) {
	if _, exists := m.scheduler.GetJob(name); exists {
		if err := m.scheduler.RemoveJob(name); err != nil {
			m.logger.Error("Failed to remove scheduled job during reload", "name", name, "error", err)
		}
	}
	m.scheduleExecutor.UnregisterProcess(name)
}

// startReloadProcesses starts new and changed processes in dependency order.
func (m *Manager) startReloadProcesses(ctx context.Context, cfg *config.Config, names map[string]bool) error {
	startupOrder, err := m.getStartupOrder()
	if err != nil {
		return fmt.Errorf("failed to determine reload startup order: %w", err)
	}

	for _, name := range startupOrder {
		if !names[name] {
			continue
		}

		procCfg := cfg.Processes[name]
		if procCfg == nil || !procCfg.Enabled {
			delete(m.processes, name)
			continue
		}

		if err := m.waitForDependencies(ctx, name, procCfg.DependsOn); err != nil {
			return err
		}

		if procCfg.Schedule != "" {
			if err := m.registerScheduledProcess(name, procCfg); err != nil {
				return err
			}
			continue
		}

		if err := m.startRegularProcess(ctx, name, procCfg); err != nil {
			return err
		}
	}

	if stats := m.scheduler.Stats(); stats.TotalJobs > 0 && !stats.Started {
		m.scheduler.Start()
	}

	return nil
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
		return nil, fmt.Errorf("process %q: %w", name, ErrProcessNotFound)
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
