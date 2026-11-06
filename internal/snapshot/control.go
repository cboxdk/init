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
type Status struct {
	Supported      bool   `json:"supported"`
	State          string `json:"state"`
	IdleSeconds    int64  `json:"idle_seconds"`
	DisabledReason string `json:"disabled_reason,omitempty"`
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
	if !c.opMu.TryLock() {
		return status
	}
	defer c.opMu.Unlock()
	status.DisabledReason = c.Disabled()
	switch {
	case status.DisabledReason != "" || c.idle <= 0:
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
