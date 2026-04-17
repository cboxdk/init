package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var stopProcessCmd = &cobra.Command{
	Use:   "stop <process>",
	Short: "Stop a running process",
	Args:  cobra.ExactArgs(1),
	Run:   runStopProcess,
}

var stopURL string

func init() {
	stopProcessCmd.Flags().StringVar(&stopURL, "url", "", "API endpoint (auto-discovers Unix socket by default)")
}

func runStopProcess(cmd *cobra.Command, args []string) {
	name := args[0]
	client := newClient(stopURL)
	if err := client.StopProcess(name); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to stop %s: %v\n", name, err)
		os.Exit(1)
	}
	fmt.Printf("✓ %s stopped\n", name)
}
