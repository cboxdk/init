package signals

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestRunSupervised_Success(t *testing.T) {
	if err := RunSupervised(exec.Command("true")); err != nil {
		t.Errorf("RunSupervised(true) = %v, want nil", err)
	}
}

func TestRunSupervised_Failure(t *testing.T) {
	if err := RunSupervised(exec.Command("false")); err == nil {
		t.Error("RunSupervised(false) = nil, want an error")
	}
}

func TestRunSupervised_StartError(t *testing.T) {
	if err := RunSupervised(exec.Command("this-command-does-not-exist-xyz")); err == nil {
		t.Error("RunSupervised(nonexistent) = nil, want a start error")
	}
}

// After RunSupervised returns, the pid must not linger in the supervised set —
// otherwise the maps grow unbounded across the many hook/health/job executions.
func TestRunSupervised_UnregistersAfterRun(t *testing.T) {
	_ = RunSupervised(exec.Command("true"))
	supervisedMu.Lock()
	n := len(supervisedPIDs) + len(reapedStatuses)
	supervisedMu.Unlock()
	if n != 0 {
		t.Errorf("supervised maps not empty after RunSupervised: %d entries", n)
	}
}

// statusToError is what recovers a reaped child's outcome. A clean exit must map
// to nil so a successful process reaped by init is not misreported as failed.
func TestStatusToError(t *testing.T) {
	// On Linux a WaitStatus encodes the exit code in bits 8..15 and "exited"
	// when the low 7 bits are zero.
	if err := statusToError(syscall.WaitStatus(0)); err != nil {
		t.Errorf("statusToError(exit 0) = %v, want nil", err)
	}
	if err := statusToError(syscall.WaitStatus(1 << 8)); err == nil {
		t.Error("statusToError(exit 1) = nil, want an error")
	}
	// Low bits set => terminated by signal (not a clean exit).
	if err := statusToError(syscall.WaitStatus(int(syscall.SIGKILL))); err == nil {
		t.Error("statusToError(signalled) = nil, want an error")
	}
}
