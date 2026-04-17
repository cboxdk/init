package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status <process>",
	Short: "Show detailed status of a process",
	Long:  `Display detailed information about a specific process including PID, scale, restarts, uptime, command, and health status.`,
	Args:  cobra.ExactArgs(1),
	Run:   runStatus,
}

var statusURL string

func init() {
	statusCmd.Flags().StringVar(&statusURL, "url", "", "API endpoint (auto-discovers Unix socket by default)")
}

func runStatus(cmd *cobra.Command, args []string) {
	processName := args[0]
	client := newClient(statusURL)

	processes, err := client.ListProcesses()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to get process status: %v\n", err)
		os.Exit(1)
	}

	var found bool
	for _, p := range processes {
		if p.Name != processName {
			continue
		}
		found = true

		fmt.Printf("Name:       %s\n", p.Name)
		fmt.Printf("Type:       %s\n", p.Type)
		fmt.Printf("Status:     %s\n", p.State)
		fmt.Printf("Scale:      %d/%d\n", p.Scale, p.DesiredScale)

		if p.MaxScale > 0 {
			fmt.Printf("Max Scale:  %d\n", p.MaxScale)
		}

		restarts := 0
		for _, inst := range p.Instances {
			restarts += inst.RestartCount
		}
		fmt.Printf("Restarts:   %d\n", restarts)

		if len(p.Instances) > 0 {
			for _, inst := range p.Instances {
				var uptime string
				if inst.StartedAt > 0 {
					d := time.Since(time.Unix(inst.StartedAt, 0))
					uptime = formatDuration(d)
				} else {
					uptime = "-"
				}
				fmt.Printf("Instance:   %s (pid=%d, state=%s, uptime=%s, restarts=%d)\n",
					inst.ID, inst.PID, inst.State, uptime, inst.RestartCount)
			}
		}

		if p.CPUPercent > 0 || p.MemoryRSSBytes > 0 {
			fmt.Printf("CPU:        %.1f%%\n", p.CPUPercent)
			fmt.Printf("Memory:     %s\n", formatBytes(p.MemoryRSSBytes))
		}

		if p.Schedule != "" {
			fmt.Printf("Schedule:   %s\n", p.Schedule)
			fmt.Printf("Sched State:%s\n", p.ScheduleState)
			if p.NextRun > 0 {
				fmt.Printf("Next Run:   %s\n", time.Unix(p.NextRun, 0).Format(time.RFC3339))
			}
		}
		break
	}

	if !found {
		fmt.Fprintf(os.Stderr, "❌ Process not found: %s\n", processName)
		os.Exit(1)
	}
}

func formatBytes(b uint64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.1fG", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1fM", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.1fK", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%dB", b)
	}
}
