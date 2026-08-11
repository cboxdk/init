package process

import (
	"log/slog"
	"os"
	"os/exec"
	"syscall"
	"testing"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
)

// A process that has already exited is stopped, not a failure to stop.
//
// The bug this covers only appeared on a CI runner slow enough to lose a race a
// laptop wins every time: the child exits between the liveness check and the
// signal, `Process.Signal` returns os.ErrProcessDone, and `scale 0` reported
// "failed to send signal" for work that was already done. Reproduced here
// deterministically by waiting for the exit first, rather than hoping for a
// slow machine.
func TestSignalProcessGroup_AlreadyExitedIsNotAnError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	sup := NewSupervisor("test-proc", &config.Process{
		Enabled: true,
		Command: []string{"true"},
		Restart: "never",
		Scale:   1,
	}, &config.GlobalConfig{LogLevel: "error", MaxRestartAttempts: 1, RestartBackoff: 1},
		logger, audit.NewLogger(logger, false), nil)

	// A process the supervisor still holds, which the operating system has
	// already finished with — exactly the state a stop races into.
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	_ = cmd.Wait()

	instance := &Instance{id: "test-proc-1", cmd: cmd, pid: cmd.Process.Pid}

	if err := sup.signalProcessGroup(instance, syscall.SIGTERM, "test"); err != nil {
		t.Errorf("signalling an already-exited process reported an error: %v", err)
	}
}

func TestSignalMoot(t *testing.T) {
	if !signalMoot(os.ErrProcessDone) {
		t.Error("os.ErrProcessDone means the process is gone, which is what stop wanted")
	}
	if !signalMoot(syscall.ESRCH) {
		t.Error("ESRCH means no such process, which is what stop wanted")
	}
	if signalMoot(syscall.EPERM) {
		t.Error("EPERM is a real failure — the process is alive and we may not signal it")
	}
}
