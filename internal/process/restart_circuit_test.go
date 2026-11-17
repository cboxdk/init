package process

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
)

func TestCircuitCooldownDoublesUpToTheCap(t *testing.T) {
	first, max := time.Minute, 10*time.Minute
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 10 * time.Minute, 10 * time.Minute}
	for n, w := range want {
		if got := circuitCooldown(first, max, n); got != w {
			t.Errorf("cooldown %d: got %v, want %v", n, got, w)
		}
	}
}

// crashingSupervisor runs `sh -c 'echo x >> starts; exit 1'`: every start leaves a line.
func crashingSupervisor(t *testing.T, global *config.GlobalConfig) (*Supervisor, string) {
	t.Helper()
	starts := filepath.Join(t.TempDir(), "starts")
	logger := batchLogger()
	sup := NewSupervisor("queue", &config.Process{
		Command: []string{"sh", "-c", "echo x >> " + starts + "; exit 1"},
		Restart: "always", Scale: 1, Enabled: true,
	}, global, logger, audit.NewLogger(logger, false), nil)
	return sup, starts
}

func countStarts(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(string(b), "x")
}

// With restart_cooldown set, a process whose restart budget is spent is not
// given up: it is reported failed (health red) and, after the cooldown, gets
// another start (half-open), and keeps getting them.
func TestSpentRestartBudgetOpensTheCircuitAndRetries(t *testing.T) {
	sup, starts := crashingSupervisor(t, &config.GlobalConfig{
		LogLevel: "error", MaxRestartAttempts: 1,
		RestartBackoffInitial: 10 * time.Millisecond, RestartBackoffMax: 10 * time.Millisecond,
		RestartCooldown: 50 * time.Millisecond, RestartCooldownMax: 50 * time.Millisecond,
		RestartStabilityWindow: -1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sup.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = sup.Stop(context.Background()) }()

	// Budget: first start + 1 restart = 2. The breaker must add more.
	deadline := time.Now().Add(5 * time.Second)
	for countStarts(t, starts) < 4 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := countStarts(t, starts); n < 4 {
		t.Fatalf("expected the circuit breaker to keep retrying (>= 4 starts), got %d", n)
	}
}

// While the circuit is open the process is failed: readiness and health stay
// red, exactly as when it was given up for good.
func TestOpenCircuitReportsTheProcessFailed(t *testing.T) {
	sup, starts := crashingSupervisor(t, &config.GlobalConfig{
		LogLevel: "error", MaxRestartAttempts: 1,
		RestartBackoffInitial: 10 * time.Millisecond, RestartBackoffMax: 10 * time.Millisecond,
		RestartCooldown: 2 * time.Second, RestartCooldownMax: 2 * time.Second,
		RestartStabilityWindow: -1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sup.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = sup.Stop(context.Background()) }()
	deadline := time.Now().Add(3 * time.Second)
	for countStarts(t, starts) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if st := sup.GetState(); st != StateFailed {
		t.Fatalf("expected StateFailed while the circuit is open, got %s", st)
	}
}

// Without restart_cooldown (the default) nothing changes: the process is given
// up for good and reported failed, as before the circuit breaker existed.
func TestWithoutCooldownTheProcessIsGivenUpAsBefore(t *testing.T) {
	sup, starts := crashingSupervisor(t, &config.GlobalConfig{
		LogLevel: "error", MaxRestartAttempts: 1,
		RestartBackoffInitial: 10 * time.Millisecond, RestartBackoffMax: 10 * time.Millisecond,
		RestartStabilityWindow: -1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sup.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = sup.Stop(context.Background()) }()

	time.Sleep(500 * time.Millisecond)
	if n := countStarts(t, starts); n != 2 {
		t.Fatalf("expected exactly 2 starts (first + 1 restart) without the breaker, got %d", n)
	}
	if st := sup.GetState(); st != StateFailed {
		t.Fatalf("expected the supervisor failed, got %s", st)
	}
}

// Stopping the process during the cooldown cancels the half-open attempt.
func TestStopDuringCooldownCancelsTheAttempt(t *testing.T) {
	sup, starts := crashingSupervisor(t, &config.GlobalConfig{
		LogLevel: "error", MaxRestartAttempts: 1,
		RestartBackoffInitial: 10 * time.Millisecond, RestartBackoffMax: 10 * time.Millisecond,
		RestartCooldown: 300 * time.Millisecond, RestartCooldownMax: 300 * time.Millisecond,
		RestartStabilityWindow: -1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sup.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for countStarts(t, starts) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond) // the circuit is open now
	_ = sup.Stop(context.Background())
	time.Sleep(600 * time.Millisecond) // past the cooldown
	if n := countStarts(t, starts); n != 2 {
		t.Fatalf("expected no start after Stop during the cooldown, got %d starts", n)
	}
}
