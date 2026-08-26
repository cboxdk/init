package process

import (
	"log/slog"
	"os"
	"syscall"
	"testing"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
)

func TestSupervisor_KillSignal(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	global := &config.GlobalConfig{LogLevel: "error", MaxRestartAttempts: 1, RestartBackoff: 1}
	aud := audit.NewLogger(logger, false)

	// Configured kill_signal is honored.
	custom := NewSupervisor("p", &config.Process{
		Command:  []string{"true"},
		Shutdown: &config.ShutdownConfig{KillSignal: "SIGTERM"},
	}, global, logger, aud, nil)
	if got := custom.killSignal(); got != syscall.SIGTERM {
		t.Errorf("killSignal() with kill_signal=SIGTERM = %v, want SIGTERM", got)
	}

	// No shutdown block: falls back to SIGKILL.
	none := NewSupervisor("p2", &config.Process{Command: []string{"true"}}, global, logger, aud, nil)
	if got := none.killSignal(); got != syscall.SIGKILL {
		t.Errorf("killSignal() with no shutdown config = %v, want SIGKILL", got)
	}
}

func TestParseSignalStrict(t *testing.T) {
	cases := map[string]syscall.Signal{
		"SIGTERM": syscall.SIGTERM,
		"TERM":    syscall.SIGTERM, // bare form
		"sigterm": syscall.SIGTERM, // case-insensitive
		"SIGKILL": syscall.SIGKILL,
		"SIGHUP":  syscall.SIGHUP,
		"SIGUSR1": syscall.SIGUSR1,
		"usr2":    syscall.SIGUSR2,
	}
	for name, want := range cases {
		got, err := parseSignalStrict(name)
		if err != nil {
			t.Errorf("parseSignalStrict(%q) unexpected error: %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("parseSignalStrict(%q) = %v, want %v", name, got, want)
		}
	}

	for _, bad := range []string{"", "SIGBOGUS", "nope", "SIG"} {
		if _, err := parseSignalStrict(bad); err == nil {
			t.Errorf("parseSignalStrict(%q) = nil error, want error for unknown signal", bad)
		}
	}
}

func TestParseSignal_FallsBackToTERM(t *testing.T) {
	if got := parseSignal("totally-invalid"); got != syscall.SIGTERM {
		t.Errorf("parseSignal(invalid) = %v, want SIGTERM fallback", got)
	}
}

// Drift guard: every signal name config accepts in validation must resolve in
// the process package's runtime parser, or a config that passes check-config
// would silently coerce to SIGTERM at runtime.
func TestSignalNames_ConfigAndRuntimeAgree(t *testing.T) {
	for name := range config.KnownSignalNames {
		if _, err := parseSignalStrict(name); err != nil {
			t.Errorf("config accepts signal %q but process.parseSignalStrict rejects it: %v", name, err)
		}
	}
}
