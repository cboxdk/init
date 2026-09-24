package process

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/testutil"
)

// A supervised process can exit while something it started still holds its
// stdout/stderr: a daemon that backgrounds a helper, or PostgreSQL, whose
// backends each sit in a session of their own. os/exec's cmd.Wait() waited for
// every holder of the output pipe to close it, so cbox-init did not see the
// exit at all — no restart for a crashed service, and a stop that ended in
// "did not exit after SIGKILL" and exit 1 although the process was long gone.

// killPIDFile kills the process whose pid a test child wrote to path, so a
// deliberately orphaned helper does not outlive the test.
func killPIDFile(t *testing.T, path string) {
	t.Helper()
	t.Cleanup(func() {
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
}

func heldPipeSupervisor(t *testing.T, name, script string, stdout bool) *Supervisor {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	on := stdout
	return NewSupervisor(name, &config.Process{
		Enabled:      true,
		Type:         "longrun",
		InitialState: "running",
		Command:      []string{"sh", "-c", script},
		Restart:      "never",
		Scale:        1,
		Stdout:       &on,
		Stderr:       &on,
		Shutdown:     &config.ShutdownConfig{Timeout: 1, KillTimeout: 1},
	}, &config.GlobalConfig{LogLevel: "error", MaxRestartAttempts: 1, RestartBackoff: 1},
		logger, audit.NewLogger(logger, false), nil)
}

func waitForExit(t *testing.T, sup *Supervisor, within time.Duration) {
	t.Helper()
	testutil.Eventually(t, func() bool {
		_, exited := sup.LastExitCode()
		return exited
	}, "the supervisor to observe the exit", within)
}

// The exit is observed promptly, with its real exit code, although a
// background child still holds stdout.
func TestExitObservedWhileChildHoldsOutput(t *testing.T) {
	for _, logged := range []bool{true, false} {
		name := "logged"
		if !logged {
			// stdout: false used io.Discard, which os/exec also serves through a
			// pipe and a copy goroutine — the same wait.
			name = "not-logged"
		}
		t.Run(name, func(t *testing.T) {
			pidFile := filepath.Join(t.TempDir(), "helper.pid")
			killPIDFile(t, pidFile)

			sup := heldPipeSupervisor(t, "held-"+name,
				"sleep 20 & echo $! > "+pidFile+"; echo exiting; exit 3", logged)
			if err := sup.Start(context.Background()); err != nil {
				t.Fatalf("start: %v", err)
			}

			waitForExit(t, sup, 3*time.Second)

			code, _ := sup.LastExitCode()
			if code != 3 {
				t.Errorf("exit code = %d, want 3", code)
			}
		})
	}
}

// Output a lingering child writes after the process itself has exited is still
// logged — it is not cut off, and the child is not killed by SIGPIPE for it.
func TestOutputOfLingeringChildIsStillLogged(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "helper.pid")
	killPIDFile(t, pidFile)

	sup := heldPipeSupervisor(t, "lingering",
		"(sleep 1.5; echo from-the-child; sleep 20) & echo $! > "+pidFile+"; echo from-the-parent; exit 0", true)
	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForExit(t, sup, 3*time.Second)

	testutil.Eventually(t, func() bool {
		for _, entry := range sup.GetLogs(0) {
			if entry.Message == "from-the-child" {
				return true
			}
		}
		return false
	}, "the lingering child's output to be logged", 5*time.Second)

	var sawParent bool
	for _, entry := range sup.GetLogs(0) {
		if entry.Message == "from-the-parent" {
			sawParent = true
		}
	}
	if !sawParent {
		t.Error("the process's own output was lost")
	}
}

// A stop whose SIGKILL leaves a descendant in another session alive — exactly
// PostgreSQL after its postmaster is SIGKILLed — reports success: the process
// it was asked to stop is gone.
func TestStopSucceedsWhileOrphanInOwnSessionHoldsOutput(t *testing.T) {
	perl, err := exec.LookPath("perl")
	if err != nil {
		t.Skip("perl not available to start a helper in its own session")
	}

	dir := t.TempDir()
	pidFile := filepath.Join(dir, "orphan.pid")
	ready := filepath.Join(dir, "ready")
	killPIDFile(t, pidFile)

	orphan := perl + ` -MPOSIX -e 'POSIX::setsid(); open(my $f, ">", "` + pidFile + `"); print $f $$; close($f); sleep 30' &`
	script := "trap '' TERM; " + orphan + " touch " + ready + "; while :; do sleep 0.05; done"

	sup := heldPipeSupervisor(t, "orphaned", script, true)
	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForFile(t, ready)
	testutil.Eventually(t, func() bool { _, err := os.Stat(pidFile); return err == nil },
		"the orphan to record its pid", 2*time.Second)

	start := time.Now()
	if err := sup.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v — the process was SIGKILLed; only its orphan was still holding the log pipe", err)
	}
	// 1s graceful timeout, 1s kill_timeout, plus the pipe drain bound.
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("stop took %v", elapsed)
	}
}
