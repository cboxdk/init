package main

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/cboxdk/init/internal/apiclient"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all processes and their status",
	Long:  `Display a table of all managed processes with their current state, scale, restart count, and uptime.`,
	Args:  cobra.NoArgs,
	Run:   runList,
}

var listURL string

func init() {
	listCmd.Flags().StringVar(&listURL, "url", "", "API endpoint (auto-discovers Unix socket by default)")
}

func runList(cmd *cobra.Command, args []string) {
	client := newClient(listURL)

	processes, err := client.ListProcesses()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to list processes: %v\n", err)
		os.Exit(1)
	}

	if len(processes) == 0 {
		fmt.Println("No processes configured")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATUS\tSCALE\tRESTARTS\tUPTIME")

	hasUnhealthy := false
	for _, p := range processes {
		status := p.State
		if status != "running" {
			hasUnhealthy = true
		}

		scale := fmt.Sprintf("%d/%d", p.Scale, p.DesiredScale)

		restarts := 0
		var uptime string
		for _, inst := range p.Instances {
			restarts += inst.RestartCount
			if inst.StartedAt > 0 && uptime == "" {
				d := time.Since(time.Unix(inst.StartedAt, 0))
				uptime = formatDuration(d)
			}
		}
		if uptime == "" {
			uptime = "-"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", p.Name, status, scale, restarts, uptime)
	}
	w.Flush()

	if hasUnhealthy {
		os.Exit(1)
	}
}

// newClient creates an API client, using --url flag or auto-discovery
func newClient(urlFlag string) *apiclient.Client {
	auth := os.Getenv("CBOX_INIT_API_AUTH")
	if urlFlag != "" {
		return apiclient.New(urlFlag, auth)
	}
	return apiclient.NewWithAutoDiscover("http://localhost:9180", auth)
}

// formatDuration formats a duration as human-readable
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	return fmt.Sprintf("%dd%dh", days, hours)
}
