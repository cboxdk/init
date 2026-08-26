package process

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
)

func batchLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// STYLE-2: a process added or updated at runtime with an invalid enum value
// (e.g. a typo'd restart policy) must be rejected, not accepted and silently
// degraded to "never".
func TestAddProcess_ValidatesDomainValues(t *testing.T) {
	logger := batchLogger()
	cfg := &config.Config{
		// RestartPolicy mirrors what SetDefaults fills at load, which longrun
		// processes inherit when they don't set restart explicitly.
		Global:    config.GlobalConfig{LogLevel: "error", RestartPolicy: "always", MaxRestartAttempts: 1, RestartBackoff: 1, ShutdownTimeout: 5},
		Processes: map[string]*config.Process{},
	}
	m := NewManager(cfg, logger, audit.NewLogger(logger, false))

	cases := map[string]*config.Process{
		"typo'd restart": {Enabled: false, Command: []string{"nginx"}, Scale: 1, Restart: "on_failure"},
		"invalid type":   {Enabled: false, Command: []string{"nginx"}, Scale: 1, Type: "weird"},
		"invalid state":  {Enabled: false, Command: []string{"nginx"}, Scale: 1, InitialState: "halfway"},
		"oneshot always": {Enabled: false, Command: []string{"true"}, Scale: 1, Type: "oneshot", Restart: "always"},
	}
	for name, proc := range cases {
		t.Run(name, func(t *testing.T) {
			err := m.AddProcess(context.Background(), "p", proc)
			if err == nil {
				t.Fatalf("AddProcess accepted an invalid definition (%s)", name)
			}
			if !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("error should be ErrInvalidArgument (→ HTTP 400), got %v", err)
			}
		})
	}

	// A valid definition (empty enums get sensible defaults) is accepted.
	t.Run("valid with defaults", func(t *testing.T) {
		err := m.AddProcess(context.Background(), "ok", &config.Process{Enabled: false, Command: []string{"nginx"}, Scale: 1})
		if err != nil {
			t.Errorf("valid process rejected: %v", err)
		}
	})
}

// ARCH-4: a checkpointed instance is alive-on-purpose; the all-instances-dead
// sweep must not treat it as dead (which would shut the container down and
// discard the checkpoint).
func TestCheckAllInstancesDead_CheckpointedCountsAsAlive(t *testing.T) {
	logger := batchLogger()
	sup := NewSupervisor("cp", &config.Process{Command: []string{"sleep", "1"}, Restart: "never", Scale: 1},
		&config.GlobalConfig{LogLevel: "error", MaxRestartAttempts: 1, RestartBackoff: 1},
		logger, audit.NewLogger(logger, false), nil)

	fired := make(chan string, 1)
	sup.SetDeathNotifier(func(name string) { fired <- name })

	// A single checkpointed instance: the sweep must NOT report all-dead.
	sup.instances = []*Instance{{id: "cp-0", state: StateCheckpointed}}
	sup.checkAllInstancesDead()
	select {
	case <-fired:
		t.Error("death notifier fired for a checkpointed (alive) instance")
	case <-time.After(200 * time.Millisecond):
		// good
	}

	// A stopped instance: the sweep MUST report all-dead.
	sup.instances = []*Instance{{id: "cp-0", state: StateStopped}}
	sup.checkAllInstancesDead()
	select {
	case <-fired:
		// good
	case <-time.After(time.Second):
		t.Error("death notifier should have fired when all instances are stopped")
	}
}
