package signals

import (
	"fmt"
	"os/exec"
	"syscall"
)

// RunSupervised starts and waits on an already-configured command under the
// wildcard reaper's coordination, so a child reaped by the PID-1 reaper before
// this Wait() does not surface as a spurious failure.
//
// As PID 1, cbox-init runs a wildcard reaper (Wait4(-1)) to collect re-parented
// orphans. That reaper races the Wait() of any child cbox-init spawns directly —
// lifecycle hooks, exec health checks, scheduled jobs. If the reaper wins, this
// process's Wait() gets ECHILD and a nil ProcessState, which callers read as a
// failure: a *successful* pre-start hook then aborts container startup, and a
// *passing* exec health check triggers a restart. Registering the pid before
// waiting lets the reaper stash the real status for recovery here.
//
// The normal (non-raced) path is unchanged: this returns exactly what cmd.Wait()
// returned (nil on success, *exec.ExitError otherwise), so callers that inspect
// the ExitError keep working. Only when the reaper won the race does this recover
// the captured status — returning nil for a clean exit (the status that matters
// most) or a descriptive error otherwise.
func RunSupervised(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	return waitSupervised(cmd)
}

func waitSupervised(cmd *exec.Cmd) error {
	pid := cmd.Process.Pid
	RegisterSupervised(pid)

	waitErr := cmd.Wait()

	// Our own Wait() reaped the child: the common case. Return its result
	// verbatim so exit-code extraction downstream is unaffected.
	if cmd.ProcessState != nil {
		UnregisterSupervised(pid)
		return waitErr
	}

	// nil ProcessState means the wildcard reaper collected the child first.
	// Recover the status it stashed.
	if ws, ok := TakeReapedStatus(pid); ok {
		return statusToError(ws)
	}

	// Neither our Wait() nor the reaper captured a status. Treat as abnormal so
	// the caller doesn't mistake it for success.
	UnregisterSupervised(pid)
	return fmt.Errorf("process exit status unavailable: %w", waitErr)
}

// statusToError turns a recovered wait status into the error a caller expects:
// nil for a clean exit (the outcome that matters most — a successful process
// reaped by init must not look like a failure), and a descriptive error for a
// non-zero exit or a signal.
func statusToError(ws syscall.WaitStatus) error {
	switch {
	case ws.Exited() && ws.ExitStatus() == 0:
		return nil
	case ws.Exited():
		return fmt.Errorf("process exited with code %d (reaped by init)", ws.ExitStatus())
	default:
		return fmt.Errorf("process terminated by signal %v (reaped by init)", ws.Signal())
	}
}
