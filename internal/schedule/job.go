package schedule

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// JobState represents the current execution state of a scheduled job.
// The state machine transitions are:
//
//	Idle → Executing (on trigger/schedule)
//	Executing → Idle (on completion)
//	Idle → Paused (on pause request)
//	Paused → Idle (on resume request)
//
// Executing → Paused is not allowed; jobs must complete before pausing.
type JobState int

const (
	// JobStateIdle - job is waiting for next scheduled run
	JobStateIdle JobState = iota
	// JobStateExecuting - job is currently running
	JobStateExecuting
	// JobStatePaused - job is paused (skips triggers)
	JobStatePaused
)

// String returns the string representation of JobState
func (s JobState) String() string {
	switch s {
	case JobStateIdle:
		return "idle"
	case JobStateExecuting:
		return "executing"
	case JobStatePaused:
		return "paused"
	default:
		return "unknown"
	}
}

// JobExecutor defines the interface for executing scheduled jobs.
// Implementations handle the actual process spawning and lifecycle management.
//
// The Execute method is called by the scheduler when a job triggers. It should:
//   - Spawn the configured process with appropriate environment
//   - Wait for completion or context cancellation
//   - Return the exit code (0 for success) and any execution error
//
// The context carries cancellation signals and execution timeouts. Implementations
// must respect context cancellation for graceful job termination.
type JobExecutor interface {
	// Execute runs the job and blocks until completion or cancellation.
	// Returns the process exit code and any execution error.
	Execute(ctx context.Context, processName string) (int, error)
}

// ScheduledJob represents a single scheduled job with complete state management.
// It wraps a cron schedule with execution tracking, pause/resume capabilities,
// and support for both scheduled and manual triggering.
//
// Key features:
//   - Cron-based scheduling with standard 5-field expressions
//   - Execution history with configurable retention
//   - Overlap prevention (jobs don't run concurrently by default)
//   - Configurable timeouts and concurrency limits
//   - Pause/resume without removing from scheduler
//   - Manual triggering (async or sync with exit code)
//
// ScheduledJob implements cron.Job interface for integration with robfig/cron.
// All public methods are thread-safe for concurrent access.
type ScheduledJob struct {
	Name          string
	Schedule      string
	Timezone      string
	State         JobState
	History       *ExecutionHistory
	LastRun       time.Time
	NextRun       time.Time
	CurrentExecID int64 // ID of currently running execution

	// Execution control
	Timeout time.Duration // Execution timeout (0 = no timeout)
	// MaxConcurrent is retained for forward compatibility but is NOT yet honored:
	// a job that is still running when its next tick fires is always skipped
	// (see executeSync's executionMu + JobStateExecuting guard). Values > 1 do
	// not enable parallel runs today.
	MaxConcurrent int

	// Internal
	cronID      cron.EntryID
	schedule    cron.Schedule
	executor    JobExecutor
	logger      *slog.Logger
	mu          sync.Mutex
	executionMu sync.Mutex      // Separate mutex for execution to allow state reads during execution
	baseCtx     context.Context // Scheduler-lifetime context; cancelled on scheduler stop (nil => Background)
}

// SetBaseContext sets the context that cron-triggered executions derive from.
// The scheduler cancels this context when it stops, so a job that is mid-run at
// shutdown is cancelled (its command killed) instead of blocking shutdown.
func (j *ScheduledJob) SetBaseContext(ctx context.Context) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.baseCtx = ctx
}

func (j *ScheduledJob) baseContext() context.Context {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.baseCtx != nil {
		return j.baseCtx
	}
	return context.Background()
}

// JobOptions contains optional configuration for a scheduled job.
// All fields have sensible zero-value defaults for basic operation.
type JobOptions struct {
	// Timeout specifies the maximum execution duration. When exceeded, the
	// job's context is cancelled. Zero means no timeout (run until completion).
	Timeout time.Duration

	// MaxConcurrent is accepted but not yet honored: overlapping runs are always
	// skipped, so a value > 1 does not enable parallel executions today. Retained
	// so existing configs keep loading and for a future parallel mode.
	MaxConcurrent int
}

// NewScheduledJob creates a new ScheduledJob with default options.
// This is a convenience wrapper around NewScheduledJobWithOptions.
//
// Parameters:
//   - name: Unique identifier for the job (used in logs and API)
//   - scheduleExpr: Cron expression (5 fields: minute hour day-of-month month day-of-week)
//   - timezone: IANA timezone name (e.g., "America/New_York") or empty for local
//   - historySize: Maximum execution history entries to retain
//   - executor: Implementation that runs the actual job process
//   - logger: Structured logger for job events
//
// Returns an error if the schedule expression is invalid.
func NewScheduledJob(name, scheduleExpr, timezone string, historySize int, executor JobExecutor, logger *slog.Logger) (*ScheduledJob, error) {
	return NewScheduledJobWithOptions(name, scheduleExpr, timezone, historySize, executor, logger, JobOptions{})
}

// NewScheduledJobWithOptions creates a new ScheduledJob with additional options
func NewScheduledJobWithOptions(name, scheduleExpr, timezone string, historySize int, executor JobExecutor, logger *slog.Logger, opts JobOptions) (*ScheduledJob, error) {
	// Parse the schedule expression in the configured timezone.
	//
	// Without this the spec is parsed with the parser's default location —
	// time.Local — so a job configured with the documented default
	// schedule_timezone: "UTC" actually fired at the container's local time. An
	// image that sets TZ ran every nightly job at the wrong hour. CRON_TZ is the
	// parser's own way to bind a spec to a location.
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	spec := scheduleExpr
	if tz := strings.TrimSpace(timezone); tz != "" && !strings.EqualFold(tz, "Local") {
		if _, err := time.LoadLocation(tz); err != nil {
			return nil, fmt.Errorf("invalid schedule_timezone %q: %w", tz, err)
		}
		spec = "CRON_TZ=" + tz + " " + scheduleExpr
	}
	schedule, err := parser.Parse(spec)
	if err != nil {
		return nil, fmt.Errorf("invalid schedule expression: %w", err)
	}

	return &ScheduledJob{
		Name:          name,
		Schedule:      scheduleExpr,
		Timezone:      timezone,
		State:         JobStateIdle,
		History:       NewExecutionHistory(historySize),
		Timeout:       opts.Timeout,
		MaxConcurrent: opts.MaxConcurrent,
		schedule:      schedule,
		executor:      executor,
		logger:        logger.With("job", name),
	}, nil
}

// GetState returns the current job state (thread-safe)
func (j *ScheduledJob) GetState() JobState {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.State
}

// SetCronID sets the cron entry ID for this job
func (j *ScheduledJob) SetCronID(id cron.EntryID) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.cronID = id
}

// GetCronID returns the cron entry ID
func (j *ScheduledJob) GetCronID() cron.EntryID {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.cronID
}

// UpdateNextRun updates the next run time
func (j *ScheduledJob) UpdateNextRun(t time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.NextRun = t
}

// GetNextRun returns the next scheduled run time
func (j *ScheduledJob) GetNextRun() time.Time {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.NextRun
}

// GetLastRun returns the last execution time
func (j *ScheduledJob) GetLastRun() time.Time {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.LastRun
}

// Pause pauses the job (it will skip scheduled triggers)
func (j *ScheduledJob) Pause() error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.State == JobStateExecuting {
		return fmt.Errorf("cannot pause job while executing")
	}
	if j.State == JobStatePaused {
		return nil // Already paused
	}

	j.State = JobStatePaused
	j.logger.Info("job paused")
	return nil
}

// Resume resumes a paused job
func (j *ScheduledJob) Resume() error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.State != JobStatePaused {
		return fmt.Errorf("job is not paused (current state: %s)", j.State)
	}

	j.State = JobStateIdle
	j.logger.Info("job resumed")
	return nil
}

// IsPaused returns true if the job is paused
func (j *ScheduledJob) IsPaused() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.State == JobStatePaused
}

// IsExecuting returns true if the job is currently executing
func (j *ScheduledJob) IsExecuting() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.State == JobStateExecuting
}

// CanExecute returns true if the job can be executed now
func (j *ScheduledJob) CanExecute() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.State == JobStateIdle
}

// Run is called by the cron scheduler when the schedule triggers
// It implements the cron.Job interface
func (j *ScheduledJob) Run() {
	// Derive from the scheduler's lifetime context so a job still running when
	// the scheduler stops is cancelled, rather than running under a Background
	// context that shutdown cannot reach.
	j.execute(j.baseContext(), "schedule")
}

// Trigger manually triggers execution (returns error if already running or paused)
func (j *ScheduledJob) Trigger(ctx context.Context) error {
	state := j.GetState()
	if state == JobStatePaused {
		return fmt.Errorf("cannot trigger paused job")
	}
	if state == JobStateExecuting {
		return fmt.Errorf("job is already executing")
	}

	// The async trigger outlives the request that started it: net/http cancels
	// r.Context() as soon as the handler returns its 202, which would cancel
	// (SIGKILL) the job mid-run. Detach from the caller's cancellation while
	// keeping its values (tracing, etc.) so the job runs to completion.
	go j.execute(context.WithoutCancel(ctx), "manual")
	return nil
}

// TriggerSync triggers execution and waits for completion
func (j *ScheduledJob) TriggerSync(ctx context.Context) (int, error) {
	state := j.GetState()
	if state == JobStatePaused {
		return -1, fmt.Errorf("cannot trigger paused job")
	}
	if state == JobStateExecuting {
		return -1, fmt.Errorf("job is already executing")
	}

	return j.executeSync(ctx, "manual")
}

// execute runs the job (internal)
// Error handling is done within executeSync (logging, state updates, callbacks)
func (j *ScheduledJob) execute(ctx context.Context, triggered string) {
	_, _ = j.executeSync(ctx, triggered)
}

// executeSync runs the job synchronously and returns the result
func (j *ScheduledJob) executeSync(ctx context.Context, triggered string) (int, error) {
	// Use execution mutex to prevent concurrent executions
	j.executionMu.Lock()
	defer j.executionMu.Unlock()

	// Check state and transition to executing
	j.mu.Lock()
	if j.State == JobStatePaused {
		j.mu.Unlock()
		j.logger.Debug("skipping execution - job paused")
		return -1, fmt.Errorf("job is paused")
	}
	if j.State == JobStateExecuting {
		j.mu.Unlock()
		j.logger.Debug("skipping execution - already running (overlap policy: skip)")
		return -1, fmt.Errorf("job is already executing")
	}

	j.State = JobStateExecuting
	j.LastRun = time.Now()
	execID := j.History.StartExecution(triggered)
	j.CurrentExecID = execID
	j.mu.Unlock()

	// Restore the state whatever happens below, including a panic.
	//
	// The state was reset only on the normal path, so a panicking executor left
	// the job in JobStateExecuting for good — and the overlap check above then
	// refused every subsequent run with "already executing". A recovered panic
	// (the cron chain recovers, so the process survives) silently retired the
	// job. The flag says whether the normal path already ran, so the happy case
	// is untouched.
	completed := false
	defer func() {
		if completed {
			return
		}

		r := recover()

		j.mu.Lock()
		j.State = JobStateIdle
		j.CurrentExecID = 0
		j.mu.Unlock()

		if r != nil {
			j.logger.Error("job execution panicked",
				"execution_id", execID,
				"panic", r,
				"stack", string(debug.Stack()),
			)
			j.History.EndExecution(execID, -1, false, fmt.Sprintf("panic: %v", r))

			panic(r) // let the cron chain's recoverer log and contain it
		}

		// Not a panic: an early return that skipped the normal completion path.
		j.History.EndExecution(execID, -1, false, "execution ended without recording a result")
	}()

	j.logger.Info("job execution started",
		"execution_id", execID,
		"triggered", triggered,
	)

	// Apply timeout if configured
	execCtx := ctx
	var cancel context.CancelFunc
	if j.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, j.Timeout)
		defer cancel()
		j.logger.Debug("job execution with timeout", "timeout", j.Timeout)
	}

	// Execute the job
	exitCode, execErr := j.executor.Execute(execCtx, j.Name)

	// Transition back to idle and record result
	j.mu.Lock()
	j.State = JobStateIdle
	j.CurrentExecID = 0
	j.mu.Unlock()

	completed = true

	success := execErr == nil && exitCode == 0
	errMsg := ""
	if execErr != nil {
		errMsg = execErr.Error()
	}
	j.History.EndExecution(execID, exitCode, success, errMsg)

	j.logger.Info("job execution completed",
		"execution_id", execID,
		"exit_code", exitCode,
		"success", success,
		"duration", j.History.GetRecent(1)[0].Duration(),
	)

	return exitCode, execErr
}

// JobStatus provides a snapshot of a scheduled job's current state.
// This struct is returned by the Status() method and is safe to serialize to JSON.
type JobStatus struct {
	Name          string       `json:"name"`
	Schedule      string       `json:"schedule"`
	Timezone      string       `json:"timezone"`
	State         string       `json:"state"`
	LastRun       time.Time    `json:"last_run"`
	NextRun       time.Time    `json:"next_run"`
	CurrentExecID int64        `json:"current_execution_id,omitempty"`
	Stats         HistoryStats `json:"stats"`
}

// Status returns the current job status
func (j *ScheduledJob) Status() JobStatus {
	j.mu.Lock()
	defer j.mu.Unlock()

	return JobStatus{
		Name:          j.Name,
		Schedule:      j.Schedule,
		Timezone:      j.Timezone,
		State:         j.State.String(),
		LastRun:       j.LastRun,
		NextRun:       j.NextRun,
		CurrentExecID: j.CurrentExecID,
		Stats:         j.History.Stats(),
	}
}
