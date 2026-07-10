package process

import (
	"time"
)

// RestartPolicy defines the restart behavior for a process.
// Implementations determine whether a process should be restarted after exit
// and calculate the backoff delay between restart attempts.
//
// The process manager uses RestartPolicy to implement automatic restart
// with exponential backoff, preventing restart storms while ensuring
// services recover from transient failures.
//
// Built-in implementations:
//   - AlwaysRestartPolicy: Restart regardless of exit code
//   - OnFailureRestartPolicy: Restart only on non-zero exit codes
//   - NeverRestartPolicy: Never restart (for oneshot/batch processes)
type RestartPolicy interface {
	// ShouldRestart returns true if the process should be restarted.
	// exitCode is the process exit code, restartCount is the number of restarts so far.
	ShouldRestart(exitCode int, restartCount int) bool
	// BackoffDuration returns the delay before the next restart attempt.
	// Typically implements exponential backoff with a maximum cap.
	BackoffDuration(restartCount int) time.Duration
}

// AlwaysRestartPolicy restarts processes regardless of exit code, up to maxAttempts.
// Use this for long-running services that should always be kept running.
// Set maxAttempts to 0 for unlimited restarts.
type AlwaysRestartPolicy struct {
	maxAttempts    int
	initialBackoff time.Duration
	maxBackoff     time.Duration
}

// NewAlwaysRestartPolicy creates a new always restart policy
func NewAlwaysRestartPolicy(maxAttempts int, initial, max time.Duration) *AlwaysRestartPolicy {
	return &AlwaysRestartPolicy{
		maxAttempts:    maxAttempts,
		initialBackoff: initial,
		maxBackoff:     max,
	}
}

func (p *AlwaysRestartPolicy) ShouldRestart(exitCode int, restartCount int) bool {
	if p.maxAttempts <= 0 {
		return true
	}
	return restartCount < p.maxAttempts
}

func (p *AlwaysRestartPolicy) BackoffDuration(restartCount int) time.Duration {
	return calculateBackoff(p.initialBackoff, p.maxBackoff, restartCount)
}

// OnFailureRestartPolicy restarts processes only when they exit with a non-zero code.
// Clean exits (exit code 0) are not restarted. Use this for services where normal
// termination should be respected but crashes should trigger recovery.
type OnFailureRestartPolicy struct {
	maxAttempts    int
	initialBackoff time.Duration
	maxBackoff     time.Duration
}

// NewOnFailureRestartPolicy creates a new on-failure restart policy
func NewOnFailureRestartPolicy(maxAttempts int, initial, max time.Duration) *OnFailureRestartPolicy {
	return &OnFailureRestartPolicy{
		maxAttempts:    maxAttempts,
		initialBackoff: initial,
		maxBackoff:     max,
	}
}

func (p *OnFailureRestartPolicy) ShouldRestart(exitCode int, restartCount int) bool {
	if exitCode == 0 {
		return false // Clean exit, don't restart
	}
	if p.maxAttempts <= 0 {
		return true
	}
	return restartCount < p.maxAttempts
}

func (p *OnFailureRestartPolicy) BackoffDuration(restartCount int) time.Duration {
	return calculateBackoff(p.initialBackoff, p.maxBackoff, restartCount)
}

// NeverRestartPolicy never restarts processes regardless of exit code.
// Use this for oneshot processes, batch jobs, and scheduled tasks that
// should run once and exit.
type NeverRestartPolicy struct{}

// NewNeverRestartPolicy creates a new never restart policy
func NewNeverRestartPolicy() *NeverRestartPolicy {
	return &NeverRestartPolicy{}
}

func (p *NeverRestartPolicy) ShouldRestart(exitCode int, restartCount int) bool {
	return false
}

func (p *NeverRestartPolicy) BackoffDuration(restartCount int) time.Duration {
	return 0
}

// NewRestartPolicy creates a restart policy based on the policy type string.
// Supported types:
//   - "always": Restart regardless of exit code (for long-running services)
//   - "on-failure": Restart only on non-zero exit codes (for services that can exit cleanly)
//   - "never" or any other value: Never restart (for oneshot/batch processes)
//
// Parameters:
//   - policyType: One of "always", "on-failure", or "never"
//   - maxAttempts: Maximum restart attempts (0 = unlimited for always/on-failure)
//   - initial: Initial backoff delay between restarts
//   - max: Maximum backoff delay (exponential backoff capped at this value)
func NewRestartPolicy(policyType string, maxAttempts int, initial, max time.Duration) RestartPolicy {
	switch policyType {
	case "always":
		return NewAlwaysRestartPolicy(maxAttempts, initial, max)
	case "on-failure":
		return NewOnFailureRestartPolicy(maxAttempts, initial, max)
	default:
		return NewNeverRestartPolicy()
	}
}

// resetRestartBudget reports whether an instance that has been up for uptime
// should have its restart counter reset to zero. An instance that stays up
// past the stability window is treated as recovered, giving it a fresh restart
// budget so occasional crashes over a long lifetime don't permanently exhaust
// MaxRestartAttempts. A window of zero (or negative) disables the reset.
func resetRestartBudget(stabilityWindow, uptime time.Duration) bool {
	return stabilityWindow > 0 && uptime >= stabilityWindow
}

func calculateBackoff(initial, max time.Duration, restartCount int) time.Duration {
	if initial <= 0 {
		initial = 1 * time.Second
	}
	// Cap restart count to prevent integer overflow (max 62 for safe bit shift)
	if restartCount < 0 {
		restartCount = 0
	}
	const maxShift = 62
	if restartCount > maxShift {
		restartCount = maxShift
	}
	delay := initial * time.Duration(1<<uint(restartCount)) // #nosec G115 -- bounds checked above
	if max > 0 && delay > max {
		return max
	}
	return delay
}
