package process

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/deps"
	"github.com/cboxdk/init/internal/hooks"
	"github.com/cboxdk/init/internal/metrics"
	"github.com/cboxdk/init/internal/schedule"
	"github.com/cboxdk/init/internal/tracing"
	"go.opentelemetry.io/otel/attribute"
)

// Start starts all enabled processes in dependency order.
// It executes pre-start hooks, starts processes respecting dependencies,
// registers scheduled processes, and executes post-start hooks.
func (m *Manager) Start(ctx context.Context) error {
	ctx, span := tracing.StartProcessManagerSpan(ctx, "start",
		attribute.Int("process_count", len(m.config.Processes)))
	defer span.End()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Execute pre-start hooks
	if len(m.config.Hooks.PreStart) > 0 {
		m.logger.Info("Executing pre-start hooks", "count", len(m.config.Hooks.PreStart))
		executor := hooks.NewExecutor(m.logger)
		for _, hook := range m.config.Hooks.PreStart {
			if err := executor.ExecuteWithType(ctx, &hook, "pre_start"); err != nil {
				return fmt.Errorf("pre-start hook %s failed: %w", hook.Name, err)
			}
		}
		m.logger.Info("Pre-start hooks completed successfully")
	}

	// Get startup order (topological sort by priority and dependencies)
	startupOrder, err := m.getStartupOrder()
	if err != nil {
		return fmt.Errorf("failed to determine startup order: %w", err)
	}

	m.logger.Info("Starting processes",
		"count", len(startupOrder),
		"order", startupOrder,
	)

	// Record manager process count
	metrics.SetManagerProcessCount(len(startupOrder))

	// Start processes in order
	for i, name := range startupOrder {
		m.logger.Debug("Processing startup queue",
			"index", i,
			"total", len(startupOrder),
			"process", name,
		)

		procCfg, ok := m.config.Processes[name]
		if !ok || !procCfg.Enabled {
			m.logger.Debug("Skipping disabled/missing process", "name", name)
			continue
		}

		// Wait for dependencies to be ready before starting this process
		if err := m.waitForDependencies(ctx, name, procCfg.DependsOn); err != nil {
			return err
		}

		// Handle scheduled processes separately
		if procCfg.Schedule != "" {
			if err := m.registerScheduledProcess(name, procCfg); err != nil {
				return err
			}
			continue // Skip normal process startup
		}

		// Start regular process
		if err := m.startRegularProcess(ctx, name, procCfg); err != nil {
			return err
		}
	}

	// Start the scheduler for scheduled processes
	schedulerStats := m.scheduler.Stats()
	if schedulerStats.TotalJobs > 0 {
		m.scheduler.Start()
		m.logger.Info("Scheduler started",
			"scheduled_jobs", schedulerStats.TotalJobs,
		)
	}

	m.logger.Debug("Finished starting all processes, checking for post-start hooks",
		"hook_count", len(m.config.Hooks.PostStart),
	)

	// Execute post-start hooks
	if len(m.config.Hooks.PostStart) > 0 {
		m.logger.Info("Executing post-start hooks", "count", len(m.config.Hooks.PostStart))
		executor := hooks.NewExecutor(m.logger)
		for _, hook := range m.config.Hooks.PostStart {
			if err := executor.ExecuteWithType(ctx, &hook, "post_start"); err != nil {
				// Post-start failures are warnings, not fatal
				m.logger.Warn("Post-start hook failed", "hook", hook.Name, "error", err)
			}
		}
		m.logger.Info("Post-start hooks completed successfully")
	}

	m.logger.Debug("Manager.Start() about to return nil - startup complete")

	return nil
}

// waitForDependencies waits for all process dependencies to be ready.
func (m *Manager) waitForDependencies(ctx context.Context, name string, dependencies []string) error {
	if len(dependencies) == 0 {
		return nil
	}

	m.logger.Info("Waiting for dependencies",
		"process", name,
		"dependencies", dependencies,
	)

	for _, depName := range dependencies {
		m.logger.Debug("Checking dependency", "process", name, "dependency", depName)

		depSup, ok := m.processes[depName]
		if !ok {
			return fmt.Errorf("dependency %s not found for process %s", depName, name)
		}

		// Wait for dependency readiness
		m.logger.Debug("Waiting for dependency readiness",
			"process", name,
			"dependency", depName,
			"timeout", m.dependencyTimeout,
		)

		if err := depSup.WaitForReadiness(ctx, m.dependencyTimeout); err != nil {
			return fmt.Errorf("dependency %s not ready for process %s: %w", depName, name, err)
		}

		m.logger.Debug("Dependency ready", "process", name, "dependency", depName)
	}

	m.logger.Info("All dependencies ready", "process", name)
	return nil
}

// registerScheduledProcess registers a process with the scheduler.
func (m *Manager) registerScheduledProcess(name string, procCfg *config.Process) error {
	m.logger.Info("Registering scheduled process",
		"name", name,
		"schedule", procCfg.Schedule,
		"timezone", procCfg.ScheduleTimezone,
	)

	// Parse timeout duration if configured
	var timeout time.Duration
	if procCfg.ScheduleTimeout != "" {
		var err error
		timeout, err = time.ParseDuration(procCfg.ScheduleTimeout)
		if err != nil {
			return fmt.Errorf("invalid schedule_timeout for process %s: %w", name, err)
		}
	}

	// Register with executor (includes log capture)
	if err := m.scheduleExecutor.RegisterProcess(name, schedule.ProcessConfig{
		Command:    procCfg.Command,
		WorkingDir: procCfg.WorkingDir,
		Env:        procCfg.Env,
		Timeout:    timeout,
		Logging:    procCfg.Logging,
	}); err != nil {
		return fmt.Errorf("failed to register scheduled process %s: %w", name, err)
	}

	// Add to scheduler with options
	jobOpts := schedule.JobOptions{
		Timeout:       timeout,
		MaxConcurrent: procCfg.ScheduleMaxConcurrent,
	}
	if err := m.scheduler.AddJobWithOptions(name, procCfg.Schedule, procCfg.ScheduleTimezone, jobOpts); err != nil {
		return fmt.Errorf("failed to schedule process %s: %w", name, err)
	}

	m.logger.Info("Process scheduled successfully",
		"name", name,
		"schedule", procCfg.Schedule,
		"timeout", timeout,
		"max_concurrent", procCfg.ScheduleMaxConcurrent,
	)
	return nil
}

// startRegularProcess starts a non-scheduled process.
func (m *Manager) startRegularProcess(ctx context.Context, name string, procCfg *config.Process) error {
	m.logger.Info("Starting process",
		"name", name,
		"command", procCfg.Command,
		"scale", procCfg.Scale,
	)

	// Record desired scale
	metrics.SetDesiredScale(name, procCfg.Scale)

	// Create supervisor for this process
	sup := NewSupervisor(name, procCfg, &m.config.Global, m.logger, m.auditLogger, m.resourceCollector)
	sup.SetDeathNotifier(m.NotifyProcessDeath)
	sup.SetOneshotHistory(m.oneshotHistory)
	sup.SetLogBroadcaster(m.logBroadcaster)
	m.processes[name] = sup

	// Start the process only if initial_state is "running"
	if procCfg.InitialState == "running" {
		// Create span for individual process start
		processCtx, processSpan := tracing.StartProcessManagerSpan(ctx, "start_process",
			attribute.String("process_name", name),
			attribute.Int("scale", procCfg.Scale))

		if err := sup.Start(processCtx); err != nil {
			tracing.RecordError(processSpan, err, "Failed to start process")
			processSpan.End()
			return fmt.Errorf("failed to start process %s: %w", name, err)
		}
		tracing.RecordSuccess(processSpan)
		processSpan.End()
		m.logger.Info("Process started successfully", "name", name)
	} else {
		// CRITICAL: Mark stopped processes as ready immediately
		// This prevents deadlock when other processes depend on them
		sup.MarkReadyImmediately()
		m.logger.Info("Process in initial stopped state (can be started via TUI/API)",
			"name", name,
			"initial_state", procCfg.InitialState,
		)
	}
	return nil
}

// Shutdown gracefully shuts down all processes in reverse dependency order.
// It stops the scheduler, executes pre-stop hooks, stops all processes,
// and executes post-stop hooks.
func (m *Manager) Shutdown(ctx context.Context) error {
	ctx, span := tracing.StartProcessManagerSpan(ctx, "shutdown",
		attribute.Int("process_count", len(m.processes)))
	defer span.End()

	shutdownStart := time.Now()
	defer func() {
		// Record shutdown duration metric
		duration := time.Since(shutdownStart).Seconds()
		metrics.RecordShutdownDuration(duration)
		m.logger.Info("Shutdown completed",
			"duration_seconds", duration,
		)
	}()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Close shutdownCh exactly once to prevent double-close panic
	m.shutdownOnce.Do(func() {
		close(m.shutdownCh)
	})

	if err := m.StopReadinessManager(); err != nil {
		m.logger.Warn("Failed to stop readiness manager", "error", err)
	}

	// Stop the scheduler first (prevents new job executions)
	if m.scheduler.IsStarted() {
		m.logger.Info("Stopping scheduler")
		schedulerCtx := m.scheduler.Stop()
		<-schedulerCtx.Done() // Wait for scheduler to stop
		m.logger.Info("Scheduler stopped")
	}

	// Execute pre-stop hooks
	if len(m.config.Hooks.PreStop) > 0 {
		m.logger.Info("Executing pre-stop hooks", "count", len(m.config.Hooks.PreStop))
		executor := hooks.NewExecutor(m.logger)
		for _, hook := range m.config.Hooks.PreStop {
			if err := executor.ExecuteWithType(ctx, &hook, "pre_stop"); err != nil {
				m.logger.Warn("Pre-stop hook failed", "hook", hook.Name, "error", err)
			}
		}
	}

	m.logger.Info("Shutting down processes", "count", len(m.processes))

	// Get shutdown order (reverse of startup order)
	shutdownOrder := m.getShutdownOrder()

	var wg sync.WaitGroup
	errChan := make(chan error, len(shutdownOrder))

	// Shutdown processes in reverse order
	for _, name := range shutdownOrder {
		sup, ok := m.processes[name]
		if !ok {
			continue
		}

		wg.Add(1)
		go func(name string, sup *Supervisor) {
			defer wg.Done()

			m.logger.Info("Stopping process", "name", name)

			if err := sup.Stop(ctx); err != nil {
				m.logger.Error("Failed to stop process",
					"name", name,
					"error", err,
				)
				errChan <- fmt.Errorf("process %s: %w", name, err)
				return
			}

			m.logger.Info("Process stopped successfully", "name", name)
		}(name, sup)
	}

	wg.Wait()
	close(errChan)

	// Collect errors
	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}

	// Execute post-stop hooks
	if len(m.config.Hooks.PostStop) > 0 {
		m.logger.Info("Executing post-stop hooks", "count", len(m.config.Hooks.PostStop))
		executor := hooks.NewExecutor(m.logger)
		for _, hook := range m.config.Hooks.PostStop {
			if err := executor.ExecuteWithType(ctx, &hook, "post_stop"); err != nil {
				m.logger.Warn("Post-stop hook failed", "hook", hook.Name, "error", err)
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown completed with %d errors: %v", len(errs), errs)
	}

	return nil
}

// getStartupOrder returns processes in startup order (topological sort).
func (m *Manager) getStartupOrder() ([]string, error) {
	// Use DAG-based topological sort with dependency resolution
	graph, err := deps.NewGraphFromConfig(m.config.Processes)
	if err != nil {
		return nil, fmt.Errorf("failed to build dependency graph: %w", err)
	}

	return graph.TopologicalSort()
}

// getShutdownOrder returns processes in shutdown order (reverse of startup).
func (m *Manager) getShutdownOrder() []string {
	startupOrder, _ := m.getStartupOrder()

	// Reverse the order
	shutdownOrder := make([]string, len(startupOrder))
	for i, name := range startupOrder {
		shutdownOrder[len(startupOrder)-1-i] = name
	}

	return shutdownOrder
}

// StartProcess starts a stopped process by name.
func (m *Manager) StartProcess(ctx context.Context, name string) error {
	// Input validation
	if name == "" {
		return fmt.Errorf("process name cannot be empty")
	}

	m.mu.RLock()
	sup, ok := m.processes[name]
	procCfg, cfgOk := m.config.Processes[name]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("process %s not found in running processes", name)
	}
	if !cfgOk {
		return fmt.Errorf("process %s not found in configuration", name)
	}

	// Check if process is enabled
	if !procCfg.Enabled {
		return fmt.Errorf("process %s is disabled in configuration", name)
	}

	// Check current state
	currentState := sup.GetState()
	if currentState != StateStopped {
		return fmt.Errorf("process %s is not stopped (current state: %s)", name, currentState)
	}

	m.logger.Info("Starting process via control command",
		"name", name,
		"previous_state", currentState,
	)

	// Use background context for supervisor lifetime (not the request context)
	// The supervisor should live independently of the API request that started it
	if err := sup.Start(context.Background()); err != nil {
		m.logger.Error("Failed to start process",
			"name", name,
			"error", err,
		)
		return fmt.Errorf("failed to start process %s: %w", name, err)
	}

	m.logger.Info("Process started successfully via control command", "name", name)
	return nil
}

// StopProcess stops a running process by name.
func (m *Manager) StopProcess(ctx context.Context, name string) error {
	// Input validation
	if name == "" {
		return fmt.Errorf("process name cannot be empty")
	}

	m.mu.RLock()
	sup, ok := m.processes[name]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("process %s not found", name)
	}

	// Check current state
	currentState := sup.GetState()
	if currentState == StateStopped {
		m.logger.Info("Process already stopped",
			"name", name,
		)
		return nil // Idempotent - not an error
	}

	m.logger.Info("Stopping process via control command",
		"name", name,
		"current_state", currentState,
	)

	// Stop the process with timeout
	stopCtx, cancel := context.WithTimeout(ctx, m.processStopTimeout)
	defer cancel()

	if err := sup.Stop(stopCtx); err != nil {
		m.logger.Error("Failed to stop process",
			"name", name,
			"error", err,
		)
		return fmt.Errorf("failed to stop process %s: %w", name, err)
	}

	m.logger.Info("Process stopped successfully via control command", "name", name)
	return nil
}

// RestartProcess restarts a process by name.
func (m *Manager) RestartProcess(ctx context.Context, name string) error {
	// Input validation
	if name == "" {
		return fmt.Errorf("process name cannot be empty")
	}

	m.mu.RLock()
	sup, ok := m.processes[name]
	_, cfgOk := m.config.Processes[name]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("process %s not found", name)
	}
	if !cfgOk {
		return fmt.Errorf("process %s not found in configuration", name)
	}

	currentState := sup.GetState()
	m.logger.Info("Restarting process via control command",
		"name", name,
		"current_state", currentState,
	)

	// If already stopped, just start it
	if currentState == StateStopped {
		return m.StartProcess(ctx, name)
	}

	// Stop with timeout
	stopCtx, stopCancel := context.WithTimeout(ctx, m.processStopTimeout)
	if err := sup.Stop(stopCtx); err != nil {
		stopCancel()
		m.logger.Error("Failed to stop process during restart",
			"name", name,
			"error", err,
		)
		return fmt.Errorf("failed to stop process %s: %w", name, err)
	}
	stopCancel()

	// Start process with fresh background context so it isn't tied to API request lifetime
	if err := sup.Start(context.Background()); err != nil {
		m.logger.Error("Failed to start process after restart",
			"name", name,
			"error", err,
		)
		return fmt.Errorf("failed to start process %s after restart: %w", name, err)
	}

	m.logger.Info("Process restarted successfully", "name", name)
	return nil
}
