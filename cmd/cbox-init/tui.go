package main

import (
	"fmt"
	"os"

	"github.com/cboxdk/init/internal/tui"
	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch interactive terminal dashboard",
	Long: `Launch a k9s-style interactive terminal dashboard for managing processes.

The TUI connects to a running Cbox Init daemon via API and provides:
- Real-time process status monitoring
- Interactive log viewing
- Process control (restart/stop/start/scale)
- Dynamic log level changes
- Keyboard-driven interface

The TUI requires a running daemon (cbox-init serve) with API enabled.

Usage:
  # Terminal 1: Start daemon
  cbox-init serve

  # Terminal 2: Connect TUI
  cbox-init tui`,
	Run: runTUI,
}

var (
	tuiRemote string
)

func init() {
	tuiCmd.Flags().StringVar(&tuiRemote, "remote", "http://localhost:9180", "API endpoint to connect to")
}

func runTUI(cmd *cobra.Command, args []string) {
	// TUI is always remote - connects to running daemon
	runTUIRemote(tuiRemote)
}

func runTUIRemote(apiURL string) {
	fmt.Fprintf(os.Stderr, "🔗 Connecting to remote API: %s\n", apiURL)

	// Get auth token if set
	auth := os.Getenv("CBOX_INIT_API_AUTH")

	// Launch remote TUI
	if err := tui.RunRemote(apiURL, auth); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Remote TUI error: %v\n", err)
		fmt.Fprintf(os.Stderr, "\n💡 Make sure daemon is running:\n")
		fmt.Fprintf(os.Stderr, "   cbox-init serve\n\n")
		fmt.Fprintf(os.Stderr, "💡 For remote access, ensure API is enabled:\n")
		fmt.Fprintf(os.Stderr, "   global:\n")
		fmt.Fprintf(os.Stderr, "     api_enabled: true\n")
		fmt.Fprintf(os.Stderr, "     api_port: 9180\n")
		os.Exit(1)
	}
}
