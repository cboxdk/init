package schedule

import (
	"context"
	"sync"
	"testing"
	"time"
)

type funcExecutor struct {
	fn func(ctx context.Context, name string) (int, error)
}

func (f funcExecutor) Execute(ctx context.Context, name string) (int, error) {
	return f.fn(ctx, name)
}

// CONC-5: an async Trigger must outlive the request that started it. net/http
// cancels the request context as soon as the handler returns its 202; if the
// job derived from it directly, it would be cancelled mid-run.
func TestTrigger_DetachesFromCallerContext(t *testing.T) {
	result := make(chan error, 1)
	exec := funcExecutor{fn: func(ctx context.Context, name string) (int, error) {
		select {
		case <-time.After(200 * time.Millisecond):
			result <- nil
			return 0, nil
		case <-ctx.Done():
			result <- ctx.Err()
			return -1, ctx.Err()
		}
	}}
	job, err := NewScheduledJob("j", "* * * * *", "", 10, exec, testLogger())
	if err != nil {
		t.Fatalf("NewScheduledJob: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := job.Trigger(ctx); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	cancel() // caller cancels immediately, as a handler returning 202 does

	select {
	case err := <-result:
		if err != nil {
			t.Errorf("job was cancelled by the caller's context: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("job never completed")
	}
}

// CONC-4: a job still running when the scheduler stops must be cancelled via the
// scheduler's run context, not left running under Background where shutdown
// can't reach it.
func TestScheduledJob_BaseContextCancelsRunningJob(t *testing.T) {
	started := make(chan struct{})
	result := make(chan error, 1)
	var once sync.Once
	exec := funcExecutor{fn: func(ctx context.Context, name string) (int, error) {
		once.Do(func() { close(started) })
		select {
		case <-time.After(5 * time.Second):
			result <- nil
			return 0, nil
		case <-ctx.Done():
			result <- ctx.Err()
			return -1, ctx.Err()
		}
	}}
	job, err := NewScheduledJob("j", "* * * * *", "", 10, exec, testLogger())
	if err != nil {
		t.Fatalf("NewScheduledJob: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	job.SetBaseContext(ctx) // what Scheduler.Start does
	go job.Run()            // what cron does on schedule

	<-started
	cancel() // what Scheduler.Stop does

	select {
	case err := <-result:
		if err == nil {
			t.Error("running job was not cancelled when its base context was cancelled")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("job was not cancelled in time")
	}
}
