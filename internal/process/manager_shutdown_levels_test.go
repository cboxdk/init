package process

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/testutil"
)

// TestManager_ShutdownWaitsForDependents.
//
// The reverse shutdown order used to be computed and then thrown away: every
// stop launched as a parallel goroutine, so nginx and php-fpm received their
// signals in the same microsecond and nginx kept accepting requests its
// backend could no longer serve (measured on the php-baseimages stop
// benchmark: 502s/resets for 3-15% of in-flight traffic in the stop window).
//
// This pins the contract: a dependency ("backend") must not be SIGNALLED
// until its dependent ("frontend") has FULLY exited - including the
// dependent's drain time, not merely its signal delivery.
func TestManager_ShutdownWaitsForDependents(t *testing.T) {
	dir := t.TempDir()
	frontExit := filepath.Join(dir, "front-exited")
	backSignal := filepath.Join(dir, "back-signalled")

	// frontend traps TERM, drains for 500ms, records its exit time in ns.
	frontCmd := fmt.Sprintf(
		`trap 'sleep 0.5; date +%%s%%N > %s; exit 0' TERM; while true; do sleep 0.1; done`,
		frontExit)
	// backend records when it is signalled, in ns, then exits.
	backCmd := fmt.Sprintf(
		`trap 'date +%%s%%N > %s; exit 0' TERM; while true; do sleep 0.1; done`,
		backSignal)

	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			"backend": {
				Enabled:      true,
				InitialState: "running",
				Command:      []string{"sh", "-c", backCmd},
				Restart:      "never",
				Scale:        1,
				Shutdown:     &config.ShutdownConfig{Timeout: 5},
			},
			"frontend": {
				Enabled:      true,
				InitialState: "running",
				Command:      []string{"sh", "-c", frontCmd},
				DependsOn:    []string{"backend"},
				Restart:      "never",
				Scale:        1,
				Shutdown:     &config.ShutdownConfig{Timeout: 5},
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	auditLogger := audit.NewLogger(logger, false)
	manager := NewManager(cfg, logger, auditLogger)

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start processes: %v", err)
	}
	testutil.Eventually(t, func() bool {
		for _, p := range manager.ListProcesses() {
			if p.Scale < 1 || p.State != "running" {
				return false
			}
		}
		return len(manager.ListProcesses()) == 2
	}, "both processes running", 5*time.Second)
	time.Sleep(100 * time.Millisecond)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := manager.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	readNs := func(path, what string) int64 {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s was never recorded (%v) - did the trap run?", what, err)
		}
		n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		if err != nil {
			t.Fatalf("parsing %s: %v", what, err)
		}
		return n
	}
	frontDone := readNs(frontExit, "frontend exit time")
	backSig := readNs(backSignal, "backend signal time")

	if backSig <= frontDone {
		t.Fatalf("backend was signalled %.0fms BEFORE its dependent finished draining: "+
			"the stop window serves 502s exactly like the parallel-shutdown bug",
			float64(frontDone-backSig)/1e6)
	}
}
