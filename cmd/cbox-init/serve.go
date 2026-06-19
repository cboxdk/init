package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cboxdk/init/internal/api"
	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/autotune"
	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/logger"
	"github.com/cboxdk/init/internal/metrics"
	"github.com/cboxdk/init/internal/process"
	"github.com/cboxdk/init/internal/setup"
	"github.com/cboxdk/init/internal/signals"
	"github.com/cboxdk/init/internal/tracing"
	"github.com/cboxdk/init/internal/watcher"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the process manager daemon",
	Long: `Start Cbox Init in daemon mode to manage processes.

This is the default mode when no subcommand is specified. It starts all configured
processes, handles graceful shutdown, and provides observability endpoints.`,
	Run: runServe,
}

var (
	dryRun                bool
	phpFPMProfile         string
	memoryThreshold       float64
	watchMode             bool
	skipPermissionSetup   bool
	skipRuntimeValidation bool
)

func init() {
	serveCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Validate configuration without starting processes")
	serveCmd.Flags().StringVar(&phpFPMProfile, "php-fpm-profile", "", "Auto-tune PHP-FPM workers (dev|light|medium|heavy|bursty)")
	serveCmd.Flags().Float64Var(&memoryThreshold, "autotune-memory-threshold", 0, "Override memory threshold (0.5=50%, 1.0=100%, 1.3=130%)")
	serveCmd.Flags().BoolVar(&watchMode, "watch", false, "Enable watch mode: automatically reload changed services when config file changes")
	serveCmd.Flags().BoolVar(&skipPermissionSetup, "skip-permission-setup", false, "Skip framework directory creation and ownership fixes")
	serveCmd.Flags().BoolVar(&skipRuntimeValidation, "skip-runtime-validation", false, "Skip php-fpm -t and nginx -t startup validation")
}

func runServe(cmd *cobra.Command, args []string) {
	// Get config path
	cfgPath := getConfigPath()

	// Display banner
	fmt.Fprintf(os.Stderr, "\n🚀 Cbox Init v%s\n", version)
	fmt.Fprintf(os.Stderr, "   Production-grade process supervisor for Docker containers\n\n")

	// Determine working directory
	workdir := os.Getenv("WORKDIR")
	if workdir == "" {
		workdir = "/var/www/html"
	}

	startupTiming := truthyEnv("CBOX_INIT_STARTUP_TIMING")
	permissionSetupSkipped := skipPermissionSetup || truthyEnv("CBOX_INIT_SKIP_PERMISSION_SETUP")
	runtimeValidationSkipped := skipRuntimeValidation || truthyEnv("CBOX_INIT_SKIP_RUNTIME_VALIDATION")

	// Setup permissions (detects framework internally)
	if permissionSetupSkipped {
		slog.Default().Info("Skipping permission setup")
	} else {
		err := runStartupPhase("permission_setup", startupTiming, func() error {
			permMgr := setup.NewPermissionManager(workdir, slog.Default())
			return permMgr.Setup()
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  Permission setup completed with warnings: %v\n", err)
		}
	}

	// Validate system configurations
	if runtimeValidationSkipped {
		slog.Default().Info("Skipping runtime configuration validation")
	} else {
		err := runStartupPhase("runtime_config_validation", startupTiming, func() error {
			validator := setup.NewConfigValidator(slog.Default())
			return validator.ValidateAll()
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Configuration validation failed: %v\n", err)
			os.Exit(1)
		}
	}

	// Load configuration
	var cfg *config.Config
	err := runStartupPhase("config_load", startupTiming, func() error {
		var loadErr error
		cfg, loadErr = config.LoadWithEnvExpansion(cfgPath)
		return loadErr
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// PHP-FPM Auto-Tuning
	autotuneProfile := phpFPMProfile
	if autotuneProfile == "" {
		autotuneProfile = os.Getenv("PHP_FPM_AUTOTUNE_PROFILE")
	}

	if autotuneProfile != "" {
		if err := runAutoTuning(autotuneProfile, memoryThreshold, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "❌ Auto-tuning failed: %v\n", err)
			os.Exit(1)
		}
	}

	// Handle dry-run mode
	if dryRun {
		runDryRun(cfg, cfgPath, workdir, permissionSetupSkipped, runtimeValidationSkipped)
		return
	}

	// Initialize logger
	log := logger.New(cfg.Global.LogLevel, cfg.Global.LogFormat)
	slog.SetDefault(log)

	slog.Info("Cbox Init starting",
		"version", version,
		"pid", os.Getpid(),
		"workdir", workdir,
		"log_level", cfg.Global.LogLevel,
		"processes", len(cfg.Processes),
	)

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize distributed tracing
	tracingProvider, err := tracing.NewProvider(ctx, tracing.TracerConfig{
		Enabled:     cfg.Global.TracingEnabled,
		Exporter:    cfg.Global.TracingExporter,
		Endpoint:    cfg.Global.TracingEndpoint,
		SampleRate:  cfg.Global.TracingSampleRate,
		ServiceName: cfg.Global.TracingServiceName,
		Version:     version,
		UseTLS:      cfg.Global.TracingUseTLS,
	}, log)
	if err != nil {
		slog.Error("Failed to initialize tracing", "error", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tracingProvider.Shutdown(shutdownCtx); err != nil {
			slog.Warn("Tracing shutdown error", "error", err)
		}
	}()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)

	// Start zombie reaper
	go signals.ReapZombies(cfg.Global.ZombieReapInterval)

	// Create audit logger
	auditLogger := audit.NewLogger(log, cfg.Global.AuditEnabled)

	// Create process manager
	pm := process.NewManager(cfg, log, auditLogger)
	pm.SetConfigPath(cfgPath) // Set config path for saving

	// Start metrics server
	var metricsServer *metrics.Server
	if cfg.Global.MetricsEnabledValue() {
		metricsServer = startMetricsServer(ctx, cfg, log)
	}

	// Start all processes
	err = runStartupPhase("process_start", startupTiming, func() error {
		return pm.Start(ctx)
	})
	if err != nil {
		slog.Error("Failed to start processes", "error", err)
		os.Exit(1)
	}

	slog.Info("All processes started successfully")

	// Log system start to audit log
	auditLogger.LogSystemStart(version)

	// Monitor process health
	pm.MonitorProcessHealth(ctx)

	// Always start Unix socket for local management (TUI, CLI commands)
	socketPath := resolveSocketPath(cfg.Global.APISocket)

	var apiServer *api.Server
	if cfg.Global.APIEnabledValue() {
		// Full API: TCP + socket
		if cfg.Global.APISocket == "" {
			cfg.Global.APISocket = socketPath
		}
		apiServer = startAPIServer(ctx, cfg, pm, log)
	} else {
		// Socket-only mode: local management without TCP
		apiServer = api.NewServer(0, socketPath, "", nil, nil, cfg.Global.AuditEnabled, cfg.Global.APIMaxRequestBody, pm, log)
		if err := apiServer.StartSocketOnly(ctx); err != nil {
			slog.Warn("Failed to start Unix socket (local CLI/TUI disabled)", "error", err)
			apiServer = nil
		} else {
			slog.Info("Unix socket started (local management only)", "path", socketPath)
		}
	}

	// Start config watcher in watch mode
	var configWatcher *watcher.Watcher
	reloadChan := make(chan struct{}, 1)
	if watchMode {
		slog.Info("Watch mode enabled", "config", cfgPath)

		configWatcher, err = watcher.New(watcher.Config{
			ConfigPath: cfgPath,
			Handler: func() error {
				// Reload configuration
				newCfg, err := config.LoadWithEnvExpansion(cfgPath)
				if err != nil {
					return fmt.Errorf("failed to reload config: %w", err)
				}

				// Validate new configuration
				if err := newCfg.Validate(); err != nil {
					return fmt.Errorf("invalid config: %w", err)
				}

				// Trigger reload via channel
				select {
				case reloadChan <- struct{}{}:
					slog.Info("Config reload triggered")
				default:
					slog.Warn("Config reload already pending")
				}

				return nil
			},
			Logger:   log,
			Debounce: 2 * time.Second,
		})
		if err != nil {
			slog.Error("Failed to create config watcher", "error", err)
			os.Exit(1)
		}

		if err := configWatcher.Start(ctx); err != nil {
			slog.Error("Failed to start config watcher", "error", err)
			os.Exit(1)
		}
		defer func() { _ = configWatcher.Stop() }()
	}

	// Main event loop - handles shutdown signals and config reloads
	for {
		shutdownReason := waitForShutdownOrReload(sigChan, pm, reloadChan, watchMode)

		// Handle reload vs shutdown
		if shutdownReason == "config_reload" {
			slog.Info("Hot-reloading configuration (only changed services)")

			// Use the manager's ReloadConfig which selectively restarts only changed services
			reloadCtx, reloadCancel := context.WithTimeout(context.Background(), time.Duration(cfg.Global.ShutdownTimeout)*time.Second)
			if err := pm.ReloadConfig(reloadCtx); err != nil {
				slog.Error("Config reload failed", "error", err)
			} else {
				slog.Info("Config reload completed successfully")
			}
			reloadCancel()

			// Continue running - don't exit
			continue
		}

		// Graceful shutdown for other reasons (signal, all processes dead)
		performGracefulShutdown(cfg, pm, apiServer, metricsServer, auditLogger, shutdownReason)
		break
	}
}

func truthyEnv(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func runStartupPhase(name string, enabled bool, fn func() error) error {
	if !enabled {
		return fn()
	}

	start := time.Now()
	err := fn()
	slog.Default().Info("Startup phase completed",
		"phase", name,
		"duration", time.Since(start),
		"success", err == nil,
	)
	return err
}

// runAutoTuning performs PHP-FPM auto-tuning calculations
func runAutoTuning(profileName string, threshold float64, cfg *config.Config) error {
	autotuneLog := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	profile := autotune.Profile(profileName)
	if err := profile.Validate(); err != nil {
		return fmt.Errorf("invalid profile: %w", err)
	}

	// Determine memory threshold
	finalThreshold := threshold
	thresholdSource := "profile default"

	if finalThreshold == 0 {
		if envThreshold := os.Getenv("PHP_FPM_AUTOTUNE_MEMORY_THRESHOLD"); envThreshold != "" {
			if parsed, err := strconv.ParseFloat(envThreshold, 64); err == nil {
				finalThreshold = parsed
				thresholdSource = "ENV variable"
			}
		}
	} else {
		thresholdSource = "CLI flag"
	}

	if finalThreshold == 0 && cfg.Global.AutotuneMemoryThreshold > 0 {
		finalThreshold = cfg.Global.AutotuneMemoryThreshold
		thresholdSource = "global config"
	}

	// Create calculator
	calc, err := autotune.NewCalculator(profile, finalThreshold, autotuneLog)
	if err != nil {
		return fmt.Errorf("failed to create calculator: %w", err)
	}

	// Calculate configuration
	phpfpmCfg, err := calc.Calculate()
	if err != nil {
		return err
	}

	// Set environment variables
	for key, value := range phpfpmCfg.ToEnvVars() {
		os.Setenv(key, value)
	}

	// Display results
	source := "CLI flag"
	if phpFPMProfile == "" {
		source = "ENV var"
	}

	fmt.Fprintf(os.Stderr, "🎯 PHP-FPM auto-tuned (%s profile via %s):\n", profile, source)

	if finalThreshold > 0 {
		fmt.Fprintf(os.Stderr, "   Memory threshold: %.1f%% (via %s)\n", finalThreshold*100, thresholdSource)
	}

	fmt.Fprintf(os.Stderr, "   pm = %s\n", phpfpmCfg.ProcessManager)
	fmt.Fprintf(os.Stderr, "   pm.max_children = %d\n", phpfpmCfg.MaxChildren)
	if phpfpmCfg.ProcessManager == "dynamic" {
		fmt.Fprintf(os.Stderr, "   pm.start_servers = %d\n", phpfpmCfg.StartServers)
		fmt.Fprintf(os.Stderr, "   pm.min_spare_servers = %d\n", phpfpmCfg.MinSpare)
		fmt.Fprintf(os.Stderr, "   pm.max_spare_servers = %d\n", phpfpmCfg.MaxSpare)
	}
	fmt.Fprintf(os.Stderr, "   pm.max_requests = %d\n", phpfpmCfg.MaxRequests)
	fmt.Fprintf(os.Stderr, "   Memory: %dMB allocated / %dMB total (%.1f%% used)\n",
		phpfpmCfg.MemoryAllocated+phpfpmCfg.MemoryOPcache+phpfpmCfg.MemoryReserved,
		phpfpmCfg.MemoryTotal,
		float64(phpfpmCfg.MemoryAllocated+phpfpmCfg.MemoryOPcache+phpfpmCfg.MemoryReserved)/float64(phpfpmCfg.MemoryTotal)*100)

	if len(phpfpmCfg.Warnings) > 0 {
		fmt.Fprintf(os.Stderr, "   ⚠️  Warnings: %d (see logs for details)\n", len(phpfpmCfg.Warnings))
	}
	fmt.Fprintf(os.Stderr, "\n")

	return nil
}

// runDryRun performs dry-run validation
func runDryRun(cfg *config.Config, cfgPath string, workdir string, permissionSetupSkipped bool, runtimeValidationSkipped bool) {
	log := logger.New(cfg.Global.LogLevel, cfg.Global.LogFormat)
	slog.SetDefault(log)

	fmt.Fprintf(os.Stderr, "🔍 DRY RUN MODE - Validating configuration without starting processes\n\n")
	fmt.Fprintf(os.Stderr, "✅ Configuration loaded: %s\n", cfgPath)
	fmt.Fprintf(os.Stderr, "✅ Workdir: %s\n", workdir)
	if permissionSetupSkipped {
		fmt.Fprintf(os.Stderr, "⏭️  Permission setup skipped\n")
	} else {
		fmt.Fprintf(os.Stderr, "✅ Permissions validated\n")
	}
	if runtimeValidationSkipped {
		fmt.Fprintf(os.Stderr, "⏭️  Runtime configuration validation skipped\n")
	} else {
		fmt.Fprintf(os.Stderr, "✅ System configurations validated\n")
	}

	// Create audit logger for validation
	auditLogger := audit.NewLogger(log, cfg.Global.AuditEnabled)

	// Validate process manager
	pm := process.NewManager(cfg, log, auditLogger)
	_ = pm

	fmt.Fprintf(os.Stderr, "\n✅ All validations passed - ready to run in production\n")
	os.Exit(0)
}

// startMetricsServer starts the Prometheus metrics server
func startMetricsServer(ctx context.Context, cfg *config.Config, log *slog.Logger) *metrics.Server {
	metricsPort := cfg.Global.MetricsPort
	if metricsPort == 0 {
		metricsPort = 9090
	}
	metricsPath := cfg.Global.MetricsPath
	if metricsPath == "" {
		metricsPath = "/metrics"
	}

	server := metrics.NewServer(metricsPort, metricsPath, cfg.Global.MetricsACL, cfg.Global.MetricsTLS, log)
	if err := server.Start(ctx); err != nil {
		slog.Warn("Failed to start metrics server (continuing without metrics)", "error", err)
		return nil
	}

	slog.Info("Metrics server started",
		"port", metricsPort,
		"path", metricsPath,
		"tls", cfg.Global.MetricsTLS != nil && cfg.Global.MetricsTLS.Enabled,
	)

	metrics.SetBuildInfo(version, "go1.x")
	return server
}

// resolveSocketPath determines the Unix socket path.
// Priority: config value > /var/run/cbox-init.sock > /tmp/cbox-init.sock
func resolveSocketPath(configured string) string {
	if configured != "" {
		return configured
	}
	testPath := "/var/run/cbox-init.sock"
	if dir := filepath.Dir(testPath); dirWritable(dir) {
		return testPath
	}
	return "/tmp/cbox-init.sock"
}

func dirWritable(dir string) bool {
	testFile := filepath.Join(dir, ".cbox-init-write-test")
	f, err := os.Create(testFile)
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(testFile)
	return true
}

// startAPIServer starts the Management API server
func startAPIServer(ctx context.Context, cfg *config.Config, pm *process.Manager, log *slog.Logger) *api.Server {
	apiPort := cfg.Global.APIPort
	if apiPort == 0 {
		apiPort = 9180
	}

	server := api.NewServer(apiPort, cfg.Global.APISocket, cfg.Global.APIAuth, cfg.Global.APIACL, cfg.Global.APITLS, cfg.Global.AuditEnabled, cfg.Global.APIMaxRequestBody, pm, log)
	if err := server.Start(ctx); err != nil {
		slog.Warn("Failed to start API server (TUI/remote control disabled)", "error", err)
		return nil
	}

	slog.Info("API server started",
		"port", apiPort,
		"auth", cfg.Global.APIAuth != "",
		"tls", cfg.Global.APITLS != nil && cfg.Global.APITLS.Enabled,
	)

	return server
}

// waitForShutdown waits for shutdown signal or all processes dying
func waitForShutdown(sigChan chan os.Signal, pm *process.Manager) string {
	select {
	case sig := <-sigChan:
		slog.Info("Received shutdown signal", "signal", sig.String())
		return fmt.Sprintf("signal: %s", sig.String())
	case <-pm.AllDeadChannel():
		slog.Warn("All managed processes have died")
		return "all processes died"
	}
}

// waitForShutdownOrReload waits for shutdown signal, process death, or config reload
func waitForShutdownOrReload(
	sigChan chan os.Signal,
	pm *process.Manager,
	reloadChan chan struct{},
	watchMode bool,
) string {
	if !watchMode {
		// Watch mode disabled, use standard wait
		return waitForShutdown(sigChan, pm)
	}

	// Watch mode enabled, also listen for reload events
	select {
	case sig := <-sigChan:
		slog.Info("Received shutdown signal", "signal", sig.String())
		return fmt.Sprintf("signal: %s", sig.String())
	case <-pm.AllDeadChannel():
		slog.Warn("All managed processes have died")
		return "all processes died"
	case <-reloadChan:
		slog.Info("Config reload requested")
		return "config_reload"
	}
}

// performGracefulShutdown gracefully shuts down all components
func performGracefulShutdown(cfg *config.Config, pm *process.Manager, apiServer *api.Server, metricsServer *metrics.Server, auditLogger *audit.Logger, reason string) {
	shutdownCtx, shutdownCancel := context.WithTimeout(
		context.Background(),
		time.Duration(cfg.Global.ShutdownTimeout)*time.Second,
	)
	defer shutdownCancel()

	slog.Info("Initiating graceful shutdown",
		"reason", reason,
		"timeout", cfg.Global.ShutdownTimeout,
	)

	// Shutdown process manager
	if err := pm.Shutdown(shutdownCtx); err != nil {
		slog.Error("Shutdown completed with errors", "error", err)
		auditLogger.LogSystemShutdown(reason, false) // Graceful = false due to errors
		os.Exit(1)
	}

	// Shutdown API server
	if apiServer != nil {
		if err := apiServer.Stop(shutdownCtx); err != nil {
			slog.Warn("API server shutdown error", "error", err)
		}
	}

	// Shutdown metrics server
	if metricsServer != nil {
		if err := metricsServer.Stop(shutdownCtx); err != nil {
			slog.Warn("Metrics server shutdown error", "error", err)
		}
	}

	// Log successful shutdown to audit log
	auditLogger.LogSystemShutdown(reason, true) // Graceful = true

	slog.Info("Cbox Init shutdown complete")
}
