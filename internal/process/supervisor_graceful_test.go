package process

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/testutil"
)

// waitForFile blocks until path exists, so a test only signals a child after
// the child has installed its signal trap — otherwise the stop signal races
// trap installation and the child dies on the default disposition.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	testutil.Eventually(t, func() bool {
		_, err := os.Stat(path)
		return err == nil
	}, "child to install its signal trap", 2*time.Second)
}

// Graceful shutdown must deliver the configured stop signal to the child and
// give it time to handle it — not SIGKILL it out from under the handler.
//
// The bug this covers: instances are launched with exec.CommandContext(s.ctx),
// and Stop() called s.cancel() *before* sending the shutdown signal. The os/exec
// context watchdog then SIGKILLed every child instantly, so the pre-stop hook,
// the configured signal, and the graceful timeout were all dead code. No
// existing test asked the child whether it ever observed the signal, which is
// exactly why it survived. This test asks.
func TestStop_ChildObservesSIGTERM(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	marker := filepath.Join(dir, "term-received")

	// A child that traps SIGTERM, records that it saw it, and exits 0. It touches
	// the ready file only after the trap is installed. If it is SIGKILLed (which
	// cannot be trapped), the marker file never appears.
	script := "trap 'echo caught > " + marker + "; exit 0' TERM; " +
		"touch " + ready + "; " +
		"while true; do sleep 0.05; done"

	sup := NewSupervisor("graceful-proc", &config.Process{
		Enabled:  true,
		Type:     "longrun",
		Command:  []string{"sh", "-c", script},
		Restart:  "never",
		Scale:    1,
		Shutdown: &config.ShutdownConfig{Timeout: 5},
	}, &config.GlobalConfig{LogLevel: "error", MaxRestartAttempts: 1, RestartBackoff: 1},
		logger, audit.NewLogger(logger, false), nil)

	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForFile(t, ready)

	stopStart := time.Now()
	if err := sup.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// The child trapped TERM and exited on its own — the graceful path, not the
	// force-kill fallback — so Stop returns well within the 5s timeout.
	if elapsed := time.Since(stopStart); elapsed > 3*time.Second {
		t.Errorf("Stop took %v; graceful path should return promptly, force-kill fallback was likely used", elapsed)
	}

	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("child never recorded receiving SIGTERM (marker %s missing): %v — it was SIGKILLed before the signal was delivered", marker, err)
	}
	if string(data) != "caught\n" {
		t.Errorf("marker content = %q, want %q", string(data), "caught\n")
	}
}

// A child that ignores SIGTERM must still be stopped: the graceful wait times
// out and the supervisor escalates to SIGKILL. This proves the escalation path
// (previously count 0 in coverage) actually fires and is bounded by the
// configured timeout.
func TestStop_ForceKillsWhenChildIgnoresSIGTERM(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")

	// Ignore TERM entirely (and touch ready once the trap is installed); only
	// SIGKILL can stop this.
	script := "trap '' TERM; touch " + ready + "; while true; do sleep 0.05; done"

	sup := NewSupervisor("stubborn-proc", &config.Process{
		Enabled:  true,
		Type:     "longrun",
		Command:  []string{"sh", "-c", script},
		Restart:  "never",
		Scale:    1,
		Shutdown: &config.ShutdownConfig{Timeout: 1}, // 1s graceful budget, then SIGKILL
	}, &config.GlobalConfig{LogLevel: "error", MaxRestartAttempts: 1, RestartBackoff: 1},
		logger, audit.NewLogger(logger, false), nil)

	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForFile(t, ready)

	stopStart := time.Now()
	if err := sup.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	elapsed := time.Since(stopStart)

	// It must wait roughly the graceful timeout (the child ignores TERM), then
	// force-kill — so not instant, but also bounded well above the 1s budget.
	if elapsed < 900*time.Millisecond {
		t.Errorf("Stop returned in %v; expected to wait ~1s graceful timeout before SIGKILL", elapsed)
	}
	if elapsed > 4*time.Second {
		t.Errorf("Stop took %v; force-kill escalation did not fire within a reasonable bound", elapsed)
	}
	if sup.GetState() != StateStopped {
		t.Errorf("state = %v, want stopped", sup.GetState())
	}
}
