package snapshot

import (
	"errors"
	"syscall"
	"time"
)

// WaitFunc matches syscall.Wait4 so tests can drive the drain without spawning
// real processes, mirroring how internal/signals makes its reaper testable.
type WaitFunc func(pid int, wstatus *syscall.WaitStatus, options int, rusage *syscall.Rusage) (int, error)

// DrainOptions tunes the post-checkpoint drain.
type DrainOptions struct {
	// Wait defaults to syscall.Wait4.
	Wait WaitFunc
	// Timeout bounds the whole drain. Re-parenting is asynchronous, so a
	// grandchild can become ours a moment after the dump returns.
	Timeout time.Duration
	// Settle is how long the drain must come up empty before it concludes the
	// tree is fully reaped.
	Settle time.Duration
	// Sleep defaults to time.Sleep; tests replace it to avoid real waiting.
	Sleep func(time.Duration)
}

// DrainCheckpointed reaps every process left behind by a checkpoint and returns
// the PIDs it collected.
//
// A dumped process is a zombie until someone waits on it, and a zombie keeps
// its PID allocated, so the restore — which recreates the tree at exactly the
// PIDs it recorded — fails with "Can't fork: File exists". Spike 1 established
// that for the supervised child. What it did not establish, and what cost a
// second round of failures, is that the same applies to every descendant: a
// grandchild whose own parent has already gone is re-parented onto PID 1, and
// if PID 1 only waits on the process it knows about then the grandchild's
// zombie holds *its* PID instead and the restore fails for that one.
//
// So this is a wildcard drain, not a targeted wait, and it polls: the last
// descendant may not have landed on us yet when the dump returns.
func DrainCheckpointed(opts DrainOptions) []int {
	wait := opts.Wait
	if wait == nil {
		wait = syscall.Wait4
	}
	sleep := opts.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	settle := opts.Settle
	if settle <= 0 {
		settle = 50 * time.Millisecond
	}

	const step = 5 * time.Millisecond

	var reaped []int
	var quiet time.Duration
	for elapsed := time.Duration(0); elapsed < timeout; elapsed += step {
		var status syscall.WaitStatus
		pid, err := wait(-1, &status, syscall.WNOHANG, nil)

		if pid > 0 {
			reaped = append(reaped, pid)
			quiet = 0
			continue
		}

		// ECHILD means there is nothing left to wait for at all, which is a
		// definitive answer rather than a reason to keep polling.
		if errors.Is(err, syscall.ECHILD) {
			break
		}

		quiet += step
		if quiet >= settle && len(reaped) > 0 {
			break
		}
		if quiet >= timeout {
			break
		}
		sleep(step)
	}

	return reaped
}
