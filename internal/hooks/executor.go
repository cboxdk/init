package hooks

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/metrics"
)

// Executor executes lifecycle hooks with retry logic
type Executor struct {
	logger *slog.Logger
}

// NewExecutor creates a new hook executor
func NewExecutor(log *slog.Logger) *Executor {
	return &Executor{logger: log}
}

// Execute runs a single hook with retry logic
func (e *Executor) Execute(ctx context.Context, hook *config.Hook) error {
	return e.ExecuteWithType(ctx, hook, "unknown")
}

// ExecuteWithType runs a single hook with retry logic and records metrics with hook type
func (e *Executor) ExecuteWithType(ctx context.Context, hook *config.Hook, hookType string) error {
	e.logger.Info("Executing hook",
		"name", hook.Name,
		"type", hookType,
		"command", hook.Command,
	)

	startTime := time.Now()
	var lastErr error
	attempts := hook.Retry + 1 // Retry + initial attempt

	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			e.logger.Info("Retrying hook",
				"name", hook.Name,
				"attempt", attempt+1,
				"max_attempts", attempts,
			)

			// Wait before retry
			if hook.RetryDelay > 0 {
				select {
				case <-time.After(time.Duration(hook.RetryDelay) * time.Second):
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}

		err := e.executeOnce(ctx, hook)
		if err == nil {
			duration := time.Since(startTime).Seconds()
			e.logger.Info("Hook completed successfully", "name", hook.Name)
			metrics.RecordHookExecution(hook.Name, hookType, duration, true)
			return nil
		}

		lastErr = err
		e.logger.Warn("Hook failed",
			"name", hook.Name,
			"attempt", attempt+1,
			"error", err,
		)
	}

	duration := time.Since(startTime).Seconds()

	if hook.ContinueOnError {
		e.logger.Warn("Hook failed but continuing due to continue_on_error",
			"name", hook.Name,
			"error", lastErr,
		)
		metrics.RecordHookExecution(hook.Name, hookType, duration, false)
		return nil
	}

	metrics.RecordHookExecution(hook.Name, hookType, duration, false)
	return fmt.Errorf("hook %s failed after %d attempts: %w", hook.Name, attempts, lastErr)
}

func (e *Executor) executeOnce(ctx context.Context, hook *config.Hook) error {
	if len(hook.Command) == 0 {
		return fmt.Errorf("empty command")
	}

	// Create command with timeout
	timeout := time.Duration(hook.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second // Default timeout
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, hook.Command[0], hook.Command[1:]...)

	// Set working directory
	if hook.WorkingDir != "" {
		cmd.Dir = hook.WorkingDir
	}

	// Set environment variables
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, e.buildEnv(hook)...)

	// Capture output for logging
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command failed: %w (output: %s)", err, string(output))
	}

	if len(output) > 0 {
		e.logger.Debug("Hook output",
			"name", hook.Name,
			"output", string(output),
		)
	}

	return nil
}

func (e *Executor) buildEnv(hook *config.Hook) []string {
	env := []string{
		fmt.Sprintf("CBOX_INIT_HOOK_NAME=%s", hook.Name),
	}

	for key, value := range hook.Env {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}

	return env
}

// ExecuteSequence runs multiple hooks in order
func (e *Executor) ExecuteSequence(ctx context.Context, hooks []config.Hook) error {
	for i, hook := range hooks {
		e.logger.Debug("Executing hook in sequence",
			"index", i+1,
			"total", len(hooks),
			"name", hook.Name,
		)

		if err := e.Execute(ctx, &hook); err != nil {
			return fmt.Errorf("failed to execute hook %s: %w", hook.Name, err)
		}
	}
	return nil
}
