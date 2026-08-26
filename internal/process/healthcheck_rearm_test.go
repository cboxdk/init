package process

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/config"
)

func hmLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// PID1-4: after a health-triggered restart the monitor must forget the failures
// that killed the old instance and give the replacement a fresh warmup grace,
// or a slow-booting service is killed on the next probe and abandoned.
func TestHealthMonitor_Rearm_ResetsCountersAndGraces(t *testing.T) {
	cfg := &config.HealthCheck{
		Type: "exec", Command: []string{"false"}, // always fails
		Timeout: 2, Period: 1, FailureThreshold: 2, InitialDelay: 5,
	}
	hm, err := NewHealthMonitor("p", cfg, hmLogger())
	if err != nil {
		t.Fatalf("NewHealthMonitor: %v", err)
	}
	ctx := context.Background()

	hm.performCheck(ctx) // fail 1
	st := hm.performCheck(ctx)
	if st.Healthy {
		t.Fatalf("expected unhealthy after %d failures", cfg.FailureThreshold)
	}

	hm.Rearm()

	if !hm.inGrace() {
		t.Error("expected a warmup grace window after Rearm")
	}
	hm.mu.Lock()
	fails, healthy := hm.consecutiveFails, hm.currentlyHealthy
	hm.mu.Unlock()
	if fails != 0 {
		t.Errorf("consecutiveFails = %d after Rearm, want 0", fails)
	}
	if !healthy {
		t.Error("currentlyHealthy should be reset to true after Rearm")
	}
}

// CONC-9: the monitor goroutine must exit when its context is cancelled, even
// when a status is already buffered and it is trying to send the next one. If
// the send weren't ctx-guarded it would block forever on the capacity-1 channel
// and leak the goroutine and its ticker. Proven here by the status channel
// closing (the goroutine's deferred close) after cancel.
func TestHealthMonitor_NoLeakOnCancel(t *testing.T) {
	cfg := &config.HealthCheck{
		Type: "exec", Command: []string{"true"},
		Timeout: 2, Period: 1, FailureThreshold: 1, InitialDelay: 0,
	}
	hm, err := NewHealthMonitor("p", cfg, hmLogger())
	if err != nil {
		t.Fatalf("NewHealthMonitor: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch := hm.Start(ctx)

	// Deliberately do NOT consume: the first status buffers, and the next tick's
	// send would block without the ctx guard.
	time.Sleep(1500 * time.Millisecond)
	cancel()

	deadline := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // channel closed => goroutine returned, no leak
			}
		case <-deadline:
			t.Fatal("health monitor goroutine leaked: status channel never closed after ctx cancel")
		}
	}
}
