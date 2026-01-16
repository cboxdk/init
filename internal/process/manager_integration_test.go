package process

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/testutil"
)

// TestManager_ProcessDeathNotification tests that process death notification triggers all-dead check
func TestManager_ProcessDeathNotification(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    10,
			LogLevel:           "error",
			MaxRestartAttempts: 0,
			RestartBackoff:     1,
		},
		Processes: map[string]*config.Process{
			"short-lived": {
				Enabled:      true,
				InitialState: "running",
				Command:      []string{"sh", "-c", "exit 0"},
				Restart:      "never",
				Scale:        1,
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false)
	manager := NewManager(cfg, logger, auditLogger)

	ctx := context.Background()

	// Start health monitoring before processes
	monitorCtx, monitorCancel := context.WithCancel(ctx)
	defer monitorCancel()
	manager.MonitorProcessHealth(monitorCtx)

	// Start processes
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start processes: %v", err)
	}

	// Wait for all-dead signal with timeout
	select {
	case <-manager.AllDeadChannel():
		t.Log("Received all-dead signal as expected")
	case <-time.After(5 * time.Second):
		t.Error("Expected all-dead signal but didn't receive it within timeout")
	}

	// Cleanup
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = manager.Shutdown(shutdownCtx)
}

// TestManager_NotifyProcessDeath_DirectCall tests direct NotifyProcessDeath calls
func TestManager_NotifyProcessDeath_DirectCall(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    10,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"persistent": {
				Enabled:      true,
				InitialState: "running",
				Command:      []string{"sleep", "300"},
				Restart:      "never",
				Scale:        1,
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false)
	manager := NewManager(cfg, logger, auditLogger)

	ctx := context.Background()

	// Start processes
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start processes: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	}()

	// Wait for process to start
	testutil.Eventually(t, func() bool {
		processes := manager.ListProcesses()
		return len(processes) > 0
	}, "process to start")

	// Direct call should not block (tests channel-full fallback)
	for i := 0; i < 20; i++ {
		manager.NotifyProcessDeath("persistent")
	}

	// Verify no deadlock occurred and process is still listed
	processes := manager.ListProcesses()
	if len(processes) == 0 {
		t.Error("Expected at least one process to be listed")
	}
}

// TestManager_CheckAllProcessesDead_WithRunningProcesses tests check when processes are running
func TestManager_CheckAllProcessesDead_WithRunningProcesses(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    10,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"long-running": {
				Enabled:      true,
				InitialState: "running",
				Command:      []string{"sleep", "300"},
				Restart:      "never",
				Scale:        2,
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false)
	manager := NewManager(cfg, logger, auditLogger)

	ctx := context.Background()

	// Start health monitoring
	monitorCtx, monitorCancel := context.WithCancel(ctx)
	defer monitorCancel()
	manager.MonitorProcessHealth(monitorCtx)

	// Start processes
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start processes: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	}()

	// Wait for process to start
	testutil.Eventually(t, func() bool {
		processes := manager.ListProcesses()
		if len(processes) == 0 {
			return false
		}
		for _, p := range processes {
			if p.State == "running" {
				return true
			}
		}
		return false
	}, "process to be running")

	// With running processes, all-dead should NOT be signaled
	select {
	case <-manager.AllDeadChannel():
		t.Error("Should not receive all-dead signal when processes are running")
	case <-time.After(500 * time.Millisecond):
		t.Log("Correctly did not receive all-dead signal while processes running")
	}
}

// TestManager_MonitorProcessHealth_ContextCancellation tests monitor stops on context cancel
func TestManager_MonitorProcessHealth_ContextCancellation(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    10,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"test": {
				Enabled:      true,
				InitialState: "running",
				Command:      []string{"sleep", "10"},
				Restart:      "never",
				Scale:        1,
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false)
	manager := NewManager(cfg, logger, auditLogger)

	// Create cancellable context for monitor
	monitorCtx, monitorCancel := context.WithCancel(context.Background())

	// Start monitoring
	manager.MonitorProcessHealth(monitorCtx)

	// Start processes
	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start processes: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	}()

	// Wait for process to start
	testutil.Eventually(t, func() bool {
		processes := manager.ListProcesses()
		return len(processes) > 0
	}, "process to start")

	// Cancel monitor context - should stop gracefully without panic
	monitorCancel()

	// Give monitor goroutine time to exit
	time.Sleep(100 * time.Millisecond)

	// Manager should still be functional
	processes := manager.ListProcesses()
	if len(processes) == 0 {
		t.Error("Manager should still have processes after monitor cancellation")
	}
}

// TestManager_ListProcesses_MultipleProcesses tests listing multiple processes
func TestManager_ListProcesses_MultipleProcesses(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    10,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"process-a": {
				Enabled:      true,
				InitialState: "running",
				Command:      []string{"sleep", "300"},
				Restart:      "never",
				Scale:        1,
			},
			"process-b": {
				Enabled:      true,
				InitialState: "running",
				Command:      []string{"sleep", "300"},
				Restart:      "never",
				Scale:        2,
			},
			"disabled-process": {
				Enabled:      false,
				InitialState: "running",
				Command:      []string{"sleep", "300"},
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
		t.Fatalf("Failed to start processes: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	}()

	// Wait for processes to start
	testutil.Eventually(t, func() bool {
		processes := manager.ListProcesses()
		return len(processes) >= 2
	}, "processes to start")

	// Check listed processes
	processes := manager.ListProcesses()

	// Should have 2 enabled processes
	if len(processes) != 2 {
		t.Errorf("Expected 2 processes, got %d", len(processes))
	}

	// Find specific processes
	foundA := false
	foundB := false
	for _, p := range processes {
		if p.Name == "process-a" {
			foundA = true
			if p.Scale != 1 {
				t.Errorf("process-a should have scale 1, got %d", p.Scale)
			}
		}
		if p.Name == "process-b" {
			foundB = true
			if p.Scale != 2 {
				t.Errorf("process-b should have scale 2, got %d", p.Scale)
			}
		}
	}

	if !foundA {
		t.Error("process-a not found in list")
	}
	if !foundB {
		t.Error("process-b not found in list")
	}
}

// TestManager_AllDeadChannel_NotSignaledWithoutProcesses tests all-dead not signaled for empty manager
func TestManager_AllDeadChannel_NotSignaledWithoutProcesses(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    10,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"disabled": {
				Enabled:      false,
				InitialState: "running",
				Command:      []string{"sleep", "300"},
				Restart:      "never",
				Scale:        1,
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false)
	manager := NewManager(cfg, logger, auditLogger)

	ctx := context.Background()

	// Start health monitoring
	monitorCtx, monitorCancel := context.WithCancel(ctx)
	defer monitorCancel()
	manager.MonitorProcessHealth(monitorCtx)

	// Start manager (with no enabled processes)
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start processes: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	}()

	// Trigger NotifyProcessDeath to call checkAllProcessesDead
	manager.NotifyProcessDeath("test")

	// With no processes, all-dead should NOT be signaled (requires at least 1 process)
	select {
	case <-manager.AllDeadChannel():
		t.Error("Should not receive all-dead signal when no processes are managed")
	case <-time.After(300 * time.Millisecond):
		t.Log("Correctly did not receive all-dead signal with no processes")
	}
}

// TestManager_MultipleProcessesDying tests that all-dead is signaled when last process dies
func TestManager_MultipleProcessesDying(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    10,
			LogLevel:           "error",
			MaxRestartAttempts: 0,
			RestartBackoff:     1,
		},
		Processes: map[string]*config.Process{
			"short-1": {
				Enabled:      true,
				InitialState: "running",
				Command:      []string{"sh", "-c", "sleep 0.1 && exit 0"},
				Restart:      "never",
				Scale:        1,
			},
			"short-2": {
				Enabled:      true,
				InitialState: "running",
				Command:      []string{"sh", "-c", "sleep 0.2 && exit 0"},
				Restart:      "never",
				Scale:        1,
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false)
	manager := NewManager(cfg, logger, auditLogger)

	ctx := context.Background()

	// Start health monitoring
	monitorCtx, monitorCancel := context.WithCancel(ctx)
	defer monitorCancel()
	manager.MonitorProcessHealth(monitorCtx)

	// Start processes
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start processes: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	}()

	// Wait for all-dead signal (both processes should exit quickly)
	select {
	case <-manager.AllDeadChannel():
		t.Log("Received all-dead signal after both processes died")
	case <-time.After(5 * time.Second):
		t.Error("Expected all-dead signal but didn't receive it")
	}
}

// TestManager_AllDeadChannel_DoubleClose tests that all-dead channel can't be double-closed
func TestManager_AllDeadChannel_DoubleClose(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    10,
			LogLevel:           "error",
			MaxRestartAttempts: 0,
			RestartBackoff:     1,
		},
		Processes: map[string]*config.Process{
			"instant-exit": {
				Enabled:      true,
				InitialState: "running",
				Command:      []string{"sh", "-c", "exit 0"},
				Restart:      "never",
				Scale:        1,
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false)
	manager := NewManager(cfg, logger, auditLogger)

	ctx := context.Background()

	// Start health monitoring
	monitorCtx, monitorCancel := context.WithCancel(ctx)
	defer monitorCancel()
	manager.MonitorProcessHealth(monitorCtx)

	// Start processes
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start processes: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	}()

	// Wait for all-dead signal
	select {
	case <-manager.AllDeadChannel():
		t.Log("First all-dead signal received")
	case <-time.After(5 * time.Second):
		t.Fatal("Expected all-dead signal but didn't receive it")
	}

	// Multiple NotifyProcessDeath calls after all-dead should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Panic on subsequent NotifyProcessDeath: %v", r)
		}
	}()

	for i := 0; i < 10; i++ {
		manager.NotifyProcessDeath("instant-exit")
	}

	// Channel should still be closed (reading should return immediately)
	select {
	case <-manager.AllDeadChannel():
		t.Log("Channel remains closed as expected")
	default:
		t.Error("Channel should be closed but wasn't")
	}
}

// TestManager_ProcessLifecycle_StartStopRestart tests complete lifecycle
func TestManager_ProcessLifecycle_StartStopRestart(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    10,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     1,
		},
		Processes: map[string]*config.Process{
			"lifecycle-test": {
				Enabled:      true,
				InitialState: "running",
				Command:      []string{"sleep", "300"},
				Restart:      "never",
				Scale:        1,
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false)
	manager := NewManager(cfg, logger, auditLogger)

	ctx := context.Background()

	// Phase 1: Start
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start processes: %v", err)
	}

	// Verify process is running
	testutil.Eventually(t, func() bool {
		processes := manager.ListProcesses()
		return len(processes) > 0 && processes[0].State == "running"
	}, "process to start")

	// Phase 2: Stop process
	stopCtx, stopCancel := context.WithTimeout(ctx, 5*time.Second)
	defer stopCancel()

	if err := manager.StopProcess(stopCtx, "lifecycle-test"); err != nil {
		t.Fatalf("Failed to stop process: %v", err)
	}

	// Verify process is stopped
	testutil.Eventually(t, func() bool {
		processes := manager.ListProcesses()
		return len(processes) > 0 && processes[0].State == "stopped"
	}, "process to stop")

	// Phase 3: Restart process
	restartCtx, restartCancel := context.WithTimeout(ctx, 5*time.Second)
	defer restartCancel()

	if err := manager.StartProcess(restartCtx, "lifecycle-test"); err != nil {
		t.Fatalf("Failed to restart process: %v", err)
	}

	// Verify process is running again
	testutil.Eventually(t, func() bool {
		processes := manager.ListProcesses()
		return len(processes) > 0 && processes[0].State == "running"
	}, "process to restart")

	// Cleanup
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = manager.Shutdown(shutdownCtx)
}

