package process

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/metrics"
)

// processSeriesLeft returns every exposed cbox_init_ series that carries the
// process under either of the keys in use: "name" (lifecycle, health, scaling)
// or "process" (resource metrics).
func processSeriesLeft(t *testing.T, process string) []string {
	t.Helper()

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	var left []string
	for _, mf := range families {
		if !strings.HasPrefix(mf.GetName(), "cbox_init_") {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if (lp.GetName() == "name" || lp.GetName() == "process") && lp.GetValue() == process {
					left = append(left, mf.GetName()+"{"+lp.GetName()+"}")
				}
			}
		}
	}
	return left
}

// hasSeries reports whether the process has a series under the given key.
func hasSeries(t *testing.T, process, key string) bool {
	t.Helper()
	for _, s := range processSeriesLeft(t, process) {
		if strings.HasSuffix(s, "{"+key+"}") {
			return true
		}
	}
	return false
}

// hasFamily reports whether the process has a series in the given family.
func hasFamily(t *testing.T, process, family string) bool {
	t.Helper()
	for _, s := range processSeriesLeft(t, process) {
		if strings.HasPrefix(s, family+"{") {
			return true
		}
	}
	return false
}

// bufferKeysFor returns the resource collector's buffer keys for instances of
// process. Keys are "<process>-<instance>" and instance IDs are
// "<process>-<n>", so an exact instance-ID match cannot confuse "a" with "a-b".
func bufferKeysFor(rc *metrics.ResourceCollector, process string, scale int) []string {
	var keys []string
	sizes := rc.GetBufferSizes()
	for i := 0; i < scale; i++ {
		key := fmt.Sprintf("%s-%s-%d", process, process, i)
		if _, ok := sizes[key]; ok {
			keys = append(keys, key)
		}
	}
	return keys
}

// telemetryConfig renders a config whose processes produce every kind of
// per-process series: lifecycle and scaling (name=), health checks (name=)
// and, with resource metrics on a 1s interval, resource series (process=).
func telemetryConfig(processes ...string) string {
	var b strings.Builder
	b.WriteString("version: \"1.0\"\n" +
		"global:\n  shutdown_timeout: 5\n  log_level: error\n" +
		"  resource_metrics_enabled: true\n  resource_metrics_interval: 1\n" +
		"processes:\n")
	for _, p := range processes {
		fmt.Fprintf(&b, "  %s:\n    enabled: true\n    type: longrun\n"+
			"    command: [\"sleep\", \"60\"]\n    restart: never\n    scale: 2\n"+
			"    health_check:\n      type: exec\n      command: [\"true\"]\n"+
			"      initial_delay: 0\n      period: 1\n      timeout: 1\n", p)
	}
	return b.String()
}

// startTelemetryManager writes the config, starts a manager on it and waits
// until every process has published both name= and process= series and
// resource buffers, so the removal assertions cannot pass vacuously.
func startTelemetryManager(t *testing.T, processes ...string) (*Manager, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "cbox-init.yaml")
	if err := os.WriteFile(path, []byte(telemetryConfig(processes...)), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.LoadWithEnvExpansion(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(cfg, logger, audit.NewLogger(logger, false))
	m.SetConfigPath(path)

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("start manager: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.Shutdown(ctx)
	})

	rc := m.GetResourceCollector()
	if rc == nil {
		t.Fatal("resource collector not created; resource series would never appear")
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		ready := true
		for _, p := range processes {
			ready = ready &&
				hasSeries(t, p, "name") &&
				hasSeries(t, p, "process") &&
				hasFamily(t, p, "cbox_init_health_check_status") &&
				len(bufferKeysFor(rc, p, 2)) == 2
		}
		if ready {
			return m, path
		}
		if time.Now().After(deadline) {
			for _, p := range processes {
				t.Logf("%s: series %v, buffers %v", p, processSeriesLeft(t, p), bufferKeysFor(rc, p, 2))
			}
			t.Fatal("processes never published lifecycle, health and resource series")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// assertForgotten checks that removed has no series and no resource buffers
// left, while kept still has both.
func assertForgotten(t *testing.T, m *Manager, removed, kept string) {
	t.Helper()

	// Removal is synchronous, so no polling: anything present now stays.
	if left := processSeriesLeft(t, removed); len(left) > 0 {
		t.Errorf("removed process %q still exposes %v", removed, left)
	}
	rc := m.GetResourceCollector()
	if keys := bufferKeysFor(rc, removed, 2); len(keys) > 0 {
		t.Errorf("removed process %q still holds resource buffers %v", removed, keys)
	}

	if !hasSeries(t, kept, "name") || !hasSeries(t, kept, "process") {
		t.Errorf("process %q that was not removed lost series: %v", kept, processSeriesLeft(t, kept))
	}
	if keys := bufferKeysFor(rc, kept, 2); len(keys) != 2 {
		t.Errorf("process %q that was not removed lost resource buffers, has %v", kept, keys)
	}
}

// TestManager_RemoveProcessDropsSeries: removing a process through the API
// path must drop its series. Before, RemoveProcessMetrics was never called, so
// cbox_init_process_up{name=...} sat at 0 forever after the removal.
func TestManager_RemoveProcessDropsSeries(t *testing.T) {
	// "rmapi" is a prefix of "rmapi-keep": removal must not match by prefix.
	const removed, kept = "rmapi", "rmapi-keep"
	m, _ := startTelemetryManager(t, removed, kept)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.RemoveProcess(ctx, removed); err != nil {
		t.Fatalf("RemoveProcess: %v", err)
	}

	assertForgotten(t, m, removed, kept)
}

// TestManager_ReloadRemovalDropsSeries: a reload whose config no longer
// contains a process removes it, and must drop its series too.
func TestManager_ReloadRemovalDropsSeries(t *testing.T) {
	const removed, kept = "rmreload", "rmreload-keep"
	m, path := startTelemetryManager(t, removed, kept)

	if err := os.WriteFile(path, []byte(telemetryConfig(kept)), 0600); err != nil {
		t.Fatalf("write reduced config: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := m.ReloadConfig(ctx); err != nil {
		t.Fatalf("ReloadConfig: %v", err)
	}

	assertForgotten(t, m, removed, kept)
}

// TestManager_RolledBackReloadKeepsSeries: a reload that removes a process but
// then fails and rolls back brings that process back, so its series (counters
// included) must survive. Cleaning up before the reload commits would reset
// them.
func TestManager_RolledBackReloadKeepsSeries(t *testing.T) {
	const dropped = "rmrollback"
	m, path := startTelemetryManager(t, dropped)

	// A counter only this test writes: if it survives, the rollback path did
	// not run the cleanup. A restarted process would recreate process_up, but
	// nothing recreates this.
	metrics.RecordProcessRestart(dropped, "rollback_marker")

	broken := "version: \"1.0\"\n" +
		"global:\n  shutdown_timeout: 5\n  log_level: error\n" +
		"  resource_metrics_enabled: true\n  resource_metrics_interval: 1\n" +
		"processes:\n" +
		"  rmrollback-broken:\n    enabled: true\n    type: longrun\n" +
		"    command: [\"/nonexistent/binary_xyz\"]\n    restart: never\n    scale: 1\n"
	if err := os.WriteFile(path, []byte(broken), 0600); err != nil {
		t.Fatalf("write broken config: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := m.ReloadConfig(ctx); err == nil {
		t.Fatal("expected ReloadConfig to fail and roll back")
	}

	if _, ok := m.processes[dropped]; !ok {
		t.Fatalf("%q was not restored by the rollback", dropped)
	}
	// restart: never, so the marker is the only restarts series this process has.
	if !hasFamily(t, dropped, "cbox_init_process_restarts_total") {
		t.Errorf("rolled-back reload dropped %q's restarts counter; series left: %v", dropped, processSeriesLeft(t, dropped))
	}
}
