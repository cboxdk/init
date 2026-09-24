package hooks

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/config"
)

// A hook that starts something in the background and exits is done when it
// exits. os/exec used to wait for the background child to close the captured
// output pipe — past the hook's own timeout, since that only kills the hook —
// so a pre-start hook like that held container startup for as long as the
// child lived.
func TestExecute_ReturnsWhenHookExitsWhileChildHoldsOutput(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "helper.pid")
	t.Cleanup(func() {
		if data, err := os.ReadFile(pidFile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})

	e := NewExecutor(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	hook := func(exit string) *config.Hook {
		return &config.Hook{
			Name:    "spawns-daemon",
			Command: []string{"sh", "-c", "sleep 20 & echo $! > " + pidFile + "; echo started; exit " + exit},
			Timeout: 30,
		}
	}

	start := time.Now()
	if err := e.Execute(context.Background(), hook("0")); err != nil {
		t.Errorf("hook that exited 0 reported %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Execute took %v: it waited for the background child, not the hook", elapsed)
	}

	if err := e.Execute(context.Background(), hook("2")); err == nil {
		t.Error("hook that exited 2 reported success")
	}
}
