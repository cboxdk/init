package process

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
)

// A configured user/group that cannot be resolved must fail the process closed,
// not silently run it as PID 1's uid (root). Previously a typo in `user:` was
// logged and ignored, dropping the privilege drop.
func TestStart_UnresolvableUserFailsClosed(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	sup := NewSupervisor("cred-proc", &config.Process{
		Enabled: true,
		Type:    "longrun",
		Command: []string{"sleep", "30"},
		Restart: "never",
		Scale:   1,
		User:    "definitely-not-a-real-user-9999",
	}, &config.GlobalConfig{LogLevel: "error", MaxRestartAttempts: 1, RestartBackoff: 1},
		logger, audit.NewLogger(logger, false), nil)

	if sup.credentialErr == nil {
		t.Fatal("expected credentialErr to be set for an unresolvable user")
	}

	err := sup.Start(context.Background())
	if err == nil {
		_ = sup.Stop(context.Background())
		t.Fatal("Start() = nil, want error (process must not start as root when its user cannot be resolved)")
	}
	if !strings.Contains(err.Error(), "resolve user") {
		t.Errorf("Start() error = %v, want it to mention the unresolved user", err)
	}
}

// A process with no user/group configured starts normally (the fail-closed
// change must not affect the common case).
func TestStart_NoCredentialsStartsNormally(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	sup := NewSupervisor("plain-proc", &config.Process{
		Enabled: true,
		Type:    "longrun",
		Command: []string{"sleep", "30"},
		Restart: "never",
		Scale:   1,
	}, &config.GlobalConfig{LogLevel: "error", MaxRestartAttempts: 1, RestartBackoff: 1},
		logger, audit.NewLogger(logger, false), nil)

	if sup.credentialErr != nil {
		t.Fatalf("credentialErr = %v, want nil when no user/group configured", sup.credentialErr)
	}
	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start() = %v, want nil", err)
	}
	_ = sup.Stop(context.Background())
}
