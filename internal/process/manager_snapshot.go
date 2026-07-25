package process

import (
	"context"
	"fmt"

	"github.com/cboxdk/init/internal/snapshot"
)

// The manager's side of the warm tier.
//
// The snapshot coordinator talks to the whole container, not to one supervisor:
// a checkpoint stops every eligible process at once, because a workload with
// nginx dumped and php-fpm still running is neither asleep nor awake. So these
// methods fan out and aggregate, and the coordinator never has to know how many
// processes there are or what they are called.
//
// Oneshot processes are excluded throughout. They have already run, or are
// running exactly once, and dumping one would leave the restore replaying a
// migration.

// SetSnapshotStreams makes supervisors own their children's stdio pipes so they
// can survive a checkpoint. It must be called before Start: os/exec's choice of
// pipe is made when a process starts, and only a supervisor-owned pipe leaves a
// write end to hand back at restore.
func (m *Manager) SetSnapshotStreams(enabled bool) {
	m.mu.Lock()
	m.snapshotStreams = enabled
	m.mu.Unlock()
}

// SnapshotStreams reports whether supervisors own their children's stdio.
func (m *Manager) SnapshotStreams() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshotStreams
}

// BeginCheckpoint marks the exits about to happen as intended rather than as
// crashes, and returns the PIDs the agent should dump.
//
// It has to be called before the dump, not after: the dump stops the processes
// and the monitoring goroutine sees the exit immediately. A supervisor that
// learns about the checkpoint afterwards has already logged a crash, spent a
// restart from the budget, and started a replacement process that has none of
// the state the dump just wrote.
func (m *Manager) BeginCheckpoint() []int {
	var pids []int
	for _, sup := range m.snapshotSupervisors() {
		pids = append(pids, sup.BeginCheckpoint()...)
	}
	return pids
}

// AbortCheckpoint returns every process to ordinary supervision after a dump
// that failed. The workload was never touched; this only undoes the
// expectation.
func (m *Manager) AbortCheckpoint() {
	for _, sup := range m.snapshotSupervisors() {
		sup.AbortCheckpoint()
	}
}

// CompleteCheckpoint reaps the dumped tree and settles the state of every
// instance that was checkpointed. It must be called as soon as the dump
// returns: a dumped process is a zombie until waited on, and its zombie holds
// the PID the restore has to recreate.
func (m *Manager) CompleteCheckpoint() {
	for _, sup := range m.snapshotSupervisors() {
		sup.CompleteCheckpoint()
	}
}

// EndCheckpoint puts restored processes back into ordinary supervision. Without
// it the warm tier works exactly once: the restored tree is genuinely back but
// nothing is watching it, so it can neither sleep again nor be noticed if it
// dies.
func (m *Manager) EndCheckpoint() {
	for _, sup := range m.snapshotSupervisors() {
		sup.EndCheckpoint()
	}
}

// IsCheckpointed reports whether every eligible process is checkpointed.
func (m *Manager) IsCheckpointed() bool {
	sups := m.snapshotSupervisors()
	if len(sups) == 0 {
		return false
	}
	for _, sup := range sups {
		if !sup.IsCheckpointed() {
			return false
		}
	}
	return true
}

// ColdStart replaces checkpointed processes with fresh ones after a checkpoint
// that can no longer be loaded. The in-memory state is gone; this is the
// recovery, not a repair, and the caller is expected to have said so.
func (m *Manager) ColdStart(ctx context.Context) error {
	var errs []error
	recovered := 0

	for _, sup := range m.snapshotSupervisors() {
		n, err := sup.RecoverCheckpointed(ctx)
		recovered += n
		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("cold starting after a lost checkpoint: %v", errs)
	}
	// Nothing checkpointed is success, not failure. Several connections can
	// arrive at a workload whose checkpoint was lost, and the second one asking
	// for a cold start wants the same outcome the first one already produced: a
	// running workload. Reporting an error here would fail a connection that
	// could have been served.
	if recovered == 0 {
		m.logger.Debug("cold start found nothing checkpointed; the workload is already running")
	}
	return nil
}

// snapshotSupervisors returns the supervisors that participate in the warm
// tier.
func (m *Manager) snapshotSupervisors() []*Supervisor {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sups := make([]*Supervisor, 0, len(m.processes))
	for name, sup := range m.processes {
		if cfg, ok := m.config.Processes[name]; ok && cfg.Type == "oneshot" {
			continue
		}
		sups = append(sups, sup)
	}
	return sups
}

// SnapshotTarget returns the single process the agent will checkpoint: its PID
// and the supervisor-owned pipe its stdout goes to.
//
// Single, and refused rather than guessed when there is more than one. The
// control protocol registers one child tree and one pipe, so a container
// running nginx and php-fpm as separate supervised processes has no single
// answer here — dumping one and leaving the other is a container that is
// neither asleep nor awake. Registering several trees is the next piece of this
// design and is deliberately not faked in the meantime: a container that is not
// eligible must run normally and never pretend to offer the warm tier.
func (m *Manager) SnapshotTarget() (int, *snapshot.Stream, error) {
	sups := m.snapshotSupervisors()
	if len(sups) == 0 {
		return 0, nil, fmt.Errorf("no supervised process to checkpoint")
	}
	if len(sups) > 1 {
		return 0, nil, fmt.Errorf("the warm tier supports one supervised process, found %d", len(sups))
	}
	return sups[0].SnapshotTarget()
}
