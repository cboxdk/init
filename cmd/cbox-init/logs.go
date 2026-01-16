package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/logger"
	"github.com/cboxdk/init/internal/process"
	"github.com/cboxdk/init/internal/setup"
	"github.com/cboxdk/init/internal/signals"
	"github.com/cboxdk/init/internal/tui"
	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs [process...]",
	Short: "Tail logs from processes",
	Long: `Tail logs from one or more processes in real-time.

If no process names are specified, shows logs from all processes.

Examples:
  cbox-init logs                    # All processes
  cbox-init logs nginx              # Single process
  cbox-init logs nginx horizon      # Multiple processes
  cbox-init logs --level=error      # Filter by level
  cbox-init logs --tail=100         # Last 100 lines`,
	Run: runLogs,
}

var (
	logsLevel  string
	logsTail   int
	logsFollow bool
)

func init() {
	logsCmd.Flags().StringVar(&logsLevel, "level", "all", "Filter by log level (debug|info|warn|error|all)")
	logsCmd.Flags().IntVar(&logsTail, "tail", 100, "Number of lines to show")
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", true, "Follow log output")
}

func runLogs(cmd *cobra.Command, args []string) {
	// Get config path
	cfgPath := getConfigPath()

	// Setup environment (minimal for log viewer)
	workdir := os.Getenv("WORKDIR")
	if workdir == "" {
		workdir = "/var/www/html"
	}

	// Setup permissions (silent, detects framework internally)
	permMgr := setup.NewPermissionManager(workdir, slog.Default())
	_ = permMgr.Setup()

	// Validate system (silent)
	validator := setup.NewConfigValidator(slog.Default())
	_ = validator.ValidateAll()

	// Load configuration
	cfg, err := config.LoadWithEnvExpansion(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger with specified level
	logLevel := cfg.Global.LogLevel
	if logsLevel != "all" {
		logLevel = logsLevel
	}
	log := logger.New(logLevel, "text") // Text format for readability

	slog.SetDefault(log)

	// Create audit logger
	auditLogger := audit.NewLogger(log, cfg.Global.AuditEnabled)

	// Create process manager
	pm := process.NewManager(cfg, log, auditLogger)

	// Start zombie reaper
	go signals.ReapZombies(cfg.Global.ZombieReapInterval)

	// Start processes
	ctx := context.Background()
	if err := pm.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to start processes: %v\n", err)
		os.Exit(1)
	}

	// Monitor process health
	pm.MonitorProcessHealth(ctx)

	// Display header
	fmt.Fprintf(os.Stderr, "📋 Tailing logs")
	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, " for: %v", args)
	} else {
		fmt.Fprintf(os.Stderr, " for all processes")
	}
	fmt.Fprintf(os.Stderr, " (level: %s)\n", logLevel)
	fmt.Fprintf(os.Stderr, "Press Ctrl+C to exit\n\n")

	// Launch simple log viewer
	if err := tui.RunLogs(pm); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error: %v\n", err)
		os.Exit(1)
	}

	// Shutdown when viewer exits
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Global.ShutdownTimeout)*time.Second)
	defer cancel()

	_ = pm.Shutdown(shutdownCtx)
}
