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

// createReadinessTestManager creates a manager with readiness config for testing
func createReadinessTestManager(t *testing.T) (*Manager, func()) {
	t.Helper()

	// Create temp file for readiness
	tmpFile, err := os.CreateTemp("", "readiness-test-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    5,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     1,
			Readiness: &config.ReadinessConfig{
				Enabled:  true,
				Path: tmpPath,
				Mode:     "all_running",
			},
		},
		Processes: map[string]*config.Process{
			"test-worker": {
				Enabled:      true,
				Type:         "longrun",
				InitialState: "running",
				Command:      []string{"sleep", "60"},
				Restart:      "always",
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

	cleanup := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
		_ = os.Remove(tmpPath)
	}

	return manager, cleanup
}

// TestManager_GetReadinessManager verifies GetReadinessManager returns readiness manager
func TestManager_GetReadinessManager(t *testing.T) {
	manager, cleanup := createReadinessTestManager(t)
	defer cleanup()

	rm := manager.GetReadinessManager()
	if rm == nil {
		t.Error("GetReadinessManager() returned nil, expected non-nil readiness manager")
	}
}

// TestManager_GetReadinessManager_NilWhenDisabled tests readiness manager is nil when disabled
func TestManager_GetReadinessManager_NilWhenDisabled(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout: 5,
			LogLevel:        "error",
			// No readiness config - disabled by default
		},
		Processes: map[string]*config.Process{},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false)
	manager := NewManager(cfg, logger, auditLogger)

	rm := manager.GetReadinessManager()
	if rm != nil {
		t.Error("GetReadinessManager() should return nil when readiness is disabled")
	}
}

// TestManager_StartReadinessMonitor tests starting readiness monitor
func TestManager_StartReadinessMonitor(t *testing.T) {
	manager, cleanup := createReadinessTestManager(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start readiness monitor
	manager.StartReadinessMonitor(ctx)

	// Give it a moment to run
	time.Sleep(200 * time.Millisecond)

	// Verify readiness manager is operational
	rm := manager.GetReadinessManager()
	if rm == nil {
		t.Error("Readiness manager should be non-nil after StartReadinessMonitor")
	}
}

// TestManager_StartReadinessMonitor_NilSafe tests nil readiness manager handling
func TestManager_StartReadinessMonitor_NilSafe(t *testing.T) {
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

	ctx := context.Background()

	// Should not panic with nil readiness manager
	manager.StartReadinessMonitor(ctx)
}

// TestManager_StopReadinessManager tests stopping readiness manager
func TestManager_StopReadinessManager(t *testing.T) {
	manager, cleanup := createReadinessTestManager(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Start readiness monitor
	manager.StartReadinessMonitor(ctx)
	time.Sleep(100 * time.Millisecond)

	// Stop readiness manager
	err := manager.StopReadinessManager()
	if err != nil {
		t.Errorf("StopReadinessManager() returned error: %v", err)
	}
}

// TestManager_StopReadinessManager_NilSafe tests nil readiness manager stop
func TestManager_StopReadinessManager_NilSafe(t *testing.T) {
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

	// Should not panic or error with nil readiness manager
	err := manager.StopReadinessManager()
	if err != nil {
		t.Errorf("StopReadinessManager() with nil manager returned error: %v", err)
	}
}

// TestManager_UpdateReadinessStates tests readiness state updates
func TestManager_UpdateReadinessStates(t *testing.T) {
	manager, cleanup := createReadinessTestManager(t)
	defer cleanup()

	// Give process time to start
	time.Sleep(200 * time.Millisecond)

	// Update readiness states manually
	manager.updateReadinessStates()

	// Verify it completed without error
	rm := manager.GetReadinessManager()
	if rm == nil {
		t.Error("Readiness manager should exist")
	}
}

// TestManager_UpdateReadinessStates_NilSafe tests nil readiness state updates
func TestManager_UpdateReadinessStates_NilSafe(t *testing.T) {
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

	// Should not panic with nil readiness manager
	manager.updateReadinessStates()
}

// TestManager_ReadinessWithSpecificProcesses tests readiness with explicit process list
func TestManager_ReadinessWithSpecificProcesses(t *testing.T) {
	// Create temp file for readiness
	tmpFile, err := os.CreateTemp("", "readiness-specific-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    5,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     1,
			Readiness: &config.ReadinessConfig{
				Enabled:   true,
				Path:      tmpPath,
				Mode:      "all_healthy",
				Processes: []string{"worker-a"}, // Specific process
			},
		},
		Processes: map[string]*config.Process{
			"worker-a": {
				Enabled:      true,
				Type:         "longrun",
				InitialState: "running",
				Command:      []string{"sleep", "60"},
				Restart:      "always",
				Scale:        1,
			},
			"worker-b": {
				Enabled:      true,
				Type:         "longrun",
				InitialState: "running",
				Command:      []string{"sleep", "60"},
				Restart:      "always",
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	}()

	// Start readiness monitor - should only track worker-a
	ctxMonitor, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	manager.StartReadinessMonitor(ctxMonitor)

	time.Sleep(200 * time.Millisecond)

	rm := manager.GetReadinessManager()
	if rm == nil {
		t.Error("Readiness manager should be non-nil")
	}
}

// TestManager_MonitorReadinessStates tests the goroutine monitoring
func TestManager_MonitorReadinessStates(t *testing.T) {
	manager, cleanup := createReadinessTestManager(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start monitoring in background
	manager.StartReadinessMonitor(ctx)

	// Let it run a few cycles
	time.Sleep(1500 * time.Millisecond)

	// The context should trigger shutdown of the monitoring goroutine
	// Test passes if we get here without hanging
}

// TestManager_ReadinessStateMapping tests various process state mappings
func TestManager_ReadinessStateMapping(t *testing.T) {
	// Create temp file for readiness
	tmpFile, err := os.CreateTemp("", "readiness-states-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    5,
			LogLevel:           "error",
			MaxRestartAttempts: 0, // No restarts to allow failure state
			RestartBackoff:     1,
			Readiness: &config.ReadinessConfig{
				Enabled:  true,
				Path: tmpPath,
				Mode:     "all_running",
			},
		},
		Processes: map[string]*config.Process{
			"quick-exit": {
				Enabled:      true,
				Type:         "longrun",
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

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	}()

	// Give process time to complete and update states
	time.Sleep(500 * time.Millisecond)

	// Trigger state update
	manager.updateReadinessStates()
}
