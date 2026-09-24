package process

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
)

// instanceSeriesLeft returns every exposed cbox_init_ series carrying the given
// instance label, whatever its process-label key.
func instanceSeriesLeft(t *testing.T, instanceID string) []string {
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
				if lp.GetName() == "instance" && lp.GetValue() == instanceID {
					left = append(left, mf.GetName())
				}
			}
		}
	}
	return left
}

// TestManager_ScaleDownDropsInstanceSeries drives a real scale-down and checks
// that the removed instances disappear from /metrics. Before the fix,
// cbox_init_process_up{name=...,instance=...-2} stayed at 0 forever because the
// cleanup matched on "process", a label that family does not have.
func TestManager_ScaleDownDropsInstanceSeries(t *testing.T) {
	const name = "scale-metrics-proc"

	cfg := &config.Config{
		Global: config.GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "error",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*config.Process{
			name: {
				Enabled:      true,
				InitialState: "running",
				Command:      []string{"sleep", "60"},
				Restart:      "never",
				Scale:        3,
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := NewManager(cfg, logger, audit.NewLogger(logger, false))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("start manager: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	}()

	removed := []string{name + "-1", name + "-2"}
	kept := name + "-0"

	// Wait until every instance has published its series.
	deadline := time.Now().Add(5 * time.Second)
	for {
		ready := len(instanceSeriesLeft(t, kept)) > 0
		for _, id := range removed {
			ready = ready && len(instanceSeriesLeft(t, id)) > 0
		}
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("instances never published cbox_init_ series")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := manager.ScaleProcess(ctx, name, 1); err != nil {
		t.Fatalf("scale down: %v", err)
	}

	// ScaleDown drops the series before it returns, so no polling here: a
	// series still present now is a series that stays.
	for _, id := range removed {
		if left := instanceSeriesLeft(t, id); len(left) > 0 {
			t.Errorf("instance %s was scaled away but still exposes %v", id, left)
		}
	}
	if len(instanceSeriesLeft(t, kept)) == 0 {
		t.Errorf("surviving instance %s lost its series", kept)
	}
}
