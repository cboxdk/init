package signals

import (
	"log/slog"
	"os"
	"sync"
	"syscall"
	"time"
)

// WaitFunc is the function signature for syscall.Wait4
// Allows mocking in tests
type WaitFunc func(pid int, wstatus *syscall.WaitStatus, options int, rusage *syscall.Rusage) (wpid int, err error)

// waitFunc is the function used for waiting on child processes
// Can be replaced in tests for mocking
var waitFunc WaitFunc = syscall.Wait4
var waitFuncMu sync.RWMutex

// getWaitFunc returns the current wait function with proper synchronization
func getWaitFunc() WaitFunc {
	waitFuncMu.RLock()
	defer waitFuncMu.RUnlock()
	return waitFunc
}

// setWaitFunc sets the wait function with proper synchronization (for testing)
func setWaitFunc(f WaitFunc) {
	waitFuncMu.Lock()
	defer waitFuncMu.Unlock()
	waitFunc = f
}

// Supervised-PID coordination.
//
// As PID 1 we must reap re-parented orphan grandchildren (double-forked
// processes), which requires a wildcard reaper calling Wait4(-1). But the
// process supervisor also waits on each of its own children with cmd.Wait(),
// which internally calls wait4(pid). These two waiters race: if the wildcard
// reaper wins, it collects a supervised child's exit status and the
// supervisor's cmd.Wait() returns ECHILD with a nil ProcessState — which would
// panic and permanently abandon the process (never restarted).
//
// To make the reaper race-safe, supervisors register the PIDs they own. When
// the reaper reaps a registered PID it stashes the WaitStatus instead of
// discarding it, so the supervisor can recover the real exit code via
// TakeReapedStatus. Orphan PIDs (not registered) are reaped and dropped as
// before.
var (
	supervisedMu   sync.Mutex
	supervisedPIDs = make(map[int]struct{})           // PIDs owned by a supervisor
	reapedStatuses = make(map[int]syscall.WaitStatus) // statuses the reaper grabbed before the supervisor's own Wait()
)

// RegisterSupervised marks pid as owned by a supervisor. Call it right after
// starting the child, before its Wait() call, so a racing reaper captures the
// exit status rather than discarding it.
func RegisterSupervised(pid int) {
	supervisedMu.Lock()
	supervisedPIDs[pid] = struct{}{}
	supervisedMu.Unlock()
}

// UnregisterSupervised removes pid from the supervised set and drops any
// captured status. Call it when the supervisor collected the exit itself
// (its own Wait() won the race), so the maps do not grow unbounded.
func UnregisterSupervised(pid int) {
	supervisedMu.Lock()
	delete(supervisedPIDs, pid)
	delete(reapedStatuses, pid)
	supervisedMu.Unlock()
}

// TakeReapedStatus returns and clears the WaitStatus the reaper captured for a
// supervised pid. ok is false in the normal case where the supervisor's own
// Wait() reaped the child (nothing was captured). Taking a status also
// unregisters the pid.
func TakeReapedStatus(pid int) (syscall.WaitStatus, bool) {
	supervisedMu.Lock()
	defer supervisedMu.Unlock()
	st, ok := reapedStatuses[pid]
	if ok {
		delete(reapedStatuses, pid)
		delete(supervisedPIDs, pid)
	}
	return st, ok
}

// captureIfSupervised stashes the status for a supervised pid so the owning
// supervisor can recover the exit code. Returns true if the pid was tracked.
func captureIfSupervised(pid int, status syscall.WaitStatus) bool {
	supervisedMu.Lock()
	defer supervisedMu.Unlock()
	if _, tracked := supervisedPIDs[pid]; tracked {
		reapedStatuses[pid] = status
		return true
	}
	return false
}

// ReapZombies continuously reaps zombie processes.
// This is critical when running as PID 1 in a container.
// Without this, defunct processes accumulate and can exhaust PIDs.
// The interval parameter controls how often zombie reaping occurs.
// If interval is 0 or negative, it defaults to 1 second.
func ReapZombies(interval time.Duration) {
	if interval <= 0 {
		interval = 1 * time.Second // Default to 1 second if not configured
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		reapAll()
	}
}

// reapAll reaps all zombie child processes
func reapAll() {
	waitFn := getWaitFunc()
	for {
		var status syscall.WaitStatus
		pid, err := waitFn(-1, &status, syscall.WNOHANG, nil)

		if err != nil || pid <= 0 {
			// No more zombies to reap
			break
		}

		// If a supervisor owns this pid, stash the status so it can recover
		// the exit code instead of seeing a nil ProcessState.
		captureIfSupervised(pid, status)

		slog.Debug("Reaped zombie process",
			"pid", pid,
			"status", status,
		)
	}
}

// ReapCount returns the number of zombies reaped in a single pass
// Useful for testing and monitoring
func ReapCount() int {
	waitFn := getWaitFunc()
	count := 0
	for {
		var status syscall.WaitStatus
		pid, err := waitFn(-1, &status, syscall.WNOHANG, nil)

		if err != nil || pid <= 0 {
			break
		}

		captureIfSupervised(pid, status)

		count++
		slog.Debug("Reaped zombie process",
			"pid", pid,
			"status", status,
		)
	}
	return count
}

// IsPID1 returns true if the current process is PID 1
func IsPID1() bool {
	return os.Getpid() == 1
}
