# CLI Commands & Always-On Unix Socket — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable local process management via always-on Unix socket and full CLI command suite (list, status, start, stop, restart, scale, reload-config, logs with SSE streaming).

**Architecture:** Extract the existing TUI API client to `internal/apiclient/` for shared use. Make the daemon always start a Unix socket listener regardless of `api_enabled`. Add CLI commands as thin Cobra wrappers that auto-discover the socket. Add SSE log streaming via a pub/sub mechanism on the process manager's log buffers.

**Tech Stack:** Go 1.24, Cobra CLI, net/http SSE, Unix sockets

**Spec:** `docs/superpowers/specs/2026-04-17-cli-commands-and-always-on-socket-design.md`

---

## File Structure

### New files
| File | Responsibility |
|------|---------------|
| `internal/apiclient/client.go` | API client (moved from tui/client.go), auto-discovery, all HTTP methods |
| `internal/apiclient/client_test.go` | Client tests (moved from tui/client_test.go) |
| `internal/apiclient/stream.go` | SSE log streaming client (`StreamLogs`) |
| `internal/apiclient/stream_test.go` | SSE stream tests |
| `internal/logger/subscriber.go` | Log pub/sub: `LogBroadcaster` with Subscribe/Unsubscribe |
| `internal/logger/subscriber_test.go` | Broadcaster tests |
| `cmd/cbox-init/list.go` | `cbox-init list` command |
| `cmd/cbox-init/status.go` | `cbox-init status <name>` command |
| `cmd/cbox-init/start_cmd.go` | `cbox-init start <name>` command |
| `cmd/cbox-init/stop_cmd.go` | `cbox-init stop <name>` command |
| `cmd/cbox-init/restart.go` | `cbox-init restart <name>` command |
| `cmd/cbox-init/scale.go` | `cbox-init scale <name> <count>` command |
| `cmd/cbox-init/reload_config.go` | `cbox-init reload-config` command |

### Modified files
| File | Change |
|------|--------|
| `internal/tui/model.go` | Change `client *APIClient` to `client *apiclient.Client`, update constructors |
| `internal/tui/tui.go` | Update imports to use `apiclient.New` |
| `internal/tui/update.go` | Update client method calls (if signatures change) |
| `internal/tui/client_test.go` | Remove (moved to apiclient) |
| `internal/tui/client.go` | Remove (moved to apiclient) |
| `internal/logger/log_buffer.go` | Add `AddWithBroadcast` or hook broadcaster into `Add` |
| `internal/logger/process_writer.go` | Wire broadcaster into log entry pipeline |
| `internal/process/manager.go` | Expose `LogBroadcaster` via getter, pass to supervisors |
| `internal/process/manager_logs.go` | Add `SubscribeLogs` method |
| `internal/api/server.go` | Add `StartSocketOnly` method, add SSE `/api/v1/logs/stream` endpoint |
| `cmd/cbox-init/serve.go` | Always start socket, TCP gated behind `api_enabled` |
| `cmd/cbox-init/root.go` | Register new commands, update help examples |
| `cmd/cbox-init/logs.go` | Rewrite to use apiclient + SSE streaming |
| `cmd/cbox-init/tui.go` | Update error message (remove "Ensure API is enabled" hint) |

---

## Task 1: Extract API Client to `internal/apiclient/`

Move the existing TUI client to a shared package. No behavior changes — pure refactoring.

**Files:**
- Create: `internal/apiclient/client.go`
- Create: `internal/apiclient/client_test.go`
- Delete: `internal/tui/client.go`
- Delete: `internal/tui/client_test.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/tui.go`
- Modify: `internal/tui/update.go`

- [ ] **Step 1: Create `internal/apiclient/client.go`**

Copy `internal/tui/client.go` to `internal/apiclient/client.go`. Change package declaration to `package apiclient`. Rename the type from `APIClient` to `Client`. Update the constructor from `NewAPIClient` to `New`. All methods stay the same. Update imports:

```go
package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/logger"
	"github.com/cboxdk/init/internal/process"
)

// Client connects to a running Cbox Init daemon via API
type Client struct {
	baseURL    string
	socketPath string
	auth       string
	client     *http.Client
}

// New creates a new API client with auto-detection
// Tries Unix socket first, falls back to TCP
func New(baseURL, auth string) *Client {
	client := &Client{
		baseURL: baseURL,
		auth:    auth,
	}

	// Auto-detect socket paths (priority order)
	socketPaths := []string{
		"/var/run/cbox-init.sock",
		"/tmp/cbox-init.sock",
		"/run/cbox-init.sock",
	}

	// Try each socket path
	for _, socketPath := range socketPaths {
		if client.trySocket(socketPath) {
			client.socketPath = socketPath
			client.client = client.createSocketClient(socketPath)
			return client
		}
	}

	// Fall back to TCP
	client.client = &http.Client{
		Timeout: 10 * time.Second,
	}

	return client
}
```

All remaining methods are copied verbatim but with receiver type `*Client` instead of `*APIClient`. No logic changes.

- [ ] **Step 2: Create `internal/apiclient/client_test.go`**

Copy `internal/tui/client_test.go` to `internal/apiclient/client_test.go`. Change package to `package apiclient`. Replace all `NewAPIClient` calls with `New`. Replace all `APIClient` type references with `Client`. All test logic stays the same.

- [ ] **Step 3: Run apiclient tests to verify they pass**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && go test ./internal/apiclient/ -v -count=1`
Expected: All tests PASS

- [ ] **Step 4: Update `internal/tui/model.go`**

Change the `client` field type and update constructors:

In the import block, add:
```go
"github.com/cboxdk/init/internal/apiclient"
```

Change the Model struct field (line 74):
```go
// Old:
client       *APIClient       // For remote mode
// New:
client       *apiclient.Client // For remote mode
```

Change `NewRemoteModel` (line 163-176):
```go
// Old:
client:        NewAPIClient(apiURL, auth),
// New:
client:        apiclient.New(apiURL, auth),
```

- [ ] **Step 5: Update `internal/tui/tui.go`**

No import changes needed — `tui.go` only calls `model.client.HealthCheck()` which already goes through the model. The model owns the client reference. No changes required to this file.

- [ ] **Step 6: Update `internal/tui/update.go`**

Check if `update.go` references `APIClient` type directly. It should only reference `m.client` which is already typed via the model. If there are any direct type references (e.g. in type assertions), update them to `*apiclient.Client`. Based on code review, `update.go` only uses `m.client.MethodName()` calls — no type references to update.

- [ ] **Step 7: Delete old files**

Remove `internal/tui/client.go` and `internal/tui/client_test.go`.

- [ ] **Step 8: Run all TUI tests to verify refactoring**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && go test ./internal/tui/ -v -count=1`
Expected: All tests PASS

- [ ] **Step 9: Run full test suite**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && go test ./... -count=1`
Expected: All tests PASS

- [ ] **Step 10: Commit**

```bash
git add internal/apiclient/ internal/tui/
git commit -m "refactor: extract API client to internal/apiclient for shared CLI/TUI use"
```

---

## Task 2: Always-On Unix Socket

Make the daemon always start a Unix socket listener, regardless of `api_enabled`.

**Files:**
- Modify: `internal/api/server.go`
- Modify: `cmd/cbox-init/serve.go`
- Modify: `cmd/cbox-init/tui.go`

- [ ] **Step 1: Add `StartSocketOnly` method to `internal/api/server.go`**

Add a new method after `Start()` (after line 341). This method creates the mux with all routes (same as `Start`) but only starts the socket listener — no TCP, no ACL, no rate limiter on socket:

```go
// StartSocketOnly starts only the Unix socket listener (no TCP).
// Used when api_enabled is false but local management is still needed.
// The socket handler has no ACL or rate limiting — file permissions provide security.
func (s *Server) StartSocketOnly(ctx context.Context) error {
	if s.socketPath == "" {
		return fmt.Errorf("no socket path configured")
	}

	mux := http.NewServeMux()

	// Same routes as Start() but without rate limiting or ACL
	mux.HandleFunc("/api/v1/health", s.wrapHandler(s.handleHealth, false))
	mux.HandleFunc("/api/v1/processes", s.wrapHandler(s.handleProcesses, false))
	mux.HandleFunc("/api/v1/processes/", s.wrapHandler(s.handleProcessAction, false))
	mux.HandleFunc("/api/v1/logs", s.wrapHandler(s.handleStackLogs, false))
	mux.HandleFunc("/api/v1/config/save", s.wrapHandler(s.handleConfigSave, false))
	mux.HandleFunc("/api/v1/config/reload", s.wrapHandler(s.handleConfigReload, false))
	mux.HandleFunc("/api/v1/metrics/history", s.wrapHandler(s.handleMetricsHistory, false))
	mux.HandleFunc("/api/v1/oneshot/history", s.wrapHandler(s.handleOneshotHistory, false))

	return s.startSocketListener(mux)
}
```

Note: `requireAuth` is set to `false` for all routes when socket-only — socket relies on file permissions for security.

Also update socket permissions from `0600` to `0660` in `startSocketListener` (line 357 of server.go) to allow group access as specified in the design:

```go
// Old:
if err := os.Chmod(s.socketPath, 0600); err != nil {
// New:
if err := os.Chmod(s.socketPath, 0660); err != nil {
```

- [ ] **Step 2: Add `resolveSocketPath` helper to `cmd/cbox-init/serve.go`**

Add before `startAPIServer`:

```go
// resolveSocketPath determines the Unix socket path.
// Priority: config value > /var/run/cbox-init.sock > /tmp/cbox-init.sock
func resolveSocketPath(configured string) string {
	if configured != "" {
		return configured
	}

	// Try /var/run first (preferred, persistent)
	testPath := "/var/run/cbox-init.sock"
	if dir := filepath.Dir(testPath); dirWritable(dir) {
		return testPath
	}

	// Fall back to /tmp
	return "/tmp/cbox-init.sock"
}

// dirWritable checks if a directory is writable
func dirWritable(dir string) bool {
	testFile := filepath.Join(dir, ".cbox-init-write-test")
	f, err := os.Create(testFile)
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(testFile)
	return true
}
```

Add `"path/filepath"` to the imports in serve.go.

- [ ] **Step 3: Modify `serve.go` to always start socket**

Change the API server startup section (around lines 176-180):

```go
// Always start Unix socket for local management (TUI, CLI commands)
socketPath := resolveSocketPath(cfg.Global.APISocket)

// Start API server
var apiServer *api.Server
if cfg.Global.APIEnabledValue() {
	// Full API: TCP + socket
	if cfg.Global.APISocket == "" {
		cfg.Global.APISocket = socketPath
	}
	apiServer = startAPIServer(ctx, cfg, pm, log)
} else {
	// Socket-only mode: local management without TCP
	apiServer = api.NewServer(0, socketPath, "", nil, nil, cfg.Global.AuditEnabled, cfg.Global.APIMaxRequestBody, pm, log)
	if err := apiServer.StartSocketOnly(ctx); err != nil {
		slog.Warn("Failed to start Unix socket (local CLI/TUI disabled)", "error", err)
		apiServer = nil
	} else {
		slog.Info("Unix socket started (local management only)", "path", socketPath)
	}
}
```

- [ ] **Step 4: Update TUI error message in `cmd/cbox-init/tui.go`**

The old error message tells users to enable `api_enabled`. Since the socket is now always active, update the hint (lines 56-63):

```go
	if err := tui.RunRemote(apiURL, auth); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Remote TUI error: %v\n", err)
		fmt.Fprintf(os.Stderr, "\n💡 Make sure daemon is running:\n")
		fmt.Fprintf(os.Stderr, "   cbox-init serve\n\n")
		fmt.Fprintf(os.Stderr, "💡 For remote access, ensure API is enabled:\n")
		fmt.Fprintf(os.Stderr, "   global:\n")
		fmt.Fprintf(os.Stderr, "     api_enabled: true\n")
		fmt.Fprintf(os.Stderr, "     api_port: 9180\n")
		os.Exit(1)
	}
```

- [ ] **Step 5: Build and verify**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && make build`
Expected: Build succeeds

- [ ] **Step 6: Run tests**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && go test ./internal/api/ ./cmd/cbox-init/ -v -count=1`
Expected: All tests PASS

- [ ] **Step 7: Commit**

```bash
git add internal/api/server.go cmd/cbox-init/serve.go cmd/cbox-init/tui.go
git commit -m "feat: always-on Unix socket for local management without TCP"
```

---

## Task 3: CLI Helper + `list` and `status` Commands

Create shared CLI helper and the read-only commands.

**Files:**
- Create: `cmd/cbox-init/list.go`
- Create: `cmd/cbox-init/status.go`
- Modify: `cmd/cbox-init/root.go`

- [ ] **Step 1: Create `cmd/cbox-init/list.go`**

```go
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

		// Calculate restarts from instances
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
	return apiclient.New("http://localhost:9180", auth)
}

// formatDuration formats a duration as human-readable (e.g., "2h34m", "5m12s")
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
```

- [ ] **Step 2: Create `cmd/cbox-init/status.go`**

```go
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

	// Find the process
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

		// Aggregate instance info
		restarts := 0
		for _, inst := range p.Instances {
			restarts += inst.RestartCount
		}
		fmt.Printf("Restarts:   %d\n", restarts)

		// Show instance details
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

		// Show CPU/Memory if available
		if p.CPUPercent > 0 || p.MemoryRSSBytes > 0 {
			fmt.Printf("CPU:        %.1f%%\n", p.CPUPercent)
			fmt.Printf("Memory:     %s\n", formatBytes(p.MemoryRSSBytes))
		}

		// Schedule info
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

// formatBytes formats bytes as human-readable
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
```

- [ ] **Step 3: Register commands in `cmd/cbox-init/root.go`**

Add to the `init()` function, replacing the commented-out lines (lines 64-69):

```go
	// Process control commands
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(statusCmd)
```

Keep the other commented-out lines for now — we'll replace them in later tasks.

- [ ] **Step 4: Build and verify**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && make build`
Expected: Build succeeds

- [ ] **Step 5: Commit**

```bash
git add cmd/cbox-init/list.go cmd/cbox-init/status.go cmd/cbox-init/root.go
git commit -m "feat: add list and status CLI commands"
```

---

## Task 4: Process Control Commands (start, stop, restart, scale, reload-config)

**Files:**
- Create: `cmd/cbox-init/start_cmd.go`
- Create: `cmd/cbox-init/stop_cmd.go`
- Create: `cmd/cbox-init/restart.go`
- Create: `cmd/cbox-init/scale.go`
- Create: `cmd/cbox-init/reload_config.go`
- Modify: `cmd/cbox-init/root.go`

- [ ] **Step 1: Create `cmd/cbox-init/start_cmd.go`**

(Named `start_cmd.go` to avoid conflict with Go's `start` identifier)

```go
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
```

- [ ] **Step 2: Create `cmd/cbox-init/stop_cmd.go`**

```go
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
```

- [ ] **Step 3: Create `cmd/cbox-init/restart.go`**

```go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:   "restart <process>",
	Short: "Restart a process",
	Args:  cobra.ExactArgs(1),
	Run:   runRestart,
}

var restartURL string

func init() {
	restartCmd.Flags().StringVar(&restartURL, "url", "", "API endpoint (auto-discovers Unix socket by default)")
}

func runRestart(cmd *cobra.Command, args []string) {
	name := args[0]
	client := newClient(restartURL)

	if err := client.RestartProcess(name); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to restart %s: %v\n", name, err)
		os.Exit(1)
	}

	fmt.Printf("✓ %s restarted\n", name)
}
```

- [ ] **Step 4: Create `cmd/cbox-init/scale.go`**

```go
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
```

- [ ] **Step 5: Create `cmd/cbox-init/reload_config.go`**

```go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var reloadConfigCmd = &cobra.Command{
	Use:   "reload-config",
	Short: "Reload configuration from disk",
	Long:  `Reload the configuration file from disk without restarting the daemon. Processes will be updated according to the new configuration.`,
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
```

- [ ] **Step 6: Register all commands in `cmd/cbox-init/root.go`**

Replace the commented-out future commands section (lines 64-69) with:

```go
	// Process control commands
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(startProcessCmd)
	rootCmd.AddCommand(stopProcessCmd)
	rootCmd.AddCommand(restartCmd)
	rootCmd.AddCommand(scaleCmd)
	rootCmd.AddCommand(reloadConfigCmd)
```

Also update the help examples in rootCmd Long string (line 34-36) to reflect actual commands:

```go
Examples:
  cbox-init serve                    # Start daemon
  cbox-init tui                      # Interactive dashboard
  cbox-init list                     # List all processes
  cbox-init status nginx             # Show process details
  cbox-init restart horizon          # Restart horizon
  cbox-init scale queue-default 10   # Scale to 10 workers
  cbox-init logs nginx -f            # Stream nginx logs`,
```

- [ ] **Step 7: Build and verify**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && make build`
Expected: Build succeeds

- [ ] **Step 8: Verify help output**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && ./build/cbox-init --help`
Expected: Shows all new commands (list, status, start, stop, restart, scale, reload-config)

- [ ] **Step 9: Commit**

```bash
git add cmd/cbox-init/start_cmd.go cmd/cbox-init/stop_cmd.go cmd/cbox-init/restart.go cmd/cbox-init/scale.go cmd/cbox-init/reload_config.go cmd/cbox-init/root.go
git commit -m "feat: add start, stop, restart, scale, reload-config CLI commands"
```

---

## Task 5: Log Subscriber (Pub/Sub for SSE)

Add a broadcast mechanism to the logger so the API can subscribe to real-time log entries.

**Files:**
- Create: `internal/logger/subscriber.go`
- Create: `internal/logger/subscriber_test.go`
- Modify: `internal/logger/log_buffer.go`
- Modify: `internal/logger/process_writer.go`

- [ ] **Step 1: Write failing test for LogBroadcaster**

Create `internal/logger/subscriber_test.go`:

```go
package logger

import (
	"testing"
	"time"
)

func TestLogBroadcaster_SubscribeReceivesEntries(t *testing.T) {
	b := NewLogBroadcaster()

	ch, unsub := b.Subscribe("")
	defer unsub()

	entry := LogEntry{
		Timestamp:   time.Now(),
		ProcessName: "nginx",
		InstanceID:  "nginx-0",
		Stream:      "stdout",
		Message:     "hello",
		Level:       "info",
	}

	b.Broadcast(entry)

	select {
	case got := <-ch:
		if got.Message != "hello" || got.ProcessName != "nginx" {
			t.Errorf("unexpected entry: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for entry")
	}
}

func TestLogBroadcaster_FilterByProcess(t *testing.T) {
	b := NewLogBroadcaster()

	ch, unsub := b.Subscribe("nginx")
	defer unsub()

	// Send entry for different process — should NOT be received
	b.Broadcast(LogEntry{ProcessName: "php-fpm", Message: "wrong"})

	// Send entry for matching process — should be received
	b.Broadcast(LogEntry{ProcessName: "nginx", Message: "correct"})

	select {
	case got := <-ch:
		if got.Message != "correct" {
			t.Errorf("expected 'correct', got %q", got.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for filtered entry")
	}
}

func TestLogBroadcaster_UnsubscribeStopsDelivery(t *testing.T) {
	b := NewLogBroadcaster()

	ch, unsub := b.Subscribe("")
	unsub()

	b.Broadcast(LogEntry{Message: "after-unsub"})

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("received entry after unsubscribe")
		}
	case <-time.After(100 * time.Millisecond):
		// Expected: channel closed, no entry
	}
}

func TestLogBroadcaster_MultipleSubscribers(t *testing.T) {
	b := NewLogBroadcaster()

	ch1, unsub1 := b.Subscribe("")
	defer unsub1()
	ch2, unsub2 := b.Subscribe("")
	defer unsub2()

	b.Broadcast(LogEntry{Message: "both"})

	for _, ch := range []<-chan LogEntry{ch1, ch2} {
		select {
		case got := <-ch:
			if got.Message != "both" {
				t.Errorf("unexpected: %q", got.Message)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out")
		}
	}
}

func TestLogBroadcaster_SlowSubscriberDropsEntries(t *testing.T) {
	b := NewLogBroadcaster()

	ch, unsub := b.Subscribe("")
	defer unsub()

	// Fill the channel buffer (256) + more — should not block
	for i := 0; i < 300; i++ {
		b.Broadcast(LogEntry{Message: "flood"})
	}

	// Drain what we can — we should get at most 256
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	if count > 256 {
		t.Errorf("got %d entries, expected at most 256", count)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && go test ./internal/logger/ -run TestLogBroadcaster -v -count=1`
Expected: FAIL — `NewLogBroadcaster` undefined

- [ ] **Step 3: Implement `internal/logger/subscriber.go`**

```go
package logger

import (
	"sync"
)

// LogBroadcaster delivers log entries to multiple subscribers in real-time.
// Subscribers receive entries on a buffered channel. If a subscriber can't
// keep up, entries are dropped (non-blocking send).
type LogBroadcaster struct {
	mu          sync.RWMutex
	subscribers map[uint64]*subscription
	nextID      uint64
}

type subscription struct {
	ch     chan LogEntry
	filter string // process name filter, empty = all
}

// NewLogBroadcaster creates a new broadcaster.
func NewLogBroadcaster() *LogBroadcaster {
	return &LogBroadcaster{
		subscribers: make(map[uint64]*subscription),
	}
}

// Subscribe registers a new subscriber.
// filter is a process name — empty string receives all processes.
// Returns a read channel and an unsubscribe function.
// The channel is closed when unsubscribe is called.
func (b *LogBroadcaster) Subscribe(filter string) (<-chan LogEntry, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.nextID
	b.nextID++

	ch := make(chan LogEntry, 256)
	b.subscribers[id] = &subscription{ch: ch, filter: filter}

	unsub := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if sub, ok := b.subscribers[id]; ok {
			close(sub.ch)
			delete(b.subscribers, id)
		}
	}

	return ch, unsub
}

// Broadcast sends a log entry to all matching subscribers.
// Non-blocking: if a subscriber's buffer is full, the entry is dropped.
func (b *LogBroadcaster) Broadcast(entry LogEntry) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, sub := range b.subscribers {
		if sub.filter != "" && sub.filter != entry.ProcessName {
			continue
		}
		// Non-blocking send
		select {
		case sub.ch <- entry:
		default:
			// Subscriber too slow, drop entry
		}
	}
}
```

- [ ] **Step 4: Run subscriber tests**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && go test ./internal/logger/ -run TestLogBroadcaster -v -count=1`
Expected: All PASS

- [ ] **Step 5: Wire broadcaster into `LogBuffer.Add`**

Modify `internal/logger/log_buffer.go`. Add a broadcaster field and hook it into `Add`:

Add to the `LogBuffer` struct (after line 24):
```go
	broadcaster *LogBroadcaster // optional, for real-time subscribers
```

Add a setter method after `Size()`:
```go
// SetBroadcaster attaches a broadcaster for real-time log delivery.
func (lb *LogBuffer) SetBroadcaster(b *LogBroadcaster) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.broadcaster = b
}
```

Modify `Add` (line 41-52) to broadcast after adding:
```go
func (lb *LogBuffer) Add(entry LogEntry) {
	lb.mu.Lock()
	lb.entries[lb.index] = entry
	lb.index++
	if lb.index >= lb.size {
		lb.index = 0
		lb.full = true
	}
	b := lb.broadcaster
	lb.mu.Unlock()

	// Broadcast outside the lock to avoid holding it during channel sends
	if b != nil {
		b.Broadcast(entry)
	}
}
```

- [ ] **Step 6: Run existing log buffer tests**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && go test ./internal/logger/ -v -count=1`
Expected: All tests PASS (existing + new)

- [ ] **Step 7: Commit**

```bash
git add internal/logger/subscriber.go internal/logger/subscriber_test.go internal/logger/log_buffer.go
git commit -m "feat: add log broadcaster for real-time SSE subscription"
```

---

## Task 6: Wire Broadcaster Through Process Manager

Connect the broadcaster to all ProcessWriters so every log entry gets broadcast.

**Files:**
- Modify: `internal/process/manager.go`
- Modify: `internal/process/manager_logs.go`
- Modify: `internal/process/supervisor.go` (where ProcessWriters are created)

- [ ] **Step 1: Add broadcaster field to Manager**

In `internal/process/manager.go`, add to the Manager struct (after line 63, the `startTime` field):

```go
	logBroadcaster *logger.LogBroadcaster // Real-time log delivery to API subscribers
```

Add import for `logger` if not already present.

In `NewManager`, initialize it (in the constructor, wherever the struct is created):

```go
	logBroadcaster: logger.NewLogBroadcaster(),
```

Add a getter method (can go at end of file or in manager_logs.go):

```go
// LogBroadcaster returns the broadcaster for real-time log subscriptions.
func (m *Manager) LogBroadcaster() *logger.LogBroadcaster {
	return m.logBroadcaster
}
```

- [ ] **Step 2: Add `SubscribeLogs` convenience method to `manager_logs.go`**

Add to `internal/process/manager_logs.go`:

```go
// SubscribeLogs registers a subscriber for real-time log entries.
// filter is a process name — empty string receives all processes.
// Returns a read channel and an unsubscribe function.
func (m *Manager) SubscribeLogs(filter string) (<-chan logger.LogEntry, func()) {
	return m.logBroadcaster.Subscribe(filter)
}
```

- [ ] **Step 3: Pass broadcaster to Supervisors**

In the Supervisor, when creating ProcessWriters in `startInstance()` (in `supervisor.go`), the ProcessWriter's LogBuffer needs the broadcaster attached. The cleanest way: after creating each ProcessWriter, call `SetBroadcaster` on its internal LogBuffer.

The ProcessWriter's logBuffer is a private field. We need to expose it or pass the broadcaster to `NewProcessWriter`. The simplest approach: add an optional `SetBroadcaster` method on ProcessWriter that delegates to the logBuffer:

In `internal/logger/process_writer.go`, add after the `Flush` method:

```go
// SetBroadcaster attaches a broadcaster to the internal log buffer.
func (pw *ProcessWriter) SetBroadcaster(b *LogBroadcaster) {
	if pw.logBuffer != nil {
		pw.logBuffer.SetBroadcaster(b)
	}
}
```

Then in the Supervisor's `startInstance` method (in `supervisor.go`), right after creating the stdout/stderr ProcessWriters (the `logger.NewProcessWriter` calls), add:

```go
	if s.logBroadcaster != nil {
		stdoutWriter.SetBroadcaster(s.logBroadcaster)
		stderrWriter.SetBroadcaster(s.logBroadcaster)
	}
```

The Supervisor needs the broadcaster. Add a field to the Supervisor struct:

```go
	logBroadcaster *logger.LogBroadcaster
```

Add a setter on Supervisor (avoids changing the NewSupervisor signature):

```go
func (s *Supervisor) SetLogBroadcaster(b *logger.LogBroadcaster) {
	s.logBroadcaster = b
}
```

Then in the Manager, after each supervisor is created (search for `NewSupervisor` calls in `manager.go` — they're in the `Start` method loop where supervisors are created from config), call:

```go
sup.SetLogBroadcaster(m.logBroadcaster)
```

Also do the same in any code path that creates supervisors dynamically (e.g., `handleAddProcess` in the API, or `AddProcess` on the Manager if it exists).

- [ ] **Step 4: Build and run tests**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && go test ./internal/process/ ./internal/logger/ -v -count=1`
Expected: All tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/process/manager.go internal/process/manager_logs.go internal/process/supervisor.go internal/logger/process_writer.go
git commit -m "feat: wire log broadcaster through process manager to all supervisors"
```

---

## Task 7: SSE Log Stream API Endpoint

Add the Server-Sent Events endpoint to the API server.

**Files:**
- Modify: `internal/api/server.go`

- [ ] **Step 1: Add SSE route registration**

In `Start()` method (after line 262, the existing `/api/v1/logs` route), add:

```go
	mux.HandleFunc("/api/v1/logs/stream", s.wrapHandler(s.handleLogStream, true))
```

Also add it to `StartSocketOnly()`:

```go
	mux.HandleFunc("/api/v1/logs/stream", s.wrapHandler(s.handleLogStream, false))
```

- [ ] **Step 2: Implement `handleLogStream` SSE handler**

Add the handler to `server.go`:

```go
// handleLogStream provides a Server-Sent Events stream of real-time log entries.
// Query params:
//   - process: filter by process name (optional, empty = all)
func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	processFilter := r.URL.Query().Get("process")

	ch, unsub := s.manager.SubscribeLogs(processFilter)
	defer unsub()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering
	flusher.Flush()

	// Heartbeat ticker to detect dead connections
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case entry, ok := <-ch:
			if !ok {
				return // Channel closed
			}
			data, err := json.Marshal(map[string]interface{}{
				"timestamp": entry.Timestamp.Format(time.RFC3339Nano),
				"process":   entry.ProcessName,
				"instance":  entry.InstanceID,
				"stream":    entry.Stream,
				"level":     entry.Level,
				"message":   entry.Message,
			})
			if err != nil {
				s.logger.Error("Failed to marshal log entry", "error", err)
				continue
			}
			fmt.Fprintf(w, "event: log\ndata: %s\n\n", data)
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
```

Add `"encoding/json"` and `"time"` to the imports if not already present (they likely already are in server.go).

- [ ] **Step 3: Increase WriteTimeout for SSE connections**

The current `WriteTimeout: 10 * time.Second` on the server will kill SSE connections. The SSE handler needs a longer timeout. The cleanest approach: set `WriteTimeout: 0` (disabled) on the socket server since SSE needs indefinite write, and rely on heartbeat + context cancellation for cleanup. For the TCP server, keep the existing timeout but override it per-request in the SSE handler by using `http.ResponseController`:

In `handleLogStream`, before the main loop, add:

```go
	// Disable write deadline for this SSE connection
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		s.logger.Warn("Failed to disable write deadline for SSE", "error", err)
	}
```

- [ ] **Step 4: Build and verify**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && make build`
Expected: Build succeeds

- [ ] **Step 5: Run API tests**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && go test ./internal/api/ -v -count=1`
Expected: All tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/api/server.go
git commit -m "feat: add SSE log stream endpoint at /api/v1/logs/stream"
```

---

## Task 8: SSE Client (StreamLogs)

Add the client-side SSE parsing for `cbox-init logs -f`.

**Files:**
- Create: `internal/apiclient/stream.go`
- Create: `internal/apiclient/stream_test.go`

- [ ] **Step 1: Write failing test for StreamLogs**

Create `internal/apiclient/stream_test.go`:

```go
package apiclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/logger"
)

func TestStreamLogs_ReceivesEntries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/logs/stream" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer is not a flusher")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send two events
		fmt.Fprintf(w, "event: log\ndata: {\"timestamp\":\"2026-04-17T10:00:00Z\",\"process\":\"nginx\",\"level\":\"info\",\"message\":\"hello\"}\n\n")
		flusher.Flush()

		fmt.Fprintf(w, "event: log\ndata: {\"timestamp\":\"2026-04-17T10:00:01Z\",\"process\":\"nginx\",\"level\":\"info\",\"message\":\"world\"}\n\n")
		flusher.Flush()

		// Keep connection open until client disconnects
		<-r.Context().Done()
	}))
	defer server.Close()

	client := New(server.URL, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := client.StreamLogs(ctx, "")
	if err != nil {
		t.Fatalf("StreamLogs error: %v", err)
	}

	// Should receive two entries
	entry1 := <-ch
	if entry1.Message != "hello" || entry1.ProcessName != "nginx" {
		t.Errorf("unexpected first entry: %+v", entry1)
	}

	entry2 := <-ch
	if entry2.Message != "world" {
		t.Errorf("unexpected second entry: %+v", entry2)
	}
}

func TestStreamLogs_WithProcessFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		process := r.URL.Query().Get("process")
		if process != "nginx" {
			t.Errorf("expected process=nginx, got %q", process)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: log\ndata: {\"process\":\"nginx\",\"message\":\"filtered\"}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	client := New(server.URL, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := client.StreamLogs(ctx, "nginx")
	if err != nil {
		t.Fatalf("StreamLogs error: %v", err)
	}

	entry := <-ch
	if entry.Message != "filtered" {
		t.Errorf("unexpected: %+v", entry)
	}
}

func TestStreamLogs_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	client := New(server.URL, "")

	ctx, cancel := context.WithCancel(context.Background())

	ch, err := client.StreamLogs(ctx, "")
	if err != nil {
		t.Fatalf("StreamLogs error: %v", err)
	}

	// Cancel context
	cancel()

	// Channel should close
	select {
	case _, ok := <-ch:
		if ok {
			// Might get a zero value, that's fine
		}
	case <-time.After(2 * time.Second):
		t.Fatal("channel not closed after context cancel")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && go test ./internal/apiclient/ -run TestStream -v -count=1`
Expected: FAIL — `StreamLogs` undefined

- [ ] **Step 3: Implement `internal/apiclient/stream.go`**

```go
package apiclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cboxdk/init/internal/logger"
)

// StreamLogs connects to the SSE log stream and returns a channel of log entries.
// The channel is closed when the context is cancelled or the connection drops.
// If process is non-empty, only logs from that process are streamed.
func (c *Client) StreamLogs(ctx context.Context, process string) (<-chan logger.LogEntry, error) {
	path := "/api/v1/logs/stream"
	if process != "" {
		path = fmt.Sprintf("%s?process=%s", path, process)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", c.getURL(path), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	if c.auth != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.auth))
	}

	// Use a client without timeout for streaming
	streamClient := *c.client
	streamClient.Timeout = 0

	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to log stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("log stream returned status %d", resp.StatusCode)
	}

	ch := make(chan logger.LogEntry, 256)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()

			// Skip comments (keepalive) and empty lines
			if line == "" || strings.HasPrefix(line, ":") {
				continue
			}

			// Parse SSE data line
			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				var entry struct {
					Timestamp string `json:"timestamp"`
					Process   string `json:"process"`
					Instance  string `json:"instance"`
					Stream    string `json:"stream"`
					Level     string `json:"level"`
					Message   string `json:"message"`
				}
				if err := json.Unmarshal([]byte(data), &entry); err != nil {
					continue // Skip malformed entries
				}

				ts, _ := time.Parse(time.RFC3339Nano, entry.Timestamp)

				logEntry := logger.LogEntry{
					Timestamp:   ts,
					ProcessName: entry.Process,
					InstanceID:  entry.Instance,
					Stream:      entry.Stream,
					Level:       entry.Level,
					Message:     entry.Message,
				}

				select {
				case ch <- logEntry:
				case <-ctx.Done():
					return
				}
			}

			// Skip "event:" lines — we only care about data
		}
	}()

	return ch, nil
}
```

- [ ] **Step 4: Run stream tests**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && go test ./internal/apiclient/ -run TestStream -v -count=1`
Expected: All PASS

- [ ] **Step 5: Run full apiclient tests**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && go test ./internal/apiclient/ -v -count=1`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/apiclient/stream.go internal/apiclient/stream_test.go
git commit -m "feat: add SSE log stream client for cbox-init logs -f"
```

---

## Task 9: Rewrite `logs` Command

Replace the existing logs command (which creates its own process manager) with one that uses the API client.

**Files:**
- Modify: `cmd/cbox-init/logs.go`

- [ ] **Step 1: Rewrite `cmd/cbox-init/logs.go`**

Replace the entire file:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/cboxdk/init/internal/apiclient"
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

	// Fetch historical logs
	var logs []apiclient.LogEntry
	var err error
	if processName != "" {
		logs, err = client.GetLogs(processName, logsTail)
	} else {
		logs, err = client.GetStackLogs(logsTail)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to fetch logs: %v\n", err)
		os.Exit(1)
	}

	// Print historical logs
	for _, entry := range logs {
		printLogEntry(entry)
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
		fmt.Fprintf(os.Stderr, "❌ Failed to connect to log stream: %v\n", err)
		os.Exit(1)
	}

	for entry := range ch {
		printLogEntry(entry)
	}
}

func printLogEntry(entry apiclient.LogEntry) {
	ts := entry.Timestamp.Format("15:04:05.000")
	fmt.Printf("%s [%s] %s: %s\n", ts, entry.Level, entry.ProcessName, entry.Message)
}
```

Wait — the `LogEntry` type is in `logger` package, not `apiclient`. The client methods return `logger.LogEntry`. So we need to use `logger.LogEntry` in the logs command. Let me fix:

Actually, the `GetLogs` and `GetStackLogs` methods on `Client` return `[]logger.LogEntry`. So the import should use `logger.LogEntry`. Let me update:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

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

	// Fetch historical logs
	var logs []logger.LogEntry
	var err error
	if processName != "" {
		logs, err = client.GetLogs(processName, logsTail)
	} else {
		logs, err = client.GetStackLogs(logsTail)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to fetch logs: %v\n", err)
		os.Exit(1)
	}

	// Print historical logs
	for _, entry := range logs {
		printLogEntry(entry)
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
		fmt.Fprintf(os.Stderr, "❌ Failed to connect to log stream: %v\n", err)
		os.Exit(1)
	}

	for entry := range ch {
		printLogEntry(entry)
	}
}

func printLogEntry(entry logger.LogEntry) {
	ts := entry.Timestamp.Format("15:04:05.000")
	fmt.Printf("%s [%s] %s: %s\n", ts, entry.Level, entry.ProcessName, entry.Message)
}
```

- [ ] **Step 2: Build and verify**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && make build`
Expected: Build succeeds

- [ ] **Step 3: Verify help output**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && ./build/cbox-init logs --help`
Expected: Shows new flags (--tail, -f, --url, --level)

- [ ] **Step 4: Commit**

```bash
git add cmd/cbox-init/logs.go
git commit -m "feat: rewrite logs command to use API client with SSE streaming"
```

---

## Task 10: Full Test Suite + Final Verification

Run everything and fix any issues.

**Files:** None (verification only)

- [ ] **Step 1: Run full test suite**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && go test ./... -count=1 -race`
Expected: All tests PASS with race detector

- [ ] **Step 2: Build all platforms**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && make build`
Expected: Build succeeds

- [ ] **Step 3: Verify full CLI help**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && ./build/cbox-init --help`
Expected: All commands listed: serve, tui, list, status, start, stop, restart, scale, reload-config, logs, check-config, scaffold, version

- [ ] **Step 4: Verify individual command help**

Run each:
```bash
./build/cbox-init list --help
./build/cbox-init status --help
./build/cbox-init start --help
./build/cbox-init stop --help
./build/cbox-init restart --help
./build/cbox-init scale --help
./build/cbox-init reload-config --help
./build/cbox-init logs --help
```
Expected: Each shows correct usage, flags, and descriptions

- [ ] **Step 5: Final commit**

If any fixes were needed, commit them:
```bash
git add -A
git commit -m "fix: address test and build issues from CLI commands implementation"
```
