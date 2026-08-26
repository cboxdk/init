package process

import (
	"context"
	"errors"
	"os"
	"testing"

	"log/slog"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
)

// The manager's errors must carry sentinels so the API can classify them with
// errors.Is rather than matching message substrings.
func TestManagerErrors_CarrySentinels(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := &config.Config{
		Global: config.GlobalConfig{LogLevel: "error", MaxRestartAttempts: 1, RestartBackoff: 1, ShutdownTimeout: 5},
		Processes: map[string]*config.Process{
			"web": {Enabled: true, InitialState: "running", Type: "longrun", Command: []string{"sleep", "60"}, Restart: "always", Scale: 1},
		},
	}
	m := NewManager(cfg, logger, audit.NewLogger(logger, false))
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3e9)
		defer cancel()
		_ = m.Shutdown(ctx)
	}()

	t.Run("unknown process -> ErrProcessNotFound", func(t *testing.T) {
		err := m.StopProcess(context.Background(), "nope")
		if !errors.Is(err, ErrProcessNotFound) {
			t.Errorf("StopProcess(unknown) err = %v, want wrapping ErrProcessNotFound", err)
		}
	})

	t.Run("add duplicate -> ErrProcessExists", func(t *testing.T) {
		err := m.AddProcess(context.Background(), "web", &config.Process{Command: []string{"x"}, Scale: 1})
		if !errors.Is(err, ErrProcessExists) {
			t.Errorf("AddProcess(existing) err = %v, want wrapping ErrProcessExists", err)
		}
	})

	t.Run("start running -> ErrInvalidState", func(t *testing.T) {
		err := m.StartProcess(context.Background(), "web")
		if !errors.Is(err, ErrInvalidState) {
			t.Errorf("StartProcess(running) err = %v, want wrapping ErrInvalidState", err)
		}
	})

	t.Run("empty name -> ErrInvalidArgument", func(t *testing.T) {
		err := m.StopProcess(context.Background(), "")
		if !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("StopProcess(\"\") err = %v, want wrapping ErrInvalidArgument", err)
		}
	})
}
