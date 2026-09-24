package process

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/cboxdk/init/internal/config"
)

// seriesByInstance returns every exposed cbox_init_ series that carries the
// given instance label value.
func seriesByInstance(t *testing.T, instanceID string) []string {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	var found []string
	for _, mf := range families {
		if !strings.HasPrefix(mf.GetName(), "cbox_init_") {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "instance" && lp.GetValue() == instanceID {
					found = append(found, mf.GetName())
				}
			}
		}
	}
	return found
}

// telemetryConfigScaled is telemetryConfig with an explicit scale and command,
// so a test can change either and have the process count as updated.
func telemetryConfigScaled(name string, scale int, sleep string) string {
	cfg := telemetryConfig(name)
	cfg = strings.Replace(cfg, "scale: 2", "scale: "+strconv.Itoa(scale), 1)
	return strings.Replace(cfg, `["sleep", "60"]`, `["sleep", "`+sleep+`"]`, 1)
}

// assertInstancesGone checks that the instances scaled away by an update left
// no series, while the ones still running keep theirs.
func assertInstancesGone(t *testing.T, gone, kept []string) {
	t.Helper()
	for _, id := range gone {
		if left := seriesByInstance(t, id); len(left) > 0 {
			t.Errorf("instance %s no longer exists but still exposes %v", id, left)
		}
	}
	for _, id := range kept {
		if len(seriesByInstance(t, id)) == 0 {
			t.Errorf("running instance %s has no series", id)
		}
	}
}

// waitForInstanceSeries waits until the instance has published series.
func waitForInstanceSeries(t *testing.T, ids ...string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		ready := true
		for _, id := range ids {
			ready = ready && len(seriesByInstance(t, id)) > 0
		}
		if ready {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("instances %v never published series", ids)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestManager_ReloadLowerScaleDropsInstanceSeries: a reload that changes a
// process and lowers its scale replaces its supervisor. The old supervisor's
// higher instances are never started again, so their series must go, the
// same as after a live scale-down.
func TestManager_ReloadLowerScaleDropsInstanceSeries(t *testing.T) {
	const name = "updreload"
	m, path := startTelemetryManager(t, name)

	if err := os.WriteFile(path, []byte(telemetryConfigScaled(name, 1, "61")), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := m.ReloadConfig(ctx); err != nil {
		t.Fatalf("ReloadConfig: %v", err)
	}
	waitForInstanceSeries(t, name+"-0")

	assertInstancesGone(t, []string{name + "-1"}, []string{name + "-0"})
}

// TestManager_UpdateLowerScaleDropsInstanceSeries: the same through
// UpdateProcess (API/TUI edit).
func TestManager_UpdateLowerScaleDropsInstanceSeries(t *testing.T) {
	const name = "updapi"
	m, path := startTelemetryManager(t, name)

	cfg, err := config.LoadWithEnvExpansion(writeTemp(t, path, telemetryConfigScaled(name, 1, "61")))
	if err != nil {
		t.Fatalf("load updated config: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := m.UpdateProcess(ctx, name, cfg.Processes[name]); err != nil {
		t.Fatalf("UpdateProcess: %v", err)
	}
	waitForInstanceSeries(t, name+"-0")

	assertInstancesGone(t, []string{name + "-1"}, []string{name + "-0"})
}

// writeTemp writes content next to path and returns the new file's path.
func writeTemp(t *testing.T, path, content string) string {
	t.Helper()
	p := path + ".updated.yaml"
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// scheduledConfig turns the process into a cron job.
func scheduledConfig(name string) string {
	return "version: \"1.0\"\n" +
		"global:\n  shutdown_timeout: 5\n  log_level: error\n" +
		"  resource_metrics_enabled: true\n  resource_metrics_interval: 1\n" +
		"processes:\n" +
		"  " + name + ":\n    enabled: true\n    type: oneshot\n" +
		"    command: [\"true\"]\n    restart: never\n    schedule: \"0 3 * * *\"\n"
}

// TestManager_ReloadToScheduledDropsSeries: a reload that turns a longrun
// process into a scheduled one drops its supervisor. A scheduled process
// exports no process metrics, so every series the supervisor left (process_up
// at 0, desired_scale, health checks) is stale and must go.
func TestManager_ReloadToScheduledDropsSeries(t *testing.T) {
	const name = "updsched"
	m, path := startTelemetryManager(t, name)

	if err := os.WriteFile(path, []byte(scheduledConfig(name)), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := m.ReloadConfig(ctx); err != nil {
		t.Fatalf("ReloadConfig: %v", err)
	}

	if left := processSeriesLeft(t, name); len(left) > 0 {
		t.Errorf("process %q is now scheduled but still exposes %v", name, left)
	}
}

// TestManager_UpdateToScheduledDropsSeries: the same through UpdateProcess.
func TestManager_UpdateToScheduledDropsSeries(t *testing.T) {
	const name = "updapisched"
	m, path := startTelemetryManager(t, name)

	cfg, err := config.LoadWithEnvExpansion(writeTemp(t, path, scheduledConfig(name)))
	if err != nil {
		t.Fatalf("load updated config: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := m.UpdateProcess(ctx, name, cfg.Processes[name]); err != nil {
		t.Fatalf("UpdateProcess: %v", err)
	}

	if left := processSeriesLeft(t, name); len(left) > 0 {
		t.Errorf("process %q is now scheduled but still exposes %v", name, left)
	}
}
