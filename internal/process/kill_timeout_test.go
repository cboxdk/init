package process

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
)

// shutdown.kill_timeout: how long the supervisor waits after the kill signal
// before escalating to SIGKILL.
//
// The motivating case is PostgreSQL. Its immediate shutdown (SIGQUIT) tells
// every backend to quit and SIGKILLs the ones still there after 5s; the
// postmaster then exits. cbox-init's post-escalation wait was a fixed 5s too,
// so it SIGKILLed the postmaster just as the postmaster was about to clean up —
// orphaning the stuck backends and reporting "did not exit after SIGKILL".

// slowQuitSupervisor starts a child that ignores the graceful signal and, on
// the kill signal (SIGQUIT), takes quitSeconds to clean up before it exits on
// its own and records that it did.
func slowQuitSupervisor(t *testing.T, name string, quitSeconds float64, killTimeout int) (sup *Supervisor, cleanExit string) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	cleanExit = filepath.Join(dir, "clean-exit")

	script := "trap '' TERM; " +
		"trap 'sleep " + strconv.FormatFloat(quitSeconds, 'f', -1, 64) + "; touch " + cleanExit + "; exit 0' QUIT; " +
		"touch " + ready + "; while true; do sleep 0.05; done"

	sup = NewSupervisor(name, &config.Process{
		Enabled:      true,
		Type:         "longrun",
		InitialState: "running",
		Command:      []string{"sh", "-c", script},
		Restart:      "never",
		Scale:        1,
		Shutdown: &config.ShutdownConfig{
			Signal:      "SIGTERM",
			Timeout:     1,
			KillSignal:  "SIGQUIT",
			KillTimeout: killTimeout,
		},
	}, &config.GlobalConfig{LogLevel: "error", MaxRestartAttempts: 1, RestartBackoff: 1},
		logger, audit.NewLogger(logger, false), nil)

	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForFile(t, ready)

	return sup, cleanExit
}

// A kill_timeout longer than the old fixed 5s must be honoured: the child's
// own cleanup after the kill signal takes 6s, and it has to be allowed to
// finish rather than being SIGKILLed at 5s.
func TestStop_KillTimeoutAllowsSlowKillSignalHandler(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out a 6s kill-signal handler")
	}

	sup, cleanExit := slowQuitSupervisor(t, "slow-quit", 6, 10)

	if err := sup.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, err := os.Stat(cleanExit); err != nil {
		t.Fatalf("the child was SIGKILLed before its kill-signal handler finished (kill_timeout: 10 was not honoured): %v", err)
	}
	if sup.GetState() != StateStopped {
		t.Errorf("state = %v, want stopped", sup.GetState())
	}
}

// A kill_timeout shorter than the default must be honoured too: with 1s, a
// handler that needs 3s is cut off by SIGKILL, and Stop returns well before the
// handler would have finished.
func TestStop_KillTimeoutShorterThanDefault(t *testing.T) {
	sup, cleanExit := slowQuitSupervisor(t, "short-kill", 3, 1)

	start := time.Now()
	if err := sup.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	elapsed := time.Since(start)

	// 1s graceful timeout + 1s kill_timeout, then SIGKILL and reap.
	if elapsed > 2800*time.Millisecond {
		t.Errorf("Stop took %v; kill_timeout: 1 should have escalated to SIGKILL after ~1s of the kill signal", elapsed)
	}
	if _, err := os.Stat(cleanExit); err == nil {
		t.Error("the child finished its 3s kill-signal handler; kill_timeout: 1 was not honoured")
	}
}

func TestSupervisor_KillTimeout(t *testing.T) {
	cases := []struct {
		name     string
		shutdown *config.ShutdownConfig
		want     time.Duration
	}{
		{"no shutdown block", nil, 5 * time.Second},
		{"unset", &config.ShutdownConfig{}, 5 * time.Second},
		{"configured", &config.ShutdownConfig{KillTimeout: 12}, 12 * time.Second},
		{"negative falls back", &config.ShutdownConfig{KillTimeout: -3}, 5 * time.Second},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Supervisor{config: &config.Process{Shutdown: tc.shutdown}}
			if got := s.killTimeout(); got != tc.want {
				t.Errorf("killTimeout() = %v, want %v", got, tc.want)
			}
		})
	}
}
