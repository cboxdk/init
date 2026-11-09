package snapshot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Control is cbox-init's end of the channel to the node agent. *Client
// implements it; tests replace it.
type Control interface {
	Checkpoint(ctx context.Context) error
	Restore(ctx context.Context) error
}

// Workload is the process manager, seen from the warm tier. The container is
// the unit: a checkpoint stops every eligible process at once, because a
// workload with nginx dumped and php-fpm still running is neither asleep nor
// awake.
type Workload interface {
	// BeginCheckpoint marks the exits about to happen as intended rather than
	// as crashes, and returns the PIDs to dump.
	BeginCheckpoint() []int
	// CompleteCheckpoint reaps the dumped tree and settles its state, once the
	// dump has actually finished.
	CompleteCheckpoint()
	// AbortCheckpoint undoes the intent when the dump failed.
	AbortCheckpoint()
	// EndCheckpoint puts restored processes back into ordinary supervision.
	EndCheckpoint()
	// IsCheckpointed reports whether everything is currently checkpointed.
	IsCheckpointed() bool
	// ColdStart replaces checkpointed processes with fresh ones. The in-memory
	// state is gone; this is recovery, not repair.
	ColdStart(ctx context.Context) error
}

// Coordinator decides when the workload sleeps and what happens when any part
// of that fails.
//
// It sits between three things that each fail differently — the proxy holding
// the service port, the supervisor owning the processes, and the agent owning
// CRIU — and its job is to make sure none of those failures ends with the
// customer's state silently gone or a connection hanging forever. The
// happy path is four lines; the rest of this file is the other paths.
type Coordinator struct {
	quiesce   func(context.Context) error
	fence     func(error)
	uncertain error // guarded by opMu; no further lifecycle actions are safe
	control   Control
	workload  Workload
	proxy     *Proxy
	log       *slog.Logger

	idle time.Duration
	poll time.Duration

	// opMu makes sleeping and waking mutually exclusive. Without it a
	// connection can arrive between "the workload is idle" and "the dump has
	// started", be served against a process CRIU is about to freeze, and have
	// its socket dumped mid-request.
	opMu sync.Mutex

	mu       sync.Mutex
	failures int
	// abandoned counts restores of present checkpoints the agent gave up on
	// since the last successful restore. The first earns one fresh checkpoint;
	// the second turns the warm tier off.
	abandoned     int
	disabled      string // human explanation, "" while the warm tier is on
	disabledCause string // Cause* code matching disabled
	lastRestore   string // LastRestoreRestored or LastRestoreColdStart
	// current is the outcome of the most recent operation when it was not a
	// plain success, and nil after one. last keeps the most recent non-success
	// outcome for history even after a later success.
	current *Event
	last    *Event
}

// CoordinatorOptions configures a Coordinator.
type CoordinatorOptions struct {
	// Quiesce checks application work after proxy admission has been closed.
	// It must honor its context and must not submit work through the closed proxy.
	// A refusal keeps the workload awake and does not count as a failed dump.
	Quiesce func(context.Context) error
	// Fence must terminate container PID 1 on an ambiguous in-flight outcome.
	// If absent, the proxy stays closed and the coordinator never cold-starts.
	Fence    func(error)
	Control  Control
	Workload Workload
	Proxy    *Proxy
	Log      *slog.Logger

	// Idle is how long the workload must be idle before it is checkpointed.
	// Zero or negative disables sleeping entirely — never the reverse.
	Idle time.Duration
	// Poll is how often idleness is evaluated.
	Poll time.Duration
}

// maxCheckpointFailures is how many dumps may fail before this container stops
// trying to sleep.
//
// Bounded on purpose. The prior art retried every 200 ms and buried the real
// cause — MPTCP, in our own first run — under its own log storm. The agent
// gives up on its side too; this is the same decision taken independently,
// because the container must not depend on the agent to stop asking.
const maxCheckpointFailures = 3

// DefaultPoll is how often idleness is evaluated when unset.
const DefaultPoll = 5 * time.Second

// NewCoordinator builds a Coordinator.
func NewCoordinator(opts CoordinatorOptions) *Coordinator {
	c := &Coordinator{
		quiesce:  opts.Quiesce,
		fence:    opts.Fence,
		control:  opts.Control,
		workload: opts.Workload,
		proxy:    opts.Proxy,
		log:      opts.Log,
		idle:     opts.Idle,
		poll:     opts.Poll,
	}
	if c.log == nil {
		c.log = slog.Default()
	}
	if c.poll <= 0 {
		c.poll = DefaultPoll
	}
	return c
}

// Run evaluates idleness until ctx is cancelled.
func (c *Coordinator) Run(ctx context.Context) {
	if c.idle <= 0 {
		c.log.Info("warm tier idle timeout not set; the workload will not sleep")
		return
	}

	ticker := time.NewTicker(c.poll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.maybeSleep(ctx)
		}
	}
}

// Wake restores the workload. It is what the proxy calls when a connection
// arrives while the workload is asleep, and it returns only when there is
// something to proxy to — or an error, promptly.
//
// "Or an error, promptly" is the requirement that shapes the rest. The client
// is sitting on a connection cbox-init accepted specifically so that its
// handshake would not have to wait, and every path out of here has to end
// within the caller's deadline. A hang here is a hung request.
func (c *Coordinator) Wake(ctx context.Context) error {
	// Waits for an in-flight dump rather than racing it. The wait is bounded by
	// the caller's deadline, which is the same deadline the whole wake is held
	// to, so this cannot become a hang of its own.
	if err := c.lockOperation(ctx); err != nil {
		return err
	}
	defer c.opMu.Unlock()
	if c.uncertain != nil {
		return c.uncertain
	}

	// Single-flight: every connection that observed "asleep" queues here, so by
	// the time this one gets the lock the workload has very likely already been
	// restored by the first. Restoring again would ask the agent to restore an
	// already-running tree — and if it answers "images missing" (there are none
	// once a restore consumed them) that would cold start a perfectly healthy
	// workload, destroying its state. The same guard covers the maybeSleep
	// back-out path, where a connection can queue while the workload was marked
	// asleep but never actually dumped.
	// Asleep is the authoritative signal: a successful restore clears it, and
	// every maybeSleep back-out path clears it too — so "not asleep" means
	// either someone already restored the tree or it was never dumped.
	if !c.proxy.Asleep() {
		c.log.Debug("wake: workload is already running, nothing to restore")
		return nil
	}

	// The caller's deadline may have expired while queuing. Bail before touching
	// the control channel: a write on an expired context fails the channel and
	// latches it broken for everyone.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("wake abandoned before restore: %w", err)
	}

	err := c.control.Restore(ctx)
	if err == nil {
		// The tree is back, at the PIDs CRIU recorded, re-parented onto
		// cbox-init. Nothing is watching it until this is called, and a
		// workload nobody watches can neither sleep again nor be noticed when
		// it dies.
		c.workload.EndCheckpoint()
		c.proxy.SetAsleep(false)
		c.mu.Lock()
		c.abandoned = 0
		c.current = nil
		c.lastRestore = LastRestoreRestored
		c.mu.Unlock()
		return nil
	}

	if errors.Is(err, ErrOperationUncertain) {
		return c.fenceUncertain("restore", err)
	}

	var agentErr *AgentError
	isAgent := errors.As(err, &agentErr)

	// The caller gave up before the request was sent. Nothing happened to the
	// checkpoint and the next connection restores it; that is not a failure
	// of the warm tier and is not reported as one.
	if !isAgent && !errors.Is(err, ErrControlLost) &&
		(errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		c.log.Debug("wake abandoned by its caller before the restore was sent; the workload stays asleep", "error", err)
		return fmt.Errorf("waking the workload: %w", err)
	}

	switch {
	case isAgent && agentErr.Reason == ReasonImagesMissing:
		// The normal case after a node change: there is simply nothing here to
		// restore. Cold start, keep sleeping, take a fresh checkpoint next time.
		return c.coldStartAfter(ctx, newEvent("restore", CauseCheckpointMissing, LevelInfo,
			"no checkpoint images on this node to restore (the node was replaced or rebooted, the images were swept, or the pod moved); cold started the workload in place and warm sleep stays on, so the next sleep takes a fresh checkpoint",
			err), "", "")

	case isAgent && agentErr.Reason == ReasonCheckpointIncompatible:
		return c.coldStartAfter(ctx, newEvent("restore", CauseCheckpointIncompatible, LevelWarning,
			fmt.Sprintf("the checkpoint on this node cannot be loaded here (%s); cold started the workload in place and warm sleep stays on, so the next sleep takes a checkpoint compatible with this node",
				agentDiagnosis(err)),
			err), "", "")

	case isAgent && agentErr.Reason == ReasonRestoreAbandoned:
		c.mu.Lock()
		c.abandoned++
		abandoned := c.abandoned
		c.mu.Unlock()
		if abandoned < 2 {
			ev := newEvent("restore", CauseRestoreAbandoned, LevelError,
				fmt.Sprintf("restoring the checkpoint failed and the node agent gave up on it (%s); cold started the workload in place. Warm sleep stays on for one fresh checkpoint; if that one fails to restore too, warm sleep turns off",
					agentDiagnosis(err)),
				err)
			ev.Attempt = abandoned
			return c.coldStartAfter(ctx, ev, "", "")
		}
		message := fmt.Sprintf("restoring a checkpoint failed again after a fresh checkpoint (%s); cold started the workload in place and turned warm sleep off for this container so it stops losing state",
			agentDiagnosis(err))
		ev := newEvent("restore", CauseRestoreAbandoned, LevelError, message, err)
		ev.Attempt = abandoned
		return c.coldStartAfter(ctx, ev, CauseRestoreAbandoned, message)

	case errors.Is(err, ErrControlLost):
		// The request was never sent, so no remote operation can race the
		// in-place recovery. Sent-but-unanswered requests took the fencing
		// path above and never reach this.
		message := fmt.Sprintf("the control channel to the node agent is gone (%v), so the checkpoint cannot be restored; cold started the workload in place and turned warm sleep off for this container", err)
		return c.coldStartAfter(ctx, newEvent("restore", CauseControlLost, LevelError, message, err),
			CauseControlLost, message)

	case isAgent && agentErr.Reason == ReasonRestoreFailed:
		// Still asleep, images still on disk, and the agent will try again on
		// the next connection. The caller fails this one request rather than
		// throwing away a checkpoint that may well load.
		c.record(newEvent("restore", CauseRestoreFailed, LevelError,
			fmt.Sprintf("restoring the checkpoint failed (%s); the images are kept, the workload stays asleep and the next connection retries",
				agentDiagnosis(err)),
			err))
		return fmt.Errorf("waking the workload: %w", err)

	case isAgent:
		c.record(newEvent("restore", CauseAgentRefused, LevelError,
			fmt.Sprintf("the node agent refused the restore with a reason this cbox-init does not know (%s); the workload stays asleep and the next connection retries",
				agentDiagnosis(err)),
			err))
		return fmt.Errorf("waking the workload: %w", err)

	default:
		c.record(newEvent("restore", CauseRestoreFailed, LevelError,
			fmt.Sprintf("restoring the checkpoint failed: %v; the workload stays asleep and the next connection retries", err),
			err))
		return fmt.Errorf("waking the workload: %w", err)
	}
}

// coldStartAfter records why the checkpoint cannot come back, starts the
// workload again in place, and turns the warm tier off only when disableCause
// is set. Runs with opMu held.
func (c *Coordinator) coldStartAfter(ctx context.Context, ev Event, disableCause, disableMessage string) error {
	c.record(ev)

	if err := c.workload.ColdStart(ctx); err != nil {
		message := fmt.Sprintf("cold starting the workload after %s failed: %v; warm sleep is off for this container and the supervisor's restart policy takes over", ev.Cause, err)
		c.record(newEvent("restore", CauseColdStartFailed, LevelError, message, nil))
		c.disable(CauseColdStartFailed, message)
		return fmt.Errorf("cold starting after %s: %w", ev.Cause, err)
	}

	// Marked awake here rather than only by the caller. The cold start can
	// legitimately finish after the caller's deadline has passed — a wedged
	// CRIU can burn the whole wake budget before anyone gives up on it — and if
	// only the caller cleared the flag, the workload would be running with the
	// proxy still convinced it is asleep, sending every subsequent connection
	// into a restore that can no longer succeed.
	c.proxy.SetAsleep(false)
	c.mu.Lock()
	c.lastRestore = LastRestoreColdStart
	c.mu.Unlock()

	if disableCause != "" {
		c.disable(disableCause, disableMessage)
	}
	return nil
}

func (c *Coordinator) maybeSleep(ctx context.Context) {
	_ = c.sleep(ctx, false)
}

func (c *Coordinator) sleep(ctx context.Context, requested bool) error {
	if err := c.lockOperation(ctx); err != nil {
		return err
	}
	defer c.opMu.Unlock()

	if c.uncertain != nil {
		return c.uncertain
	}
	if reason := c.Disabled(); reason != "" {
		return fmt.Errorf("%w: warm sleep is off for this container: %s", ErrSleepUnavailable, reason)
	}
	if c.idle <= 0 {
		return fmt.Errorf("%w: no idle timeout is configured", ErrSleepUnavailable)
	}
	if c.proxy.Asleep() {
		return nil
	}
	if !requested && !c.proxy.Activity().IdleFor(c.idle) {
		return fmt.Errorf("%w: not idle for %s yet", ErrWorkloadBusy, c.idle)
	}

	// Asleep before anything else, and idleness confirmed again afterwards.
	//
	// The order is the whole race. Between deciding the workload is idle and
	// the dump actually starting, a connection can arrive; if the proxy still
	// believes the workload is awake it will splice that connection straight
	// into a process CRIU is about to freeze, and the request is dumped
	// mid-flight. Marking asleep first sends any such connection down the wake
	// path instead, where it waits on the same lock this holds and restores the
	// moment the dump finishes — the correct outcome for a workload that went
	// idle and was immediately wanted again. The second check then catches
	// anything that got in just before the flag did.
	c.proxy.SetAsleep(true)

	if open, inFlight := c.proxy.Activity().Counts(); open > 0 || inFlight > 0 {
		c.proxy.SetAsleep(false)
		return busy(open, inFlight)
	}

	if c.quiesce != nil {
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := c.quiesce(checkCtx)
		contextErr := checkCtx.Err()
		cancel()
		if err != nil || contextErr != nil {
			c.proxy.SetAsleep(false)
			if err == nil {
				err = contextErr
			}
			c.log.Debug("checkpoint deferred: the application has not confirmed it is idle", "error", err)
			if err := ctx.Err(); err != nil {
				return err
			}
			// The cause is kept as text, not wrapped: a quiescence timeout is a
			// busy workload, and must not read as the caller's own deadline.
			return fmt.Errorf("%w: the application has not confirmed it is idle: %s", ErrWorkloadBusy, err.Error())
		}
	}
	// A customer may have queued while the status request was in flight.
	// Restore ordinary admission rather than dumping a now-wanted workload.
	if open, inFlight := c.proxy.Activity().Counts(); ctx.Err() != nil || open > 0 || inFlight > 0 {
		c.proxy.SetAsleep(false)
		if err := ctx.Err(); err != nil {
			return err
		}
		return busy(open, inFlight)
	}

	pids := c.workload.BeginCheckpoint()
	if len(pids) == 0 {
		// Nothing running to dump. Not a failure, and not worth a log line
		// every poll: a container whose processes have all exited is already
		// being handled by the supervisor.
		c.proxy.SetAsleep(false)
		return fmt.Errorf("%w: no supervised process is running to checkpoint", ErrSleepUnavailable)
	}

	if err := c.control.Checkpoint(ctx); err != nil {
		if errors.Is(err, ErrOperationUncertain) {
			return c.fenceUncertain("checkpoint", err)
		}
		// An explicit dump refusal or an unsent command leaves ordinary
		// supervision in charge. Unknown sent outcomes were fenced above.
		c.proxy.SetAsleep(false)
		c.workload.AbortCheckpoint()

		var agentErr *AgentError
		if !errors.As(err, &agentErr) && !errors.Is(err, ErrControlLost) &&
			(errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			// The caller gave up before the request was sent. The agent never
			// saw it, so it is not a failed dump and does not count as one.
			c.log.Debug("checkpoint abandoned by its caller before it was sent; the workload keeps running", "error", err)
			return fmt.Errorf("checkpointing the workload: %w", err)
		}
		c.checkpointFailed(err)
		return fmt.Errorf("checkpointing the workload: %w", err)
	}

	// Immediately, and before anything can ask for a restore: the dumped tree
	// is a set of zombies holding exactly the PIDs the restore will need.
	c.workload.CompleteCheckpoint()

	c.mu.Lock()
	c.failures = 0
	c.current = nil
	c.mu.Unlock()

	open, inFlight := c.proxy.Activity().Counts()
	c.log.Info("workload checkpointed", "pids", pids, "open_connections", open, "in_flight", inFlight)
	return nil
}

// busy explains a refusal to sleep because of traffic.
func busy(open, inFlight int) error {
	return fmt.Errorf("%w: %d open connections, %d requests in flight", ErrWorkloadBusy, open, inFlight)
}

// checkpointFailed records a failed dump, says why, and turns the warm tier
// off when retrying cannot help or has stopped helping.
func (c *Coordinator) checkpointFailed(err error) {
	var agentErr *AgentError
	isAgent := errors.As(err, &agentErr)

	if errors.Is(err, ErrControlLost) {
		// Nothing to retry against. The workload is awake and stays awake,
		// which is the failure this whole design prefers.
		message := fmt.Sprintf("the control channel to the node agent is gone (%v); the workload keeps running and warm sleep is off for this container", err)
		c.record(newEvent("checkpoint", CauseControlLost, LevelError, message, err))
		c.disable(CauseControlLost, message)
		return
	}

	if isAgent && agentErr.Reason == ReasonCheckpointDisabled {
		// The agent answers every later request the same way, so asking again
		// only adds log lines. Say so once and stop.
		message := fmt.Sprintf("the node agent no longer accepts checkpoints for this container (%s). It does this after it abandoned a checkpoint, after repeated dump failures, or when admission was revoked; the workload keeps running and warm sleep is off",
			agentDiagnosis(err))
		c.mu.Lock()
		previous := c.last
		c.mu.Unlock()
		if previous != nil && previous.Cause == CauseCheckpointMissing {
			message += ". It followed a missing checkpoint, which is not a failure: a node agent that disables warm sleep for that needs updating"
		}
		c.record(newEvent("checkpoint", CauseAgentDisabled, LevelError, message, err))
		c.disable(CauseAgentDisabled, message)
		return
	}

	c.mu.Lock()
	c.failures++
	failures := c.failures
	c.mu.Unlock()

	cause := CauseCheckpointFailed
	if isAgent && agentErr.Reason != ReasonDumpFailed {
		cause = CauseAgentRefused
	}
	message := fmt.Sprintf("checkpoint failed (%s); the workload keeps running and serving, attempt %d of %d",
		agentDiagnosis(err), failures, maxCheckpointFailures)
	if failures >= maxCheckpointFailures {
		message = fmt.Sprintf("checkpoint failed %d times in a row, last: %s; the workload keeps running and warm sleep is off for this container",
			failures, agentDiagnosis(err))
	}
	ev := newEvent("checkpoint", cause, LevelError, message, err)
	ev.Attempt = failures
	c.record(ev)
	if failures >= maxCheckpointFailures {
		c.disable(CauseCheckpointFailed, message)
	}
}

// record logs an outcome at its level and makes it visible in Status.
func (c *Coordinator) record(ev Event) {
	ev.log(c.log)
	c.mu.Lock()
	c.current = &ev
	c.last = &ev
	c.mu.Unlock()
}

// disable takes this container off the warm tier, keeping the first cause:
// later failures are usually consequences of it.
func (c *Coordinator) disable(cause, message string) {
	c.mu.Lock()
	already := c.disabled != ""
	if !already {
		c.disabled = message
		c.disabledCause = cause
	}
	c.mu.Unlock()

	if !already {
		c.log.Warn("warm tier disabled for this container; it keeps running and stops sleeping",
			"cause", cause, "reason", message)
	}
}

// Disabled reports why this container is no longer offered the warm tier, or ""
// while it still is. Exposed so status output can show it: a container that
// silently stopped sleeping is the failure that survives review.
func (c *Coordinator) Disabled() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.disabled
}

// DisabledCause reports the Cause* code for Disabled, or "".
func (c *Coordinator) DisabledCause() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.disabledCause
}

// fenceUncertain runs with opMu held. Never clear the asleep gate or resume
// supervision: the node agent may still complete the operation after a timeout.
func (c *Coordinator) fenceUncertain(operation string, err error) error {
	c.uncertain = fmt.Errorf("%s requires container fencing: %w", operation, err)
	c.proxy.SetAsleep(true)
	message := fmt.Sprintf("the outcome of the %s is unknown (%v); the container is being fenced so the orchestrator restarts it cleanly", operation, err)
	c.record(newEvent(operation, CauseOperationUncertain, LevelError, message, err))
	c.disable(CauseOperationUncertain, message)
	if c.fence != nil {
		c.fence(c.uncertain)
	}
	return c.uncertain
}
