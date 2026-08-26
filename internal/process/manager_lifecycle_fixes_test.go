package process

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/testutil"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// CONC-2: concurrent Start on a scale-1 supervisor must yield exactly one
// instance, not one per caller. The manager's pre-call state check is outside
// any lock, so racing callers all pass it; Start must be idempotent under
// operationMu.
func TestSupervisor_Start_ConcurrentIsIdempotent(t *testing.T) {
	logger := quietLogger()
	sup := NewSupervisor("conc", &config.Process{
		Enabled: true, Type: "longrun", InitialState: "running",
		Command: []string{"sleep", "30"}, Restart: "never", Scale: 1,
	}, &config.GlobalConfig{LogLevel: "error", MaxRestartAttempts: 1, RestartBackoff: 1},
		logger, audit.NewLogger(logger, false), nil)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = sup.Start(context.Background()) }()
	}
	wg.Wait()
	defer func() { _ = sup.Stop(context.Background()) }()

	if n := len(sup.GetInstances()); n != 1 {
		t.Errorf("scale-1 supervisor has %d instances after 4 concurrent Start calls, want 1", n)
	}
}

// ARCH-1: updating one process must restart only that process, not bounce the
// whole stack. A sibling's instance PID must be unchanged after the update.
func TestManager_UpdateProcess_DoesNotRestartSiblings(t *testing.T) {
	logger := quietLogger()
	cfg := &config.Config{
		Global: config.GlobalConfig{LogLevel: "error", MaxRestartAttempts: 3, RestartBackoff: 1, ShutdownTimeout: 5},
		Processes: map[string]*config.Process{
			"target":  {Enabled: true, InitialState: "running", Type: "longrun", Command: []string{"sleep", "60"}, Restart: "always", Scale: 1},
			"sibling": {Enabled: true, InitialState: "running", Type: "longrun", Command: []string{"sleep", "60"}, Restart: "always", Scale: 1},
		},
	}
	m := NewManager(cfg, logger, audit.NewLogger(logger, false))
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = m.Shutdown(ctx)
	}()

	siblingPIDBefore := firstInstancePID(t, m, "sibling")

	newCfg := &config.Process{Enabled: true, InitialState: "running", Type: "longrun", Command: []string{"sleep", "61"}, Restart: "always", Scale: 1}
	if err := m.UpdateProcess(context.Background(), "target", newCfg); err != nil {
		t.Fatalf("UpdateProcess: %v", err)
	}

	siblingPIDAfter := firstInstancePID(t, m, "sibling")
	if siblingPIDBefore != siblingPIDAfter {
		t.Errorf("sibling was restarted by a one-process update: pid %d -> %d", siblingPIDBefore, siblingPIDAfter)
	}
}

// ARCH-3: a process added at runtime must be wired for death detection. If it
// dies with restarts exhausted and it is the only process, the manager's
// all-dead channel must close — which only happens if the added supervisor's
// death notifier reached the manager.
func TestManager_AddProcess_WiresDeathDetection(t *testing.T) {
	logger := quietLogger()
	cfg := &config.Config{
		Global:    config.GlobalConfig{LogLevel: "error", MaxRestartAttempts: 1, RestartBackoff: 1, ShutdownTimeout: 5},
		Processes: map[string]*config.Process{},
	}
	m := NewManager(cfg, logger, audit.NewLogger(logger, false))
	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	defer monitorCancel()
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Start the death-notification consumer, as serve.go does.
	m.MonitorProcessHealth(monitorCtx)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = m.Shutdown(ctx)
	}()

	added := &config.Process{Enabled: true, InitialState: "running", Type: "longrun", Command: []string{"true"}, Restart: "never", Scale: 1}
	if err := m.AddProcess(context.Background(), "ephemeral", added); err != nil {
		t.Fatalf("AddProcess: %v", err)
	}

	select {
	case <-m.AllDeadChannel():
		// good: the added process's death was noticed and propagated.
	case <-time.After(5 * time.Second):
		t.Fatal("all-dead channel never closed: the added process's death notifier was not wired")
	}
}

func firstInstancePID(t *testing.T, m *Manager, name string) int {
	t.Helper()
	var pid int
	testutil.Eventually(t, func() bool {
		for _, p := range m.ListProcesses() {
			if p.Name == name && len(p.Instances) > 0 && p.Instances[0].PID > 0 {
				pid = p.Instances[0].PID
				return true
			}
		}
		return false
	}, "process "+name+" to have a running instance", 3*time.Second)
	return pid
}
