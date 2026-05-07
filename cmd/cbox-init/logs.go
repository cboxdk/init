package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/cboxdk/init/internal/logger"
	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs [process]",
	Short: "Tail logs from a process or all processes",
	Long: `Tail logs from one or all processes via the daemon API.

Examples:
  cbox-init logs                      # All processes, last 100 lines
  cbox-init logs nginx                # Specific process
  cbox-init logs nginx --tail 50      # Last 50 lines
  cbox-init logs -f                   # Stream all processes
  cbox-init logs nginx -f             # Stream specific process
  cbox-init logs nginx --tail 20 -f   # Last 20 lines then stream`,
	Args: cobra.MaximumNArgs(1),
	Run:  runLogs,
}

var (
	logsTail   int
	logsFollow bool
	logsLevel  string
	logsURL    string
)

func init() {
	logsCmd.Flags().IntVar(&logsTail, "tail", 100, "Number of lines to show")
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Stream new log entries")
	logsCmd.Flags().StringVar(&logsLevel, "level", "all", "Filter by log level (debug|info|warn|error|all)")
	logsCmd.Flags().StringVar(&logsURL, "url", "", "API endpoint (auto-discovers Unix socket by default)")
}

func runLogs(cmd *cobra.Command, args []string) {
	var processName string
	if len(args) > 0 {
		processName = args[0]
	}

	client := newClient(logsURL)
	levelFilter, err := parseLogLevelFilter(logsLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid log level %q: %v\n", logsLevel, err)
		os.Exit(1)
	}

	// Fetch historical logs
	var logs []logger.LogEntry
	if processName != "" {
		logs, err = client.GetLogs(processName, logsTail)
	} else {
		logs, err = client.GetStackLogs(logsTail)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to fetch logs: %v\n", err)
		os.Exit(1)
	}

	// Print historical logs
	for _, entry := range logs {
		if shouldPrintLogEntry(entry, levelFilter) {
			printLogEntry(entry)
		}
	}

	// If not following, we're done
	if !logsFollow {
		return
	}

	// Stream new entries via SSE
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	ch, err := client.StreamLogs(ctx, processName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to log stream: %v\n", err)
		os.Exit(1)
	}

	for entry := range ch {
		if shouldPrintLogEntry(entry, levelFilter) {
			printLogEntry(entry)
		}
	}
}

func printLogEntry(entry logger.LogEntry) {
	ts := entry.Timestamp.Format("15:04:05.000")
	fmt.Printf("%s [%s] %s: %s\n", ts, entry.Level, entry.ProcessName, entry.Message)
}

func parseLogLevelFilter(level string) (int, error) {
	switch strings.ToLower(level) {
	case "", "all":
		return -1, nil
	case "debug":
		return 0, nil
	case "info":
		return 1, nil
	case "warn", "warning":
		return 2, nil
	case "error":
		return 3, nil
	default:
		return 0, fmt.Errorf("must be one of debug, info, warn, error, all")
	}
}

func shouldPrintLogEntry(entry logger.LogEntry, minLevel int) bool {
	if minLevel < 0 {
		return true
	}

	entryLevel, err := parseLogLevelFilter(entry.Level)
	if err != nil {
		entryLevel = 1
	}

	return entryLevel >= minLevel
}
