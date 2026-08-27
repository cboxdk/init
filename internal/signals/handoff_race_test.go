package signals

import (
	"os/exec"
	"sync"
	"testing"
	"time"
)

// Hammer RunSupervised while a tight reaper loop races it, the way PID 1's
// wildcard reaper races hooks/health checks/scheduled jobs.
// The wildcard reaper races the Wait() of every child cbox-init spawns
// directly. If it reaps one before the status is stashed, a SUCCESSFUL process
// is reported as failed — a passing health check triggers a restart, a
// successful pre-start hook aborts container startup. Measured at ~7% before
// the wait+capture and start+register handoffs were made atomic.
func TestSupervisedHandoffUnderTightReaper(t *testing.T) {
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = ReapCount()
				time.Sleep(50 * time.Microsecond)
			}
		}
	}()

	spurious, runs := 0, 400
	for i := 0; i < runs; i++ {
		if err := RunSupervised(exec.Command("/usr/bin/true")); err != nil {
			spurious++
		}
	}
	close(stop)
	wg.Wait()

	t.Logf("runs=%d spurious-failures=%d", runs, spurious)
	if spurious > 0 {
		t.Errorf("%d/%d successful processes reported as failures", spurious, runs)
	}
}
