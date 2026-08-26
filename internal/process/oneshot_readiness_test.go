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

// PID1-9: a oneshot with no health check must become ready only when it
// completes successfully, so a dependent (depends_on) waits for it — the
// migrate-then-serve pattern — rather than starting the instant it forks.
func TestSupervisor_OneshotReadinessGatedOnCompletion(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	sup := NewSupervisor("migrate", &config.Process{
		Enabled:      true,
		Type:         "oneshot",
		InitialState: "running",
		Command:      []string{"sh", "-c", "sleep 0.3; exit 0"},
		Restart:      "never",
		Scale:        1,
	}, &config.GlobalConfig{LogLevel: "error", MaxRestartAttempts: 1, RestartBackoff: 1},
		logger, audit.NewLogger(logger, false), nil)

	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	start := time.Now()
	if err := sup.WaitForReadiness(context.Background(), 5*time.Second); err != nil {
		t.Fatalf("WaitForReadiness: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed < 200*time.Millisecond {
		t.Errorf("WaitForReadiness returned in %v; a oneshot must gate readiness on completion (~300ms), not at fork", elapsed)
	}
}

// A long-running process with no health check is ready as soon as it is running
// — the PID1-9 change must not make longruns wait.
func TestSupervisor_LongrunNoHealthCheckReadyImmediately(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	sup := NewSupervisor("web", &config.Process{
		Enabled:      true,
		Type:         "longrun",
		InitialState: "running",
		Command:      []string{"sh", "-c", "sleep 10"},
		Restart:      "never",
		Scale:        1,
	}, &config.GlobalConfig{LogLevel: "error", MaxRestartAttempts: 1, RestartBackoff: 1},
		logger, audit.NewLogger(logger, false), nil)

	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_ = sup.Stop(ctx)
	}()

	start := time.Now()
	if err := sup.WaitForReadiness(context.Background(), 5*time.Second); err != nil {
		t.Fatalf("WaitForReadiness: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Errorf("WaitForReadiness took %v for a running longrun; it should be ready immediately", elapsed)
	}
}
