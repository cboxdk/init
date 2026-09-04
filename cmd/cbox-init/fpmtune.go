package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cboxdk/fpm-tune/plan"
	"github.com/cboxdk/fpm-tune/serve"
	"github.com/cboxdk/fpm-tune/state"

	"github.com/cboxdk/init/internal/config"
)

// The p95 hybrid is the intended default sizing basis: size on the 95th
// percentile of the worker memory distribution plus a small margin, floored so it
// still reacts to a real jump in one scrape. It mirrors fpm-tune's own default
// (`serve -sizing p95`); the zero value would instead be the pure peak-follower,
// which sizes forever on the worst worker ever seen and consistently over-provisions.
const (
	fpmSizingPercentile = 0.95
	fpmSizingMargin     = 0.10
)

// startFPMTune starts the embedded runtime PHP-FPM autotuner as a background
// loop when it is enabled. It returns a stop function that halts the loop and
// waits for it to finish — releasing its state-file lock and saving learned
// baselines. Call the stop function BEFORE php-fpm is torn down, because the loop
// rewrites php-fpm's pool configuration and reloads it; a nil stop function means
// the autotuner was disabled and nothing was started.
//
// The loop embeds github.com/cboxdk/fpm-tune. It does its own discovery of the
// running master (scan-and-retry), so starting it here, right after the processes
// are up, is fine even if php-fpm is still coming up. Its config is loaded per
// round, so a pool's boot-time pm.max_children (set by the calculator before
// php-fpm started) is the seed it refines, not something it fights.
func startFPMTune(ctx context.Context, cfg *config.Config, log *slog.Logger) (func(), error) {
	ft := cfg.Global.FPMTune
	if ft == nil || !ft.Enabled {
		return nil, nil
	}

	sc := serve.Config{
		// apply is the default and the point of embedding it; advisory is the only
		// opt-out. The empty paths (state, backup, drop-in) are resolved by
		// fpm-tune's own Config.Defaults() exactly as the standalone tool resolves them.
		Apply:           ft.Mode != "advisory",
		Interval:        ft.Interval,
		DropInDir:       ft.DropInDir,
		StatePath:       ft.StatePath,
		BackupDir:       ft.BackupDir,
		MetricsAddr:     ft.MetricsAddr,
		RecommendPath:   ft.RecommendPath,
		ReserveFraction: ft.ReserveFraction,
		Workload:        resolveFPMWorkload(ft.Workload, log),
		Version:         version, // reported on the loop's /history.json

		StateOptions: state.Options{
			Sizing: state.Sizing{Percentile: fpmSizingPercentile, Margin: fpmSizingMargin},
		},
	}

	loop, err := serve.New(sc, log)
	if err != nil {
		return nil, fmt.Errorf("fpm-tune: %w", err)
	}

	// The loop gets its own context so shutdown can stop it independently: the
	// serve context is cancelled only after graceful shutdown has already run, and
	// a tuner that keeps rewriting and reloading php-fpm while it is being drained
	// is the one thing we must not allow.
	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := loop.Run(loopCtx); err != nil {
			log.Error("Runtime PHP-FPM autotuner exited with error", "error", err)
		}
	}()

	log.Info("Runtime PHP-FPM autotuner started",
		"mode", ft.Mode,
		"apply", sc.Apply,
		"interval", ft.Interval,
		"metrics", ft.MetricsAddr,
	)

	stop := func() {
		cancel()
		<-done // Run's deferred Close() releases the state lock and saves baselines.
	}

	return stop, nil
}

// resolveFPMWorkload maps the configured workload name to a class, warning on an
// unknown name rather than failing — the same forgiving behaviour as the fpm-tune
// CLI, so a typo degrades to the safe web default instead of stopping the daemon.
func resolveFPMWorkload(name string, log *slog.Logger) plan.Workload {
	if name == "" {
		return plan.WorkloadWeb
	}
	w, ok := plan.WorkloadByName(name, plan.WorkloadWeb)
	if !ok {
		log.Warn("Unknown fpm_tune.workload; using the web default", "name", name)
	}

	return w
}
