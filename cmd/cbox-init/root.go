package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const version = "1.0.0"

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
  cbox-init serve                    # Start daemon
  cbox-init tui                      # Interactive dashboard
  cbox-init logs nginx               # Tail nginx logs
  cbox-init restart horizon          # Restart horizon
  cbox-init scale queue-default 10   # Scale to 10 workers`,
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
	// Process control commands (future):
	// rootCmd.AddCommand(restartCmd)
	// rootCmd.AddCommand(stopCmd)
	// rootCmd.AddCommand(startCmd)
	// rootCmd.AddCommand(scaleCmd)
	// rootCmd.AddCommand(statusCmd)
}
