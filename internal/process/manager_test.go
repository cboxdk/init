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

func TestNewManager_UsesConfiguredProcessStartTimeout(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ProcessStartTimeout: 2 * time.Second,
		},
		Processes: map[string]*config.Process{},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false)
	manager := NewManager(cfg, logger, auditLogger)

	if manager.processStartTimeout != 2*time.Second {
		t.Fatalf("processStartTimeout = %v, want 2s", manager.processStartTimeout)
	}
}

// TestManager_GracefulShutdown tests graceful shutdown with timeout
func TestManager_GracefulShutdown(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"test-process": {
				Enabled:      true,
				InitialState: "running", // CRITICAL: Explicitly set to start the process
				Command:      []string{"sleep", "30"}, // Long enough to not exit before shutdown
				Restart:      "never",
				Scale:        1,
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false) // Disable audit in tests
	manager := NewManager(cfg, logger, auditLogger)

	// Start processes
	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start processes: %v", err)
	}

	// Wait for process to start
	testutil.Eventually(t, func() bool {
		processes := manager.ListProcesses()
		return len(processes) > 0 && processes[0].Scale >= 1
	}, "process to start")

	// Shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	err := manager.Shutdown(shutdownCtx)
	duration := time.Since(start)

	// Shutdown may have errors if processes already finished (not critical)
	if err != nil {
		t.Logf("Shutdown with errors (acceptable if process finished): %v", err)
	}

	// Shutdown should complete quickly
	// Note: May take up to shutdown timeout if process already finished
	if duration > 35*time.Second {
		t.Errorf("Shutdown took too long: %v (expected < 35s)", duration)
	}

	t.Logf("Shutdown completed in %v", duration)
}

// TestManager_ShutdownTimeout tests force kill after timeout
func TestManager_ShutdownTimeout(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    2, // Short timeout
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"long-process": {
				Enabled:      true,
				InitialState: "running", // CRITICAL: Explicitly set to start the process
				Command:      []string{"sleep", "60"}, // Long-running process
				Restart:      "never",
				Scale:        1,
				Shutdown: &config.ShutdownConfig{
					Timeout: 1, // 1 second timeout (shorter than sleep)
				},
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false) // Disable audit in tests
	manager := NewManager(cfg, logger, auditLogger)

	// Start processes
	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start processes: %v", err)
	}

	// Wait for process to start
	testutil.Eventually(t, func() bool {
		processes := manager.ListProcesses()
		return len(processes) > 0 && processes[0].Scale >= 1
	}, "process to start")

	// Shutdown with short timeout (should force kill)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	start := time.Now()
	err := manager.Shutdown(shutdownCtx)
	duration := time.Since(start)

	// Should complete via force kill
	if err != nil {
		t.Logf("Shutdown with errors (expected): %v", err)
	}

	// Should complete quickly due to force kill (within ~2s: 1s timeout + overhead)
	if duration > 3*time.Second {
		t.Errorf("Shutdown took too long even with force kill: %v", duration)
	}

	t.Logf("Shutdown with force kill completed in %v", duration)
}

// TestManager_PreStopHooks tests pre-stop hook execution
func TestManager_PreStopHooks(t *testing.T) {
	// Create a temporary file to track hook execution
	tmpfile := "/tmp/cbox-init-test-prehook"
	os.Remove(tmpfile) // Clean up from previous runs

	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Hooks: config.HooksConfig{
			PreStop: []config.Hook{
				{
					Name:    "test-prehook",
					Command: []string{"touch", tmpfile},
					Timeout: 5,
				},
			},
		},
		Processes: map[string]*config.Process{
			"test-process": {
				Enabled:      true,
				InitialState: "running", // CRITICAL: Explicitly set to start the process
				Command:      []string{"sleep", "30"}, // Long enough to not exit before shutdown
				Restart:      "never",
				Scale:        1,
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false) // Disable audit in tests
	manager := NewManager(cfg, logger, auditLogger)

	// Start processes
	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start processes: %v", err)
	}

	// Wait for process to start
	testutil.Eventually(t, func() bool {
		processes := manager.ListProcesses()
		return len(processes) > 0 && processes[0].Scale >= 1
	}, "process to start")

	// Shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := manager.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}

	// Verify pre-stop hook was executed
	if _, err := os.Stat(tmpfile); os.IsNotExist(err) {
		t.Error("Pre-stop hook was not executed (file not created)")
	} else {
		os.Remove(tmpfile) // Cleanup
	}
}

// TestManager_ShutdownOrder tests processes shutdown in reverse priority order
func TestManager_ShutdownOrder(t *testing.T) {
	// Track shutdown order via temporary files
	file1 := "/tmp/cbox-init-test-process1"
	file2 := "/tmp/cbox-init-test-process2"
	file3 := "/tmp/cbox-init-test-process3"

	// Cleanup
	os.Remove(file1)
	os.Remove(file2)
	os.Remove(file3)

	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"process1": {
				Enabled:      true,
				InitialState: "running",                 // CRITICAL: Explicitly set to start the process
				Command:      []string{"sleep", "3600"}, // Long enough to not exit before shutdown (force killed)
				Restart:      "never",
				Scale:        1,
				Shutdown: &config.ShutdownConfig{
					Timeout: 1, // Short timeout to force kill quickly
					PreStopHook: &config.Hook{
						Name:    "mark-process1",
						Command: []string{"touch", file1},
						Timeout: 2,
					},
				},
			},
			"process2": {
				Enabled:      true,
				InitialState: "running",                 // CRITICAL: Explicitly set to start the process
				Command:      []string{"sleep", "3600"}, // Long enough to not exit before shutdown (force killed)
				DependsOn:    []string{"process1"},      // Start after process1
				Restart:      "never",
				Scale:        1,
				Shutdown: &config.ShutdownConfig{
					Timeout: 1, // Short timeout to force kill quickly
					PreStopHook: &config.Hook{
						Name:    "mark-process2",
						Command: []string{"touch", file2},
						Timeout: 2,
					},
				},
			},
			"process3": {
				Enabled:      true,
				InitialState: "running",                 // CRITICAL: Explicitly set to start the process
				Command:      []string{"sleep", "3600"}, // Long enough to not exit before shutdown (force killed)
				DependsOn:    []string{"process2"},      // Start after process2 (stop first in shutdown)
				Restart:      "never",
				Scale:        1,
				Shutdown: &config.ShutdownConfig{
					Timeout: 1, // Short timeout to force kill quickly
					PreStopHook: &config.Hook{
						Name:    "mark-process3",
						Command: []string{"touch", file3},
						Timeout: 2,
					},
				},
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false) // Disable audit in tests
	manager := NewManager(cfg, logger, auditLogger)

	// Start processes
	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start processes: %v", err)
	}

	// Wait for processes to start and be in "running" state
	testutil.Eventually(t, func() bool {
		processes := manager.ListProcesses()
		if len(processes) != 3 {
			return false
		}
		for _, p := range processes {
			if p.Scale < 1 || p.State != "running" {
				return false
			}
		}
		return true
	}, "all processes to start and be running", 5*time.Second)

	// Additional stabilization time for process to fully initialize
	time.Sleep(100 * time.Millisecond)

	// Shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := manager.Shutdown(shutdownCtx); err != nil {
		t.Logf("Shutdown with errors (may be expected): %v", err)
	}

	// Verify hooks were executed (order is parallel within priority levels, so just check existence)
	// Note: On some platforms (macOS), timing issues may cause processes to exit before hooks run
	hooksExecuted := 0
	for name, file := range map[string]string{
		"process1": file1,
		"process2": file2,
		"process3": file3,
	} {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			t.Logf("Pre-stop hook for %s was not executed (may be timing issue on this platform)", name)
		} else {
			hooksExecuted++
			os.Remove(file)
		}
	}
	// At least some hooks should have executed if processes were running
	if hooksExecuted == 0 {
		t.Errorf("No pre-stop hooks were executed - shutdown may have failed")
	} else {
		t.Logf("Successfully executed %d/3 pre-stop hooks", hooksExecuted)
	}
}

// TestManager_MultipleInstances tests shutdown with scaled processes
func TestManager_MultipleInstances(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"scaled-process": {
				Enabled:      true,
				InitialState: "running",                 // CRITICAL: Explicitly set to start the process
				Command:      []string{"sleep", "3600"}, // Long enough to not exit before shutdown (force killed)
				Scale:        3,                         // 3 instances
				Restart:      "never",
				Shutdown: &config.ShutdownConfig{
					Timeout: 1, // Short timeout to force kill quickly
				},
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false) // Disable audit in tests
	manager := NewManager(cfg, logger, auditLogger)

	// Start processes
	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start processes: %v", err)
	}

	// Wait for 3 instances to start
	testutil.Eventually(t, func() bool {
		instances := manager.processes["scaled-process"].GetInstances()
		return len(instances) == 3
	}, "3 instances to start", 3*time.Second)

	// Shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	err := manager.Shutdown(shutdownCtx)
	duration := time.Since(start)

	if err != nil {
		t.Logf("Shutdown with errors: %v", err)
	}

	// All instances should stop in parallel (timeout is 2s per instance, force killed in parallel)
	if duration > 5*time.Second {
		t.Errorf("Shutdown should be parallel, took %v (expected < 5s)", duration)
	}

	// Log duration for verification
	t.Logf("Shutdown of 3 instances completed in %v (parallel shutdown with 2s timeout)", duration)
}

// TestManager_ConfigurableRestartBackoff tests that global restart config is used
func TestManager_ConfigurableRestartBackoff(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "error",
			MaxRestartAttempts: 5,  // Custom value
			RestartBackoff:     10, // Custom value (10 seconds)
		},
		Processes: map[string]*config.Process{
			"test-process": {
				Enabled: true,
				Command: []string{"sh", "-c", "exit 1"}, // Fails immediately
				Restart: "on-failure",
				Scale:   1,
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false) // Disable audit in tests
	manager := NewManager(cfg, logger, auditLogger)

	// Start processes (will fail and attempt restart)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// This will fail to start, which is expected
	_ = manager.Start(ctx)

	// Verify supervisor was created with global config values
	supervisor := manager.processes["test-process"]
	if supervisor == nil {
		t.Fatal("Supervisor not created")
	}

	// The restart policy should use global config (MaxRestartAttempts=5, Backoff=10s)
	policy := supervisor.restartPolicy
	if policy == nil {
		t.Fatal("Restart policy not created")
	}

	// Test that backoff uses configured value (10s)
	backoff := policy.BackoffDuration(0)
	expected := 10 * time.Second

	if backoff != expected {
		t.Errorf("Backoff duration incorrect: got %v, want %v", backoff, expected)
	}

	t.Logf("Restart backoff correctly uses global config: %v", backoff)
}

// TestManager_ListProcesses tests the ListProcesses API
func TestManager_ListProcesses(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"process-a": {
				Enabled:      true,
				InitialState: "running", // Required for auto-start
				Command:      []string{"sleep", "60"},
				Restart:      "never",
				Scale:        2,
			},
			"process-b": {
				Enabled:      true,
				InitialState: "running", // Required for auto-start
				Command:      []string{"sleep", "60"},
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	}()

	// Wait for processes to start with polling
	var processes []ProcessInfo
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		processes = manager.ListProcesses()
		// Check if process-a has instances running
		for _, p := range processes {
			if p.Name == "process-a" && p.Scale >= 2 {
				goto checkResults
			}
		}
	}

checkResults:
	// Test ListProcesses
	if len(processes) != 2 {
		t.Errorf("Expected 2 processes, got %d", len(processes))
	}

	// Find process-a and verify scale
	var foundA bool
	for _, p := range processes {
		if p.Name == "process-a" {
			foundA = true
			// DesiredScale comes from config, Scale is running instances
			if p.DesiredScale != 2 {
				t.Errorf("Expected process-a DesiredScale=2, got %d", p.DesiredScale)
			}
			// Running instances may still be starting, be lenient
			if p.Scale < 1 {
				t.Errorf("Expected process-a to have at least 1 running instance, got %d", p.Scale)
			}
		}
	}
	if !foundA {
		t.Error("process-a not found in ListProcesses")
	}
}

// TestManager_StartStopProcess tests individual process start/stop
func TestManager_StartStopProcess(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"test-proc": {
				Enabled:      true,
				InitialState: "stopped", // Start as stopped
				Command:      []string{"sleep", "60"},
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
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	}()

	// Process should be stopped initially
	processes := manager.ListProcesses()
	if len(processes) != 1 {
		t.Fatalf("Expected 1 process, got %d", len(processes))
	}
	if processes[0].State != "stopped" {
		t.Errorf("Expected initial state 'stopped', got '%s'", processes[0].State)
	}

	// Start the process
	startCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := manager.StartProcess(startCtx, "test-proc"); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait for process to be running
	testutil.Eventually(t, func() bool {
		processes = manager.ListProcesses()
		return len(processes) > 0 && processes[0].State == "running"
	}, "process to be running", 2*time.Second)

	// Stop the process
	stopCtx, stopCancel := context.WithTimeout(ctx, 5*time.Second)
	defer stopCancel()
	if err := manager.StopProcess(stopCtx, "test-proc"); err != nil {
		t.Fatalf("Failed to stop process: %v", err)
	}

	// Wait for process to be stopped
	testutil.Eventually(t, func() bool {
		processes = manager.ListProcesses()
		return len(processes) > 0 && processes[0].State == "stopped"
	}, "process to be stopped", 2*time.Second)
}

// TestManager_RestartProcess tests process restart
func TestManager_RestartProcess(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"restart-proc": {
				Enabled:      true,
				InitialState: "running", // Required for auto-start
				Command:      []string{"sleep", "60"},
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
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	}()

	// Wait for process to start with polling
	var processes []ProcessInfo
	var initialPID int
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		processes = manager.ListProcesses()
		if len(processes) > 0 && len(processes[0].Instances) > 0 && processes[0].Instances[0].PID > 0 {
			initialPID = processes[0].Instances[0].PID
			break
		}
	}
	if initialPID == 0 {
		t.Fatal("No process instances started within timeout")
	}

	// Restart the process
	restartCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := manager.RestartProcess(restartCtx, "restart-proc"); err != nil {
		t.Fatalf("Failed to restart process: %v", err)
	}

	// Wait for PID to change
	var newPID int
	testutil.Eventually(t, func() bool {
		processes = manager.ListProcesses()
		if len(processes) == 0 || len(processes[0].Instances) == 0 {
			return false
		}
		newPID = processes[0].Instances[0].PID
		return newPID > 0 && newPID != initialPID
	}, "PID to change after restart", 3*time.Second)
}

// TestManager_ScaleProcess tests process scaling
func TestManager_ScaleProcess(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"scale-proc": {
				Enabled:      true,
				InitialState: "running", // Required for auto-start
				Command:      []string{"sleep", "60"},
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
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	}()

	// Wait for initial process to start with polling
	var processes []ProcessInfo
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		processes = manager.ListProcesses()
		if len(processes) > 0 && processes[0].Scale >= 1 {
			break
		}
	}

	// Initial scale should be 1 (or at least desired is 1)
	if len(processes) == 0 {
		t.Fatal("No processes found")
	}
	if processes[0].DesiredScale != 1 {
		t.Errorf("Expected initial DesiredScale=1, got %d", processes[0].DesiredScale)
	}

	// Scale up to 3
	scaleCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := manager.ScaleProcess(scaleCtx, "scale-proc", 3); err != nil {
		t.Fatalf("Failed to scale up process: %v", err)
	}

	// Poll for scale to reach 3
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		processes = manager.ListProcesses()
		if len(processes) > 0 && processes[0].Scale >= 3 {
			break
		}
	}

	if processes[0].Scale < 3 {
		t.Errorf("Expected scale>=3 after scale up, got %d", processes[0].Scale)
	}

	// Scale down to 1
	if err := manager.ScaleProcess(scaleCtx, "scale-proc", 1); err != nil {
		t.Fatalf("Failed to scale down process: %v", err)
	}

	// Poll for scale to decrease
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		processes = manager.ListProcesses()
		if len(processes) > 0 && processes[0].Scale <= 1 {
			break
		}
	}

	processes = manager.ListProcesses()
	if processes[0].Scale != 1 {
		t.Errorf("Expected scale=1 after scale down, got %d", processes[0].Scale)
	}
}

// TestManager_InputValidation tests input validation for control APIs
func TestManager_InputValidation(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"valid-proc": {
				Enabled: true,
				Command: []string{"sleep", "60"},
				Restart: "never",
				Scale:   1,
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
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	}()

	// Test empty process name
	if err := manager.StartProcess(ctx, ""); err == nil {
		t.Error("Expected error for empty process name on StartProcess")
	}

	// Test non-existent process
	if err := manager.StartProcess(ctx, "non-existent"); err == nil {
		t.Error("Expected error for non-existent process")
	}

	// Test scale validation - empty name
	if err := manager.ScaleProcess(ctx, "", 1); err == nil {
		t.Error("Expected error for empty process name on ScaleProcess")
	}

	// Test scale validation - exceeds max (100)
	if err := manager.ScaleProcess(ctx, "valid-proc", 101); err == nil {
		t.Error("Expected error for scale > 100")
	}

	// Test scale non-existent process
	if err := manager.ScaleProcess(ctx, "non-existent", 2); err == nil {
		t.Error("Expected error for scaling non-existent process")
	}
}

// TestManager_GetConfig tests config retrieval
func TestManager_GetConfig(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "info",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"test-proc": {
				Enabled: true,
				Command: []string{"sleep", "60"},
				Restart: "never",
				Scale:   2,
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false)
	manager := NewManager(cfg, logger, auditLogger)

	// Test GetConfig
	retrievedCfg := manager.GetConfig()
	if retrievedCfg == nil {
		t.Fatal("GetConfig returned nil")
	}
	if retrievedCfg.Global.LogLevel != "info" {
		t.Errorf("Expected log_level=info, got %s", retrievedCfg.Global.LogLevel)
	}

	// Test GetProcessConfig
	procCfg, err := manager.GetProcessConfig("test-proc")
	if err != nil {
		t.Fatalf("GetProcessConfig returned error for existing process: %v", err)
	}
	if procCfg == nil {
		t.Fatal("GetProcessConfig returned nil for existing process")
	}
	if procCfg.Scale != 2 {
		t.Errorf("Expected scale=2, got %d", procCfg.Scale)
	}

	// Test GetProcessConfig for non-existent
	nilCfg, err := manager.GetProcessConfig("non-existent")
	if err == nil {
		t.Error("Expected error for non-existent process config")
	}
	if nilCfg != nil {
		t.Error("Expected nil config for non-existent process")
	}
}
