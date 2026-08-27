package process

import (
	"context"
	"log/slog"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
)

// TestStopInstanceRetryDoesNotFakeSuccess covers how a failed stop turned into
// an orphaned process.
//
// stopInstance short-circuited on any state other than StateRunning, and a stop
// that fails leaves the instance in StateStopping. So the SECOND call found
// "not running", returned nil, and Stop() took that nil as permission to drop
// the instance from its list — abandoning a process that was still alive. That
// is precisely the orphaning that keeping the instances on error was written to
// prevent, and rollbackReload retried automatically, so no operator had to do
// anything to trigger it.
func TestStopInstanceRetryDoesNotFakeSuccess(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	s := NewSupervisor("retry-probe", &config.Process{
		Command: []string{"/bin/sh", "-c", "sleep 30"},
		Restart: "never",
		Scale:   1,
	},
		&config.GlobalConfig{LogLevel: "error", MaxRestartAttempts: 1, RestartBackoff: 1},
		log, audit.NewLogger(log, false), nil)

	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	s.mu.RLock()
	if len(s.instances) == 0 {
		s.mu.RUnlock()
		t.Fatal("no instances after Start")
	}
	inst := s.instances[0]
	s.mu.RUnlock()

	// Put the instance in the state a failed stop leaves behind, without
	// actually killing anything: the process is still alive.
	inst.mu.Lock()
	inst.state = StateStopping
	inst.mu.Unlock()

	if err := stopIsRealFor(t, s, inst); err != nil {
		t.Fatal(err)
	}
}

func stopIsRealFor(t *testing.T, s *Supervisor, inst *Instance) error {
	t.Helper()

	if err := s.stopInstance(context.Background(), inst); err != nil {
		// An error is a perfectly acceptable outcome — the caller then keeps the
		// instance. What must not happen is a nil error with a live process.
		t.Logf("stopInstance reported: %v", err)
		return nil
	}

	// nil error means "this instance is stopped". Hold the caller to it.
	inst.mu.Lock()
	state := inst.state
	pid := inst.pid
	inst.mu.Unlock()

	if state == StateRunning {
		t.Errorf("stopInstance returned nil but the instance is still running (pid %d)", pid)
	}

	if pid > 0 && processIsAlive(pid) {
		t.Errorf("stopInstance returned nil but pid %d is still alive; "+
			"the caller will now drop it from the instance list and orphan it", pid)
	}

	return nil
}

func processIsAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 probes for existence without delivering anything.
	return p.Signal(syscall.Signal(0)) == nil
}

// TestUpdateProcessKeepsScheduledProcessesScheduled: AddProcess knows that a
// scheduled process belongs to the scheduler rather than to a supervisor;
// updateProcessLocked did not. Editing a cron expression through the API or the
// TUI therefore built a longrun supervisor AND left the cron job registered — a
// nightly `php artisan backup:run` became a continuous restart loop that also
// still fired on schedule.
func TestUpdateProcessKeepsScheduledProcessesScheduled(t *testing.T) {
	m := newSchedulingTestManager(t)
	ctx := context.Background()

	job := &config.Process{
		Enabled:  true,
		Command:  []string{"/bin/true"},
		Type:     "oneshot",
		Restart:  "never",
		Scale:    1,
		Schedule: "0 3 * * *",
	}
	if err := m.AddProcess(ctx, "backup", job); err != nil {
		t.Fatalf("AddProcess: %v", err)
	}

	if _, hasSupervisor := m.processes["backup"]; hasSupervisor {
		t.Fatal("AddProcess built a supervisor for a scheduled process")
	}

	// Edit the schedule, exactly as the API's PUT handler does.
	edited := *job
	edited.Schedule = "0 4 * * *"
	if err := m.UpdateProcess(ctx, "backup", &edited); err != nil {
		t.Fatalf("UpdateProcess: %v", err)
	}

	if sup, hasSupervisor := m.processes["backup"]; hasSupervisor {
		running := 0
		sup.mu.RLock()
		running = len(sup.instances)
		sup.mu.RUnlock()
		t.Errorf("editing a cron expression started a longrun supervisor with %d instance(s); "+
			"the job now runs continuously as well as on its schedule", running)
	}

	job2, ok := m.scheduler.GetJob("backup")
	if !ok {
		t.Fatal("the scheduled job disappeared after the edit")
	}
	if job2.Schedule != "0 4 * * *" {
		t.Errorf("the job still holds the old schedule %q", job2.Schedule)
	}
}

func newSchedulingTestManager(t *testing.T) *Manager {
	t.Helper()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := &config.Config{
		Version:   "1.0",
		Processes: map[string]*config.Process{},
	}
	cfg.SetDefaults()

	m := NewManager(cfg, log, audit.NewLogger(log, false))
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	return m
}

// TestClientDisconnectDoesNotForceKill: stopInstance selected on ctx.Done(),
// which cannot tell a deadline from a cancellation. API handlers build their
// context from r.Context(), so it dies the moment the client goes away — and
// Ctrl-C on `cbox-init stop`, a curl timeout, or an ingress read timeout became
// an immediate SIGKILL of a worker mid-job.
func TestClientDisconnectDoesNotForceKill(t *testing.T) {
	configured := 3 * time.Second

	// A cancelled context must not shorten the graceful window at all.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	got, bounded := effectiveStopTimeout(cancelled, configured)
	if got != configured || bounded {
		t.Errorf("a cancelled context shortened the graceful window to %v (deadline-bound=%v); "+
			"a client hanging up must not SIGKILL the workload", got, bounded)
	}

	// No deadline: the configured value stands.
	got, bounded = effectiveStopTimeout(context.Background(), configured)
	if got != configured || bounded {
		t.Errorf("a context with no deadline gave %v (deadline-bound=%v)", got, bounded)
	}

	// A deadline further out than the configured timeout also leaves it alone.
	far, cancelFar := context.WithTimeout(context.Background(), time.Minute)
	defer cancelFar()

	got, bounded = effectiveStopTimeout(far, configured)
	if got != configured || bounded {
		t.Errorf("a distant deadline shortened the window to %v (deadline-bound=%v)", got, bounded)
	}

	// A nearer deadline DOES shorten it — that is the shutdown path, where the
	// global budget has to win over a larger per-process timeout.
	near, cancelNear := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancelNear()

	got, bounded = effectiveStopTimeout(near, configured)
	if !bounded || got > configured {
		t.Errorf("a nearer deadline gave %v (deadline-bound=%v); the global shutdown budget must win", got, bounded)
	}

	// An already-expired deadline still leaves a minimal window, so SIGTERM and
	// SIGKILL are not delivered in the same instant.
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer cancelExpired()

	got, _ = effectiveStopTimeout(expired, configured)
	if got < minStopGrace {
		t.Errorf("an expired deadline gave a %v window; SIGTERM would not land before SIGKILL", got)
	}
}
