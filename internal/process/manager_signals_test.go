package process

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/testutil"
)

// ForwardSignal must relay an operational signal (e.g. SIGUSR1) to the managed
// process group without stopping it. This is what lets `docker kill -s USR1`
// reach php-fpm/nginx for log rotation or reload while cbox-init, as PID 1,
// keeps running.
func TestSupervisor_ForwardSignal_ReachesChild(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	got := filepath.Join(dir, "got-usr1")

	// Trap SIGUSR1, record it, and keep running (the loop continues after the
	// trap fires). Touch ready once the trap is installed.
	script := "trap 'echo got > " + got + "' USR1; touch " + ready + "; " +
		"while true; do sleep 0.05; done"

	sup := NewSupervisor("signal-proc", &config.Process{
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
	defer func() { _ = sup.Stop(context.Background()) }()

	testutil.Eventually(t, func() bool {
		_, err := os.Stat(ready)
		return err == nil
	}, "child to install its USR1 trap", 2*time.Second)

	sup.ForwardSignal(syscall.SIGUSR1)

	testutil.Eventually(t, func() bool {
		_, err := os.Stat(got)
		return err == nil
	}, "child to receive the forwarded SIGUSR1", 2*time.Second)

	// The process must still be running — forwarding is not a stop.
	if sup.GetState() != StateRunning {
		t.Errorf("state after forwarding = %v, want running", sup.GetState())
	}
}

// SignalProcess targets one named process and validates the signal name.
func TestManager_SignalProcess(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	got := filepath.Join(dir, "got")

	script := "trap 'echo x > " + got + "' USR1; touch " + ready + "; while true; do sleep 0.05; done"
	cfg := &config.Config{
		Global: config.GlobalConfig{LogLevel: "error", MaxRestartAttempts: 1, RestartBackoff: 1},
		Processes: map[string]*config.Process{
			"web": {Enabled: true, InitialState: "running", Type: "longrun",
				Command: []string{"sh", "-c", script}, Restart: "never", Scale: 1},
		},
	}
	m := NewManager(cfg, logger, audit.NewLogger(logger, false))
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = m.Shutdown(ctx)
	}()

	testutil.Eventually(t, func() bool { _, err := os.Stat(ready); return err == nil },
		"child to install trap", 2*time.Second)

	// Unknown process -> error.
	if err := m.SignalProcess("nope", "SIGUSR1"); err == nil {
		t.Error("SignalProcess(unknown) = nil, want not-found error")
	}
	// Invalid signal -> error.
	if err := m.SignalProcess("web", "SIGBOGUS"); err == nil {
		t.Error("SignalProcess(invalid signal) = nil, want error")
	}
	// Valid: bare spelling accepted, reaches the child.
	if err := m.SignalProcess("web", "USR1"); err != nil {
		t.Fatalf("SignalProcess(web, USR1) = %v, want nil", err)
	}
	testutil.Eventually(t, func() bool { _, err := os.Stat(got); return err == nil },
		"child to receive SIGUSR1", 2*time.Second)
}

// Forwarding to a supervisor with no running instances must be a safe no-op.
func TestManager_ForwardSignal_NoProcessesIsSafe(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := &config.Config{
		Global:    config.GlobalConfig{LogLevel: "error"},
		Processes: map[string]*config.Process{},
	}
	m := NewManager(cfg, logger, audit.NewLogger(logger, false))

	// Must not panic with an empty process set.
	m.ForwardSignal(syscall.SIGUSR1)
}
