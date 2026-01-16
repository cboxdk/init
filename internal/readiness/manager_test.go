package readiness

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/cboxdk/init/internal/config"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestNewManager(t *testing.T) {
	cfg := &config.ReadinessConfig{
		Enabled: true,
		Path:    "/tmp/test-ready",
		Mode:    "all_healthy",
	}
	logger := newTestLogger()

	m := NewManager(cfg, logger)

	if m == nil {
		t.Fatal("Expected non-nil manager")
	}
	if m.config != cfg {
		t.Error("Expected config to be set")
	}
	if m.processes == nil {
		t.Error("Expected processes map to be initialized")
	}
	if m.stopCh == nil {
		t.Error("Expected stopCh to be initialized")
	}
	if m.isReady {
		t.Error("Expected isReady to be false initially")
	}
}

func TestManager_StartStop_Disabled(t *testing.T) {
	cfg := &config.ReadinessConfig{
		Enabled: false,
		Path:    "/tmp/test-ready",
	}
	logger := newTestLogger()
	m := NewManager(cfg, logger)

	// Start should do nothing when disabled
	err := m.Start(context.Background())
	if err != nil {
		t.Errorf("Expected no error on disabled start, got: %v", err)
	}

	// Stop should also work fine
	err = m.Stop()
	if err != nil {
		t.Errorf("Expected no error on stop, got: %v", err)
	}
}

func TestManager_StartStop_NilConfig(t *testing.T) {
	logger := newTestLogger()
	m := NewManager(nil, logger)

	err := m.Start(context.Background())
	if err != nil {
		t.Errorf("Expected no error on nil config start, got: %v", err)
	}
}

func TestManager_StartStop_Enabled(t *testing.T) {
	tmpDir := t.TempDir()
	readinessPath := filepath.Join(tmpDir, "ready")

	cfg := &config.ReadinessConfig{
		Enabled: true,
		Path:    readinessPath,
		Mode:    "all_healthy",
	}
	logger := newTestLogger()
	m := NewManager(cfg, logger)

	// Start should create directory if needed
	err := m.Start(context.Background())
	if err != nil {
		t.Fatalf("Expected no error on start, got: %v", err)
	}

	// Readiness file should NOT exist initially (not ready)
	if _, err := os.Stat(readinessPath); !os.IsNotExist(err) {
		t.Error("Expected readiness file to not exist initially")
	}

	// Stop should work
	err = m.Stop()
	if err != nil {
		t.Errorf("Expected no error on stop, got: %v", err)
	}

	// Double stop should be idempotent
	err = m.Stop()
	if err != nil {
		t.Errorf("Expected no error on double stop, got: %v", err)
	}
}

func TestManager_SetTrackedProcesses(t *testing.T) {
	cfg := &config.ReadinessConfig{
		Enabled: true,
		Path:    "/tmp/test-ready",
		Mode:    "all_healthy",
	}
	logger := newTestLogger()
	m := NewManager(cfg, logger)

	processes := []string{"php-fpm", "nginx", "horizon"}
	m.SetTrackedProcesses(processes)

	status := m.GetStatus()
	if len(status) != 3 {
		t.Errorf("Expected 3 processes, got: %d", len(status))
	}

	for _, name := range processes {
		proc, exists := status[name]
		if !exists {
			t.Errorf("Expected process %q to exist", name)
			continue
		}
		if proc.State != StateStopped {
			t.Errorf("Expected process %q state to be %q, got: %q", name, StateStopped, proc.State)
		}
		if proc.Health != "unknown" {
			t.Errorf("Expected process %q health to be 'unknown', got: %q", name, proc.Health)
		}
	}

	// Setting again should clear and reset
	m.SetTrackedProcesses([]string{"single"})
	status = m.GetStatus()
	if len(status) != 1 {
		t.Errorf("Expected 1 process after reset, got: %d", len(status))
	}
}

func TestManager_UpdateProcessState(t *testing.T) {
	tmpDir := t.TempDir()
	readinessPath := filepath.Join(tmpDir, "ready")

	cfg := &config.ReadinessConfig{
		Enabled: true,
		Path:    readinessPath,
		Mode:    "all_healthy",
	}
	logger := newTestLogger()
	m := NewManager(cfg, logger)

	_ = m.Start(context.Background())
	m.SetTrackedProcesses([]string{"php-fpm"})

	// Update to running
	m.UpdateProcessState("php-fpm", StateRunning, "unknown")
	status := m.GetStatus()
	if status["php-fpm"].State != StateRunning {
		t.Errorf("Expected state %q, got: %q", StateRunning, status["php-fpm"].State)
	}

	// Not ready yet because mode is all_healthy and health is unknown
	if m.IsReady() {
		t.Error("Expected not ready with all_healthy mode and unknown health")
	}

	// Update to healthy
	m.UpdateProcessState("php-fpm", StateHealthy, "healthy")
	if !m.IsReady() {
		t.Error("Expected ready after process becomes healthy")
	}

	// Readiness file should exist
	if _, err := os.Stat(readinessPath); os.IsNotExist(err) {
		t.Error("Expected readiness file to exist when ready")
	}

	// Update to unhealthy
	m.UpdateProcessState("php-fpm", StateUnhealthy, "unhealthy")
	if m.IsReady() {
		t.Error("Expected not ready after process becomes unhealthy")
	}

	// Readiness file should be removed
	if _, err := os.Stat(readinessPath); !os.IsNotExist(err) {
		t.Error("Expected readiness file to be removed when not ready")
	}
}

func TestManager_UpdateProcessState_Disabled(t *testing.T) {
	cfg := &config.ReadinessConfig{
		Enabled: false,
		Path:    "/tmp/test-ready",
	}
	logger := newTestLogger()
	m := NewManager(cfg, logger)

	// Should not panic and should be no-op
	m.UpdateProcessState("test", StateRunning, "healthy")

	// Status should be empty
	status := m.GetStatus()
	if len(status) != 0 {
		t.Error("Expected empty status when disabled")
	}
}

func TestManager_UpdateProcessState_UntrackedWithFilter(t *testing.T) {
	cfg := &config.ReadinessConfig{
		Enabled:   true,
		Path:      "/tmp/test-ready",
		Mode:      "all_healthy",
		Processes: []string{"php-fpm"}, // Only track php-fpm
	}
	logger := newTestLogger()
	m := NewManager(cfg, logger)

	m.SetTrackedProcesses([]string{"php-fpm"})

	// Update tracked process
	m.UpdateProcessState("php-fpm", StateHealthy, "healthy")

	// Update untracked process - should be ignored
	m.UpdateProcessState("nginx", StateHealthy, "healthy")

	status := m.GetStatus()
	if _, exists := status["nginx"]; exists {
		t.Error("Expected nginx to not be tracked when processes filter is set")
	}
}

func TestManager_ModeAllRunning(t *testing.T) {
	tmpDir := t.TempDir()
	readinessPath := filepath.Join(tmpDir, "ready")

	cfg := &config.ReadinessConfig{
		Enabled: true,
		Path:    readinessPath,
		Mode:    "all_running",
	}
	logger := newTestLogger()
	m := NewManager(cfg, logger)

	_ = m.Start(context.Background())
	m.SetTrackedProcesses([]string{"php-fpm"})

	// In all_running mode, just being running should be enough
	m.UpdateProcessState("php-fpm", StateRunning, "unknown")
	if !m.IsReady() {
		t.Error("Expected ready in all_running mode when process is running")
	}

	// StateHealthy should also work
	m.UpdateProcessState("php-fpm", StateHealthy, "healthy")
	if !m.IsReady() {
		t.Error("Expected ready in all_running mode when process is healthy")
	}

	// Stopped should make it not ready
	m.UpdateProcessState("php-fpm", StateStopped, "unknown")
	if m.IsReady() {
		t.Error("Expected not ready in all_running mode when process is stopped")
	}

	// Failed should make it not ready
	m.UpdateProcessState("php-fpm", StateFailed, "unhealthy")
	if m.IsReady() {
		t.Error("Expected not ready in all_running mode when process is failed")
	}
}

func TestManager_ModeAllHealthy(t *testing.T) {
	tmpDir := t.TempDir()
	readinessPath := filepath.Join(tmpDir, "ready")

	cfg := &config.ReadinessConfig{
		Enabled: true,
		Path:    readinessPath,
		Mode:    "all_healthy",
	}
	logger := newTestLogger()
	m := NewManager(cfg, logger)

	_ = m.Start(context.Background())
	m.SetTrackedProcesses([]string{"php-fpm"})

	// Just running with unknown health is NOT enough
	m.UpdateProcessState("php-fpm", StateRunning, "unknown")
	if m.IsReady() {
		t.Error("Expected not ready in all_healthy mode when health is unknown")
	}

	// Running with healthy status should work
	m.UpdateProcessState("php-fpm", StateRunning, "healthy")
	if !m.IsReady() {
		t.Error("Expected ready in all_healthy mode when running and healthy")
	}

	// StateHealthy should also work
	m.UpdateProcessState("php-fpm", StateHealthy, "healthy")
	if !m.IsReady() {
		t.Error("Expected ready in all_healthy mode when state is healthy")
	}

	// Unhealthy should make not ready
	m.UpdateProcessState("php-fpm", StateUnhealthy, "unhealthy")
	if m.IsReady() {
		t.Error("Expected not ready in all_healthy mode when unhealthy")
	}
}

func TestManager_MultipleProcesses(t *testing.T) {
	tmpDir := t.TempDir()
	readinessPath := filepath.Join(tmpDir, "ready")

	cfg := &config.ReadinessConfig{
		Enabled: true,
		Path:    readinessPath,
		Mode:    "all_running",
	}
	logger := newTestLogger()
	m := NewManager(cfg, logger)

	_ = m.Start(context.Background())
	m.SetTrackedProcesses([]string{"php-fpm", "nginx"})

	// One running, one stopped - not ready
	m.UpdateProcessState("php-fpm", StateRunning, "healthy")
	if m.IsReady() {
		t.Error("Expected not ready when one process is still stopped")
	}

	// Both running - ready
	m.UpdateProcessState("nginx", StateRunning, "healthy")
	if !m.IsReady() {
		t.Error("Expected ready when all processes are running")
	}

	// One fails - not ready
	m.UpdateProcessState("php-fpm", StateFailed, "unhealthy")
	if m.IsReady() {
		t.Error("Expected not ready when one process has failed")
	}
}

func TestManager_RemoveProcess(t *testing.T) {
	tmpDir := t.TempDir()
	readinessPath := filepath.Join(tmpDir, "ready")

	cfg := &config.ReadinessConfig{
		Enabled: true,
		Path:    readinessPath,
		Mode:    "all_running",
	}
	logger := newTestLogger()
	m := NewManager(cfg, logger)

	_ = m.Start(context.Background())
	m.SetTrackedProcesses([]string{"php-fpm", "nginx"})

	// Make one ready
	m.UpdateProcessState("php-fpm", StateRunning, "healthy")
	if m.IsReady() {
		t.Error("Expected not ready when nginx is still stopped")
	}

	// Remove the stopped one - now should be ready
	m.RemoveProcess("nginx")
	if !m.IsReady() {
		t.Error("Expected ready after removing non-ready process")
	}

	status := m.GetStatus()
	if _, exists := status["nginx"]; exists {
		t.Error("Expected nginx to be removed from status")
	}
}

func TestManager_NoProcesses(t *testing.T) {
	tmpDir := t.TempDir()
	readinessPath := filepath.Join(tmpDir, "ready")

	cfg := &config.ReadinessConfig{
		Enabled: true,
		Path:    readinessPath,
		Mode:    "all_running",
	}
	logger := newTestLogger()
	m := NewManager(cfg, logger)

	_ = m.Start(context.Background())

	// No processes tracked - should not be ready
	if m.IsReady() {
		t.Error("Expected not ready when no processes are tracked")
	}

	// Add and then remove all - should not be ready
	m.SetTrackedProcesses([]string{"test"})
	m.UpdateProcessState("test", StateRunning, "healthy")
	if !m.IsReady() {
		t.Error("Expected ready with one running process")
	}

	m.RemoveProcess("test")
	if m.IsReady() {
		t.Error("Expected not ready after removing all processes")
	}
}

func TestManager_CustomContent(t *testing.T) {
	tmpDir := t.TempDir()
	readinessPath := filepath.Join(tmpDir, "ready")
	customContent := "OK\nversion=1.0.0\n"

	cfg := &config.ReadinessConfig{
		Enabled: true,
		Path:    readinessPath,
		Mode:    "all_running",
		Content: customContent,
	}
	logger := newTestLogger()
	m := NewManager(cfg, logger)

	_ = m.Start(context.Background())
	m.SetTrackedProcesses([]string{"test"})
	m.UpdateProcessState("test", StateRunning, "healthy")

	// Read the file content
	content, err := os.ReadFile(readinessPath)
	if err != nil {
		t.Fatalf("Failed to read readiness file: %v", err)
	}

	if string(content) != customContent {
		t.Errorf("Expected content %q, got %q", customContent, string(content))
	}
}

func TestManager_DefaultContent(t *testing.T) {
	tmpDir := t.TempDir()
	readinessPath := filepath.Join(tmpDir, "ready")

	cfg := &config.ReadinessConfig{
		Enabled: true,
		Path:    readinessPath,
		Mode:    "all_running",
		Content: "", // Empty = use default
	}
	logger := newTestLogger()
	m := NewManager(cfg, logger)

	_ = m.Start(context.Background())
	m.SetTrackedProcesses([]string{"test"})
	m.UpdateProcessState("test", StateRunning, "healthy")

	// Read the file content - should contain "ready" and "timestamp="
	content, err := os.ReadFile(readinessPath)
	if err != nil {
		t.Fatalf("Failed to read readiness file: %v", err)
	}

	contentStr := string(content)
	if len(contentStr) == 0 {
		t.Error("Expected non-empty default content")
	}
	if !contains(contentStr, "ready") {
		t.Error("Expected default content to contain 'ready'")
	}
	if !contains(contentStr, "timestamp=") {
		t.Error("Expected default content to contain 'timestamp='")
	}
}

func TestManager_GetStatus(t *testing.T) {
	cfg := &config.ReadinessConfig{
		Enabled: true,
		Path:    "/tmp/test-ready",
		Mode:    "all_healthy",
	}
	logger := newTestLogger()
	m := NewManager(cfg, logger)

	m.SetTrackedProcesses([]string{"php-fpm", "nginx"})
	m.UpdateProcessState("php-fpm", StateRunning, "healthy")
	m.UpdateProcessState("nginx", StateHealthy, "healthy")

	status := m.GetStatus()

	// Verify it's a copy, not the original map
	status["test"] = ProcessStatus{Name: "test"}
	originalStatus := m.GetStatus()
	if _, exists := originalStatus["test"]; exists {
		t.Error("GetStatus should return a copy, not the original map")
	}

	// Verify correct values
	if status["php-fpm"].State != StateRunning {
		t.Errorf("Expected php-fpm state %q, got %q", StateRunning, status["php-fpm"].State)
	}
	if status["nginx"].State != StateHealthy {
		t.Errorf("Expected nginx state %q, got %q", StateHealthy, status["nginx"].State)
	}
}

func TestManager_IsReady_ThreadSafe(t *testing.T) {
	tmpDir := t.TempDir()
	readinessPath := filepath.Join(tmpDir, "ready")

	cfg := &config.ReadinessConfig{
		Enabled: true,
		Path:    readinessPath,
		Mode:    "all_running",
	}
	logger := newTestLogger()
	m := NewManager(cfg, logger)

	_ = m.Start(context.Background())
	m.SetTrackedProcesses([]string{"test"})

	// Concurrent access should not panic
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			m.UpdateProcessState("test", StateRunning, "healthy")
			m.UpdateProcessState("test", StateStopped, "unknown")
		}
		close(done)
	}()

	for i := 0; i < 100; i++ {
		_ = m.IsReady()
		_ = m.GetStatus()
	}

	<-done
}

func TestManager_Start_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	nestedPath := filepath.Join(tmpDir, "nested", "dir", "ready")

	cfg := &config.ReadinessConfig{
		Enabled: true,
		Path:    nestedPath,
		Mode:    "all_running",
	}
	logger := newTestLogger()
	m := NewManager(cfg, logger)

	err := m.Start(context.Background())
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Directory should exist
	dir := filepath.Dir(nestedPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("Expected directory to be created")
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestManager_Start_MkdirError(t *testing.T) {
	// Use a path that cannot be created (inside a file, not a directory)
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "file")

	// Create a regular file
	if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	// Try to create a readiness file inside that file (impossible)
	readinessPath := filepath.Join(filePath, "subdir", "ready")

	cfg := &config.ReadinessConfig{
		Enabled: true,
		Path:    readinessPath,
		Mode:    "all_running",
	}
	logger := newTestLogger()
	m := NewManager(cfg, logger)

	err := m.Start(context.Background())
	if err == nil {
		t.Error("Expected error when MkdirAll fails")
	}
}

func TestManager_CreateReadinessFile_Error(t *testing.T) {
	tmpDir := t.TempDir()
	dirPath := filepath.Join(tmpDir, "readonly_dir")

	// Create a directory and make it read-only
	if err := os.Mkdir(dirPath, 0555); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}
	defer func() { _ = os.Chmod(dirPath, 0755) }() // Restore permissions for cleanup

	readinessPath := filepath.Join(dirPath, "ready")

	cfg := &config.ReadinessConfig{
		Enabled: true,
		Path:    readinessPath,
		Mode:    "all_running",
	}
	logger := newTestLogger()
	m := NewManager(cfg, logger)

	// Start will work (directory already exists)
	// Manually set tracked processes and update state to trigger createReadinessFile
	m.SetTrackedProcesses([]string{"test"})

	// This should trigger createReadinessFile which will fail due to read-only directory
	// It logs an error but doesn't return it
	m.UpdateProcessState("test", StateRunning, "healthy")

	// The manager should still report ready state internally
	// even though the file couldn't be created
	// (error is logged, not propagated)
}

func TestManager_RemoveReadinessFile_WhileReady(t *testing.T) {
	tmpDir := t.TempDir()
	readinessPath := filepath.Join(tmpDir, "ready")

	cfg := &config.ReadinessConfig{
		Enabled: true,
		Path:    readinessPath,
		Mode:    "all_running",
	}
	logger := newTestLogger()
	m := NewManager(cfg, logger)

	_ = m.Start(context.Background())
	m.SetTrackedProcesses([]string{"test"})

	// Make it ready first (creates the file)
	m.UpdateProcessState("test", StateRunning, "healthy")
	if !m.IsReady() {
		t.Fatal("Expected to be ready")
	}

	// Verify file exists
	if _, err := os.Stat(readinessPath); os.IsNotExist(err) {
		t.Fatal("Expected readiness file to exist")
	}

	// Now update to make it not ready (removes the file while isReady is true)
	// This exercises the "Container is not ready" log path in removeReadinessFile
	m.UpdateProcessState("test", StateStopped, "unknown")

	if m.IsReady() {
		t.Error("Expected not ready")
	}

	// File should be removed
	if _, err := os.Stat(readinessPath); !os.IsNotExist(err) {
		t.Error("Expected readiness file to be removed")
	}
}

func TestManager_RemoveReadinessFile_Error(t *testing.T) {
	tmpDir := t.TempDir()
	dirPath := filepath.Join(tmpDir, "protected_dir")
	readinessPath := filepath.Join(dirPath, "ready")

	// Create a directory
	if err := os.Mkdir(dirPath, 0755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}

	cfg := &config.ReadinessConfig{
		Enabled: true,
		Path:    readinessPath,
		Mode:    "all_running",
	}
	logger := newTestLogger()
	m := NewManager(cfg, logger)

	_ = m.Start(context.Background())
	m.SetTrackedProcesses([]string{"test"})

	// Make it ready (creates the file)
	m.UpdateProcessState("test", StateRunning, "healthy")

	// Make directory read-only to prevent file removal
	if err := os.Chmod(dirPath, 0555); err != nil {
		t.Fatalf("Failed to make directory read-only: %v", err)
	}
	defer func() { _ = os.Chmod(dirPath, 0755) }() // Restore for cleanup

	// Try to make it not ready - this will try to remove the file and fail
	// The error is logged but not propagated
	m.UpdateProcessState("test", StateStopped, "unknown")
}

func TestManager_TrackAllProcesses_NoFilter(t *testing.T) {
	tmpDir := t.TempDir()
	readinessPath := filepath.Join(tmpDir, "ready")

	cfg := &config.ReadinessConfig{
		Enabled:   true,
		Path:      readinessPath,
		Mode:      "all_running",
		Processes: []string{}, // Empty = track all
	}
	logger := newTestLogger()
	m := NewManager(cfg, logger)

	_ = m.Start(context.Background())

	// Start with empty tracked processes but no filter
	// Any process should be added to tracking
	m.UpdateProcessState("php-fpm", StateRunning, "healthy")
	m.UpdateProcessState("nginx", StateRunning, "healthy")

	status := m.GetStatus()
	if len(status) != 2 {
		t.Errorf("Expected 2 processes tracked, got %d", len(status))
	}
}
