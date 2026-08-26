package process

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
)

// LIFE-2: PID 1's exit code when all processes have died must distinguish a
// crash (orchestrator should restart) from a clean oneshot completion.
func TestManager_TerminalExitCode(t *testing.T) {
	run := func(t *testing.T, proc *config.Process) int {
		t.Helper()
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
		cfg := &config.Config{
			Global:    config.GlobalConfig{LogLevel: "error", RestartPolicy: "always", MaxRestartAttempts: 1, RestartBackoff: 1, ShutdownTimeout: 5},
			Processes: map[string]*config.Process{"p": proc},
		}
		m := NewManager(cfg, logger, audit.NewLogger(logger, false))
		monitorCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := m.Start(context.Background()); err != nil {
			t.Fatalf("start: %v", err)
		}
		m.MonitorProcessHealth(monitorCtx)
		defer func() {
			ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
			defer c()
			_ = m.Shutdown(ctx)
		}()

		select {
		case <-m.AllDeadChannel():
		case <-time.After(5 * time.Second):
			t.Fatal("processes did not all die in time")
		}
		return m.TerminalExitCode()
	}

	t.Run("longrun crash exits non-zero", func(t *testing.T) {
		code := run(t, &config.Process{Enabled: true, InitialState: "running", Type: "longrun", Command: []string{"false"}, Restart: "never", Scale: 1})
		if code == 0 {
			t.Error("TerminalExitCode = 0 for a crashed longrun process; want non-zero")
		}
	})

	t.Run("oneshot success exits zero", func(t *testing.T) {
		code := run(t, &config.Process{Enabled: true, InitialState: "running", Type: "oneshot", Command: []string{"true"}, Restart: "never", Scale: 1})
		if code != 0 {
			t.Errorf("TerminalExitCode = %d for a successful oneshot; want 0", code)
		}
	})

	t.Run("dead longrun exits non-zero even on clean exit", func(t *testing.T) {
		code := run(t, &config.Process{Enabled: true, InitialState: "running", Type: "longrun", Command: []string{"true"}, Restart: "never", Scale: 1})
		if code == 0 {
			t.Error("TerminalExitCode = 0 for a dead longrun (exit 0); the workload is gone, want non-zero")
		}
	})
}
