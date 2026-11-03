package process

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/config"
)

type checkpointHealthChecker func(context.Context) error

func (f checkpointHealthChecker) Check(ctx context.Context) error { return f(ctx) }

func TestCheckpointHealthDiscardsInflightAndBufferedResults(t *testing.T) {
	hm, err := NewHealthMonitor("checkpoint", &config.HealthCheck{
		Type: "tcp", Timeout: 5, Period: 1, FailureThreshold: 1,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	hm.checker = &NoOpHealthChecker{}
	buffered := hm.performCheck(context.Background())
	if !hm.accepts(buffered) {
		t.Fatal("fresh result rejected")
	}

	started := make(chan struct{})
	hm.checker = checkpointHealthChecker(func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	result := make(chan HealthStatus, 1)
	go func() { result <- hm.performCheck(context.Background()) }()
	<-started
	hm.Pause()
	select {
	case stale := <-result:
		if !stale.suppress || hm.accepts(stale) {
			t.Fatal("cancelled probe accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("pause did not cancel active probe")
	}
	if hm.accepts(buffered) {
		t.Fatal("buffered result accepted while paused")
	}
	hm.checker = checkpointHealthChecker(func(context.Context) error {
		t.Fatal("probe ran while paused")
		return nil
	})
	if !hm.performCheck(context.Background()).suppress {
		t.Fatal("paused probe emitted")
	}
	hm.Rearm()
	if !hm.performCheck(context.Background()).suppress {
		t.Fatal("rearm unpaused checkpoint")
	}
	hm.Resume()
	if hm.accepts(buffered) {
		t.Fatal("old buffered result accepted after resume")
	}
	hm.checker = checkpointHealthChecker(func(context.Context) error { return errors.New("unhealthy after restore") })
	fresh := hm.performCheck(context.Background())
	if fresh.Healthy || !hm.accepts(fresh) {
		t.Fatal("fresh failure not detected after resume")
	}
}

func TestCheckpointSupervisorPausesAndResumesHealth(t *testing.T) {
	for _, recovery := range []string{"abort", "restore", "cold"} {
		t.Run(recovery, func(t *testing.T) {
			sup := checkpointSupervisor(t, "always")
			hm, err := NewHealthMonitor("checkpoint", &config.HealthCheck{
				Type: "tcp", Timeout: 1, Period: 1, FailureThreshold: 1,
			}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			hm.checker = &NoOpHealthChecker{}
			sup.healthMonitor = hm
			sup.healthKnown, sup.healthHealthy, sup.lastCheckSucceeded = true, true, true
			sup.state = StateRunning
			// No child is needed to exercise the supervisor's lifecycle hooks.
			sup.instances = []*Instance{{state: StateRunning, pid: 99999999, allowRestart: true}}
			sup.BeginCheckpoint()
			if !hm.performCheck(context.Background()).suppress {
				t.Fatal("checkpoint did not pause health")
			}
			if recovery == "abort" {
				sup.AbortCheckpoint()
			} else {
				sup.CompleteCheckpoint()
				if sup.GetState() != StateCheckpointed {
					t.Fatal("aggregate state is not checkpointed")
				}
				if recovery == "restore" {
					ctx, cancel := context.WithCancel(context.Background())
					cancel()
					sup.ctx = ctx
					sup.EndCheckpoint()
					sup.goroutines.Wait()
				} else {
					// A failed cold start must resume monitoring and report failure,
					// not leave a nonexistent process marked checkpointed/ready.
					sup.config.Command = []string{"/does-not-exist/checkpoint-test"}
					_, err := sup.RecoverCheckpointed(context.Background())
					if err == nil || sup.GetState() != StateFailed {
						t.Fatal("cold failure was not reported")
					}
					if sup.lastCheckSucceeded || sup.healthKnown {
						t.Fatal("cold replacement inherited readiness")
					}
				}
			}
			if !hm.accepts(hm.performCheck(context.Background())) {
				t.Fatal("health did not resume")
			}
		})
	}
}
