package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:   "restart <process>",
	Short: "Restart a process",
	Args:  cobra.ExactArgs(1),
	Run:   runRestart,
}

var restartURL string

func init() {
	restartCmd.Flags().StringVar(&restartURL, "url", "", "API endpoint (auto-discovers Unix socket by default)")
}

func runRestart(cmd *cobra.Command, args []string) {
	name := args[0]
	client := newClient(restartURL)
	if err := client.RestartProcess(name); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to restart %s: %v\n", name, err)
		os.Exit(1)
	}
	fmt.Printf("✓ %s restarted\n", name)
}
