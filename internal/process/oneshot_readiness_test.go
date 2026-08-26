package process

import (
	"context"
	"log/slog"
	"os"
	"strings"
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

// A oneshot that FAILS can never become ready, so its dependents must be told
// immediately rather than waiting out the whole dependency timeout. Before this,
// a failed migration blocked Manager.Start for the full 5-minute default with
// the manager write-lock held — during which PID 1 also could not act on
// SIGTERM. (PID1-9 follow-up)
func TestSupervisor_FailedOneshotFailsWaitersFast(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	sup := NewSupervisor("migrate", &config.Process{
		Enabled:      true,
		Type:         "oneshot",
		InitialState: "running",
		Command:      []string{"sh", "-c", "exit 1"},
		Restart:      "never",
		Scale:        1,
	}, &config.GlobalConfig{LogLevel: "error", MaxRestartAttempts: 1, RestartBackoff: 1},
		logger, audit.NewLogger(logger, false), nil)

	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	start := time.Now()
	err := sup.WaitForReadiness(context.Background(), 5*time.Minute)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error when the oneshot exited non-zero")
	}
	if elapsed > 10*time.Second {
		t.Errorf("WaitForReadiness took %v; a failed oneshot must fail waiters immediately, not after the dependency timeout", elapsed)
	}
	if !strings.Contains(err.Error(), "never become ready") {
		t.Errorf("error = %q, want it to explain readiness is impossible", err)
	}
}
