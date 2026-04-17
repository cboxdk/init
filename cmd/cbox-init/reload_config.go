package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var reloadConfigCmd = &cobra.Command{
	Use:   "reload-config",
	Short: "Reload configuration from disk",
	Long:  `Reload the configuration file from disk without restarting the daemon.`,
	Args:  cobra.NoArgs,
	Run:   runReloadConfig,
}

var reloadConfigURL string

func init() {
	reloadConfigCmd.Flags().StringVar(&reloadConfigURL, "url", "", "API endpoint (auto-discovers Unix socket by default)")
}

func runReloadConfig(cmd *cobra.Command, args []string) {
	client := newClient(reloadConfigURL)
	if err := client.ReloadConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to reload config: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Configuration reloaded")
}
