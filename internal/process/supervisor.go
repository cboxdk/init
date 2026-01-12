package process

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/gophpeek/phpeek-pm/internal/audit"
	"github.com/gophpeek/phpeek-pm/internal/config"
	"github.com/gophpeek/phpeek-pm/internal/hooks"
	"github.com/gophpeek/phpeek-pm/internal/logger"
	"github.com/gophpeek/phpeek-pm/internal/metrics"
)

// ProcessState represents the lifecycle state of a process instance.
// The state machine transitions are:
//
//	stopped → starting → running → stopping → stopped
//	                  ↓            ↓
//	                failed      completed (oneshot only)
//
// State transitions are logged and emit metrics for observability.
type ProcessState string

const (
	// StateStarting indicates the process is being initialized and started.
	StateStarting ProcessState = "starting"

	// StateRunning indicates the process is actively running.
	StateRunning ProcessState = "running"

	// StateStopping indicates a graceful shutdown is in progress.
	StateStopping ProcessState = "stopping"

	// StateStopped indicates the process has been cleanly stopped.
	StateStopped ProcessState = "stopped"

	// StateFailed indicates the process exited with an error or crashed.
	StateFailed ProcessState = "failed"

	// StateCompleted indicates a oneshot process ran successfully (exit code 0).
	StateCompleted ProcessState = "completed"
)

// Default timeouts for supervisor operations.
const (
	// DefaultRestartBackoffInitial is the initial delay before restarting a failed process.
	DefaultRestartBackoffInitial = 5 * time.Second

	// DefaultRestartBackoffMax is the maximum delay between restart attempts.
	DefaultRestartBackoffMax = 1 * time.Minute

	// DefaultGoroutineStopTimeout is the timeout for supervisor goroutines to stop during shutdown.
	DefaultGoroutineStopTimeout = 5 * time.Second

	// DefaultInstanceShutdownTimeout is the timeout for graceful instance shutdown.
	DefaultInstanceShutdownTimeout = 30 * time.Second
)

// Supervisor manages the lifecycle of a single process type, potentially with multiple
// scaled instances. It handles starting, stopping, restarting, scaling, and health
// monitoring for all instances of a named process.
//
// Supervisor is the core abstraction for individual process management within the
// process manager. Each process defined in the configuration gets its own Supervisor.
//
// Key responsibilities:
//   - Starting and stopping process instances based on configuration
//   - Automatic restart handling with configurable backoff policies
//   - Health check integration for readiness and liveness monitoring
//   - Horizontal scaling (adding/removing instances dynamically)
//   - Resource metrics collection per instance
//   - Signal forwarding to managed processes
//   - Graceful shutdown with configurable timeouts
//
// Supervisor is safe for concurrent use. All lifecycle operations are serialized
// via operationMu, while state reads can proceed concurrently via RWMutex.
//
// Example usage:
//
//	supervisor := process.NewSupervisor("worker", cfg, globalCfg, logger, auditLogger, collector)
//	ctx := context.Background()
//	if err := supervisor.Start(ctx); err != nil {
//	    log.Fatalf("Failed to start: %v", err)
//	}
//	defer supervisor.Stop(ctx)
type Supervisor struct {
	name               string
	config             *config.Process
	logger             *slog.Logger
	auditLogger        *audit.Logger
	instances          []*Instance
	state              ProcessState
	healthMonitor      *HealthMonitor
	healthStatus       <-chan HealthStatus
	restartPolicy      RestartPolicy
	resourceCollector  *metrics.ResourceCollector // Shared resource collector (can be nil)
	oneshotHistory     *OneshotHistory            // Shared oneshot history (can be nil)
	deathNotifier      func(string)               // Callback when all instances are dead
	credentials        *Credentials               // Resolved user/group credentials (nil = inherit)
	healthCheckStrict  bool                       // Fail startup if health monitor creation fails
	ctx                context.Context
	cancel             context.CancelFunc
	readinessCh        chan struct{}  // Closed when service becomes ready
	readinessOnce      sync.Once      // CRITICAL: Ensures readinessCh closed exactly once
	isReady            bool           // Track readiness state
	goroutines         sync.WaitGroup // CRITICAL: Track all goroutines for clean shutdown
	mu                 sync.RWMutex
	operationMu        sync.Mutex // Serializes lifecycle/scale operations so reads can proceed
}

// Instance represents a single running process instance within a Supervisor.
// Each Instance corresponds to one OS process and tracks its lifecycle state,
// PID, restart history, and I/O streams.
//
// For scaled processes (scale > 1), the Supervisor manages multiple Instance
// objects, each with a unique identifier (e.g., "worker-1", "worker-2").
//
// Instance handles:
//   - Process execution via exec.Cmd
//   - Stdout/stderr capture and logging
//   - Process exit detection and restart eligibility
//   - Oneshot execution history tracking
//
// Instance is safe for concurrent access via its embedded RWMutex.
type Instance struct {
	id            string
	index         int // Instance index (0, 1, 2, ...) for port assignment
	cmd           *exec.Cmd
	state         ProcessState
	pid           int
	started       time.Time
	restartCount  int
	doneCh        chan struct{} // Closed when process exits (monitored by monitorInstance)
	stdoutWriter  *logger.ProcessWriter
	stderrWriter  *logger.ProcessWriter
	allowRestart  bool
	oneshotExecID int64 // Tracks oneshot execution history entry ID (0 if not oneshot)
	mu            sync.RWMutex
}

// NewSupervisor creates a new Supervisor for managing the specified process.
//
// Parameters:
//   - name: Unique identifier for this process (matches config key)
//   - cfg: Process-specific configuration (command, restart policy, health checks, etc.)
//   - globalCfg: Global configuration for timeouts, restart limits, and defaults
//   - logger: Structured logger for operational logging (automatically scoped to process name)
//   - auditLogger: Security audit logger for tracking lifecycle changes
//   - resourceCollector: Optional metrics collector for CPU/memory tracking (may be nil)
//
// The Supervisor is created in a stopped state. Call Start() to begin process execution.
// User/group credentials are resolved at creation time if configured.
//
// Restart backoff and limits are derived from globalCfg with sensible defaults:
//   - Initial backoff: globalCfg.RestartBackoffInitial or 5 seconds
//   - Maximum backoff: globalCfg.RestartBackoffMax or 1 minute
//   - Maximum attempts: globalCfg.MaxRestartAttempts (0 = unlimited)
func NewSupervisor(name string, cfg *config.Process, globalCfg *config.GlobalConfig, logger *slog.Logger, auditLogger *audit.Logger, resourceCollector *metrics.ResourceCollector) *Supervisor {
	// Get restart backoff from global config (default: 5 seconds, max 1 minute)
	initialBackoff := globalCfg.RestartBackoffInitial
	if initialBackoff <= 0 {
		initialBackoff = time.Duration(globalCfg.RestartBackoff) * time.Second
		if initialBackoff <= 0 {
			initialBackoff = DefaultRestartBackoffInitial
		}
	}
	maxBackoff := globalCfg.RestartBackoffMax
	if maxBackoff <= 0 {
		maxBackoff = time.Duration(globalCfg.RestartBackoff) * time.Second
		if maxBackoff <= 0 {
			maxBackoff = DefaultRestartBackoffMax
		}
	}

	// Get max attempts from global config (default: 3)
	maxAttempts := globalCfg.MaxRestartAttempts

	// Resolve user/group credentials at initialization
	var creds *Credentials
	if cfg.User != "" || cfg.Group != "" {
		var err error
		creds, err = ResolveCredentials(cfg.User, cfg.Group)
		if err != nil {
			logger.Error("Failed to resolve credentials",
				"process", name,
				"user", cfg.User,
				"group", cfg.Group,
				"error", err,
			)
			// Continue without credentials - will run as parent process user
		} else if creds != nil {
			logger.Info("Resolved process credentials",
				"process", name,
				"uid", creds.Uid,
				"gid", creds.Gid,
			)
		}
	}

	return &Supervisor{
		name:              name,
		config:            cfg,
		logger:            logger.With("process", name),
		auditLogger:       auditLogger,
		instances:         make([]*Instance, 0, cfg.Scale),
		state:             StateStopped,
		restartPolicy:     NewRestartPolicy(cfg.Restart, maxAttempts, initialBackoff, maxBackoff),
		resourceCollector:  resourceCollector,
		credentials:        creds,
		healthCheckStrict:  globalCfg.HealthCheckStrict,
		readinessCh:        make(chan struct{}),
		isReady:            false,
	}
}

// SetDeathNotifier sets the callback for when all instances are dead
func (s *Supervisor) SetDeathNotifier(notifier func(string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deathNotifier = notifier
}

// SetOneshotHistory sets the shared oneshot history for tracking executions
func (s *Supervisor) SetOneshotHistory(history *OneshotHistory) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.oneshotHistory = history
}

// streamEnabled determines if stdout/stderr streaming is enabled for this process
func (s *Supervisor) streamEnabled(stream string) bool {
	if s.config.Logging == nil {
		return true
	}
	switch stream {
	case "stdout":
		return s.config.Logging.Stdout
	case "stderr":
		return s.config.Logging.Stderr
	default:
		return true
	}
}

// MarkReadyImmediately marks the service as ready without waiting
// Used for stopped processes to prevent dependency deadlocks
func (s *Supervisor) MarkReadyImmediately() {
	s.markReady("stopped state")
}

// markReady atomically marks service as ready (thread-safe, idempotent)
// CRITICAL: Does NOT acquire locks - caller must manage locking if needed
// The sync.Once ensures the channel is closed exactly once regardless of concurrent calls
func (s *Supervisor) markReady(reason string) {
	s.readinessOnce.Do(func() {
		close(s.readinessCh)
		// NOTE: No lock here - isReady is just a status flag for debugging
		// The readinessCh close is the actual synchronization mechanism
		s.isReady = true
		s.logger.Debug("Service marked as ready",
			"reason", reason,
		)
	})
}

// WaitForReadiness waits for the service to become ready (health check passes)
// Returns nil when ready, error on timeout or context cancellation
func (s *Supervisor) WaitForReadiness(ctx context.Context, timeout time.Duration) error {
	// If no health check configured, consider immediately ready
	if s.config.HealthCheck == nil {
		s.logger.Debug("No health check configured, considering ready immediately")
		return nil
	}

	// Check health check mode - only wait if readiness or both
	mode := s.config.HealthCheck.Mode
	if mode != "readiness" && mode != "both" && mode != "" {
		// mode is "liveness" or unset (defaults to "both")
		if mode == "liveness" {
			s.logger.Debug("Health check mode is liveness-only, not waiting for readiness")
			return nil
		}
	}

	s.logger.Info("Waiting for service readiness",
		"timeout", timeout,
		"health_check_type", s.config.HealthCheck.Type,
	)

	// Wait for readiness with timeout
	timeoutCh := time.After(timeout)
	select {
	case <-s.readinessCh:
		s.logger.Info("Service ready")
		return nil
	case <-timeoutCh:
		return fmt.Errorf("service did not become ready within %v", timeout)
	case <-ctx.Done():
		return fmt.Errorf("context cancelled while waiting for readiness")
	}
}

// Start starts all instances of the process
func (s *Supervisor) Start(ctx context.Context) error {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Create context for this supervisor's lifetime
	s.ctx, s.cancel = context.WithCancel(ctx)

	s.state = StateStarting

	// Start instances based on scale
	for i := 0; i < s.config.Scale; i++ {
		instanceID := fmt.Sprintf("%s-%d", s.name, i)

		instance, err := s.startInstance(s.ctx, instanceID, i)
		if err != nil {
			s.state = StateFailed
			s.cancel()
			return fmt.Errorf("failed to start instance %s: %w", instanceID, err)
		}

		s.instances = append(s.instances, instance)
	}

	s.state = StateRunning

	// Start health monitoring if configured
	if s.config.HealthCheck != nil {
		monitor, err := NewHealthMonitor(s.name, s.config.HealthCheck, s.logger)
		if err != nil {
			if s.healthCheckStrict {
				// In strict mode, fail startup if health monitor cannot be created
				s.logger.Error("Failed to create health monitor (strict mode)",
					"error", err,
				)
				return fmt.Errorf("health monitor creation failed: %w", err)
			}
			// Default: warn and continue, marking as ready immediately
			s.logger.Warn("Failed to create health monitor, marking ready immediately",
				"error", err,
			)
			s.markReady("health monitor creation failed")
		} else {
			s.healthMonitor = monitor
			s.healthStatus = monitor.Start(s.ctx)

			// Monitor health status in background with goroutine tracking
			s.goroutines.Add(1)
			go func() {
				defer s.goroutines.Done()
				s.handleHealthStatus(s.ctx)
			}()
		}
	} else {
		// No health check configured - consider ready immediately
		s.markReady("no health check configured")
	}

	// Start resource metrics collection if enabled
	if s.resourceCollector != nil {
		s.goroutines.Add(1)
		go func() {
			defer s.goroutines.Done()
			s.collectResourceMetrics()
		}()
	}

	return nil
}

// startInstance starts a single process instance
// instanceIndex is used for port assignment (PORT = port_base + instanceIndex)
func (s *Supervisor) startInstance(ctx context.Context, instanceID string, instanceIndex int) (*Instance, error) {
	s.logger.Info("Starting process instance",
		"instance_id", instanceID,
		"instance_index", instanceIndex,
		"command", s.config.Command,
	)

	// Create command
	cmd := exec.CommandContext(ctx, s.config.Command[0], s.config.Command[1:]...)
	if s.config.WorkingDir != "" {
		cmd.Dir = s.config.WorkingDir
	}

	// Set environment variables
	envVars := append(os.Environ(), s.envVars(instanceID, instanceIndex)...)
	envVars = append(envVars,
		fmt.Sprintf("PHPEEK_SERVICE=%s", s.name),
		fmt.Sprintf("PHPEEK_INSTANCE=%s", instanceID),
		fmt.Sprintf("PHPEEK_PROCESS=%s", s.name),
	)
	cmd.Env = envVars

	// CRITICAL: Put subprocess in its own process group
	// This prevents Ctrl+C (SIGINT) from propagating to child processes
	// Without this, Ctrl+C kills children → manager thinks crash → restarts
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Create new process group for this process
	}

	// Apply user/group credentials if configured
	// Note: Requires root privileges to switch to a different user
	if s.credentials != nil {
		s.credentials.ApplySysProcAttr(cmd.SysProcAttr)
		s.logger.Debug("Applied process credentials",
			"instance_id", instanceID,
			"uid", s.credentials.Uid,
			"gid", s.credentials.Gid,
		)
	}

	// Setup stdout/stderr capture with structured logging
	// Create ProcessWriter instances only if stream enabled
	var stdoutWriter *logger.ProcessWriter
	var stderrWriter *logger.ProcessWriter
	var err error

	if s.streamEnabled("stdout") {
		stdoutWriter, err = logger.NewProcessWriter(s.logger, s.name, instanceID, "stdout", s.config.Logging)
		if err != nil {
			return nil, fmt.Errorf("failed to create stdout writer: %w", err)
		}
	}
	if s.streamEnabled("stderr") {
		stderrWriter, err = logger.NewProcessWriter(s.logger, s.name, instanceID, "stderr", s.config.Logging)
		if err != nil {
			return nil, fmt.Errorf("failed to create stderr writer: %w", err)
		}
	}

	if stdoutWriter != nil {
		cmd.Stdout = stdoutWriter
	} else {
		cmd.Stdout = io.Discard
	}
	if stderrWriter != nil {
		cmd.Stderr = stderrWriter
	} else {
		cmd.Stderr = io.Discard
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	startTime := time.Now()
	instance := &Instance{
		id:           instanceID,
		index:        instanceIndex,
		cmd:          cmd,
		state:        StateRunning,
		pid:          cmd.Process.Pid,
		started:      startTime,
		doneCh:       make(chan struct{}),
		stdoutWriter: stdoutWriter,
		stderrWriter: stderrWriter,
		allowRestart: true,
	}

	// Record oneshot execution in history
	if s.config.Type == "oneshot" && s.oneshotHistory != nil {
		instance.oneshotExecID = s.oneshotHistory.Record(s.name, instanceID, "startup")
	}

	s.logger.Info("Process instance started",
		"instance_id", instanceID,
		"pid", instance.pid,
	)

	// Log to audit trail
	s.auditLogger.LogProcessStart(s.name, instance.pid, s.config.Scale)

	// Record metrics
	metrics.RecordProcessStart(s.name, instanceID, float64(startTime.Unix()))

	// Monitor process in background with goroutine tracking
	s.goroutines.Add(1)
	go func() {
		defer s.goroutines.Done()
		s.monitorInstance(instance)
	}()

	return instance, nil
}

// monitorInstance monitors a process instance and handles restarts
func (s *Supervisor) monitorInstance(instance *Instance) {
	// CRITICAL: Panic recovery to prevent goroutine crashes from killing daemon
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("PANIC in monitorInstance recovered",
				"instance_id", instance.id,
				"panic", r,
			)
			// Attempt to mark instance as failed
			instance.mu.Lock()
			instance.state = StateFailed
			instance.mu.Unlock()
		}
		// CRITICAL: Always close doneCh to unblock stopInstance
		close(instance.doneCh)
	}()

	err := instance.cmd.Wait()

	instance.mu.Lock()
	exitCode := instance.cmd.ProcessState.ExitCode()
	instance.state = StateStopped
	restartCount := instance.restartCount
	instance.mu.Unlock()

	// Record process stop metrics
	metrics.RecordProcessStop(s.name, instance.id, exitCode)

	// Handle oneshot processes differently - they don't restart
	if s.config.Type == "oneshot" {
		s.handleOneshotExit(instance, exitCode, err)
		return
	}

	// Check restart flag to determine if this is intentional stop
	instance.mu.RLock()
	allowRestart := instance.allowRestart
	instance.mu.RUnlock()

	// Log exit appropriately based on whether it was intentional
	s.logProcessExit(instance, exitCode, restartCount, allowRestart, err)

	// Skip restart if disabled (e.g., intentional stop/scale down)
	if !allowRestart {
		s.logger.Debug("Restart skipped because instance restart disabled",
			"instance_id", instance.id,
		)
		return
	}

	// Check if we should restart (longrun only)
	if s.restartPolicy.ShouldRestart(exitCode, restartCount) {
		s.attemptRestart(instance, exitCode, restartCount)
	} else {
		s.logger.Warn("Process instance will not be restarted",
			"instance_id", instance.id,
			"exit_code", exitCode,
			"restart_count", restartCount,
		)
		s.checkAllInstancesDead()
	}
}

// ScaleUp adds new instances to reach the target scale
func (s *Supervisor) ScaleUp(ctx context.Context, targetScale int) error {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()

	s.mu.RLock()
	currentScale := len(s.instances)
	s.mu.RUnlock()

	if targetScale <= currentScale {
		return fmt.Errorf("target scale %d must be greater than current scale %d", targetScale, currentScale)
	}

	instancesToAdd := targetScale - currentScale
	s.logger.Info("Scaling up supervisor",
		"current_scale", currentScale,
		"target_scale", targetScale,
		"instances_to_add", instancesToAdd,
	)

	newInstances := make([]*Instance, 0, instancesToAdd)

	// Start new instances
	runCtx := s.ctx
	if runCtx == nil {
		runCtx = context.Background()
	}

	for i := 0; i < instancesToAdd; i++ {
		instanceIndex := currentScale + i
		instanceID := fmt.Sprintf("%s-%d", s.name, instanceIndex)

		instance, err := s.startInstance(runCtx, instanceID, instanceIndex)
		if err != nil {
			// Clean up any instances we already started
			for _, started := range newInstances {
				if stopErr := s.stopInstance(ctx, started); stopErr != nil {
					s.logger.Warn("Failed to cleanup instance after scale-up error",
						"instance_id", started.id,
						"error", stopErr,
					)
				}
			}
			return fmt.Errorf("failed to start instance %s during scale up: %w", instanceID, err)
		}

		newInstances = append(newInstances, instance)
	}

	s.mu.Lock()
	s.instances = append(s.instances, newInstances...)
	newScale := len(s.instances)
	s.mu.Unlock()

	s.logger.Info("Scale up completed",
		"new_scale", newScale,
	)
	return nil
}

// ScaleDown removes instances to reach the target scale
func (s *Supervisor) ScaleDown(ctx context.Context, targetScale int) error {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()

	s.mu.RLock()
	currentScale := len(s.instances)
	if targetScale >= currentScale {
		s.mu.RUnlock()
		return fmt.Errorf("target scale %d must be less than current scale %d", targetScale, currentScale)
	}

	instancesToRemove := currentScale - targetScale
	// Copy pointers to the instances we plan to stop so we can release the main lock
	toStop := make([]*Instance, 0, instancesToRemove)
	actualInstances := len(s.instances)
	for i := currentScale - 1; i >= targetScale; i-- {
		// Bounds check to prevent nil pointer dereference if instances slice is shorter than expected
		if i < actualInstances && s.instances[i] != nil {
			toStop = append(toStop, s.instances[i])
		}
	}
	s.mu.RUnlock()

	s.logger.Info("Scaling down supervisor",
		"current_scale", currentScale,
		"target_scale", targetScale,
		"instances_to_remove", instancesToRemove,
	)

	// Stop instances from the end (LIFO - Last In First Out)
	var wg sync.WaitGroup
	errChan := make(chan error, instancesToRemove)

	for _, instance := range toStop {
		wg.Add(1)
		go func(inst *Instance) {
			defer wg.Done()
			if err := s.stopInstance(ctx, inst); err != nil {
				errChan <- fmt.Errorf("failed to stop instance %s: %w", inst.id, err)
			}
		}(instance)
	}

	wg.Wait()
	close(errChan)

	// Collect errors
	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}

	// Remove stopped instances from the list
	s.mu.Lock()
	s.instances = s.instances[:targetScale]
	newScale := len(s.instances)
	s.mu.Unlock()

	if len(errs) > 0 {
		return fmt.Errorf("scale down completed with %d errors: %v", len(errs), errs)
	}

	s.logger.Info("Scale down completed",
		"new_scale", newScale,
	)
	return nil
}

// Stop gracefully stops all process instances
func (s *Supervisor) Stop(ctx context.Context) error {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()

	s.mu.Lock()
	s.state = StateStopping
	s.mu.Unlock()

	// Cancel context to signal all goroutines to stop
	if s.cancel != nil {
		s.cancel()
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(s.instances))

	// Stop all instances in parallel
	s.mu.RLock()
	instances := make([]*Instance, len(s.instances))
	copy(instances, s.instances)
	s.mu.RUnlock()

	for _, instance := range instances {
		wg.Add(1)
		go func(inst *Instance) {
			defer wg.Done()

			if err := s.stopInstance(ctx, inst); err != nil {
				errChan <- fmt.Errorf("instance %s: %w", inst.id, err)
			}
		}(instance)
	}

	wg.Wait()
	close(errChan)

	// CRITICAL: Wait for all goroutines to finish with timeout
	goroutinesDone := make(chan struct{})
	go func() {
		s.goroutines.Wait()
		close(goroutinesDone)
	}()

	select {
	case <-goroutinesDone:
		s.logger.Debug("All supervisor goroutines stopped")
	case <-time.After(DefaultGoroutineStopTimeout):
		s.logger.Warn("Timeout waiting for supervisor goroutines to stop")
	}

	// Collect errors
	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}

	s.mu.Lock()
	s.state = StateStopped
	s.instances = nil
	s.ctx = nil
	s.mu.Unlock()

	if len(errs) > 0 {
		return fmt.Errorf("stop completed with %d errors: %v", len(errs), errs)
	}

	return nil
}

// stopInstance stops a single process instance
func (s *Supervisor) stopInstance(ctx context.Context, instance *Instance) error {
	// NIL safety check
	if instance == nil {
		return fmt.Errorf("cannot stop nil instance")
	}
	if instance.cmd == nil || instance.cmd.Process == nil {
		s.logger.Warn("Process already stopped or never started",
			"instance_id", instance.id,
		)
		return nil
	}

	instance.mu.Lock()
	currentState := instance.state
	if currentState != StateRunning {
		instance.mu.Unlock()
		s.logger.Debug("Process not running, skipping stop",
			"instance_id", instance.id,
			"current_state", currentState,
		)
		return nil
	}
	instance.state = StateStopping
	instance.allowRestart = false
	pid := instance.pid
	instance.mu.Unlock()

	s.logger.Info("Stopping process instance",
		"instance_id", instance.id,
		"pid", pid,
	)

	// Execute pre-stop hook if configured
	s.executePreStopHook(ctx, instance)

	// Send shutdown signal to process/process group
	if err := s.sendShutdownSignal(instance); err != nil {
		return err
	}

	// Wait for graceful shutdown with timeout
	timeout := DefaultInstanceShutdownTimeout
	if s.config.Shutdown != nil && s.config.Shutdown.Timeout > 0 {
		timeout = time.Duration(s.config.Shutdown.Timeout) * time.Second
	}

	// CRITICAL: Wait on doneCh instead of calling Wait() again to avoid double-Wait race
	// The monitorInstance goroutine is already calling Wait() and will close doneCh when done
	select {
	case <-instance.doneCh:
		s.logger.Info("Process instance stopped gracefully",
			"instance_id", instance.id,
		)
		s.cleanupInstanceResources(instance, pid, "graceful_shutdown")
		return nil

	case <-time.After(timeout):
		s.logger.Warn("Process instance did not stop gracefully, force killing",
			"instance_id", instance.id,
			"timeout", timeout,
		)

		// Force kill
		if err := instance.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill process: %w", err)
		}

		// Wait for monitorInstance to detect the exit and close doneCh
		<-instance.doneCh

		s.cleanupInstanceResources(instance, pid, "force_killed_after_timeout")
		return nil
	}
}

// executePreStopHook executes the configured pre-stop hook if present
func (s *Supervisor) executePreStopHook(ctx context.Context, instance *Instance) {
	if s.config.Shutdown == nil || s.config.Shutdown.PreStopHook == nil {
		return
	}

	s.logger.Info("Executing pre-stop hook",
		"instance_id", instance.id,
		"hook", s.config.Shutdown.PreStopHook.Name,
	)

	hookExecutor := hooks.NewExecutor(s.logger)
	if err := hookExecutor.ExecuteWithType(ctx, s.config.Shutdown.PreStopHook, "pre_stop"); err != nil {
		s.logger.Warn("Pre-stop hook failed",
			"instance_id", instance.id,
			"error", err,
		)
		// Continue with shutdown even if hook fails
	}
}

// sendShutdownSignal sends the configured shutdown signal to the process
func (s *Supervisor) sendShutdownSignal(instance *Instance) error {
	sig := syscall.SIGTERM
	if s.config.Shutdown != nil && s.config.Shutdown.Signal != "" {
		sig = parseSignal(s.config.Shutdown.Signal)
	}

	// Since we use Setpgid, we need to send signal to the process group
	pgid, err := syscall.Getpgid(instance.pid)
	if err != nil {
		// Fallback to single process signal if we can't get pgid
		s.logger.Warn("Failed to get process group, sending signal to process only",
			"instance_id", instance.id,
			"error", err,
		)
		if err := instance.cmd.Process.Signal(sig); err != nil {
			return fmt.Errorf("failed to send signal: %w", err)
		}
		return nil
	}

	// Send signal to entire process group (negative PID)
	if err := syscall.Kill(-pgid, sig); err != nil {
		// Fallback to single process signal if process group signal fails
		s.logger.Warn("Failed to send signal to process group, falling back to direct signal",
			"instance_id", instance.id,
			"pgid", pgid,
			"error", err,
		)
		if err := instance.cmd.Process.Signal(sig); err != nil {
			return fmt.Errorf("failed to send signal: %w", err)
		}
	}
	return nil
}

// cleanupInstanceResources cleans up resources after instance stops
func (s *Supervisor) cleanupInstanceResources(instance *Instance, pid int, reason string) {
	s.auditLogger.LogProcessStop(s.name, pid, reason)
	if s.resourceCollector != nil {
		s.resourceCollector.RemoveBuffer(s.name, instance.id)
	}
}

// handleOneshotExit processes completion of a oneshot process
func (s *Supervisor) handleOneshotExit(instance *Instance, exitCode int, err error) {
	instance.mu.Lock()
	execID := instance.oneshotExecID
	if exitCode == 0 {
		instance.state = StateCompleted
		s.logger.Info("Oneshot process completed successfully",
			"instance_id", instance.id,
		)
	} else {
		instance.state = StateFailed
		s.logger.Error("Oneshot process failed",
			"instance_id", instance.id,
			"exit_code", exitCode,
			"error", err,
		)
	}
	instance.mu.Unlock()

	// Record completion in oneshot history
	if execID > 0 && s.oneshotHistory != nil {
		s.oneshotHistory.Complete(execID, exitCode, err)
	}

	// Signal readiness if completed successfully (allows dependents to proceed)
	if exitCode == 0 {
		s.markReady("oneshot completed successfully")
	}

	s.checkAllInstancesDead()
}

// logProcessExit logs process exit appropriately based on whether it was intentional
func (s *Supervisor) logProcessExit(instance *Instance, exitCode int, restartCount int, allowRestart bool, err error) {
	if err != nil {
		if !allowRestart {
			// Intentional stop (shutdown/scale down) - not an error
			s.logger.Debug("Process instance terminated during shutdown",
				"instance_id", instance.id,
				"exit_code", exitCode,
				"signal", err.Error(),
			)
		} else {
			// Unexpected exit - log as error
			s.logger.Error("Process instance exited with error",
				"instance_id", instance.id,
				"exit_code", exitCode,
				"restart_count", restartCount,
				"error", err,
			)

			// Log crash to audit trail only for unexpected exits
			signal := ""
			if instance.cmd.ProcessState != nil && instance.cmd.ProcessState.Sys() != nil {
				signal = instance.cmd.ProcessState.String()
			}
			s.auditLogger.LogProcessCrash(s.name, instance.pid, exitCode, signal)
		}
	} else {
		s.logger.Info("Process instance exited",
			"instance_id", instance.id,
			"exit_code", exitCode,
			"restart_count", restartCount,
		)
	}
}

// attemptRestart attempts to restart a failed process instance
func (s *Supervisor) attemptRestart(instance *Instance, exitCode int, restartCount int) {
	backoff := s.restartPolicy.BackoffDuration(restartCount)

	// Determine restart reason
	restartReason := "crash"
	if exitCode == 0 {
		restartReason = "normal_exit"
	}

	s.logger.Info("Restarting process instance",
		"instance_id", instance.id,
		"backoff", backoff,
		"attempt", restartCount+1,
		"reason", restartReason,
	)

	// Record restart metric
	metrics.RecordProcessRestart(s.name, restartReason)

	// Wait for backoff period with context respect
	select {
	case <-time.After(backoff):
		// Continue with restart
	case <-s.ctx.Done():
		s.logger.Info("Restart cancelled due to shutdown",
			"instance_id", instance.id,
		)
		return
	}

	// Check context again before expensive restart operation
	if s.ctx.Err() != nil {
		s.logger.Debug("Context cancelled, skipping restart",
			"instance_id", instance.id,
		)
		return
	}

	// Attempt restart using supervisor context
	// Use the stored instance index to preserve port assignment
	newInstance, err := s.startInstance(s.ctx, instance.id, instance.index)
	if err != nil {
		s.logger.Error("Failed to restart process instance",
			"instance_id", instance.id,
			"error", err,
			"restart_count", restartCount,
		)
		instance.mu.Lock()
		instance.state = StateFailed
		instance.mu.Unlock()
		return
	}

	// Update restart count
	newInstance.mu.Lock()
	newInstance.restartCount = restartCount + 1
	newInstance.mu.Unlock()

	// Log restart to audit trail
	s.auditLogger.LogProcessRestart(s.name, instance.pid, newInstance.pid, restartReason)

	// Replace old instance with new one
	s.mu.Lock()
	for i, inst := range s.instances {
		if inst.id == instance.id {
			s.instances[i] = newInstance
			break
		}
	}
	s.mu.Unlock()
}

// envVars returns environment variables for a process instance
func (s *Supervisor) envVars(instanceID string, instanceIndex int) []string {
	envs := make([]string, 0, len(s.config.Env)+5)

	// Add configured environment variables
	for key, value := range s.config.Env {
		envs = append(envs, fmt.Sprintf("%s=%s", key, value))
	}

	// Add instance-specific variables
	envs = append(envs,
		fmt.Sprintf("PHPEEK_PM_PROCESS_NAME=%s", s.name),
		fmt.Sprintf("PHPEEK_PM_INSTANCE_ID=%s", instanceID),
		fmt.Sprintf("PHPEEK_PM_INSTANCE_INDEX=%d", instanceIndex),
	)

	// Add PORT env var if port_base is configured (for Node.js/web apps)
	// Each instance gets a unique port: PORT = port_base + instance_index
	if s.config.PortBase > 0 {
		port := s.config.PortBase + instanceIndex
		envs = append(envs, fmt.Sprintf("PORT=%d", port))
	}

	return envs
}

// handleHealthStatus monitors health check results and handles failures
func (s *Supervisor) handleHealthStatus(ctx context.Context) {
	// CRITICAL: Panic recovery to prevent health monitor crashes
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("PANIC in handleHealthStatus recovered",
				"panic", r,
			)
		}
	}()

	for {
		select {
		case status, ok := <-s.healthStatus:
			if !ok {
				s.logger.Debug("Health status channel closed, stopping health monitor")
				return
			}

			if status.Healthy {
				// Signal readiness on first successful health check
				s.markReady("health check passed")
			} else if !status.Healthy {
				s.logger.Error("Process unhealthy, triggering restart",
					"error", status.Error,
				)

				// Record health check restart
				metrics.RecordProcessRestart(s.name, "health_check")

				// Copy instances slice to avoid holding supervisor lock while locking instances
				s.mu.RLock()
				instancesCopy := make([]*Instance, len(s.instances))
				copy(instancesCopy, s.instances)
				s.mu.RUnlock()

				// Restart all instances that are currently running
				for _, instance := range instancesCopy {
					instance.mu.Lock()
					if instance.state == StateRunning {
						s.logger.Info("Health check failure - restarting instance",
							"instance_id", instance.id,
							"pid", instance.pid,
						)

						// Kill the unhealthy instance
						if instance.cmd.Process != nil {
							_ = instance.cmd.Process.Kill()
						}

						// The monitorInstance goroutine will handle the restart
						// based on the restart policy
					}
					instance.mu.Unlock()
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

// parseSignal converts signal name to syscall.Signal
func parseSignal(name string) syscall.Signal {
	switch name {
	case "SIGTERM":
		return syscall.SIGTERM
	case "SIGQUIT":
		return syscall.SIGQUIT
	case "SIGINT":
		return syscall.SIGINT
	case "SIGKILL":
		return syscall.SIGKILL
	default:
		return syscall.SIGTERM
	}
}

// GetState returns the current supervisor state
func (s *Supervisor) GetState() ProcessState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// InstanceInfo represents exported instance information
type InstanceInfo struct {
	ID           string
	State        string
	PID          int
	StartedAt    int64
	RestartCount int
}

// GetInstances returns information about all instances
func (s *Supervisor) GetInstances() []InstanceInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	instances := make([]InstanceInfo, 0, len(s.instances))
	for _, inst := range s.instances {
		inst.mu.RLock()
		instances = append(instances, InstanceInfo{
			ID:           inst.id,
			State:        string(inst.state),
			PID:          inst.pid,
			StartedAt:    inst.started.Unix(),
			RestartCount: inst.restartCount,
		})
		inst.mu.RUnlock()
	}

	return instances
}

// checkAllInstancesDead checks if all instances are dead and notifies manager
func (s *Supervisor) checkAllInstancesDead() {
	s.mu.RLock()
	allDead := true
	for _, inst := range s.instances {
		inst.mu.RLock()
		if inst.state == StateRunning {
			allDead = false
		}
		inst.mu.RUnlock()
		if !allDead {
			break
		}
	}
	notifier := s.deathNotifier
	s.mu.RUnlock()

	if allDead && len(s.instances) > 0 && notifier != nil {
		s.logger.Debug("All instances dead, notifying manager")
		notifier(s.name)
	}
}

// collectResourceMetrics periodically collects resource metrics for all running instances
func (s *Supervisor) collectResourceMetrics() {
	// Defensive: Check if resourceCollector is nil (should never happen if this is called)
	if s.resourceCollector == nil {
		return
	}

	// Get collection interval from manager
	interval := s.resourceCollector.GetInterval()
	if interval <= 0 {
		s.logger.Debug("Resource metrics collection disabled (interval <= 0)")
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	s.logger.Debug("Resource metrics collection started", "interval", interval)

	for {
		select {
		case <-s.ctx.Done():
			s.logger.Debug("Resource metrics collection stopped")
			return

		case <-ticker.C:
			// Collect metrics for all running instances
			s.collectInstanceMetrics()
		}
	}
}

// collectInstanceMetrics collects and records metrics for all running instances
func (s *Supervisor) collectInstanceMetrics() {
	// Defensive: Check if resourceCollector is nil
	if s.resourceCollector == nil {
		return
	}

	startTime := time.Now()

	s.mu.RLock()
	instances := make([]*Instance, len(s.instances))
	copy(instances, s.instances)
	maxMemoryMB := s.config.MaxMemoryMB
	s.mu.RUnlock()

	// Collect metrics for each instance
	for _, inst := range instances {
		inst.mu.RLock()
		pid := inst.pid
		instanceID := inst.id
		state := inst.state
		inst.mu.RUnlock()

		// Only collect metrics for running processes
		if state != StateRunning || pid == 0 {
			continue
		}

		// Collect process metrics
		sample, err := metrics.CollectProcessMetrics(pid, s.name, instanceID)
		if err != nil {
			metrics.ResourceCollectionErrors.WithLabelValues(s.name, instanceID).Inc()
			s.logger.Debug("Failed to collect metrics",
				"instance", instanceID,
				"pid", pid,
				"error", err,
			)
			continue
		}

		// Store in time series buffer
		s.resourceCollector.AddSample(s.name, instanceID, *sample)

		// Update Prometheus gauges
		metrics.UpdatePrometheusMetrics(s.name, instanceID, sample)

		// Check memory limit and trigger restart if exceeded
		// This provides memory leak protection for long-running processes
		if maxMemoryMB > 0 && sample.MemoryRSSBytes > 0 {
			memoryMB := sample.MemoryRSSBytes / (1024 * 1024)
			if memoryMB >= uint64(maxMemoryMB) {
				s.logger.Warn("Process memory limit exceeded, triggering restart",
					"instance_id", instanceID,
					"pid", pid,
					"memory_mb", memoryMB,
					"max_memory_mb", maxMemoryMB,
				)
				// Kill the process - monitorInstance will handle restart based on policy
				inst.mu.Lock()
				if inst.state == StateRunning && inst.cmd != nil && inst.cmd.Process != nil {
					// Record memory restart metric
					metrics.RecordProcessRestart(s.name, "memory_limit")
					_ = inst.cmd.Process.Kill()
				}
				inst.mu.Unlock()
			}
		}
	}

	// Record collection duration
	duration := time.Since(startTime).Seconds()
	metrics.ResourceCollectionDuration.Observe(duration)
}

// GetLogs returns log entries from all instances of this process
// Aggregates logs from stdout and stderr writers of all instances
func (s *Supervisor) GetLogs(limit int) []logger.LogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var allLogs []logger.LogEntry

	// Collect logs from all instances
	for _, inst := range s.instances {
		inst.mu.RLock()
		stdoutWriter := inst.stdoutWriter
		stderrWriter := inst.stderrWriter
		inst.mu.RUnlock()

		// Get logs from stdout
		if stdoutWriter != nil {
			if limit > 0 {
				allLogs = append(allLogs, stdoutWriter.GetRecentLogs(limit)...)
			} else {
				allLogs = append(allLogs, stdoutWriter.GetLogs()...)
			}
		}

		// Get logs from stderr
		if stderrWriter != nil {
			if limit > 0 {
				allLogs = append(allLogs, stderrWriter.GetRecentLogs(limit)...)
			} else {
				allLogs = append(allLogs, stderrWriter.GetLogs()...)
			}
		}
	}

	// Sort by timestamp (newest first)
	sort.Slice(allLogs, func(i, j int) bool {
		return allLogs[i].Timestamp.After(allLogs[j].Timestamp)
	})

	// Apply limit if specified
	if limit > 0 && len(allLogs) > limit {
		allLogs = allLogs[:limit]
	}

	return allLogs
}
