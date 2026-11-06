package snapshot

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type statefulWorkload struct {
	fakeWorkload
	checkpointed atomic.Bool
}

func (w *statefulWorkload) CompleteCheckpoint() {
	w.fakeWorkload.CompleteCheckpoint()
	w.checkpointed.Store(true)
}

func (w *statefulWorkload) EndCheckpoint() {
	w.fakeWorkload.EndCheckpoint()
	w.checkpointed.Store(false)
}

func (w *statefulWorkload) IsCheckpointed() bool { return w.checkpointed.Load() }

func TestManualSleepAndWakePreserveSingleFlight(t *testing.T) {
	control := &fakeControl{}
	w := &statefulWorkload{}
	c, p := newTestCoordinator(t, control, w)
	c.idle = time.Hour
	closed := p.Activity().Opened()
	closed()
	c.maybeSleep(context.Background())
	if control.checkpoints != 0 {
		t.Fatal("automatic sleep ignored idle timer")
	}
	for range 2 {
		if err := c.Sleep(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if status := c.Status(); status.State != "asleep" || !status.Supported || status.IdleSeconds != 3600 {
		t.Fatalf("status = %+v", status)
	}
	for range 2 {
		if err := c.Wake(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if control.checkpoints != 1 || control.restores != 1 || w.coldStarts.Load() != 0 || c.Status().State != "awake" {
		t.Fatalf("checkpoint/restore = %d/%d, status = %+v", control.checkpoints, control.restores, c.Status())
	}
}

func TestManualSleepRefusesBusyOrUnconfirmedWork(t *testing.T) {
	for _, kind := range []string{"connection", "request", "php", "arrival", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			control := &fakeControl{}
			w := &statefulWorkload{}
			c, p := newTestCoordinator(t, control, w)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := ErrWorkloadBusy
			switch kind {
			case "connection":
				defer p.Activity().Opened()()
			case "request":
				defer p.Activity().Started()()
			case "php":
				c.quiesce = func(context.Context) error { return errors.New("Finishing") }
			case "arrival":
				c.quiesce = func(context.Context) error { t.Cleanup(p.Activity().Opened()); return nil }
			case "cancel":
				want = context.Canceled
				c.quiesce = func(context.Context) error { cancel(); return context.Canceled }
			}
			if err := c.Sleep(ctx); !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
			if p.Asleep() || control.checkpoints != 0 || w.begun.Load() != 0 || c.Disabled() != "" {
				t.Fatal("refusal changed workload or penalized sleep")
			}
		})
	}
}

func TestSnapshotControlDeadlineAndNonBlockingStatus(t *testing.T) {
	c, _ := newTestCoordinator(t, &fakeControl{}, &statefulWorkload{})
	c.opMu.Lock()
	defer c.opMu.Unlock()
	for _, action := range []func(context.Context) error{c.Sleep, c.Wake} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		if err := action(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("error = %v", err)
		}
		cancel()
	}
	if status := c.Status(); status.State != "transitioning" {
		t.Fatalf("status = %+v", status)
	}
}

func TestManualSleepUncertainOutcomeFencesWithoutRetryOrColdStart(t *testing.T) {
	control := &fakeControl{checkpointer: func() error { return ErrOperationUncertain }}
	w := &statefulWorkload{}
	c, p := newTestCoordinator(t, control, w)
	fenced := 0
	c.fence = func(error) { fenced++ }
	for _, action := range []func(context.Context) error{c.Sleep, c.Sleep, c.Wake} {
		if err := action(context.Background()); !errors.Is(err, ErrOperationUncertain) {
			t.Fatalf("error = %v", err)
		}
	}
	if fenced != 1 || control.checkpoints != 1 || control.restores != 0 || w.coldStarts.Load() != 0 || w.aborted.Load() != 0 || !p.Asleep() {
		t.Fatal("uncertain operation was retried, reopened or recovered without fencing")
	}
	if status := c.Status(); status.State == "asleep" || status.State == "awake" {
		t.Fatalf("uncertain state = %+v", status)
	}
}

func TestSnapshotStatusDoesNotWakeOrRefreshIdle(t *testing.T) {
	control := &fakeControl{}
	w := &statefulWorkload{}
	c, p := newTestCoordinator(t, control, w)
	if c.Status().State != "awake" {
		t.Fatal("expected awake")
	}
	if !p.Activity().IdleFor(time.Nanosecond) {
		t.Fatal("observation refreshed idle")
	}
	p.SetAsleep(true)
	if c.Status().State != "unknown" {
		t.Fatal("proxy admission alone is not a completed checkpoint")
	}
	w.checkpointed.Store(true)
	if c.Status().State != "asleep" || control.restores != 0 {
		t.Fatal("observation woke workload")
	}
	c.disable("qualification lost")
	if status := c.Status(); status.State != "disabled" || status.DisabledReason != "qualification lost" {
		t.Fatalf("status = %+v", status)
	}
	if err := c.Sleep(context.Background()); !errors.Is(err, ErrSleepUnavailable) {
		t.Fatalf("error = %v", err)
	}
}
