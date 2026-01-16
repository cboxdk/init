package schedule

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/logger"
)

// ProcessExecutor executes scheduled process commands directly
type ProcessExecutor struct {
	configs    map[string]ProcessConfig         // Process name -> config
	logWriters map[string]*logger.ProcessWriter // Process name -> combined log writer
	logger     *slog.Logger
	mu         sync.RWMutex
}

// ProcessConfig contains the configuration needed to execute a scheduled process
type ProcessConfig struct {
	Command    []string
	WorkingDir string
	Env        map[string]string
	Timeout    time.Duration         // 0 = no timeout
	Logging    *config.LoggingConfig // Logging configuration for output capture
}

// NewProcessExecutor creates a new ProcessExecutor
func NewProcessExecutor(log *slog.Logger) *ProcessExecutor {
	return &ProcessExecutor{
		configs:    make(map[string]ProcessConfig),
		logWriters: make(map[string]*logger.ProcessWriter),
		logger:     log.With("component", "process_executor"),
	}
}

// RegisterProcess registers a process configuration for execution
// Creates a ProcessWriter for log capture if logging config is provided
func (e *ProcessExecutor) RegisterProcess(name string, cfg ProcessConfig) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.configs[name] = cfg

	// Create ProcessWriter for log capture
	// Use "scheduled" as the instance ID since scheduled jobs run one at a time
	pw, err := logger.NewProcessWriter(e.logger, name, "scheduled", "combined", cfg.Logging)
	if err != nil {
		return fmt.Errorf("failed to create log writer for %s: %w", name, err)
	}
	e.logWriters[name] = pw

	e.logger.Debug("registered process for scheduling", "name", name, "command", cfg.Command)
	return nil
}

// UnregisterProcess removes a process configuration
func (e *ProcessExecutor) UnregisterProcess(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	delete(e.configs, name)
	delete(e.logWriters, name)
	e.logger.Debug("unregistered process from scheduling", "name", name)
}

// Execute runs the process and returns when complete
func (e *ProcessExecutor) Execute(ctx context.Context, processName string) (int, error) {
	cfg, logWriter, err := e.getProcessConfig(processName)
	if err != nil {
		return -1, err
	}

	// Apply timeout if configured
	execCtx, cancel := e.applyTimeout(ctx, cfg.Timeout)
	if cancel != nil {
		defer cancel()
	}

	// Setup and configure command
	cmd := e.setupCommand(execCtx, processName, cfg, logWriter)

	e.logger.Info("executing scheduled process",
		"process", processName,
		"command", cfg.Command,
	)

	if logWriter != nil {
		logWriter.AddEvent("▶ Process started")
	}

	// Run the command
	startTime := time.Now()
	runErr := cmd.Run()
	duration := time.Since(startTime)

	if logWriter != nil {
		logWriter.Flush()
	}

	// Handle execution result
	return e.handleExecutionResult(ctx, processName, cfg, logWriter, runErr, duration)
}

// getProcessConfig retrieves and validates the process configuration.
func (e *ProcessExecutor) getProcessConfig(processName string) (ProcessConfig, *logger.ProcessWriter, error) {
	e.mu.RLock()
	cfg, exists := e.configs[processName]
	logWriter := e.logWriters[processName]
	e.mu.RUnlock()

	if !exists {
		return ProcessConfig{}, nil, fmt.Errorf("process %q not registered", processName)
	}
	if len(cfg.Command) == 0 {
		return ProcessConfig{}, nil, fmt.Errorf("process %q has no command", processName)
	}
	return cfg, logWriter, nil
}

// applyTimeout applies a timeout to the context if configured.
func (e *ProcessExecutor) applyTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(ctx, timeout)
	}
	return ctx, nil
}

// setupCommand creates and configures the command for execution.
func (e *ProcessExecutor) setupCommand(ctx context.Context, processName string, cfg ProcessConfig, logWriter *logger.ProcessWriter) *exec.Cmd {
	cmd := exec.CommandContext(ctx, cfg.Command[0], cfg.Command[1:]...)

	if cfg.WorkingDir != "" {
		cmd.Dir = cfg.WorkingDir
	}

	// Set environment
	cmd.Env = os.Environ()
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = append(cmd.Env, fmt.Sprintf("CBOX_INIT_PROCESS=%s", processName))
	cmd.Env = append(cmd.Env, "CBOX_INIT_SCHEDULED=true")

	// Setup output writers
	if logWriter != nil {
		cmd.Stdout = io.MultiWriter(os.Stdout, logWriter)
		cmd.Stderr = io.MultiWriter(os.Stderr, logWriter)
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	return cmd
}

// handleExecutionResult processes the command execution result and returns the exit code.
func (e *ProcessExecutor) handleExecutionResult(ctx context.Context, processName string, cfg ProcessConfig, logWriter *logger.ProcessWriter, err error, duration time.Duration) (int, error) {
	if err != nil {
		return e.handleExecutionError(ctx, processName, cfg, logWriter, err, duration)
	}

	// Success case
	if logWriter != nil {
		logWriter.AddEvent(fmt.Sprintf("✓ Process exited (code: 0, duration: %s)", duration.Round(time.Millisecond)))
	}
	e.logger.Info("scheduled process completed",
		"process", processName,
		"exit_code", 0,
		"duration", duration,
	)
	return 0, nil
}

// handleExecutionError handles different error types from command execution.
func (e *ProcessExecutor) handleExecutionError(ctx context.Context, processName string, cfg ProcessConfig, logWriter *logger.ProcessWriter, err error, duration time.Duration) (int, error) {
	// Check for exit error with code
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode := 1
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			exitCode = status.ExitStatus()
		}
		if logWriter != nil {
			logWriter.AddEvent(fmt.Sprintf("✗ Process failed (code: %d, duration: %s)", exitCode, duration.Round(time.Millisecond)))
		}
		e.logger.Info("scheduled process completed",
			"process", processName,
			"exit_code", exitCode,
			"duration", duration,
		)
		return exitCode, fmt.Errorf("process exited with code %d", exitCode)
	}

	// Check for timeout
	if ctx.Err() == context.DeadlineExceeded {
		if logWriter != nil {
			logWriter.AddEvent(fmt.Sprintf("⏱ Process timed out after %v", cfg.Timeout))
		}
		e.logger.Error("scheduled process timed out",
			"process", processName,
			"timeout", cfg.Timeout,
			"duration", duration,
		)
		return -1, fmt.Errorf("process timed out after %v", cfg.Timeout)
	}

	// Check for cancellation
	if ctx.Err() == context.Canceled {
		if logWriter != nil {
			logWriter.AddEvent("⊘ Process cancelled")
		}
		e.logger.Warn("scheduled process cancelled",
			"process", processName,
			"duration", duration,
		)
		return -1, fmt.Errorf("process cancelled")
	}

	// Other errors (failed to start)
	if logWriter != nil {
		logWriter.AddEvent(fmt.Sprintf("✗ Process failed to start: %v", err))
	}
	e.logger.Error("scheduled process failed to start",
		"process", processName,
		"error", err,
	)
	return -1, fmt.Errorf("failed to start process: %w", err)
}

// GetLogs returns log entries for a scheduled process
// If limit > 0, returns only the most recent 'limit' entries
func (e *ProcessExecutor) GetLogs(processName string, limit int) []logger.LogEntry {
	e.mu.RLock()
	pw, exists := e.logWriters[processName]
	e.mu.RUnlock()

	if !exists || pw == nil {
		return []logger.LogEntry{}
	}

	var logs []logger.LogEntry
	if limit > 0 {
		logs = pw.GetRecentLogs(limit)
	} else {
		logs = pw.GetLogs()
	}

	// Sort by timestamp (newest first)
	sort.Slice(logs, func(i, j int) bool {
		return logs[i].Timestamp.After(logs[j].Timestamp)
	})

	return logs
}

// HasProcess checks if a process is registered with the executor
func (e *ProcessExecutor) HasProcess(processName string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, exists := e.configs[processName]
	return exists
}
