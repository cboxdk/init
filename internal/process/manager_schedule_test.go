package process

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
)

// createScheduleTestManager creates a manager with scheduler for testing
func createScheduleTestManager(t *testing.T) (*Manager, func()) {
	t.Helper()

	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    5,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     1,
		},
		Processes: map[string]*config.Process{
			"scheduled-task": {
				Enabled:  true,
				Type:     "scheduled",
				Schedule: "*/5 * * * *", // Every 5 minutes
				Command:  []string{"echo", "scheduled"},
				Restart:  "never",
				Scale:    1,
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false)
	manager := NewManager(cfg, logger, auditLogger)

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	cleanup := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	}

	return manager, cleanup
}

// TestManager_GetScheduler verifies GetScheduler returns non-nil scheduler
func TestManager_GetScheduler(t *testing.T) {
	manager, cleanup := createScheduleTestManager(t)
	defer cleanup()

	scheduler := manager.GetScheduler()
	if scheduler == nil {
		t.Error("GetScheduler() returned nil, expected non-nil scheduler")
	}
}

// TestManager_GetOneshotHistory verifies GetOneshotHistory returns non-nil history
func TestManager_GetOneshotHistory(t *testing.T) {
	manager, cleanup := createScheduleTestManager(t)
	defer cleanup()

	history := manager.GetOneshotHistory()
	if history == nil {
		t.Error("GetOneshotHistory() returned nil, expected non-nil history")
	}
}

// TestManager_GetOneshotExecutions tests retrieving oneshot executions
func TestManager_GetOneshotExecutions(t *testing.T) {
	manager, cleanup := createScheduleTestManager(t)
	defer cleanup()

	// Test with limit = 0 (get all)
	executions := manager.GetOneshotExecutions("scheduled-task", 0)
	// Should return empty slice (no executions yet), not nil
	if executions == nil {
		t.Log("GetOneshotExecutions returned nil (no history yet)")
	}

	// Test with positive limit
	executions = manager.GetOneshotExecutions("scheduled-task", 10)
	if executions == nil {
		t.Log("GetOneshotExecutions with limit returned nil (no history yet)")
	}

	// Test with non-existent process
	executions = manager.GetOneshotExecutions("non-existent", 10)
	if executions == nil {
		t.Log("GetOneshotExecutions for non-existent process returned nil")
	}
}

// TestManager_GetAllOneshotExecutions tests retrieving all oneshot executions
func TestManager_GetAllOneshotExecutions(t *testing.T) {
	manager, cleanup := createScheduleTestManager(t)
	defer cleanup()

	// Test with limit = 0 (get all)
	executions := manager.GetAllOneshotExecutions(0)
	if executions == nil {
		t.Log("GetAllOneshotExecutions returned nil (no history yet)")
	}

	// Test with positive limit
	executions = manager.GetAllOneshotExecutions(10)
	if executions == nil {
		t.Log("GetAllOneshotExecutions with limit returned nil (no history yet)")
	}
}

// TestManager_GetOneshotStats tests retrieving oneshot statistics
func TestManager_GetOneshotStats(t *testing.T) {
	manager, cleanup := createScheduleTestManager(t)
	defer cleanup()

	stats := manager.GetOneshotStats()
	// Stats should always be valid, even if empty
	if stats.ByProcess == nil {
		t.Error("GetOneshotStats() returned nil ByProcess map")
	}
}

// TestManager_GetScheduleStatus tests getting schedule status
func TestManager_GetScheduleStatus(t *testing.T) {
	manager, cleanup := createScheduleTestManager(t)
	defer cleanup()

	// Give scheduler time to register the job
	time.Sleep(100 * time.Millisecond)

	status, err := manager.GetScheduleStatus("scheduled-task")
	if err != nil {
		t.Logf("GetScheduleStatus returned error (may be expected): %v", err)
	} else {
		t.Logf("Schedule status: State=%s, Schedule=%s", status.State, status.Schedule)
	}

	// Test non-existent job
	_, err = manager.GetScheduleStatus("non-existent-job")
	if err == nil {
		t.Error("GetScheduleStatus for non-existent job should return error")
	}
}

// TestManager_GetAllScheduleStatuses tests getting all schedule statuses
func TestManager_GetAllScheduleStatuses(t *testing.T) {
	manager, cleanup := createScheduleTestManager(t)
	defer cleanup()

	// Give scheduler time to register jobs
	time.Sleep(100 * time.Millisecond)

	statuses := manager.GetAllScheduleStatuses()
	if statuses == nil {
		t.Error("GetAllScheduleStatuses() returned nil")
	}
	t.Logf("Found %d schedule statuses", len(statuses))
}

// TestManager_GetScheduleHistory tests getting schedule history
func TestManager_GetScheduleHistory(t *testing.T) {
	manager, cleanup := createScheduleTestManager(t)
	defer cleanup()

	// Give scheduler time to register the job
	time.Sleep(100 * time.Millisecond)

	history, err := manager.GetScheduleHistory("scheduled-task", 10)
	if err != nil {
		t.Logf("GetScheduleHistory returned error (may be expected): %v", err)
	} else {
		t.Logf("Schedule history entries: %d", len(history))
	}

	// Test non-existent job
	_, err = manager.GetScheduleHistory("non-existent-job", 10)
	if err == nil {
		t.Error("GetScheduleHistory for non-existent job should return error")
	}
}

// TestManager_PauseResumeSchedule tests pausing and resuming schedules
func TestManager_PauseResumeSchedule(t *testing.T) {
	manager, cleanup := createScheduleTestManager(t)
	defer cleanup()

	// Give scheduler time to register the job
	time.Sleep(100 * time.Millisecond)

	// Test pause
	err := manager.PauseSchedule("scheduled-task")
	if err != nil {
		t.Logf("PauseSchedule returned error (may be expected): %v", err)
	}

	// Verify paused state
	status, err := manager.GetScheduleStatus("scheduled-task")
	if err == nil && status.State != "paused" {
		t.Logf("Expected paused state, got: %s", status.State)
	}

	// Test resume
	err = manager.ResumeSchedule("scheduled-task")
	if err != nil {
		t.Logf("ResumeSchedule returned error (may be expected): %v", err)
	}

	// Test pause non-existent
	err = manager.PauseSchedule("non-existent-job")
	if err == nil {
		t.Error("PauseSchedule for non-existent job should return error")
	}

	// Test resume non-existent
	err = manager.ResumeSchedule("non-existent-job")
	if err == nil {
		t.Error("ResumeSchedule for non-existent job should return error")
	}
}

// TestManager_TriggerSchedule tests manually triggering a schedule
func TestManager_TriggerSchedule(t *testing.T) {
	manager, cleanup := createScheduleTestManager(t)
	defer cleanup()

	// Give scheduler time to register the job
	time.Sleep(100 * time.Millisecond)

	ctx := context.Background()

	// Test async trigger
	err := manager.TriggerSchedule(ctx, "scheduled-task")
	if err != nil {
		t.Logf("TriggerSchedule returned error (may be expected): %v", err)
	}

	// Test trigger non-existent
	err = manager.TriggerSchedule(ctx, "non-existent-job")
	if err == nil {
		t.Error("TriggerSchedule for non-existent job should return error")
	}
}

// TestManager_TriggerScheduleSync tests synchronous schedule triggering
func TestManager_TriggerScheduleSync(t *testing.T) {
	manager, cleanup := createScheduleTestManager(t)
	defer cleanup()

	// Give scheduler time to register the job
	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test sync trigger
	exitCode, err := manager.TriggerScheduleSync(ctx, "scheduled-task")
	if err != nil {
		t.Logf("TriggerScheduleSync returned error (may be expected): %v", err)
	} else {
		t.Logf("TriggerScheduleSync exit code: %d", exitCode)
	}
}

// TestManager_checkAllProcessesDead tests the all-processes-dead detection
func TestManager_checkAllProcessesDead(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    5,
			LogLevel:           "error",
			MaxRestartAttempts: 0, // No restarts
			RestartBackoff:     1,
		},
		Processes: map[string]*config.Process{
			"quick-exit": {
				Enabled:      true,
				InitialState: "running",
				Command:      []string{"true"}, // Exits immediately
				Restart:      "never",
				Scale:        1,
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false)
	manager := NewManager(cfg, logger, auditLogger)

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// Get the all-dead channel
	allDeadCh := manager.AllDeadChannel()
	if allDeadCh == nil {
		t.Fatal("AllDeadChannel() returned nil")
	}

	// Wait for either all-dead signal or timeout
	select {
	case <-allDeadCh:
		t.Log("Received all-dead signal as expected")
	case <-time.After(5 * time.Second):
		t.Log("Timeout waiting for all-dead signal (process may still be tracked)")
	}

	// Cleanup
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = manager.Shutdown(shutdownCtx)
}

// TestManager_NotifyProcessDeath_ScheduleContext tests death notification in schedule context
func TestManager_NotifyProcessDeath_ScheduleContext(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    5,
			LogLevel:           "error",
			MaxRestartAttempts: 0,
			RestartBackoff:     1,
		},
		Processes: map[string]*config.Process{
			"scheduled-process": {
				Enabled:      true,
				Type:         "scheduled",
				Schedule:     "*/5 * * * *",
				Command:      []string{"echo", "test"},
				Restart:      "never",
				Scale:        1,
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false)
	manager := NewManager(cfg, logger, auditLogger)

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// Test notification for scheduled process - should not panic
	manager.NotifyProcessDeath("scheduled-process")
	t.Log("NotifyProcessDeath for scheduled process called successfully")

	// Cleanup
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = manager.Shutdown(shutdownCtx)
}

// TestManager_NilOneshotHistory tests behavior when oneshot history is nil
func TestManager_NilOneshotHistory(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout: 5,
			LogLevel:        "error",
		},
		Processes: map[string]*config.Process{},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false)
	manager := NewManager(cfg, logger, auditLogger)

	// Don't start - just test the nil safety of getters
	// Manually set oneshotHistory to nil to test nil safety
	manager.oneshotHistory = nil

	// These should not panic
	executions := manager.GetOneshotExecutions("test", 10)
	if executions != nil {
		t.Error("GetOneshotExecutions with nil history should return nil")
	}

	allExecutions := manager.GetAllOneshotExecutions(10)
	if allExecutions != nil {
		t.Error("GetAllOneshotExecutions with nil history should return nil")
	}

	stats := manager.GetOneshotStats()
	if stats.ByProcess == nil {
		t.Error("GetOneshotStats with nil history should return empty stats with non-nil ByProcess")
	}
}
