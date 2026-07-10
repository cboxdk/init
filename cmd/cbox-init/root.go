package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is overridden at build time via -ldflags "-X main.version=...".
// It must be a var (not a const) for the linker flag to take effect.
var version = "dev"

var (
	cfgFile string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "cbox-init",
	Short: "Production-grade process manager for Docker containers",
	Long: `Cbox Init - Production-grade process manager for Docker containers

A modern process supervisor designed for Laravel and PHP applications with:
- Multi-process orchestration with DAG dependencies
- PHP-FPM auto-tuning based on container resources
- Health monitoring (TCP/HTTP/exec) with readiness blocking
- Prometheus metrics and Management API
- Graceful shutdown with configurable timeouts
- Lifecycle hooks for Laravel optimization
- Cron-like scheduler for periodic tasks

Examples:
  cbox-init scaffold laravel         # Generate a starter config
  cbox-init check-config -c cbox-init.yaml  # Validate a config
  cbox-init serve                    # Start daemon
  cbox-init tui                      # Interactive dashboard
  cbox-init list                     # List all processes
  cbox-init status nginx             # Show process details
  cbox-init start horizon            # Start a stopped process
  cbox-init stop horizon             # Stop a running process
  cbox-init restart horizon          # Restart horizon
  cbox-init scale queue-default 10   # Scale to 10 workers
  cbox-init reload-config            # Reload config without downtime
  cbox-init logs nginx -f            # Stream nginx logs`,
	Version: version,
	// Default to serve command if no subcommand specified
	Run: func(cmd *cobra.Command, args []string) {
		// If no subcommand provided, run serve
		serveCmd.Run(cmd, args)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// Global flags available to all subcommands
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "Path to configuration file")

	// Add subcommands
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(checkConfigCmd)
	rootCmd.AddCommand(tuiCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(scaffoldCmd)
	// Process control commands
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(startProcessCmd)
	rootCmd.AddCommand(stopProcessCmd)
	rootCmd.AddCommand(restartCmd)
	rootCmd.AddCommand(scaleCmd)
	rootCmd.AddCommand(reloadConfigCmd)
}
