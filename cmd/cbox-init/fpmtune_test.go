package main

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/config"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestStartFPMTuneDisabled: a missing or disabled block starts nothing and
// returns a nil stop, so serve can call it unconditionally.
func TestStartFPMTuneDisabled(t *testing.T) {
	for _, ft := range []*config.FPMTuneConfig{nil, {Enabled: false}} {
		cfg := &config.Config{Global: config.GlobalConfig{FPMTune: ft}}
		stop, err := startFPMTune(context.Background(), cfg, discardLogger())
		if err != nil {
			t.Fatalf("disabled autotuner returned an error: %v", err)
		}
		if stop != nil {
			t.Error("disabled autotuner returned a non-nil stop function")
		}
	}
}

// TestStartFPMTuneStartsAndStops drives the real embedded loop (advisory, so it
// never writes php-fpm config) against temp paths, then stops it. The stop must
// return promptly — it releases the state lock and saves baselines. Guarded by a
// timeout so a hang fails the test instead of blocking the suite.
func TestStartFPMTuneStartsAndStops(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Global: config.GlobalConfig{
			FPMTune: &config.FPMTuneConfig{
				Enabled:   true,
				Mode:      "advisory", // never writes php-fpm config in a test
				Interval:  time.Second,
				StatePath: filepath.Join(dir, "state.json"),
				BackupDir: filepath.Join(dir, "backup"),
				// MetricsAddr empty: bind no port.
			},
		},
	}

	stop, err := startFPMTune(context.Background(), cfg, discardLogger())
	if err != nil {
		t.Fatalf("startFPMTune: %v", err)
	}
	if stop == nil {
		t.Fatal("enabled autotuner returned a nil stop function")
	}

	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("stop() did not return; the loop is not shutting down cleanly")
	}

	// The state lock is released, so a second loop on the same state path starts.
	stop2, err := startFPMTune(context.Background(), cfg, discardLogger())
	if err != nil {
		t.Fatalf("second start after stop failed (lock not released?): %v", err)
	}
	if stop2 != nil {
		stop2()
	}
}
