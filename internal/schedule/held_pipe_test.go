package schedule

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A scheduled job that leaves a background child holding its output is done
// when the job itself exits. os/exec used to wait for the child to close the
// pipe, so the job counted as running — and, with overlapping runs skipped,
// every later run of it was skipped — for as long as the child lived; a
// timeout did not help, because the pipe wait outlasts the kill.
func TestExecute_ReturnsWhenJobExitsWhileChildHoldsOutput(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "helper.pid")
	t.Cleanup(func() {
		if data, err := os.ReadFile(pidFile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})

	for _, tc := range []struct {
		name string
		exit int
	}{{"success", 0}, {"failure", 4}} {
		t.Run(tc.name, func(t *testing.T) {
			e := NewProcessExecutor(testLogger())
			if err := e.RegisterProcess("job", ProcessConfig{
				Command: []string{"sh", "-c", "sleep 20 & echo $! > " + pidFile + "; echo done; exit " + strconv.Itoa(tc.exit)},
				Timeout: 30 * time.Second,
			}); err != nil {
				t.Fatalf("register: %v", err)
			}

			start := time.Now()
			code, _ := e.Execute(context.Background(), "job")
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Fatalf("Execute took %v: it waited for the background child, not the job", elapsed)
			}
			if code != tc.exit {
				t.Errorf("exit code = %d, want %d", code, tc.exit)
			}
		})
	}
}
