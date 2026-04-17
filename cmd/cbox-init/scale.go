package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"
)

var scaleCmd = &cobra.Command{
	Use:   "scale <process> <count>",
	Short: "Scale a process to the specified number of instances",
	Long: `Scale a process to the specified number of instances.

Examples:
  cbox-init scale queue-default 10   # Scale to 10 workers
  cbox-init scale horizon 1          # Scale back to 1`,
	Args: cobra.ExactArgs(2),
	Run:  runScale,
}

var scaleURL string

func init() {
	scaleCmd.Flags().StringVar(&scaleURL, "url", "", "API endpoint (auto-discovers Unix socket by default)")
}

func runScale(cmd *cobra.Command, args []string) {
	name := args[0]
	count, err := strconv.Atoi(args[1])
	if err != nil || count < 0 {
		fmt.Fprintf(os.Stderr, "❌ Invalid scale count: %s (must be a non-negative integer)\n", args[1])
		os.Exit(1)
	}
	client := newClient(scaleURL)
	if err := client.ScaleProcess(name, count); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to scale %s: %v\n", name, err)
		os.Exit(1)
	}
	fmt.Printf("✓ %s scaled to %d instances\n", name, count)
}
