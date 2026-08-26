package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var startProcessCmd = &cobra.Command{
	Use:   "start <process>",
	Short: "Start a stopped process",
	Args:  cobra.ExactArgs(1),
	Run:   runStartProcess,
}

var startURL string

func init() {
	startProcessCmd.Flags().StringVar(&startURL, "url", "", "API endpoint (auto-discovers Unix socket by default)")
}

func runStartProcess(cmd *cobra.Command, args []string) {
	name := args[0]
	client := newClient(startURL)
	if err := client.StartProcess(name); err != nil {
		exitAPIError(err, "Failed to start %s", name)
	}
	fmt.Printf("✓ %s started\n", name)
}
