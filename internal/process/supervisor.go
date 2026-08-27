package process

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/hooks"
	"github.com/cboxdk/init/internal/logger"
	"github.com/cboxdk/init/internal/logtail"
	"github.com/cboxdk/init/internal/metrics"
	"github.com/cboxdk/init/internal/signals"
	"github.com/cboxdk/init/internal/snapshot"
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

	// StateCheckpointed indicates the process has been snapshotted by the node
	// agent and its memory image is on disk, waiting for the next connection.
	//
	// This is a running service, not a stopped one. The process really has
	// exited — CRIU stops it as part of the dump — but the exit is the intended
	// outcome, so it must not be logged as a crash, must not consume the
	// restart budget, and must not count towards "all instances dead", which
	// would shut the whole container down. It is the one state where the
	// supervisor waits on a child specifically so it can be restored, because
	// an unreaped zombie keeps its PID and the restore then fails with
	// "Can't fork: File exists".
	StateCheckpointed ProcessState = "checkpointed"
)

// Default timeouts for supervisor operations.
const (
	// DefaultRestartBackoffInitial is the initial delay before restarting a failed process.
	DefaultRestartBackoffInitial = 5 * time.Second

	// DefaultRestartBackoffMax is the maximum delay between restart attempts.
	DefaultRestartBackoffMax = 1 * time.Minute

	// DefaultRestartStabilityWindow is how long an instance must stay up before
	// its restart budget resets. A process that runs longer than this is
	// treated as recovered, so occasional crashes over a long lifetime don't
	// exhaust MaxRestartAttempts and permanently abandon the service.
	DefaultRestartStabilityWindow = 60 * time.Second

	// DefaultGoroutineStopTimeout is the timeout for supervisor goroutines to stop during shutdown.
	DefaultGoroutineStopTimeout = 5 * time.Second

	// forceKillGrace bounds how long a force-kill waits for the process to be
	// reaped before escalating to SIGKILL (or giving up after it). The kernel
	// delivers SIGKILL promptly, so this only needs to cover reaping.
	forceKillGrace = 5 * time.Second

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
	name                   string
	config                 *config.Process
	logger                 *slog.Logger
	auditLogger            *audit.Logger
	instances              []*Instance
	state                  ProcessState
	healthMonitor          *HealthMonitor
	healthStatus           <-chan HealthStatus
	restartPolicy          RestartPolicy
	restartStabilityWindow time.Duration                 // uptime after which an instance's restart budget resets (0 = never reset)
	resourceCollector      *metrics.ResourceCollector    // Shared resource collector (can be nil)
	oneshotHistory         *OneshotHistory               // Shared oneshot history (can be nil)
	deathNotifier          func(string)                  // Callback when all instances are dead
	credentials            *Credentials                  // Resolved user/group credentials (nil = inherit)
	credentialErr          error                         // user/group was configured but could not be resolved; fail closed at start
	logBroadcaster         *logger.LogBroadcaster        // Shared broadcaster for real-time log subscriptions
	fileTailers            map[string]context.CancelFunc // active file tailers, keyed by config name
	healthCheckStrict      bool                          // Fail startup if health monitor creation fails
	snapshotStreams        bool                          // own the children's stdio pipes so they can survive a checkpoint
	ctx                    context.Context
	cancel                 context.CancelFunc
	// Readiness signalling. These are re-armed on each run (see resetReadiness),
	// so they are guarded by their own mutex rather than s.mu: markReady is
	// called from Start, which already holds s.mu, so reusing s.mu here would
	// self-deadlock.
	readinessMu        sync.RWMutex
	readinessCh        chan struct{}  // Closed when service becomes ready
	readinessOnce      sync.Once      // Ensures readinessCh is closed exactly once per run
	readyFailedCh      chan struct{}  // Closed when the process can never become ready (oneshot exited non-zero)
	readyFailedOnce    sync.Once      // Ensures readyFailedCh is closed exactly once per run
	readyFailReason    string         // Why readiness became impossible
	readinessGenCh     chan struct{}  // Closed when the signals above are re-armed for a new run
	readinessGen       uint64         // Incremented on each re-arm; identifies which run a signal belongs to
	isReady            bool           // Track readiness state
	healthKnown        bool           // Whether at least one health check result has been observed
	healthHealthy      bool           // Liveness health after thresholds/hysteresis
	lastCheckSucceeded bool           // Raw result from the most recent health check
	lastExitCode       int            // Exit code of the most recent instance death (see lastExitSet)
	lastExitSet        bool           // Whether an instance has ever exited (distinguishes "exited 0" from "never ran")
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
	stdoutStream  *snapshot.Stream // supervisor-owned pipe, survives a checkpoint
	stderrStream  *snapshot.Stream
	checkpointing bool // the pending exit is a checkpoint, not a crash
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

	// Uptime after which a healthy instance's restart budget resets.
	// Negative disables the reset; zero falls back to the default.
	stabilityWindow := globalCfg.RestartStabilityWindow
	if stabilityWindow == 0 {
		stabilityWindow = DefaultRestartStabilityWindow
	} else if stabilityWindow < 0 {
		stabilityWindow = 0
	}

	// Resolve user/group credentials at initialization
	var creds *Credentials
	var credErr error
	if cfg.User != "" || cfg.Group != "" {
		var err error
		creds, err = ResolveCredentials(cfg.User, cfg.Group)
		if err != nil {
			logger.Error("Failed to resolve configured user/group; process will not be started as root",
				"process", name,
				"user", cfg.User,
				"group", cfg.Group,
				"error", err,
			)
			// Fail closed. Previously this logged and continued with no
			// credentials, so a typo in `user:` silently ran the workload as
			// PID 1's uid (root). Remember the error and refuse to start the
			// instance instead of dropping the privilege drop.
			credErr = fmt.Errorf("failed to resolve user/group (user=%q group=%q): %w", cfg.User, cfg.Group, err)
		} else if creds != nil {
			logger.Info("Resolved process credentials",
				"process", name,
				"uid", creds.Uid,
				"gid", creds.Gid,
			)
		}
	}

	return &Supervisor{
		name:                   name,
		config:                 cfg,
		logger:                 logger.With("process", name),
		auditLogger:            auditLogger,
		instances:              make([]*Instance, 0, cfg.Scale),
		state:                  StateStopped,
		restartPolicy:          NewRestartPolicy(cfg.Restart, maxAttempts, initialBackoff, maxBackoff),
		restartStabilityWindow: stabilityWindow,
		resourceCollector:      resourceCollector,
		credentials:            creds,
		credentialErr:          credErr,
		healthCheckStrict:      globalCfg.HealthCheckStrict,
		readinessCh:            make(chan struct{}),
		readyFailedCh:          make(chan struct{}),
		readinessGenCh:         make(chan struct{}),
		isReady:                false,
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

// SetLogBroadcaster sets the shared log broadcaster for real-time log subscriptions
func (s *Supervisor) SetLogBroadcaster(b *logger.LogBroadcaster) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logBroadcaster = b
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
	s.readinessMu.Lock()
	defer s.readinessMu.Unlock()
	s.readinessOnce.Do(func() {
		close(s.readinessCh)
		s.isReady = true
		s.logger.Debug("Service marked as ready",
			"reason", reason,
		)
	})
}

// resetReadiness re-arms the readiness signals for a fresh run. Readers take
// the channels under readinessMu, so replacing them here is safe.
func (s *Supervisor) resetReadiness() {
	s.readinessMu.Lock()
	defer s.readinessMu.Unlock()
	// Wake anyone blocked on the retiring signals so they re-read the new ones.
	// This must be a separate signal, never a close of readinessCh — that would
	// tell the waiter the service is ready when nothing has succeeded.
	close(s.readinessGenCh)
	s.readinessGenCh = make(chan struct{})
	s.readinessGen++
	s.readinessCh = make(chan struct{})
	s.readinessOnce = sync.Once{}
	s.isReady = false
	s.readyFailedCh = make(chan struct{})
	s.readyFailedOnce = sync.Once{}
	s.readyFailReason = ""
}

// readinessChannels returns the current readiness signals under the lock, so a
// waiter cannot race a concurrent re-arm in Start.
func (s *Supervisor) readinessChannels() (ready, failed, rearmed <-chan struct{}, gen uint64) {
	s.readinessMu.RLock()
	defer s.readinessMu.RUnlock()
	return s.readinessCh, s.readyFailedCh, s.readinessGenCh, s.readinessGen
}

// livenessGated reports whether the health check may restart the process. A
// check configured as readiness-only gates dependents; it must not kill the
// service. An unset mode means "both", which does gate liveness.
func (s *Supervisor) livenessGated() bool {
	return s.config.HealthCheck == nil || s.config.HealthCheck.Mode != "readiness"
}

// readyFailedChannel returns just the failure signal, for a non-blocking peek.
func (s *Supervisor) readyFailedChannel() <-chan struct{} {
	s.readinessMu.RLock()
	defer s.readinessMu.RUnlock()
	return s.readyFailedCh
}

// readinessGeneration returns the current run's readiness generation.
func (s *Supervisor) readinessGeneration() uint64 {
	s.readinessMu.RLock()
	defer s.readinessMu.RUnlock()
	return s.readinessGen
}

// markReadinessImpossible records that this process can never become ready, so
// anything waiting on it (a dependent's WaitForReadiness) fails immediately
// instead of burning the whole dependency timeout. Used when a oneshot exits
// non-zero: readiness for a oneshot means "completed successfully", and a failed
// run will never produce that. Thread-safe and idempotent.
func (s *Supervisor) markReadinessImpossible(reason string) {
	s.readinessMu.Lock()
	defer s.readinessMu.Unlock()
	s.readyFailedOnce.Do(func() {
		s.readyFailReason = reason
		close(s.readyFailedCh)
		s.logger.Debug("Readiness marked impossible", "reason", reason)
	})
}

// readinessFailure returns the recorded reason readiness became impossible.
func (s *Supervisor) readinessFailure() string {
	s.readinessMu.RLock()
	defer s.readinessMu.RUnlock()
	if s.readyFailReason == "" {
		return "process cannot become ready"
	}
	return s.readyFailReason
}

// WaitForReadiness waits for the service to become ready (health check passes)
// Returns nil when ready, error on timeout or context cancellation
func (s *Supervisor) WaitForReadiness(ctx context.Context, timeout time.Duration) error {
	if s.config.HealthCheck == nil {
		// A oneshot with no health check is ready only when it completes
		// successfully — so a dependent (depends_on) waits for it to finish (the
		// canonical migrate-then-serve pattern) instead of treating it as ready
		// the instant it forks. handleOneshotExit closes readinessCh on exit 0.
		// A long-running process with no health check is ready as soon as it is
		// running. (PID1-9)
		if s.config.Type != "oneshot" {
			// Ready as soon as it is running — but not if it is already dead:
			// checkAllInstancesDead marks readiness impossible for a longrun whose
			// instances have all exited, and a dependent must see that rather than
			// launch against a service that is gone.
			select {
			case <-s.readyFailedChannel():
				return fmt.Errorf("service can never become ready: %s", s.readinessFailure())
			default:
			}
			s.logger.Debug("No health check configured, considering ready immediately")
			return nil
		}
		s.logger.Info("Waiting for oneshot to complete before dependents start", "timeout", timeout)
	} else if s.config.Type == "oneshot" {
		// For a oneshot, "ready" means "finished successfully" whatever the
		// health check says — a liveness probe on a task that is supposed to exit
		// must not let dependents start early, and a readiness probe passing
		// mid-run does not mean the work is done.
		s.logger.Info("Waiting for oneshot to complete before dependents start (health check gates liveness only)",
			"timeout", timeout)
	} else {
		// Check health check mode - only wait if readiness or both
		mode := s.config.HealthCheck.Mode
		if mode == "liveness" {
			s.logger.Debug("Health check mode is liveness-only, not waiting for readiness")
			return nil
		}
		s.logger.Info("Waiting for service readiness",
			"timeout", timeout,
			"health_check_type", s.config.HealthCheck.Type,
		)
	}

	// Wait for readiness with timeout. readyFailedCh short-circuits the wait when
	// readiness has become impossible (a oneshot that exited non-zero will never
	// complete successfully), so a failed migration fails its dependents in
	// milliseconds instead of burning the full dependency timeout with the
	// manager lock held.
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		readyCh, failedCh, rearmedCh, gen := s.readinessChannels()
		select {
		case <-readyCh:
			// Ignore a signal that belongs to a run which has since been
			// replaced: a channel snapshotted just before a re-arm may already be
			// closed, and reporting that as ready would release dependents
			// against a process that is starting over.
			if s.readinessGeneration() != gen {
				continue
			}
			s.logger.Info("Service ready")
			return nil
		case <-failedCh:
			if s.readinessGeneration() != gen {
				continue
			}
			return fmt.Errorf("service can never become ready: %s", s.readinessFailure())
		case <-rearmedCh:
			// The supervisor started a new run while we were waiting; re-read the
			// signals and keep waiting on the new run within the same deadline.
			s.logger.Debug("Readiness signals re-armed while waiting; watching the new run")
		case <-timer.C:
			return fmt.Errorf("service did not become ready within %v", timeout)
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for readiness")
		}
	}
}

// Start starts all instances of the process
func (s *Supervisor) Start(ctx context.Context) error {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Guard against double-start. The manager checks state before calling Start,
	// but that check is outside any lock, so two concurrent callers (API + TUI +
	// watcher all reach the manager) can both pass it and each append `Scale`
	// instances while overwriting s.ctx/s.cancel — leaking the first set. Under
	// operationMu, re-checking here makes Start idempotent for an
	// already-running supervisor. (Reproduced in review: 4 concurrent starts on
	// a scale-1 process yielded 4 instances.)
	if s.state == StateRunning || s.state == StateStarting {
		s.logger.Debug("Start called on an already-running supervisor; ignoring", "state", s.state)
		return nil
	}

	// Create context for this supervisor's lifetime
	s.ctx, s.cancel = context.WithCancel(ctx)

	// A new run gets fresh readiness state. Without this the signals are sticky
	// for the supervisor's whole life: a restarted service would count as ready
	// before it had proven anything, and — worse — a oneshot that failed once
	// would keep failing its dependents forever, even after a successful re-run
	// (both channels closed, and select would pick between them at random).
	s.resetReadiness()

	s.state = StateStarting

	// Start instances based on scale
	for i := 0; i < s.config.Scale; i++ {
		instanceID := fmt.Sprintf("%s-%d", s.name, i)

		instance, err := s.startInstance(s.ctx, instanceID, i)
		if err != nil {
			s.state = StateFailed
			// Cancel first to suppress restarts, then force-kill the instances
			// already started in this loop. Context cancellation no longer kills
			// children (see startInstance), so the cleanup must be explicit or a
			// partial-start failure would leak the survivors.
			s.cancel()
			for _, started := range s.instances {
				if kerr := s.signalProcessGroup(started, syscall.SIGKILL, "aborted start cleanup"); kerr != nil {
					s.logger.Warn("Failed to kill instance during aborted start",
						"instance_id", started.id,
						"error", kerr,
					)
				}
			}
			return fmt.Errorf("failed to start instance %s: %w", instanceID, err)
		}

		s.instances = append(s.instances, instance)
	}

	s.state = StateRunning

	// Start file tailers (config-bound, independent of process instances)
	s.startFileTailers(s.ctx)

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
	} else if s.config.Type == "oneshot" {
		// A oneshot with no health check becomes ready when it completes
		// successfully (handleOneshotExit), NOT at fork — otherwise a dependent
		// would start before the oneshot (e.g. a migration) finished. (PID1-9)
		s.logger.Debug("Oneshot readiness gated on successful completion")
	} else {
		// Long-running process with no health check - ready as soon as running.
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
	// Fail closed on an unresolved credential: the operator asked to run this
	// process as a specific user/group, and running it as root instead would be
	// a silent privilege escalation.
	if s.credentialErr != nil {
		return nil, fmt.Errorf("refusing to start %s: %w", instanceID, s.credentialErr)
	}

	s.logger.Info("Starting process instance",
		"instance_id", instanceID,
		"instance_index", instanceIndex,
		"command", s.config.Command,
	)

	// Create command
	cmd := exec.CommandContext(ctx, s.config.Command[0], s.config.Command[1:]...)

	// Context cancellation must NOT kill the child. By default CommandContext
	// makes ctx-cancel call Process.Kill (SIGKILL); because every instance is
	// bound to s.ctx, Stop()'s s.cancel() would then SIGKILL every child before
	// the pre-stop hook and the configured shutdown signal ever run — defeating
	// graceful shutdown entirely. Graceful stop is driven explicitly by
	// stopInstance (pre-stop hook → configured signal → timeout → SIGKILL); a
	// no-op Cancel with the default zero WaitDelay leaves the process untouched
	// on cancel and hands sole stop authority to stopInstance. Aborted-start
	// cleanup (see Start) force-kills survivors explicitly for the same reason.
	cmd.Cancel = func() error { return nil }

	if s.config.WorkingDir != "" {
		cmd.Dir = s.config.WorkingDir
	}

	// Set environment variables
	envVars := append(os.Environ(), s.envVars(instanceID, instanceIndex)...)
	envVars = append(envVars,
		fmt.Sprintf("CBOX_SERVICE=%s", s.name),
		fmt.Sprintf("CBOX_INSTANCE=%s", instanceID),
		fmt.Sprintf("CBOX_PROCESS=%s", s.name),
	)
	cmd.Env = envVars

	// A session of its own for processes that may be checkpointed, an ordinary
	// process group for everything else.
	//
	// Without the session the child shares cbox-init's, CRIU classifies the
	// dump as a shell job and demands --shell-job — and a shell-job restore
	// does not outlive the process that performed it, so the workload comes
	// back and immediately vanishes.
	//
	// The two flags are exclusive rather than additive: setsid already puts the
	// process in a new group and makes it the session leader, and setpgid on a
	// session leader fails with EPERM. Everything downstream that addresses the
	// process group still works, because the new group's id is the child's pid
	// either way.
	if s.snapshotStreams {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	} else {
		// CRITICAL: Put subprocess in its own process group
		// This prevents Ctrl+C (SIGINT) from propagating to child processes
		// Without this, Ctrl+C kills children → manager thinks crash → restarts
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
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

	// Wire log broadcaster to process writers for real-time subscriptions
	if s.logBroadcaster != nil {
		if stdoutWriter != nil {
			stdoutWriter.SetBroadcaster(s.logBroadcaster)
		}
		if stderrWriter != nil {
			stderrWriter.SetBroadcaster(s.logBroadcaster)
		}
	}

	// Hand os/exec an *os.File rather than an io.Writer when this process may be
	// checkpointed.
	//
	// The difference is not stylistic. Given an io.Writer, os/exec allocates a
	// pipe of its own and copies through a goroutine, and the write end is
	// unreachable from here — so after a dump there is nothing to give CRIU,
	// which then creates a fresh pipe and the restored process dies on its
	// first write with no reader. Given an *os.File it dups the descriptor
	// straight onto the child's fd and runs no goroutine, and we keep the write
	// end for as long as the process is supervised.
	var stdoutStream, stderrStream *snapshot.Stream

	if stdoutWriter != nil {
		if s.snapshotStreams {
			stdoutStream, err = snapshot.NewStream(stdoutWriter)
			if err != nil {
				return nil, fmt.Errorf("failed to create stdout stream: %w", err)
			}
			cmd.Stdout = stdoutStream.Writer()
		} else {
			cmd.Stdout = stdoutWriter
		}
	} else {
		cmd.Stdout = io.Discard
	}
	if stderrWriter != nil {
		if s.snapshotStreams {
			stderrStream, err = snapshot.NewStream(stderrWriter)
			if err != nil {
				if stdoutStream != nil {
					_ = stdoutStream.Close()
				}
				return nil, fmt.Errorf("failed to create stderr stream: %w", err)
			}
			cmd.Stderr = stderrStream.Writer()
		} else {
			cmd.Stderr = stderrWriter
		}
	} else {
		cmd.Stderr = io.Discard
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	// Register the pid with the zombie reaper so that, if the wildcard reaper
	// wins the race to reap this child, it captures the exit status for us
	// instead of leaving cmd.Wait() with a nil ProcessState. See
	// signals.RegisterSupervised for the full rationale.
	signals.RegisterSupervised(cmd.Process.Pid)

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
		stdoutStream: stdoutStream,
		stderrStream: stderrStream,
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

// startFileTailers starts file tailers for all configured log files.
// Each tailer gets its own ProcessWriter with the file's logging config
// plus the process-level redaction config.
// Tailers are bound to configuration, not process state — they survive restarts.
func (s *Supervisor) startFileTailers(ctx context.Context) {
	if s.config.Logging == nil || len(s.config.Logging.Files) == 0 {
		return
	}

	if s.fileTailers == nil {
		s.fileTailers = make(map[string]context.CancelFunc)
	}

	for name, fileCfg := range s.config.Logging.Files {
		// Build a LoggingConfig for this file, inheriting process-level redaction
		fileLogging := &config.LoggingConfig{
			Redaction:      s.config.Logging.Redaction, // Inherited from process
			MinLevel:       fileCfg.MinLevel,
			JSON:           fileCfg.JSON,
			LevelDetection: fileCfg.LevelDetection,
			Multiline:      fileCfg.Multiline,
			Filters:        fileCfg.Filters,
		}

		// Create a ProcessWriter for this file
		pw, err := logger.NewProcessWriter(s.logger, s.name, name, "file", fileLogging)
		if err != nil {
			s.logger.Error("Failed to create process writer for log file",
				"file", name, "path", fileCfg.Path, "error", err)
			continue
		}

		// Wire broadcaster for real-time subscriptions
		if s.logBroadcaster != nil {
			pw.SetBroadcaster(s.logBroadcaster)
		}

		// Create optional rotator
		var rotator *logtail.FileRotator
		if fileCfg.Rotate != nil && fileCfg.Rotate.MaxSize != "" {
			maxBytes, err := config.ParseSize(fileCfg.Rotate.MaxSize)
			if err != nil {
				s.logger.Error("Invalid rotate max_size for log file",
					"file", name, "error", err)
				continue
			}
			rotator = logtail.NewFileRotator(maxBytes, fileCfg.Rotate.MaxFiles)
		}

		// Create and start tailer
		tailer := logtail.New(fileCfg.Path, pw, rotator)

		tailerCtx, tailerCancel := context.WithCancel(ctx)
		s.fileTailers[name] = tailerCancel

		s.goroutines.Add(1)
		go func(n, p string) {
			defer s.goroutines.Done()
			if err := tailer.Start(tailerCtx); err != nil && tailerCtx.Err() == nil {
				s.logger.Error("File tailer error", "file", n, "path", p, "error", err)
			}
		}(name, fileCfg.Path)

		s.logger.Info("Started file tailer", "file", name, "path", fileCfg.Path)
	}
}

// stopFileTailers stops all active file tailers.
func (s *Supervisor) stopFileTailers() {
	for name, cancel := range s.fileTailers {
		cancel()
		s.logger.Debug("Stopped file tailer", "file", name)
	}
	s.fileTailers = nil
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
	pid := instance.pid
	// Determine the exit code without assuming cmd.Wait() reaped the child.
	// The zombie reaper may have won the race and reaped it first, in which
	// case ProcessState is nil. Recover the exit code from the status the
	// reaper captured; matching os.ProcessState.ExitCode() semantics
	// (signalled/stopped processes report -1).
	var exitCode int
	switch {
	case instance.cmd.ProcessState != nil:
		exitCode = instance.cmd.ProcessState.ExitCode()
		signals.UnregisterSupervised(pid)
	default:
		if ws, ok := signals.TakeReapedStatus(pid); ok {
			if ws.Exited() {
				exitCode = ws.ExitStatus()
			} else {
				exitCode = -1 // signalled/stopped, mirrors ProcessState.ExitCode()
			}
		} else {
			// Neither our Wait() nor our reaper collected a status. Treat as
			// an abnormal exit so the restart policy still runs.
			exitCode = -1
			signals.UnregisterSupervised(pid)
		}
	}
	// A checkpointed instance has exited on purpose. Waiting on it here is the
	// point: the dumped process is a zombie until reaped, and the zombie keeps
	// its PID, which is exactly the PID the restore has to recreate.
	if instance.checkpointing {
		instance.state = StateCheckpointed
		instance.mu.Unlock()

		// Every descendant needs reaping too, not just the process we started.
		// A grandchild that outlived its own parent has been re-parented onto
		// us, and its zombie holds its PID just as stubbornly.
		if reaped := snapshot.DrainCheckpointed(snapshot.DrainOptions{}); len(reaped) > 0 {
			s.logger.Debug("Drained re-parented descendants after checkpoint",
				"instance_id", instance.id,
				"pids", reaped,
			)
		}

		signals.UnregisterSupervised(pid)
		s.logger.Info("Process instance checkpointed",
			"instance_id", instance.id,
			"pid", pid,
		)
		return
	}

	instance.state = StateStopped
	restartCount := instance.restartCount
	// Reset the restart budget once an instance has stayed up long enough to
	// be considered stable. Without this, restartCount accumulates over the
	// whole container lifetime and a service that crashes rarely (more than
	// MaxRestartAttempts times, but with long healthy periods between crashes)
	// would be permanently abandoned. Sliding-window semantics à la systemd's
	// StartLimitIntervalSec.
	if resetRestartBudget(s.restartStabilityWindow, time.Since(instance.started)) {
		restartCount = 0
	}
	instance.mu.Unlock()

	// Remember this instance's exit code for the container-level exit decision.
	// When every process is dead, the manager uses the worst of these to decide
	// PID 1's own exit code (see Manager.TerminalExitCode).
	s.mu.Lock()
	s.lastExitCode = exitCode
	s.lastExitSet = true
	s.mu.Unlock()

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

// SetSnapshotStreams makes the supervisor own its children's stdio pipes so
// they can survive a checkpoint. It must be set before the process starts;
// changing it later only affects instances started afterwards.
func (s *Supervisor) SetSnapshotStreams(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshotStreams = enabled
}

// BeginCheckpoint tells the supervisor that the exits about to happen are a
// checkpoint rather than a failure. It must be called before the agent dumps,
// because the dump stops the processes and the monitor goroutine will see the
// exit immediately afterwards.
//
// Returns the PIDs the agent should checkpoint, in the container's own PID
// numbering, which is what CRIU records and what it will restore.
func (s *Supervisor) BeginCheckpoint() []int {
	s.mu.RLock()
	instances := append([]*Instance(nil), s.instances...)
	s.mu.RUnlock()

	pids := make([]int, 0, len(instances))
	for _, instance := range instances {
		instance.mu.Lock()
		if instance.state == StateRunning {
			instance.checkpointing = true
			// The same flag the ordinary intentional-stop path uses, so the
			// exit is never logged as a crash and never audited as one.
			instance.allowRestart = false
			pids = append(pids, instance.pid)
		}
		instance.mu.Unlock()
	}
	return pids
}

// AbortCheckpoint undoes BeginCheckpoint when the dump failed. The processes
// are still running and must go back to being ordinary supervised processes —
// a failed checkpoint leaves the workload alone and says so, rather than
// disabling itself quietly and force-killing the container next time.
func (s *Supervisor) AbortCheckpoint() {
	s.mu.RLock()
	instances := append([]*Instance(nil), s.instances...)
	s.mu.RUnlock()

	for _, instance := range instances {
		instance.mu.Lock()
		if instance.checkpointing {
			instance.checkpointing = false
			instance.allowRestart = true
		}
		instance.mu.Unlock()
	}
}

// CompleteCheckpoint is called once the dump has actually finished, and does
// the two things that only make sense at exactly that moment.
//
// It reaps. A dumped process is a zombie until someone waits on it and the
// zombie keeps its PID, which is precisely the PID the restore has to recreate
// — "Can't fork for <pid>: File exists". Every descendant needs it, not just
// the process we started, because a grandchild that outlived its own parent has
// been re-parented onto us and holds its PID just as stubbornly. Reaping
// earlier would race the dump; reaping later means the restore has already
// failed.
//
// And it settles the state for instances nothing is waiting on. The first
// checkpoint of a process is observed by its monitoring goroutine, because
// os/exec is still waiting on it. Every checkpoint after the first is not: the
// restored tree arrived re-parented rather than forked, so there is no
// exec.Cmd behind it and no Wait to return. Without this the second dump would
// leave the instance looking like it is still running, and the warm tier would
// work exactly once.
func (s *Supervisor) CompleteCheckpoint() []int {
	reaped := snapshot.DrainCheckpointed(snapshot.DrainOptions{})

	s.mu.RLock()
	instances := append([]*Instance(nil), s.instances...)
	s.mu.RUnlock()

	for _, instance := range instances {
		instance.mu.Lock()
		if instance.checkpointing && instance.state != StateCheckpointed {
			instance.state = StateCheckpointed
			s.logger.Info("Process instance checkpointed",
				"instance_id", instance.id, "pid", instance.pid)
		}
		instance.mu.Unlock()
	}

	return reaped
}

// EndCheckpoint puts restored instances back into ordinary supervision and
// reports how many it re-armed.
//
// Without this the warm tier works exactly once. The dump ends the instance's
// lifetime as far as os/exec is concerned — cmd.Wait() has already returned, so
// its monitoring goroutine is gone — and the restored tree arrives re-parented
// onto cbox-init rather than forked from it. So the process is genuinely back,
// at the same PID CRIU recorded, but nothing is watching it: it would never be
// eligible to sleep again and its death would never be noticed.
//
// Watching it takes a different shape than watching a process we started. There
// is no wait4 on a specific PID available to us any more — the container's
// wildcard reaper collects it — so liveness is polled. A second of latency on
// noticing a crash is acceptable for a process that just came back from disk;
// silently never noticing is not.
func (s *Supervisor) EndCheckpoint() int {
	s.mu.RLock()
	instances := append([]*Instance(nil), s.instances...)
	s.mu.RUnlock()

	restored := 0
	for _, instance := range instances {
		instance.mu.Lock()
		if instance.state != StateCheckpointed {
			instance.mu.Unlock()
			continue
		}
		instance.state = StateRunning
		instance.checkpointing = false
		instance.allowRestart = true
		instance.doneCh = make(chan struct{})
		done := instance.doneCh
		pid := instance.pid
		instance.mu.Unlock()

		s.goroutines.Add(1)
		go func(inst *Instance, pid int, done chan struct{}) {
			defer s.goroutines.Done()
			s.monitorRestored(inst, pid, done)
		}(instance, pid, done)

		restored++
	}

	if restored > 0 {
		s.mu.Lock()
		s.state = StateRunning
		s.mu.Unlock()
		s.logger.Info("Process instances restored", "count", restored)
	}
	return restored
}

// monitorRestored watches a process that came back from a checkpoint image.
func (s *Supervisor) monitorRestored(instance *Instance, pid int, done chan struct{}) {
	// Panic recovery mirrors monitorInstance: a panic in the restart path
	// (attemptRestart / checkAllInstancesDead) must not take down PID 1. done is
	// always closed so nothing waiting on this watch hangs.
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("PANIC in monitorRestored recovered",
				"instance_id", instance.id,
				"pid", pid,
				"panic", r,
			)
			instance.mu.Lock()
			instance.state = StateFailed
			instance.mu.Unlock()
		}
		close(done)
	}()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}

		instance.mu.RLock()
		state := instance.state
		instance.mu.RUnlock()
		if state != StateRunning {
			// Checkpointed again, or being stopped. Either way this watch is
			// over and whoever changed the state owns what happens next.
			return
		}

		// ESRCH is the only answer that means gone. EPERM would mean the
		// process exists and is not ours, which cannot happen for a tree
		// re-parented onto this process.
		if err := syscall.Kill(pid, 0); err == nil {
			continue
		} else if err != syscall.ESRCH {
			continue
		}

		instance.mu.Lock()
		if instance.state != StateRunning {
			instance.mu.Unlock()
			return
		}
		instance.state = StateStopped
		restartCount := instance.restartCount
		allowRestart := instance.allowRestart
		instance.mu.Unlock()

		s.logger.Warn("Restored process instance exited", "instance_id", instance.id, "pid", pid)
		metrics.RecordProcessStop(s.name, instance.id, -1)

		// Record the death for the container-level exit decision, exactly as
		// monitorInstance does. Without this a restored process dying was
		// invisible to TerminalExitCode, so PID 1 could exit 0 after its whole
		// workload disappeared. -1 means "died, exit status unknown".
		s.mu.Lock()
		s.lastExitCode = -1
		s.lastExitSet = true
		s.mu.Unlock()

		if allowRestart && s.config.Type != "oneshot" && s.restartPolicy.ShouldRestart(-1, restartCount) {
			s.attemptRestart(instance, -1, restartCount)
		} else {
			s.checkAllInstancesDead()
		}
		return
	}
}

// RecoverCheckpointed replaces every checkpointed instance with a fresh one and
// reports how many it started.
//
// This is what a lost checkpoint costs, made explicit. The processes were
// dumped, so they are genuinely gone; if the images cannot be loaded there is
// nothing to bring back and the only way to have a working service again is to
// start one. The in-memory state is lost, and the caller says so — this
// function exists precisely so that losing it is a deliberate, logged act
// rather than something that happens because a restore quietly failed and the
// supervisor's ordinary restart policy picked up the pieces.
//
// The container is not recreated and the pod is not deleted. cbox-init is still
// PID 1, the pod still holds its IP, its volumes and its identity; only the
// memory image is gone.
func (s *Supervisor) RecoverCheckpointed(ctx context.Context) (int, error) {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()

	s.mu.RLock()
	instances := append([]*Instance(nil), s.instances...)
	runCtx := s.ctx
	s.mu.RUnlock()

	if runCtx == nil {
		runCtx = ctx
	}

	recovered := 0
	var errs []error

	for _, instance := range instances {
		instance.mu.Lock()
		if instance.state != StateCheckpointed {
			instance.mu.Unlock()
			continue
		}
		// The flag is cleared before the replacement starts: whatever happens
		// to the new process from here is an ordinary supervised lifetime, and
		// its exit must be treated as a crash again.
		instance.checkpointing = false
		id, index := instance.id, instance.index
		instance.mu.Unlock()

		fresh, err := s.startInstance(runCtx, id, index)
		if err != nil {
			errs = append(errs, fmt.Errorf("restarting %s: %w", id, err))
			instance.mu.Lock()
			instance.state = StateFailed
			instance.mu.Unlock()
			continue
		}

		s.mu.Lock()
		for i, existing := range s.instances {
			if existing.id == id {
				s.instances[i] = fresh
				break
			}
		}
		s.mu.Unlock()

		recovered++
	}

	if recovered > 0 {
		s.mu.Lock()
		s.state = StateRunning
		s.mu.Unlock()
	}

	if len(errs) > 0 {
		return recovered, fmt.Errorf("recovering checkpointed instances: %v", errs)
	}
	return recovered, nil
}

// IsCheckpointed reports whether every instance is currently checkpointed.
func (s *Supervisor) IsCheckpointed() bool {
	s.mu.RLock()
	instances := append([]*Instance(nil), s.instances...)
	s.mu.RUnlock()

	if len(instances) == 0 {
		return false
	}
	for _, instance := range instances {
		instance.mu.RLock()
		state := instance.state
		instance.mu.RUnlock()
		if state != StateCheckpointed {
			return false
		}
	}
	return true
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

	if len(errs) > 0 {
		// One or more instances would not stop, so they may still be running.
		// Keep them in the list: dropping them here would leave live processes
		// with nothing tracking them, and a later scale-up would start
		// replacements on the same instance indexes (and ports) alongside them.
		return fmt.Errorf("scale down completed with %d errors: %v", len(errs), errs)
	}

	// Remove stopped instances from the list
	s.mu.Lock()
	s.instances = s.instances[:targetScale]
	newScale := len(s.instances)
	s.mu.Unlock()

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

	// Stop file tailers
	s.stopFileTailers()

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

	if len(errs) > 0 {
		// At least one instance could not be stopped, so it may still be running.
		// Do NOT clear the instance list or mark the supervisor stopped: callers
		// (the reload abort and the rollback) rely on a failed stop leaving this
		// supervisor authoritative for the live process. Wiping it here would
		// orphan that process and let a second copy be started alongside it.
		s.mu.Lock()
		s.state = StateFailed
		s.mu.Unlock()
		return fmt.Errorf("stop completed with %d errors: %v", len(errs), errs)
	}

	s.mu.Lock()
	s.state = StateStopped
	s.instances = nil
	s.ctx = nil
	s.mu.Unlock()

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
	// Record the intent first, whatever the current state: calling stopInstance
	// means "this instance must not come back". If it exited on its own a moment
	// ago, its monitor goroutine may not have decided yet — leaving allowRestart
	// set here let it start a replacement that the caller (ScaleDown, Stop) has
	// already dropped from the instance list, orphaning a live process.
	instance.allowRestart = false
	if currentState != StateRunning {
		instance.mu.Unlock()
		s.logger.Debug("Process not running, skipping stop",
			"instance_id", instance.id,
			"current_state", currentState,
		)
		return nil
	}
	instance.state = StateStopping
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
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-instance.doneCh:
		s.logger.Info("Process instance stopped gracefully",
			"instance_id", instance.id,
		)
		s.cleanupInstanceResources(instance, pid, "graceful_shutdown")
		return nil

	case <-ctx.Done():
		// The caller's deadline fired first. During shutdown the manager passes a
		// context bounded by the global shutdown_timeout, so a larger per-process
		// shutdown.timeout must not be waited out here — otherwise one slow
		// service holds PID 1 (and, under the manager lock, the API) past the
		// global deadline. Escalate to the kill signal now.
		s.logger.Warn("Shutdown deadline reached before instance stopped gracefully, force killing",
			"instance_id", instance.id,
			"timeout", timeout,
			"error", ctx.Err(),
		)
		return s.forceKillInstance(instance, pid, "force_killed_shutdown_deadline")

	case <-timer.C:
		s.logger.Warn("Process instance did not stop gracefully, force killing",
			"instance_id", instance.id,
			"timeout", timeout,
		)
		return s.forceKillInstance(instance, pid, "force_killed_after_timeout")
	}
}

// forceKillInstance escalates a stuck instance to the kill signal and waits for
// monitorInstance to observe the exit before cleaning up. SIGKILL delivered to a
// direct child is guaranteed by the kernel to terminate it, so the doneCh wait
// here always completes.
func (s *Supervisor) forceKillInstance(instance *Instance, pid int, reason string) error {
	killSig := s.killSignal()
	s.logger.Warn("Force killing process instance",
		"instance_id", instance.id,
		"pid", pid,
		"kill_signal", killSig,
		"reason", reason,
	)

	if err := s.signalProcessGroup(instance, killSig, "force kill"); err != nil {
		return fmt.Errorf("failed to force kill process group: %w", err)
	}

	// Wait for monitorInstance to observe the exit. shutdown.kill_signal is
	// operator-configurable and may be something the process can ignore (a
	// SIGTERM it traps, or SIGSTOP, which does not terminate at all), so this
	// wait must be bounded: an unbounded wait here hangs PID 1 while the manager
	// lock is held. If the configured signal did not do it, escalate to a real
	// SIGKILL, which cannot be caught, blocked or ignored.
	if s.waitForInstanceExit(instance, forceKillGrace) {
		s.cleanupInstanceResources(instance, pid, reason)
		return nil
	}

	if killSig != syscall.SIGKILL {
		s.logger.Warn("Configured kill signal did not stop the instance; escalating to SIGKILL",
			"instance_id", instance.id,
			"pid", pid,
			"kill_signal", killSig,
		)
		if err := s.signalProcessGroup(instance, syscall.SIGKILL, "force kill (escalated)"); err != nil {
			return fmt.Errorf("failed to SIGKILL process group: %w", err)
		}
		if s.waitForInstanceExit(instance, forceKillGrace) {
			s.cleanupInstanceResources(instance, pid, reason)
			return nil
		}
	}

	// SIGKILL was delivered and the process still has not been reaped. Do not
	// block the manager any longer; report it so the caller can decide.
	return fmt.Errorf("instance %s (pid %d) did not exit after SIGKILL", instance.id, pid)
}

// waitForInstanceExit waits up to d for monitorInstance to close doneCh,
// reporting whether the instance exited.
func (s *Supervisor) waitForInstanceExit(instance *Instance, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-instance.doneCh:
		return true
	case <-timer.C:
		return false
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
	if err := hookExecutor.ExecuteWithType(ctx, s.config.Shutdown.PreStopHook, hooks.TypePreStop); err != nil {
		s.logger.Warn("Pre-stop hook failed",
			"instance_id", instance.id,
			"error", err,
		)
		// Continue with shutdown even if hook fails
	}
}

// LastExitCode returns the exit code of the most recent instance death and
// whether any instance has ever exited. It feeds the container-level exit-code
// decision when all processes are dead.
func (s *Supervisor) LastExitCode() (code int, exited bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastExitCode, s.lastExitSet
}

// ForwardSignal delivers sig to every running instance's process group. It is
// how operational signals received by cbox-init as PID 1 (SIGHUP, SIGUSR1,
// SIGUSR2) reach the workload — e.g. an nginx config reload or php-fpm log
// rotation — without being treated as a stop. Instances that are not running
// are skipped, and a per-instance failure is logged, not fatal.
func (s *Supervisor) ForwardSignal(sig syscall.Signal) {
	s.mu.RLock()
	instances := make([]*Instance, len(s.instances))
	copy(instances, s.instances)
	s.mu.RUnlock()

	for _, inst := range instances {
		inst.mu.RLock()
		running := inst.state == StateRunning
		inst.mu.RUnlock()
		if !running {
			continue
		}
		if err := s.signalProcessGroup(inst, sig, "forwarded signal"); err != nil {
			s.logger.Warn("Failed to forward signal to instance",
				"instance_id", inst.id,
				"signal", sig,
				"error", err,
			)
		}
	}
}

// sendShutdownSignal sends the configured shutdown signal to the process
func (s *Supervisor) sendShutdownSignal(instance *Instance) error {
	sig := syscall.SIGTERM
	if s.config.Shutdown != nil && s.config.Shutdown.Signal != "" {
		sig = parseSignal(s.config.Shutdown.Signal)
	}

	return s.signalProcessGroup(instance, sig, "shutdown")
}

// killSignal returns the signal used to force-kill an instance that did not
// stop within its graceful timeout. Honors the per-process shutdown.kill_signal
// (defaulted to SIGKILL by config.SetDefaults); falls back to SIGKILL so a
// process can always be stopped even if the field is somehow empty.
func (s *Supervisor) killSignal() syscall.Signal {
	if s.config.Shutdown != nil && s.config.Shutdown.KillSignal != "" {
		return parseSignal(s.config.Shutdown.KillSignal)
	}
	return syscall.SIGKILL
}

// signalMoot reports whether a signal failed only because the process had
// already exited.
//
// NOT A FAILURE TO STOP SOMETHING THAT HAS STOPPED. The signal races the
// child's own exit: on a loaded machine a process can go between the liveness
// check and the kill, and `os.ErrProcessDone` came back as "failed to send
// signal". `cbox-init stop` and `scale 0` then reported an error for work that
// was already done — and a caller who retries a stop that actually succeeded is
// a caller who restarts something. Caught by a CI runner slow enough to lose
// the race that a laptop wins every time.
func signalMoot(err error) bool {
	return errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH)
}

// signalProcessGroup sends a signal to the instance's process group, falling
// back to the parent process if the process group cannot be addressed.
func (s *Supervisor) signalProcessGroup(instance *Instance, sig syscall.Signal, reason string) error {
	if instance == nil || instance.cmd == nil || instance.cmd.Process == nil {
		return fmt.Errorf("process is not available for signal")
	}

	pgid, err := syscall.Getpgid(instance.pid)
	if err != nil {
		s.logger.Warn("Failed to get process group, sending signal to process only",
			"instance_id", instance.id,
			"reason", reason,
			"error", err,
		)
		if err := instance.cmd.Process.Signal(sig); err != nil && !signalMoot(err) {
			return fmt.Errorf("failed to send signal: %w", err)
		}
		return nil
	}

	if err := syscall.Kill(-pgid, sig); err != nil {
		if signalMoot(err) {
			return nil
		}
		s.logger.Warn("Failed to send signal to process group, falling back to direct signal",
			"instance_id", instance.id,
			"pgid", pgid,
			"reason", reason,
			"error", err,
		)
		if err := instance.cmd.Process.Signal(sig); err != nil && !signalMoot(err) {
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

	// The process is gone: drop its cached resource handle (a dead PID), but keep
	// its metrics history, which the API still serves.
	if s.resourceCollector != nil {
		s.resourceCollector.ReleaseHandle(s.name, instance.id)
	}

	// Record completion in oneshot history
	if execID > 0 && s.oneshotHistory != nil {
		s.oneshotHistory.Complete(execID, exitCode, err)
	}

	// Signal readiness if completed successfully (allows dependents to proceed).
	// On failure, signal that readiness is impossible so dependents fail fast
	// rather than waiting out the whole dependency timeout.
	if exitCode == 0 {
		s.markReady("oneshot completed successfully")
	} else {
		s.markReadinessImpossible(fmt.Sprintf("oneshot %q exited with code %d", s.name, exitCode))
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

	// The replacement has to prove itself again: without re-arming, a dependency
	// that became ready once and then crashed would still look ready, so a
	// dependent started later (by a reload, or a scale-up) would launch against a
	// process that is only just booting.
	s.resetReadiness()

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
		// The replacement never came up: tell anything waiting on readiness (it
		// was re-armed above) and let the manager re-evaluate whether every
		// process is now dead, instead of leaving both hanging.
		s.markReadinessImpossible(fmt.Sprintf("restart of %q failed: %v", s.name, err))
		s.checkAllInstancesDead()
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
		fmt.Sprintf("CBOX_INIT_PROCESS_NAME=%s", s.name),
		fmt.Sprintf("CBOX_INIT_INSTANCE_ID=%s", instanceID),
		fmt.Sprintf("CBOX_INIT_INSTANCE_INDEX=%d", instanceIndex),
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

			s.mu.Lock()
			s.healthKnown = true
			s.healthHealthy = status.Healthy
			s.lastCheckSucceeded = status.LastCheckSucceeded
			s.mu.Unlock()

			if status.LastCheckSucceeded {
				// Signal readiness on first successful health check.
				// Liveness can stay optimistic across transient failures, but
				// readiness must only pass after a real probe success.
				//
				// A oneshot is exempt: its readiness means "completed
				// successfully" (handleOneshotExit), so a probe passing while the
				// task is still running must not release its dependents early.
				if s.config.Type != "oneshot" {
					s.markReady("health check passed")
				}
			} else if !status.Healthy && s.livenessGated() {
				s.logger.Error("Process unhealthy, triggering restart",
					"error", status.Error,
				)

				// Re-arm the monitor: reset its failure history and start a fresh
				// warmup grace window so the restarted instance is judged from a
				// clean slate and gets time to boot, instead of being killed again
				// on the next probe and abandoned once restarts run out.
				if s.healthMonitor != nil {
					s.healthMonitor.Rearm()
				}

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

						if err := s.signalProcessGroup(instance, syscall.SIGKILL, "health check restart"); err != nil {
							s.logger.Warn("Failed to kill unhealthy process group",
								"instance_id", instance.id,
								"error", err,
							)
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
// signalsByName maps the signal names cbox-init accepts in configuration to
// their syscall value. Both the "SIGTERM" and bare "TERM" spellings are
// accepted. Kept to signals that are meaningful to send to a supervised
// workload; parseSignalStrict rejects anything outside this set.
var signalsByName = map[string]syscall.Signal{
	"SIGTERM":  syscall.SIGTERM,
	"SIGINT":   syscall.SIGINT,
	"SIGQUIT":  syscall.SIGQUIT,
	"SIGKILL":  syscall.SIGKILL,
	"SIGHUP":   syscall.SIGHUP,
	"SIGUSR1":  syscall.SIGUSR1,
	"SIGUSR2":  syscall.SIGUSR2,
	"SIGWINCH": syscall.SIGWINCH,
	"SIGCONT":  syscall.SIGCONT,
	"SIGSTOP":  syscall.SIGSTOP,
	"SIGTSTP":  syscall.SIGTSTP,
	"SIGABRT":  syscall.SIGABRT,
}

// parseSignalStrict resolves a configured signal name (case-insensitive, with
// or without the "SIG" prefix) to its syscall value, returning an error for an
// unknown name so configuration can be rejected at load time rather than
// silently coerced.
func parseSignalStrict(name string) (syscall.Signal, error) {
	key := strings.ToUpper(strings.TrimSpace(name))
	if key == "" {
		return 0, fmt.Errorf("empty signal name")
	}
	if !strings.HasPrefix(key, "SIG") {
		key = "SIG" + key
	}
	if sig, ok := signalsByName[key]; ok {
		return sig, nil
	}
	return 0, fmt.Errorf("unknown signal %q", name)
}

// parseSignal resolves a signal name for runtime use. Unknown names fall back to
// SIGTERM — configuration is validated with parseSignalStrict at load, so this
// path should only see valid names; the fallback is defense in depth.
func parseSignal(name string) syscall.Signal {
	if sig, err := parseSignalStrict(name); err == nil {
		return sig
	}
	return syscall.SIGTERM
}

// GetState returns the current supervisor state
func (s *Supervisor) GetState() ProcessState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// HealthSnapshot returns the latest health check state for readiness reporting.
// Health is "healthy", "unhealthy", or "unknown". For processes without a
// health check, running is treated as healthy by callers that need a value.
func (s *Supervisor) HealthSnapshot() (health string, lastCheckSucceeded bool, hasHealthCheck bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.config.HealthCheck == nil {
		return "healthy", true, false
	}
	if !s.healthKnown {
		return "unknown", false, true
	}
	if s.lastCheckSucceeded {
		return "healthy", true, true
	}
	return "unhealthy", false, true
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
		// A checkpointed instance is alive-on-purpose, not dead — see
		// StateCheckpointed. Treating it as dead would trip the manager's
		// all-processes-dead shutdown while the workload is merely snapshotted.
		if inst.state == StateRunning || inst.state == StateCheckpointed {
			allDead = false
		}
		inst.mu.RUnlock()
		if !allDead {
			break
		}
	}
	notifier := s.deathNotifier
	s.mu.RUnlock()

	if allDead && len(s.instances) > 0 {
		// Reflect it in the supervisor's own state: readiness and the API derive
		// health from it, so a supervisor still claiming StateRunning with no
		// live instance reports a dead service as healthy.
		// A oneshot that finished is not a failure — that is its whole job — so
		// only a long-running process moves to Failed here.
		if s.config.Type != "oneshot" {
			s.mu.Lock()
			if s.state == StateRunning || s.state == StateStarting {
				s.state = StateFailed
			}
			s.mu.Unlock()
		}

		// A long-running process with no live instance cannot serve anything, so
		// stop advertising it as ready: otherwise a dependent started later (a
		// reload, a scale-up) would see the previous run's readiness and launch
		// against a process that is gone. A oneshot is exempt — for it, "all
		// instances finished" IS the success condition, and its readiness was set
		// deliberately by handleOneshotExit.
		if s.config.Type != "oneshot" {
			s.markReadinessImpossible("all instances exited and will not restart")
		}
		if notifier != nil {
			s.logger.Debug("All instances dead, notifying manager")
			notifier(s.name)
		}
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

		// Collect process metrics. Use the collector's Collect (not the package
		// CollectProcessMetrics) so the gopsutil handle is reused across ticks and
		// CPUPercent reflects recent usage, not the lifetime average (PERF-3).
		sample, err := s.resourceCollector.Collect(pid, s.name, instanceID)
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

				inst.mu.Lock()
				if inst.state == StateRunning && inst.cmd != nil && inst.cmd.Process != nil {
					metrics.RecordProcessRestart(s.name, "memory_limit")
					if err := s.signalProcessGroup(inst, syscall.SIGKILL, "memory limit restart"); err != nil {
						s.logger.Warn("Failed to kill memory-limited process group",
							"instance_id", instanceID,
							"error", err,
						)
					}
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

// SnapshotTarget returns the instance the agent will checkpoint and the pipe
// its stdout is on. It refuses a scaled process for the same reason the manager
// refuses several processes: one registration describes one tree.
func (s *Supervisor) SnapshotTarget() (int, *snapshot.Stream, error) {
	s.mu.RLock()
	instances := append([]*Instance(nil), s.instances...)
	s.mu.RUnlock()

	if len(instances) != 1 {
		return 0, nil, fmt.Errorf("process %s has %d instances; the warm tier registers one tree", s.name, len(instances))
	}

	inst := instances[0]
	inst.mu.RLock()
	pid, stream, state := inst.pid, inst.stdoutStream, inst.state
	inst.mu.RUnlock()

	if state != StateRunning {
		return 0, nil, fmt.Errorf("process %s is %s, not running", s.name, state)
	}
	if stream == nil {
		// os/exec owns the pipe, so there is no write end left to hand CRIU at
		// restore and the restored process would die of SIGPIPE on its first
		// write. Refusing here is the difference between no warm tier and a
		// warm tier that loses the workload on the first wake.
		return 0, nil, fmt.Errorf("process %s does not have a supervisor-owned stdout pipe", s.name)
	}
	return pid, stream, nil
}
