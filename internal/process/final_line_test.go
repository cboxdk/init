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

// A crash message is characteristically an UNTERMINATED final line — fprintf to
// stderr then abort, or a line cut short by SIGKILL. The writers buffer an
// incomplete line waiting for its newline, so without a flush when the process
// exits that last output is dropped: exactly the log needed to debug a crash
// loop. (With multiline enabled the whole buffered stack trace goes with it.)
func TestSupervisor_FinalPartialLineIsCaptured(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	sup := NewSupervisor("crasher", &config.Process{
		Enabled: true, Type: "oneshot", InitialState: "running", Restart: "never", Scale: 1,
		Command: []string{"sh", "-c", "echo COMPLETE-LINE; printf 'FATAL-PARTIAL-LINE'; exit 3"},
		Logging: &config.LoggingConfig{Stdout: true, Stderr: true},
	}, &config.GlobalConfig{LogLevel: "error", MaxRestartAttempts: 1, RestartBackoff: 1},
		logger, audit.NewLogger(logger, false), nil)

	if err := sup.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)

	var all string
	for _, e := range sup.GetLogs(0) {
		all += e.Message + "\n"
	}
	t.Logf("captured logs:\n%s", all)
	if !strings.Contains(all, "COMPLETE-LINE") {
		t.Error("complete line missing")
	}
	if !strings.Contains(all, "FATAL-PARTIAL-LINE") {
		t.Error("final unterminated line was lost — a crash message would be dropped")
	}
}
