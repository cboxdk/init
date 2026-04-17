package main

import (
	"fmt"
	"os"

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
		fmt.Fprintf(os.Stderr, "❌ Failed to start %s: %v\n", name, err)
		os.Exit(1)
	}
	fmt.Printf("✓ %s started\n", name)
}
