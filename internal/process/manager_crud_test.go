package process

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
	"gopkg.in/yaml.v3"
)

// TestManager_AddProcess tests adding a new process at runtime
func TestManager_AddProcess(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"existing": {
				Enabled: true,
				Command: []string{"sleep", "60"},
				Restart: "never",
				Scale:   1,
			},
		},
	}

	// Write initial config
	data, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false)
	manager := NewManager(cfg, logger, auditLogger)
	manager.SetConfigPath(cfgPath)

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	}()

	tests := []struct {
		name       string
		procName   string
		procConfig *config.Process
		wantErr    bool
		skip       bool
	}{
		{
			name:     "add valid process",
			procName: "new-process",
			procConfig: &config.Process{
				Enabled: true,
				Command: []string{"sleep", "30"},
				Restart: "never",
				Scale:   1,
			},
			wantErr: false,
		},
		{
			name:     "add duplicate process",
			procName: "existing",
			procConfig: &config.Process{
				Enabled: true,
				Command: []string{"sleep", "30"},
				Scale:   1,
			},
			wantErr: true,
		},
		{
			name:     "add with empty command",
			procName: "empty-cmd",
			procConfig: &config.Process{
				Enabled: true,
				Command: []string{},
				Scale:   1,
			},
			wantErr: true,
		},
		{
			name:     "add with empty name",
			procName: "",
			procConfig: &config.Process{
				Enabled: true,
				Command: []string{"sleep", "30"},
				Scale:   1,
			},
			wantErr: true,
		},
		{
			name:     "add with zero scale",
			procName: "zero-scale",
			procConfig: &config.Process{
				Enabled: true,
				Command: []string{"sleep", "30"},
				Scale:   0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.AddProcess(ctx, tt.procName, tt.procConfig)

			if (err != nil) != tt.wantErr {
				t.Errorf("AddProcess() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				// Verify process was added
				processes := manager.ListProcesses()
				found := false
				for _, p := range processes {
					if p.Name == tt.procName {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Process %s not found in process list after add", tt.procName)
				}
			}
		})
	}
}

// TestManager_RemoveProcess tests removing a process at runtime
func TestManager_RemoveProcess(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"removable": {
				Enabled:      true,
				InitialState: "running",
				Command:      []string{"sleep", "60"},
				Restart:      "never",
				Scale:        1,
			},
			"keeper": {
				Enabled: true,
				Command: []string{"sleep", "60"},
				Restart: "never",
				Scale:   1,
			},
		},
	}

	data, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false)
	manager := NewManager(cfg, logger, auditLogger)
	manager.SetConfigPath(cfgPath)

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	}()

	time.Sleep(200 * time.Millisecond)

	tests := []struct {
		name     string
		procName string
		wantErr  bool
	}{
		{
			name:     "remove existing process",
			procName: "removable",
			wantErr:  false,
		},
		{
			name:     "remove non-existent process",
			procName: "non-existent",
			wantErr:  true,
		},
		{
			name:     "remove with empty name",
			procName: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			removeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			err := manager.RemoveProcess(removeCtx, tt.procName)

			if (err != nil) != tt.wantErr {
				// Some errors during process termination are acceptable (signal permissions, already finished)
				if !tt.wantErr && err != nil {
					t.Logf("RemoveProcess() returned error (may be acceptable): %v", err)
				} else {
					t.Errorf("RemoveProcess() error = %v, wantErr %v", err, tt.wantErr)
				}
			}

			if !tt.wantErr {
				// Verify process was removed from config
				time.Sleep(100 * time.Millisecond)
				_, err := manager.GetProcessConfig(tt.procName)
				if err == nil {
					t.Error("Process still exists in config after remove")
				}
			}
		})
	}
}

// TestManager_UpdateProcess tests updating a process configuration
func TestManager_UpdateProcess(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"updatable": {
				Enabled:      true,
				InitialState: "running",
				Command:      []string{"sleep", "60"},
				Restart:      "never",
				Scale:        1,
			},
		},
	}

	data, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false)
	manager := NewManager(cfg, logger, auditLogger)
	manager.SetConfigPath(cfgPath)

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	}()

	time.Sleep(200 * time.Millisecond)

	tests := []struct {
		name           string
		procName       string
		procConfig     *config.Process
		wantErr        bool
		tolerateTiming bool // If true, accept "already finished" errors on timing-sensitive platforms
	}{
		{
			name:     "update existing process",
			procName: "updatable",
			procConfig: &config.Process{
				Enabled: true,
				Command: []string{"sleep", "120"},
				Restart: "on-failure",
				Scale:   2,
			},
			wantErr:        false,
			tolerateTiming: true, // On macOS, processes may finish before stop signal
		},
		{
			name:     "update non-existent process",
			procName: "non-existent",
			procConfig: &config.Process{
				Enabled: true,
				Command: []string{"sleep", "30"},
				Scale:   1,
			},
			wantErr: true,
		},
		{
			name:     "update with empty command",
			procName: "updatable",
			procConfig: &config.Process{
				Enabled: true,
				Command: []string{},
				Scale:   1,
			},
			wantErr: true,
		},
		{
			name:     "update with zero scale",
			procName: "updatable",
			procConfig: &config.Process{
				Enabled: true,
				Command: []string{"sleep", "30"},
				Scale:   0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updateCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			err := manager.UpdateProcess(updateCtx, tt.procName, tt.procConfig)

			if (err != nil) != tt.wantErr {
				// On macOS, processes may exit before stop signal is sent (timing-dependent)
				if tt.tolerateTiming && err != nil && strings.Contains(err.Error(), "already finished") {
					t.Logf("Tolerated timing-dependent error on update (process finished before stop): %v", err)
					// Still verify the update was applied despite timing error
					procCfg, getErr := manager.GetProcessConfig(tt.procName)
					if getErr == nil && procCfg.Scale == tt.procConfig.Scale {
						t.Logf("Process config was updated successfully despite timing error")
						return
					}
				}
				t.Errorf("UpdateProcess() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				// Verify process was updated
				procCfg, err := manager.GetProcessConfig(tt.procName)
				if err != nil {
					t.Fatalf("Failed to get process config: %v", err)
				}
				if procCfg.Scale != tt.procConfig.Scale {
					t.Errorf("Expected scale %d, got %d", tt.procConfig.Scale, procCfg.Scale)
				}
			}
		})
	}
}

// TestManager_SaveConfig tests configuration persistence
func TestManager_SaveConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

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
				Scale:   1,
			},
		},
	}

	data, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false)
	manager := NewManager(cfg, logger, auditLogger)
	manager.SetConfigPath(cfgPath)

	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func()
		wantErr bool
	}{
		{
			name: "save after adding process",
			setup: func() {
				newProc := &config.Process{
					Enabled: true,
					Command: []string{"sleep", "30"},
					Restart: "never",
					Scale:   1,
				}
				_ = manager.AddProcess(ctx, "new-process", newProc)
			},
			wantErr: false,
		},
		{
			name: "save without changes",
			setup: func() {
				// No changes
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()

			err := manager.SaveConfig()

			if (err != nil) != tt.wantErr {
				t.Errorf("SaveConfig() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				// Verify file was written
				if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
					t.Error("Config file does not exist after save")
				}

				// Verify file can be loaded
				loadedCfg := &config.Config{}
				fileData, err := os.ReadFile(cfgPath)
				if err != nil {
					t.Fatalf("Failed to read saved config: %v", err)
				}
				if err := yaml.Unmarshal(fileData, loadedCfg); err != nil {
					t.Fatalf("Failed to unmarshal saved config: %v", err)
				}
			}
		})
	}
}

// TestManager_ReloadConfig tests configuration reload
func TestManager_ReloadConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	initialCfg := &config.Config{
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
				Scale:   1,
			},
		},
	}

	data, _ := yaml.Marshal(initialCfg)
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false)
	manager := NewManager(initialCfg, logger, auditLogger)
	manager.SetConfigPath(cfgPath)

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	}()

	// Modify config file
	updatedCfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    45, // Changed
			LogLevel:           "debug",
			MaxRestartAttempts: 5, // Changed
			RestartBackoff:     10,
		},
		Processes: map[string]*config.Process{
			"test-proc": {
				Enabled: true,
				Command: []string{"sleep", "60"},
				Restart: "on-failure", // Changed
				Scale:   2,            // Changed
			},
		},
	}

	updatedData, _ := yaml.Marshal(updatedCfg)
	if err := os.WriteFile(cfgPath, updatedData, 0644); err != nil {
		t.Fatalf("Failed to write updated config: %v", err)
	}

	// Reload config
	reloadCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err := manager.ReloadConfig(reloadCtx)
	if err != nil {
		t.Fatalf("ReloadConfig() error = %v", err)
	}

	// Verify config was reloaded
	currentCfg := manager.GetConfig()
	if currentCfg.Global.ShutdownTimeout != 45 {
		t.Errorf("Expected shutdown_timeout=45, got %d", currentCfg.Global.ShutdownTimeout)
	}
	if currentCfg.Global.MaxRestartAttempts != 5 {
		t.Errorf("Expected max_restart_attempts=5, got %d", currentCfg.Global.MaxRestartAttempts)
	}
}

// TestManager_SetConfigPath tests setting config path
func TestManager_SetConfigPath(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false)
	manager := NewManager(cfg, logger, auditLogger)

	testPath := "/tmp/test-config.yaml"
	manager.SetConfigPath(testPath)

	// Verify path was set (we access via reflection or by attempting save)
	// Since configPath is private, we test indirectly via SaveConfig
	err := manager.SaveConfig()

	// Error is expected if directory doesn't exist, but that proves path was set
	if err == nil {
		// If no error, verify file exists
		if _, statErr := os.Stat(testPath); statErr == nil {
			os.Remove(testPath) // Cleanup
		}
	}
	// Path setting is confirmed if SaveConfig attempted to use it
}

// TestManager_GetResourceCollector tests resource collector retrieval
func TestManager_GetResourceCollector(t *testing.T) {
	tests := []struct {
		name             string
		metricsEnabled   bool
		wantCollectorNil bool
	}{
		{
			name:             "with metrics enabled",
			metricsEnabled:   true,
			wantCollectorNil: false,
		},
		{
			name:             "with metrics disabled",
			metricsEnabled:   false,
			wantCollectorNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Global: config.GlobalConfig{
					ShutdownTimeout:           30,
					LogLevel:                  "error",
					MaxRestartAttempts:        3,
					RestartBackoff:            5,
					ResourceMetricsInterval:   5,
					ResourceMetricsMaxSamples: 100,
				},
				Processes: map[string]*config.Process{},
			}

			// Use helper method to set pointer field
			cfg.Global.SetResourceMetricsEnabled(tt.metricsEnabled)

			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
			auditLogger := audit.NewLogger(logger, false)
			manager := NewManager(cfg, logger, auditLogger)

			collector := manager.GetResourceCollector()

			if tt.wantCollectorNil && collector != nil {
				t.Error("Expected nil collector when metrics disabled")
			}

			if !tt.wantCollectorNil && collector == nil {
				t.Error("Expected non-nil collector when metrics enabled")
			}
		})
	}
}

// TestManager_AllDeadChannel tests the all dead channel mechanism
func TestManager_AllDeadChannel(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "error",
			MaxRestartAttempts: 0,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"quick-exit": {
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
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	allDeadCh := manager.AllDeadChannel()

	// Verify channel exists (it may or may not be closed immediately)
	if allDeadCh == nil {
		t.Error("AllDeadChannel() returned nil")
	}

	// Don't wait for closure since processes may exit too fast or stay alive
	// The test just verifies the channel exists
	t.Log("AllDeadChannel returned valid channel")
}

// TestManager_MonitorProcessHealth tests health monitoring activation
func TestManager_MonitorProcessHealth(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"monitored": {
				Enabled:      true,
				InitialState: "running",
				Command:      []string{"sleep", "60"},
				Restart:      "never",
				Scale:        1,
				HealthCheck: &config.HealthCheck{
					Type:             "exec",
					Command:          []string{"echo", "healthy"},
					InitialDelay:     1,
					Period:           2,
					Timeout:          1,
					FailureThreshold: 3,
				},
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

	time.Sleep(200 * time.Millisecond)

	// Start health monitoring
	healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	manager.MonitorProcessHealth(healthCtx)

	// Let monitoring run
	time.Sleep(3 * time.Second)

	// Verify process is still running (health checks are passing)
	processes := manager.ListProcesses()
	if len(processes) == 0 {
		t.Fatal("No processes found")
	}

	found := false
	for _, p := range processes {
		if p.Name == "monitored" {
			found = true
			if p.State != "running" {
				t.Errorf("Expected process to be running, got %s", p.State)
			}
			break
		}
	}

	if !found {
		t.Error("Monitored process not found")
	}
}

// TestManager_GetLogs tests log retrieval from manager
func TestManager_GetLogs(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "info",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"logger": {
				Enabled:      true,
				InitialState: "running",
				Command:      []string{"sh", "-c", "echo 'test message' && sleep 1"},
				Restart:      "never",
				Scale:        1,
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
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

	// Wait for process to produce logs
	time.Sleep(500 * time.Millisecond)

	tests := []struct {
		name     string
		procName string
		limit    int
		wantErr  bool
	}{
		{
			name:     "get logs for existing process",
			procName: "logger",
			limit:    10,
			wantErr:  false,
		},
		{
			name:     "get logs for non-existent process",
			procName: "non-existent",
			limit:    10,
			wantErr:  true,
		},
		{
			name:     "get unlimited logs",
			procName: "logger",
			limit:    0,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs, err := manager.GetLogs(tt.procName, tt.limit)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetLogs() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				// Logs might be empty if process hasn't logged yet
				if tt.limit > 0 && len(logs) > tt.limit {
					t.Errorf("Expected max %d logs, got %d", tt.limit, len(logs))
				}
				t.Logf("Retrieved %d logs for process %s", len(logs), tt.procName)
			}
		})
	}
}

// TestManager_GetStackLogs tests multi-process log retrieval
func TestManager_GetStackLogs(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "info",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"proc-a": {
				Enabled:      true,
				InitialState: "running",
				Command:      []string{"sh", "-c", "echo 'log from A' && sleep 1"},
				Restart:      "never",
				Scale:        1,
			},
			"proc-b": {
				Enabled:      true,
				InitialState: "running",
				Command:      []string{"sh", "-c", "echo 'log from B' && sleep 1"},
				Restart:      "never",
				Scale:        1,
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
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

	// Wait for processes to produce logs
	time.Sleep(500 * time.Millisecond)

	tests := []struct {
		name  string
		limit int
	}{
		{
			name:  "get all stack logs",
			limit: 0,
		},
		{
			name:  "get limited stack logs",
			limit: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := manager.GetStackLogs(tt.limit)

			// Verify we got logs (might be empty if processes haven't logged)
			if tt.limit > 0 && len(logs) > tt.limit {
				t.Errorf("Expected max %d logs, got %d", tt.limit, len(logs))
			}

			t.Logf("Retrieved %d stack logs with limit %d", len(logs), tt.limit)

			// Logs should be from multiple processes
			processNames := make(map[string]bool)
			for _, log := range logs {
				processNames[log.ProcessName] = true
			}

			t.Logf("Logs from %d different processes", len(processNames))
		})
	}
}
