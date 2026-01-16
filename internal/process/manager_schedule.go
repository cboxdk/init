package process

import (
	"context"

	"github.com/cboxdk/init/internal/schedule"
)

// GetScheduler returns the scheduler for scheduled processes.
func (m *Manager) GetScheduler() *schedule.Scheduler {
	return m.scheduler
}

// GetScheduleStatus returns the status of a scheduled job.
func (m *Manager) GetScheduleStatus(name string) (schedule.JobStatus, error) {
	return m.scheduler.GetJobStatus(name)
}

// GetAllScheduleStatuses returns the status of all scheduled jobs.
func (m *Manager) GetAllScheduleStatuses() map[string]schedule.JobStatus {
	return m.scheduler.GetAllJobStatuses()
}

// GetScheduleHistory returns the execution history of a scheduled job.
func (m *Manager) GetScheduleHistory(name string, limit int) ([]schedule.ExecutionEntry, error) {
	return m.scheduler.GetJobHistory(name, limit)
}

// PauseSchedule pauses a scheduled job.
func (m *Manager) PauseSchedule(name string) error {
	return m.scheduler.PauseJob(name)
}

// ResumeSchedule resumes a paused scheduled job.
func (m *Manager) ResumeSchedule(name string) error {
	return m.scheduler.ResumeJob(name)
}

// TriggerSchedule manually triggers a scheduled job (async).
func (m *Manager) TriggerSchedule(ctx context.Context, name string) error {
	return m.scheduler.TriggerJob(ctx, name)
}

// TriggerScheduleSync manually triggers a scheduled job and waits for completion.
func (m *Manager) TriggerScheduleSync(ctx context.Context, name string) (int, error) {
	return m.scheduler.TriggerJobSync(ctx, name)
}
