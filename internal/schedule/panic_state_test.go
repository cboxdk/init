package schedule

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

type panickingExecutor struct{ calls int }

func (p *panickingExecutor) Execute(ctx context.Context, name string) (int, error) {
	p.calls++
	panic("executor blew up")
}

// TestPanickingJobDoesNotWedgeInExecuting: the state was reset only on the
// normal return path, so a panicking executor left the job in
// JobStateExecuting for good. The overlap check then refused every subsequent
// run with "already executing", and because the cron chain recovers the panic,
// the process survived — the job was silently retired with nothing to explain
// it.
func TestPanickingJobDoesNotWedgeInExecuting(t *testing.T) {
	lg := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	exec := &panickingExecutor{}

	job, err := NewScheduledJob("boom", "* * * * *", "UTC", 10, exec, lg)
	if err != nil {
		t.Fatalf("NewScheduledJob: %v", err)
	}

	runAndRecover := func() {
		defer func() { _ = recover() }()
		_, _ = job.executeSync(context.Background(), "test")
	}

	runAndRecover()

	if state := job.GetState(); state != JobStateIdle {
		t.Errorf("after a panic the job is %v, want idle; every later run is refused as "+
			"\"already executing\"", state)
	}

	// The job must actually run again.
	runAndRecover()

	if exec.calls != 2 {
		t.Errorf("executor called %d times; the job was retired after the first panic", exec.calls)
	}

	// And the failed attempts are recorded, not lost.
	if recent := job.History.GetRecent(2); len(recent) != 2 {
		t.Errorf("history holds %d executions, want 2 — a panicking run left no record", len(recent))
	}
}
