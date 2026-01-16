package hooks

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/config"
)

func TestExecutor_SuccessfulExecution(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	executor := NewExecutor(logger)

	hook := &config.Hook{
		Name:    "test-hook",
		Command: []string{"echo", "hello"},
		Timeout: 5,
	}

	ctx := context.Background()
	err := executor.Execute(ctx, hook)
	if err != nil {
		t.Fatalf("Expected successful execution, got error: %v", err)
	}
}

func TestExecutor_RetryLogic(t *testing.T) {
	tests := []struct {
		name           string
		retry          int
		retryDelay     int
		expectAttempts int
		shouldFail     bool
	}{
		{
			name:           "no_retry",
			retry:          0,
			retryDelay:     0,
			expectAttempts: 1,
			shouldFail:     true,
		},
		{
			name:           "retry_once",
			retry:          1,
			retryDelay:     0,
			expectAttempts: 2,
			shouldFail:     true,
		},
		{
			name:           "retry_three_times",
			retry:          3,
			retryDelay:     0,
			expectAttempts: 4,
			shouldFail:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
			executor := NewExecutor(logger)

			hook := &config.Hook{
				Name:       "failing-hook",
				Command:    []string{"false"}, // Command that always fails
				Timeout:    5,
				Retry:      tt.retry,
				RetryDelay: tt.retryDelay,
			}

			ctx := context.Background()
			startTime := time.Now()
			err := executor.Execute(ctx, hook)
			elapsed := time.Since(startTime)

			if !tt.shouldFail && err != nil {
				t.Errorf("Expected success, got error: %v", err)
			}

			if tt.shouldFail && err == nil {
				t.Errorf("Expected failure, got success")
			}

			// Verify retry delay is respected (with some tolerance)
			expectedDelay := time.Duration(tt.retry*tt.retryDelay) * time.Second
			if tt.retryDelay > 0 && elapsed < expectedDelay {
				t.Errorf("Expected delay of at least %v, got %v", expectedDelay, elapsed)
			}
		})
	}
}

func TestExecutor_RetryWithDelay(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	executor := NewExecutor(logger)

	hook := &config.Hook{
		Name:       "retry-hook",
		Command:    []string{"false"},
		Timeout:    5,
		Retry:      2,
		RetryDelay: 1, // 1 second delay between retries
	}

	ctx := context.Background()
	startTime := time.Now()
	err := executor.Execute(ctx, hook)
	elapsed := time.Since(startTime)

	if err == nil {
		t.Fatal("Expected error from failing command")
	}

	// Should have 1 initial attempt + 2 retries = 3 attempts
	// With 1 second delay between each retry = at least 2 seconds total
	if elapsed < 2*time.Second {
		t.Errorf("Expected at least 2 seconds for retries with delays, got %v", elapsed)
	}
}

func TestExecutor_TimeoutHandling(t *testing.T) {
	tests := []struct {
		name          string
		timeout       int
		command       []string
		shouldTimeout bool
		expectError   bool
	}{
		{
			name:          "short_timeout",
			timeout:       1,
			command:       []string{"sleep", "5"},
			shouldTimeout: true,
			expectError:   true,
		},
		{
			name:          "sufficient_timeout",
			timeout:       5,
			command:       []string{"echo", "quick"},
			shouldTimeout: false,
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
			executor := NewExecutor(logger)

			hook := &config.Hook{
				Name:    "timeout-test",
				Command: tt.command,
				Timeout: tt.timeout,
			}

			ctx := context.Background()
			startTime := time.Now()
			err := executor.Execute(ctx, hook)
			elapsed := time.Since(startTime)

			if tt.expectError && err == nil {
				t.Error("Expected error, got nil")
			}

			if !tt.expectError && err != nil {
				t.Errorf("Expected success, got error: %v", err)
			}

			if tt.shouldTimeout {
				// Timeout should happen around the specified duration
				expectedDuration := time.Duration(tt.timeout) * time.Second
				tolerance := 500 * time.Millisecond
				if elapsed < expectedDuration-tolerance || elapsed > expectedDuration+tolerance*2 {
					t.Errorf("Expected timeout around %v, got %v", expectedDuration, elapsed)
				}
			}
		})
	}
}

func TestExecutor_ContextCancellation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	executor := NewExecutor(logger)

	hook := &config.Hook{
		Name:    "long-running",
		Command: []string{"sleep", "10"},
		Timeout: 30,
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after 1 second
	go func() {
		time.Sleep(1 * time.Second)
		cancel()
	}()

	startTime := time.Now()
	err := executor.Execute(ctx, hook)
	elapsed := time.Since(startTime)

	if err == nil {
		t.Fatal("Expected error due to context cancellation")
	}

	// Should have stopped quickly (within 2 seconds)
	if elapsed > 2*time.Second {
		t.Errorf("Expected quick cancellation, took %v", elapsed)
	}
}

func TestExecutor_EnvironmentVariables(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	executor := NewExecutor(logger)

	// Create a temp script that prints environment variables
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "test-env.sh")
	script := `#!/bin/bash
echo "HOOK_NAME=$CBOX_INIT_HOOK_NAME"
echo "CUSTOM_VAR=$CUSTOM_VAR"
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("Failed to create test script: %v", err)
	}

	hook := &config.Hook{
		Name:    "env-test",
		Command: []string{"bash", scriptPath},
		Timeout: 5,
		Env: map[string]string{
			"CUSTOM_VAR": "test-value",
		},
	}

	ctx := context.Background()
	err := executor.Execute(ctx, hook)
	if err != nil {
		t.Fatalf("Hook execution failed: %v", err)
	}

	// Note: We can't easily verify the output without modifying the executor
	// This test verifies that env vars are passed without errors
}

func TestExecutor_ContinueOnError(t *testing.T) {
	tests := []struct {
		name            string
		continueOnError bool
		shouldReturnErr bool
	}{
		{
			name:            "fail_and_stop",
			continueOnError: false,
			shouldReturnErr: true,
		},
		{
			name:            "fail_and_continue",
			continueOnError: true,
			shouldReturnErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
			executor := NewExecutor(logger)

			hook := &config.Hook{
				Name:            "error-test",
				Command:         []string{"false"},
				Timeout:         5,
				ContinueOnError: tt.continueOnError,
			}

			ctx := context.Background()
			err := executor.Execute(ctx, hook)

			if tt.shouldReturnErr && err == nil {
				t.Error("Expected error to be returned")
			}

			if !tt.shouldReturnErr && err != nil {
				t.Errorf("Expected no error with continue_on_error, got: %v", err)
			}
		})
	}
}

func TestExecutor_SequenceExecution(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	executor := NewExecutor(logger)

	// Create temp file to track execution order
	tempDir := t.TempDir()
	orderFile := filepath.Join(tempDir, "order.txt")

	hooks := []config.Hook{
		{
			Name:    "hook-1",
			Command: []string{"bash", "-c", "echo '1' >> " + orderFile},
			Timeout: 5,
		},
		{
			Name:    "hook-2",
			Command: []string{"bash", "-c", "echo '2' >> " + orderFile},
			Timeout: 5,
		},
		{
			Name:    "hook-3",
			Command: []string{"bash", "-c", "echo '3' >> " + orderFile},
			Timeout: 5,
		},
	}

	ctx := context.Background()
	err := executor.ExecuteSequence(ctx, hooks)
	if err != nil {
		t.Fatalf("Sequence execution failed: %v", err)
	}

	// Verify execution order
	content, err := os.ReadFile(orderFile)
	if err != nil {
		t.Fatalf("Failed to read order file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 3 {
		t.Errorf("Expected 3 lines, got %d", len(lines))
	}

	expected := []string{"1", "2", "3"}
	for i, line := range lines {
		if line != expected[i] {
			t.Errorf("Expected line %d to be %s, got %s", i, expected[i], line)
		}
	}
}

func TestExecutor_SequenceFailsOnError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	executor := NewExecutor(logger)

	tempDir := t.TempDir()
	orderFile := filepath.Join(tempDir, "order.txt")

	hooks := []config.Hook{
		{
			Name:    "hook-1",
			Command: []string{"bash", "-c", "echo '1' >> " + orderFile},
			Timeout: 5,
		},
		{
			Name:    "hook-2-fail",
			Command: []string{"false"},
			Timeout: 5,
		},
		{
			Name:    "hook-3-should-not-run",
			Command: []string{"bash", "-c", "echo '3' >> " + orderFile},
			Timeout: 5,
		},
	}

	ctx := context.Background()
	err := executor.ExecuteSequence(ctx, hooks)
	if err == nil {
		t.Fatal("Expected error from failing hook in sequence")
	}

	// Verify only first hook executed
	content, err := os.ReadFile(orderFile)
	if err != nil {
		t.Fatalf("Failed to read order file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 1 || lines[0] != "1" {
		t.Errorf("Expected only first hook to execute, got: %v", lines)
	}
}

func TestExecutor_EmptyCommand(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	executor := NewExecutor(logger)

	hook := &config.Hook{
		Name:    "empty-command",
		Command: []string{},
		Timeout: 5,
	}

	ctx := context.Background()
	err := executor.Execute(ctx, hook)
	if err == nil {
		t.Fatal("Expected error for empty command")
	}

	if !strings.Contains(err.Error(), "empty command") {
		t.Errorf("Expected 'empty command' error, got: %v", err)
	}
}

func TestExecutor_WorkingDirectory(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	executor := NewExecutor(logger)

	tempDir := t.TempDir()
	testFile := "test.txt"
	testFilePath := filepath.Join(tempDir, testFile)

	hook := &config.Hook{
		Name:       "working-dir-test",
		Command:    []string{"touch", testFile},
		Timeout:    5,
		WorkingDir: tempDir,
	}

	ctx := context.Background()
	err := executor.Execute(ctx, hook)
	if err != nil {
		t.Fatalf("Hook execution failed: %v", err)
	}

	// Verify file was created in the working directory
	if _, err := os.Stat(testFilePath); os.IsNotExist(err) {
		t.Errorf("Expected file to be created in working directory: %s", testFilePath)
	}
}

func TestExecutor_DefaultTimeout(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	executor := NewExecutor(logger)

	hook := &config.Hook{
		Name:    "default-timeout",
		Command: []string{"echo", "test"},
		Timeout: 0, // Should use default 30 seconds
	}

	ctx := context.Background()
	err := executor.Execute(ctx, hook)
	if err != nil {
		t.Fatalf("Hook execution failed: %v", err)
	}
}

func TestExecutor_CommandOutput(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	executor := NewExecutor(logger)

	hook := &config.Hook{
		Name:    "output-test",
		Command: []string{"echo", "test output"},
		Timeout: 5,
	}

	ctx := context.Background()
	err := executor.Execute(ctx, hook)
	if err != nil {
		t.Fatalf("Hook execution failed: %v", err)
	}

	// Output is captured but only logged at debug level
	// This test verifies no error occurs with output
}

func TestExecutor_NonZeroExitCode(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	executor := NewExecutor(logger)

	hook := &config.Hook{
		Name:    "exit-code-test",
		Command: []string{"sh", "-c", "exit 42"},
		Timeout: 5,
	}

	ctx := context.Background()
	err := executor.Execute(ctx, hook)
	if err == nil {
		t.Fatal("Expected error for non-zero exit code")
	}

	if !strings.Contains(err.Error(), "command failed") {
		t.Errorf("Expected command failure error, got: %v", err)
	}
}

func TestExecutor_ExecuteWithType(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	executor := NewExecutor(logger)

	hook := &config.Hook{
		Name:    "typed-hook",
		Command: []string{"echo", "test"},
		Timeout: 5,
	}

	ctx := context.Background()
	err := executor.ExecuteWithType(ctx, hook, "pre_start")
	if err != nil {
		t.Fatalf("Hook execution failed: %v", err)
	}

	// Metrics should be recorded with hook type "pre_start"
	// This is verified by the metrics package
}
