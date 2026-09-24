package api

import (
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/process"
)

// A stop through the API must be given room for the whole stop sequence the
// process is configured with — the graceful timeout AND the wait after the kill
// signal — or the request deadline cuts the kill_timeout short and the API
// reports a failed stop for a process that was still being stopped correctly.
func TestStopContext_IncludesKillTimeout(t *testing.T) {
	cases := []struct {
		name     string
		shutdown *config.ShutdownConfig
		want     time.Duration
	}{
		{"no shutdown block keeps the default", nil, defaultAPIActionTimeout},
		{"default kill_timeout keeps the old 15s margin", &config.ShutdownConfig{Timeout: 100}, 115 * time.Second},
		{"kill_timeout extends the budget", &config.ShutdownConfig{Timeout: 100, KillTimeout: 30}, 140 * time.Second},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
			mgr := process.NewManager(&config.Config{
				Global: config.GlobalConfig{ShutdownTimeout: 30, LogLevel: "error", MaxRestartAttempts: 1, RestartBackoff: 1},
				Processes: map[string]*config.Process{
					"db": {
						Enabled: true, Command: []string{"sleep", "300"}, Restart: "never",
						Scale: 1, InitialState: "stopped", Shutdown: tc.shutdown,
					},
				},
			}, logger, audit.NewLogger(logger, false))
			server := NewServer(9180, "", "", nil, nil, false, 0, mgr, logger)

			ctx, cancel := server.stopContext(httptest.NewRequest("POST", "/api/v1/processes/db/stop", nil), "db")
			defer cancel()

			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("stop context has no deadline")
			}
			got := time.Until(deadline)
			if got > tc.want || got < tc.want-2*time.Second {
				t.Errorf("stop budget = %v, want ~%v", got.Round(time.Second), tc.want)
			}
		})
	}
}
