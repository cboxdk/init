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
	control  Control
	workload Workload
	proxy    *Proxy
	log      *slog.Logger

	idle time.Duration
	poll time.Duration

	// opMu makes sleeping and waking mutually exclusive. Without it a
	// connection can arrive between "the workload is idle" and "the dump has
	// started", be served against a process CRIU is about to freeze, and have
	// its socket dumped mid-request.
	opMu sync.Mutex

	mu       sync.Mutex
	failures int
	disabled string
}

// CoordinatorOptions configures a Coordinator.
type CoordinatorOptions struct {
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
	c.opMu.Lock()
	defer c.opMu.Unlock()

	err := c.control.Restore(ctx)
	if err == nil {
		// The tree is back, at the PIDs CRIU recorded, re-parented onto
		// cbox-init. Nothing is watching it until this is called, and a
		// workload nobody watches can neither sleep again nor be noticed when
		// it dies.
		c.workload.EndCheckpoint()
		c.proxy.SetAsleep(false)
		return nil
	}

	c.log.Error("restore failed", "error", err)

	if !c.unrecoverable(err) {
		// Still asleep, images still on disk, and the agent will try again on
		// the next connection. The caller fails this one request rather than
		// throwing away a checkpoint that may well load.
		return fmt.Errorf("waking the workload: %w", err)
	}

	// The dumped processes are never coming back. Say what that costs before
	// doing anything about it: this is the one outcome that must never be
	// reported as a success, and the prior art's equivalent path lost state
	// without saying anything at all.
	c.log.Error("checkpoint lost; cold starting the workload. in-memory state from before the checkpoint is gone",
		"cause", err)

	if err := c.workload.ColdStart(ctx); err != nil {
		c.disable("cold start after a lost checkpoint failed")
		return fmt.Errorf("cold starting after a lost checkpoint: %w", err)
	}

	// Marked awake here rather than only by the caller. The cold start can
	// legitimately finish after the caller's deadline has passed — a wedged
	// CRIU can burn the whole wake budget before anyone gives up on it — and if
	// only the caller cleared the flag, the workload would be running with the
	// proxy still convinced it is asleep, sending every subsequent connection
	// into a restore that can no longer succeed.
	c.proxy.SetAsleep(false)
	c.disable("a checkpoint was lost")
	c.log.Warn("workload cold started and the warm tier is off for this container; it will keep running and stop sleeping")
	return nil
}

// unrecoverable reports whether the checkpointed processes can never be brought
// back, which is the only case where a cold start is the right answer.
//
// Two ways to get there. The agent can say so — it abandons a checkpoint that
// failed to load twice, and it says when the images are gone. Or the control
// channel itself can break, which means the same thing for a different reason:
// the agent is the only thing holding the images and the descriptor the
// workload's output goes back onto, so nothing else can ever restore this tree.
func (c *Coordinator) unrecoverable(err error) bool {
	var agentErr *AgentError
	if errors.As(err, &agentErr) {
		return agentErr.Lost()
	}
	return errors.Is(err, ErrControlLost)
}

func (c *Coordinator) maybeSleep(ctx context.Context) {
	c.opMu.Lock()
	defer c.opMu.Unlock()

	if c.Disabled() != "" || c.proxy.Asleep() {
		return
	}
	if !c.proxy.Activity().IdleFor(c.idle) {
		return
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
		return
	}

	pids := c.workload.BeginCheckpoint()
	if len(pids) == 0 {
		// Nothing running to dump. Not a failure, and not worth a log line
		// every poll: a container whose processes have all exited is already
		// being handled by the supervisor.
		c.proxy.SetAsleep(false)
		return
	}

	if err := c.control.Checkpoint(ctx); err != nil {
		// The workload was never touched. It is still running, still holding
		// its state, and still able to serve — the only thing that failed is
		// the sleeping.
		c.proxy.SetAsleep(false)
		c.workload.AbortCheckpoint()
		c.penalise(err)
		return
	}

	// Immediately, and before anything can ask for a restore: the dumped tree
	// is a set of zombies holding exactly the PIDs the restore will need.
	c.workload.CompleteCheckpoint()

	c.mu.Lock()
	c.failures = 0
	c.mu.Unlock()

	open, inFlight := c.proxy.Activity().Counts()
	c.log.Info("workload checkpointed", "pids", pids, "open_connections", open, "in_flight", inFlight)
}

// penalise records a failed checkpoint and gives up after enough of them.
func (c *Coordinator) penalise(err error) {
	c.mu.Lock()
	c.failures++
	failures := c.failures
	c.mu.Unlock()

	c.log.Error("checkpoint failed; the workload keeps running and serving",
		"attempt", failures, "error", err)

	if errors.Is(err, ErrControlLost) {
		// Nothing to retry against. The workload is awake and stays awake,
		// which is the failure this whole design prefers.
		c.disable("the control channel to the node agent is gone")
		return
	}
	if failures >= maxCheckpointFailures {
		c.disable("repeated checkpoint failures")
	}
}

func (c *Coordinator) disable(reason string) {
	c.mu.Lock()
	already := c.disabled != ""
	if !already {
		c.disabled = reason
	}
	c.mu.Unlock()

	if !already {
		c.log.Error("warm tier disabled for this container; it keeps running and stops sleeping",
			"reason", reason)
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
