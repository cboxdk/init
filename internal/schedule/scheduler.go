package schedule

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// Scheduler coordinates all scheduled jobs using cron-style scheduling.
// It wraps the robfig/cron library to provide job management, execution
// tracking, pause/resume capabilities, and manual triggering.
//
// The Scheduler uses standard 5-field cron expressions (minute, hour, day-of-month,
// month, day-of-week) without second-level precision. Timezone support is
// available for each job.
//
// Key features:
//   - Job lifecycle management (add, remove, pause, resume)
//   - Manual job triggering (async or sync with exit code)
//   - Execution history tracking per job
//   - Concurrent execution limits and timeouts
//   - Thread-safe operations
//
// Scheduler is safe for concurrent use from multiple goroutines.
type Scheduler struct {
	cron        *cron.Cron
	jobs        map[string]*ScheduledJob
	executor    JobExecutor
	historySize int
	logger      *slog.Logger
	mu          sync.RWMutex
	started     bool
	runCtx      context.Context // cancelled on Stop so in-flight cron jobs are cancelled
	runCancel   context.CancelFunc
}

// NewScheduler creates a new Scheduler
func NewScheduler(executor JobExecutor, historySize int, logger *slog.Logger) *Scheduler {
	// Create cron with second-level precision disabled (standard cron format)
	c := cron.New(cron.WithParser(cron.NewParser(
		cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
	)))

	return &Scheduler{
		cron:        c,
		jobs:        make(map[string]*ScheduledJob),
		executor:    executor,
		historySize: historySize,
		logger:      logger.With("component", "scheduler"),
	}
}

// AddJob adds a new scheduled job
func (s *Scheduler) AddJob(name, scheduleExpr, timezone string) error {
	return s.AddJobWithOptions(name, scheduleExpr, timezone, JobOptions{})
}

// AddJobWithOptions adds a new scheduled job with additional options
func (s *Scheduler) AddJobWithOptions(name, scheduleExpr, timezone string, opts JobOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.jobs[name]; exists {
		return fmt.Errorf("job %q already exists", name)
	}

	job, err := NewScheduledJobWithOptions(name, scheduleExpr, timezone, s.historySize, s.executor, s.logger, opts)
	if err != nil {
		return fmt.Errorf("failed to create job: %w", err)
	}

	// Add to cron scheduler
	entryID, err := s.cron.AddJob(scheduleExpr, job)
	if err != nil {
		return fmt.Errorf("failed to add job to cron: %w", err)
	}

	job.SetCronID(entryID)
	// If the scheduler is already running, hand the new job the live run context
	// so it is cancellable on stop like the rest.
	if s.started && s.runCtx != nil {
		job.SetBaseContext(s.runCtx)
	}
	s.jobs[name] = job

	// Update next run time from cron
	if entry := s.cron.Entry(entryID); entry.ID != 0 {
		job.UpdateNextRun(entry.Next)
	}

	logFields := []any{
		"job", name,
		"schedule", scheduleExpr,
		"timezone", timezone,
	}
	if opts.Timeout > 0 {
		logFields = append(logFields, "timeout", opts.Timeout)
	}
	if opts.MaxConcurrent > 0 {
		logFields = append(logFields, "max_concurrent", opts.MaxConcurrent)
	}
	s.logger.Info("job added", logFields...)

	return nil
}

// RemoveJob removes a scheduled job
func (s *Scheduler) RemoveJob(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, exists := s.jobs[name]
	if !exists {
		return fmt.Errorf("job %q not found", name)
	}

	// Remove from cron
	s.cron.Remove(job.GetCronID())

	// Remove from our map
	delete(s.jobs, name)

	s.logger.Info("job removed", "job", name)
	return nil
}

// GetJob returns a job by name
func (s *Scheduler) GetJob(name string) (*ScheduledJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, exists := s.jobs[name]
	return job, exists
}

// GetAllJobs returns all jobs
func (s *Scheduler) GetAllJobs() map[string]*ScheduledJob {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]*ScheduledJob, len(s.jobs))
	for k, v := range s.jobs {
		result[k] = v
	}
	return result
}

// Start starts the scheduler
func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return
	}

	// A scheduler-lifetime context that cron-triggered jobs derive from, so Stop
	// can cancel any that are still running.
	s.runCtx, s.runCancel = context.WithCancel(context.Background())
	for _, job := range s.jobs {
		job.SetBaseContext(s.runCtx)
	}

	s.cron.Start()
	s.started = true

	// Update next run times for all jobs
	for _, job := range s.jobs {
		if entry := s.cron.Entry(job.GetCronID()); entry.ID != 0 {
			job.UpdateNextRun(entry.Next)
		}
	}

	s.logger.Info("scheduler started", "job_count", len(s.jobs))
}

// Stop stops the scheduler gracefully
func (s *Scheduler) Stop() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}

	s.started = false
	s.logger.Info("scheduler stopping")

	// Cancel the run context first so any job still executing is cancelled (its
	// command killed) rather than blocking cron.Stop()'s wait indefinitely.
	if s.runCancel != nil {
		s.runCancel()
	}

	return s.cron.Stop()
}

// IsStarted returns true if the scheduler is running
func (s *Scheduler) IsStarted() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.started
}

// PauseJob pauses a specific job
func (s *Scheduler) PauseJob(name string) error {
	s.mu.RLock()
	job, exists := s.jobs[name]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("job %q not found", name)
	}

	return job.Pause()
}

// ResumeJob resumes a paused job
func (s *Scheduler) ResumeJob(name string) error {
	s.mu.RLock()
	job, exists := s.jobs[name]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("job %q not found", name)
	}

	return job.Resume()
}

// TriggerJob manually triggers a job
func (s *Scheduler) TriggerJob(ctx context.Context, name string) error {
	s.mu.RLock()
	job, exists := s.jobs[name]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("job %q not found", name)
	}

	return job.Trigger(ctx)
}

// TriggerJobSync triggers a job and waits for completion
func (s *Scheduler) TriggerJobSync(ctx context.Context, name string) (int, error) {
	s.mu.RLock()
	job, exists := s.jobs[name]
	s.mu.RUnlock()

	if !exists {
		return -1, fmt.Errorf("job %q not found", name)
	}

	return job.TriggerSync(ctx)
}

// GetJobStatus returns the status of a specific job
func (s *Scheduler) GetJobStatus(name string) (JobStatus, error) {
	s.mu.RLock()
	job, exists := s.jobs[name]
	if exists {
		// Update NextRun time from cron before returning status
		if entry := s.cron.Entry(job.GetCronID()); entry.ID != 0 {
			job.UpdateNextRun(entry.Next)
		}
	}
	s.mu.RUnlock()

	if !exists {
		return JobStatus{}, fmt.Errorf("job %q not found", name)
	}

	return job.Status(), nil
}

// GetAllJobStatuses returns the status of all jobs
func (s *Scheduler) GetAllJobStatuses() map[string]JobStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Update NextRun times from cron before returning statuses
	for _, job := range s.jobs {
		if entry := s.cron.Entry(job.GetCronID()); entry.ID != 0 {
			job.UpdateNextRun(entry.Next)
		}
	}

	result := make(map[string]JobStatus, len(s.jobs))
	for name, job := range s.jobs {
		result[name] = job.Status()
	}
	return result
}

// GetJobHistory returns the execution history of a job
func (s *Scheduler) GetJobHistory(name string, limit int) ([]ExecutionEntry, error) {
	s.mu.RLock()
	job, exists := s.jobs[name]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("job %q not found", name)
	}

	if limit <= 0 {
		return job.History.GetAll(), nil
	}
	return job.History.GetRecent(limit), nil
}

// UpdateNextRunTimes updates the next run times for all jobs from cron
func (s *Scheduler) UpdateNextRunTimes() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, job := range s.jobs {
		if entry := s.cron.Entry(job.GetCronID()); entry.ID != 0 {
			job.UpdateNextRun(entry.Next)
		}
	}
}

// Stats returns aggregate scheduler statistics
type SchedulerStats struct {
	TotalJobs     int       `json:"total_jobs"`
	IdleJobs      int       `json:"idle_jobs"`
	ExecutingJobs int       `json:"executing_jobs"`
	PausedJobs    int       `json:"paused_jobs"`
	Started       bool      `json:"started"`
	StartTime     time.Time `json:"start_time,omitempty"`
}

// Stats returns scheduler statistics
func (s *Scheduler) Stats() SchedulerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := SchedulerStats{
		TotalJobs: len(s.jobs),
		Started:   s.started,
	}

	for _, job := range s.jobs {
		switch job.GetState() {
		case JobStateIdle:
			stats.IdleJobs++
		case JobStateExecuting:
			stats.ExecutingJobs++
		case JobStatePaused:
			stats.PausedJobs++
		}
	}

	return stats
}
