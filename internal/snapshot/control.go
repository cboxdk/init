package snapshot

import (
	"context"
	"errors"
	"time"
)

var (
	ErrWorkloadBusy     = errors.New("workload has active or unconfirmed work")
	ErrSleepUnavailable = errors.New("sleep is not available for this workload")
)

// Status is a non-waking observation, not proof of billable compute time.
//
// Fields are only ever added. Cause, Level and Message describe the most
// recent operation when it was not a plain success, so a caller learns why
// without reading logs; they clear after the next success. LastEvent keeps the
// most recent non-success outcome even after that.
type Status struct {
	Supported   bool   `json:"supported"`
	State       string `json:"state"`
	IdleSeconds int64  `json:"idle_seconds"`
	// DisabledReason explains, in a sentence, why warm sleep is off.
	DisabledReason string `json:"disabled_reason,omitempty"`
	// DisabledCause is the Cause* code for DisabledReason.
	DisabledCause string `json:"disabled_cause,omitempty"`
	// LastRestore is "restored" or "cold_start" once a wake has happened.
	LastRestore string `json:"last_restore,omitempty"`
	// Cause is the Cause* code of the most recent operation, when it was not a
	// plain success.
	Cause string `json:"cause,omitempty"`
	// Level is info, warning or error for Cause.
	Level string `json:"level,omitempty"`
	// Message explains Cause in a sentence.
	Message string `json:"message,omitempty"`
	// LastEvent is the most recent non-success outcome with its full detail.
	LastEvent *Event `json:"last_event,omitempty"`
}

// Sleep skips the idle timer but never skips connection or application quiescence.
// It checkpoints the existing process tree; it does not scale a pod to zero.
// A new request may wake it again immediately. Repeating an already completed
// sleep is harmless; uncertain agent outcomes retain the existing fencing policy.
func (c *Coordinator) Sleep(ctx context.Context) error {
	return c.sleep(ctx, true)
}

// Status never waits behind a checkpoint and never wakes or resets activity.
func (c *Coordinator) Status() Status {
	status := Status{Supported: true, State: "transitioning", IdleSeconds: int64(c.idle / time.Second)}
	c.mu.Lock()
	status.DisabledReason = c.disabled
	status.DisabledCause = c.disabledCause
	status.LastRestore = c.lastRestore
	if c.current != nil {
		status.Cause = c.current.Cause
		status.Level = c.current.Level
		status.Message = c.current.Message
	}
	if c.last != nil {
		last := *c.last
		status.LastEvent = &last
	}
	c.mu.Unlock()
	if status.DisabledReason == "" && c.idle <= 0 {
		status.DisabledCause = CauseIdleNotConfigured
		status.DisabledReason = "no idle timeout is configured, so the workload never sleeps"
	}
	if !c.opMu.TryLock() {
		return status
	}
	defer c.opMu.Unlock()
	switch {
	case status.DisabledReason != "":
		status.State = "disabled"
	case c.proxy.Asleep() && c.workload.IsCheckpointed():
		status.State = "asleep"
	case !c.proxy.Asleep() && !c.workload.IsCheckpointed():
		status.State = "awake"
	default:
		status.State = "unknown"
	}
	return status
}

// A canceled caller must not wait indefinitely for another lifecycle operation,
// then act on a stale intention after that operation finally returns.
func (c *Coordinator) lockOperation(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.opMu.TryLock() {
		return nil
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := ctx.Err(); err != nil {
				return err
			}
			if c.opMu.TryLock() {
				return nil
			}
		}
	}
}
