package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var signalCmd = &cobra.Command{
	Use:   "signal <process> <signal>",
	Short: "Send an operational signal to a process",
	Long: `Send an operational signal to a single managed process's group.

Use it to target one service without touching the rest of the stack — for
example an nginx configuration reload or php-fpm log rotation:

  cbox-init signal nginx SIGHUP      # nginx -s reload
  cbox-init signal php-fpm SIGUSR2   # php-fpm graceful reload
  cbox-init signal php-fpm SIGUSR1   # php-fpm reopen log files

The signal may be written with or without the "SIG" prefix (HUP == SIGHUP).`,
	Args: cobra.ExactArgs(2),
	Run:  runSignal,
}

var signalURL string

func init() {
	signalCmd.Flags().StringVar(&signalURL, "url", "", "API endpoint (auto-discovers Unix socket by default)")
}

func runSignal(cmd *cobra.Command, args []string) {
	name, sig := args[0], args[1]
	client := newClient(signalURL)
	if err := client.SignalProcess(name, sig); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to signal %s with %s: %v\n", name, sig, err)
		os.Exit(1)
	}
	fmt.Printf("✓ sent %s to %s\n", sig, name)
}
