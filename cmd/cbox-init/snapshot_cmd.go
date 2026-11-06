package main

import (
	"encoding/json"
	"fmt"

	"github.com/cboxdk/init/internal/snapshot"
	"github.com/spf13/cobra"
)

var snapshotCmd = &cobra.Command{
	Use:       "snapshot <status|sleep|wake>",
	Short:     "Observe or safely sleep and wake the qualified workload",
	Long:      "Sleep skips the idle timer but refuses active or unconfirmed work. It preserves the existing process tree; it does not scale pods to zero. New traffic may wake the workload immediately. Commands print the observed state as JSON.",
	Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	ValidArgs: []string{"status", "sleep", "wake"},
	RunE:      runSnapshot,
}

var snapshotURL string

func init() {
	snapshotCmd.Flags().StringVar(&snapshotURL, "url", "", "API endpoint (auto-discovers Unix socket by default)")
}

func runSnapshot(cmd *cobra.Command, args []string) error {
	client := newClient(snapshotURL)
	var status snapshot.Status
	var err error
	switch args[0] {
	case "status":
		status, err = client.Snapshot(cmd.Context())
	case "sleep":
		status, err = client.Sleep(cmd.Context())
	case "wake":
		status, err = client.Wake(cmd.Context())
	}
	if err != nil {
		return fmt.Errorf("snapshot %s: %w", args[0], err)
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(status)
}
