# CLI Commands & Always-On Unix Socket

**Date:** 2026-04-17
**Status:** Draft

## Problem

1. The TUI and CLI management commands don't work unless `api_enabled: true` and the TCP port is exposed. Users who `docker exec` into a running container expect local management to just work.
2. The root command help text advertises `restart`, `scale`, and other subcommands that don't exist. The API endpoints and process manager methods are fully implemented — only the CLI wrappers are missing.
3. The `logs` command creates its own local process manager instead of connecting to the running daemon. It should use the API and support real-time streaming.

## Design

### 1. Always-On Unix Socket

The daemon always starts a Unix socket listener, regardless of `api_enabled`. The TCP listener remains gated behind `api_enabled` as before.

**Socket path resolution:**
1. If `api_socket` is set in config, use that path
2. Otherwise, try `/var/run/cbox-init.sock`
3. If `/var/run/` is not writable, fall back to `/tmp/cbox-init.sock`

**Socket properties:**
- Permissions: `0660` (owner + group read/write)
- Serves the same HTTP handler as the TCP listener
- No ACL middleware (file permissions are the security model)
- No rate limiting (local access only)
- Cleaned up on daemon shutdown (remove socket file)

**Changes to `serve.go`:**
- Socket listener starts unconditionally before the `if api_enabled` check
- If `api_enabled: true`, TCP listener also starts (existing behavior)
- Both listeners share the same `http.Handler` and process manager reference

**Changes to `server.go`:**
- New exported method: `StartSocketOnly(socketPath string) error` — starts only the Unix socket listener with the shared handler. Does not start TCP.
- Existing `Start()` method continues to handle both TCP + optional socket as before (used when `api_enabled: true`)
- On startup, removes any stale socket file at the target path before binding (existing behavior in `startSocketListener`, preserving it)

**Config:**
- No new config fields required
- `api_socket` can still override the default path
- `api_enabled: false` now means "no TCP port" rather than "no API at all"

### 2. API Client Refactoring

The API client moves from `internal/tui/client.go` to `internal/apiclient/client.go`.

**Package:** `internal/apiclient`

**Constructors:**
```go
// New creates a client with an explicit base URL
func New(baseURL string, authToken string) *Client

// NewWithAutoDiscover creates a client that auto-detects Unix socket,
// falling back to TCP on localhost:9180
func NewWithAutoDiscover(authToken string) *Client
```

**Auto-discovery order** (unchanged from current TUI client):
1. `/var/run/cbox-init.sock`
2. `/tmp/cbox-init.sock`
3. `/run/cbox-init.sock`
4. Fall back to `http://localhost:9180`

**Methods:**
```go
// Process management
ListProcesses() ([]ProcessInfo, error)
GetProcessConfig(name string) (*ProcessDetail, error)
StartProcess(name string) error
StopProcess(name string) error
RestartProcess(name string) error
ScaleProcess(name string, desired int) error
ScaleProcessDelta(name string, delta int) error
DeleteProcess(name string) error
UpdateProcess(name string, config interface{}) error
AddProcess(ctx context.Context, name string, cmd []string, scale int, restart string, enabled bool) error

// Logs
GetLogs(process string, limit int) ([]LogEntry, error)
GetStackLogs(limit int) ([]LogEntry, error)
StreamLogs(ctx context.Context, process string) (<-chan LogEntry, error)

// Config
ReloadConfig() error
SaveConfig() error

// Health
HealthCheck(ctx context.Context) error

// Schedule
PauseSchedule(name string) error
ResumeSchedule(name string) error
TriggerSchedule(name string) error
```

**TUI update:**
- `internal/tui/*.go` imports `internal/apiclient` instead of using its own client
- No functional change to TUI behavior

### 3. CLI Commands

All commands share a common pattern:
1. Create `apiclient.NewWithAutoDiscover(authToken)` (token from `CBOX_INIT_API_AUTH` env var)
2. Support `--url` flag to override auto-discovery
3. Call the appropriate API method
4. Print result to stdout
5. Exit with code 0 on success, 1 on error

#### `cbox-init list`

```
$ cbox-init list
NAME              STATUS    SCALE  RESTARTS  UPTIME
php-fpm           running   1/1    0         2h34m
nginx             running   1/1    0         2h34m
horizon           running   1/1    2         1h12m
queue-default     running   3/5    0         2h34m
scheduler         stopped   0/1    0         -
```

- Table format with color-coded status (green=running, red=stopped, yellow=degraded)
- Exit code 0 if all processes healthy, 1 if any unhealthy
- No arguments required

#### `cbox-init status <name>`

```
$ cbox-init status horizon
Name:       horizon
Status:     running
PID:        1234
Scale:      1/1
Restarts:   2
Uptime:     1h12m
Command:    php artisan horizon
Health:     healthy (last check 5s ago)
```

- Detailed view of a single process
- Exit code 1 if process not found

#### `cbox-init start <name>`

```
$ cbox-init start horizon
✓ horizon started
```

#### `cbox-init stop <name>`

```
$ cbox-init stop horizon
✓ horizon stopped
```

#### `cbox-init restart <name>`

```
$ cbox-init restart horizon
✓ horizon restarted
```

#### `cbox-init scale <name> <count>`

```
$ cbox-init scale queue-default 10
✓ queue-default scaled to 10 instances
```

- Takes an absolute count (no relative +N/-N syntax)
- Validates count is a positive integer

#### `cbox-init reload-config`

```
$ cbox-init reload-config
✓ Configuration reloaded
```

#### `cbox-init logs` (reworked)

```
$ cbox-init logs                      # All processes, last 100 lines
$ cbox-init logs nginx                # Specific process, last 100 lines
$ cbox-init logs nginx --tail 50      # Last 50 lines
$ cbox-init logs -f                   # Follow all processes (SSE)
$ cbox-init logs nginx -f             # Follow specific process (SSE)
$ cbox-init logs nginx --tail 20 -f   # Last 20 lines then follow
```

**Flags:**
- `--tail N` (default 100): Number of historical lines to show
- `-f` / `--follow`: Stream new log entries via SSE after showing history
- `--level`: Filter by log level (existing flag, preserved)

**Follow mode behavior:**
1. Fetch last `--tail` lines via `GET /api/v1/logs` or `GET /api/v1/processes/{name}/logs`
2. Print them
3. Connect to SSE stream endpoint
4. Print new entries as they arrive
5. Ctrl+C (SIGINT) cancels context and exits cleanly

### 4. SSE Log Streaming

#### New API endpoint

```
GET /api/v1/logs/stream?process={name}
```

- `process` query param is optional. If omitted, streams all processes.
- Response: `Content-Type: text/event-stream`
- Requires auth (same as other endpoints)
- On Unix socket: no auth required (same as other socket requests)

#### Event format

```
event: log
data: {"timestamp":"2026-04-17T10:23:45Z","process":"nginx","level":"info","message":"GET /api/health 200 1ms"}

: keepalive

event: log
data: {"timestamp":"2026-04-17T10:23:46Z","process":"horizon","level":"info","message":"Processing job: SendEmail"}
```

- Each event has type `log` with a JSON payload
- Heartbeat comment (`: keepalive\n\n`) every 15 seconds to detect dead connections

#### Server-side implementation

**Log subscription in process manager:**

The process manager (in `internal/process/`) exposes a subscriber interface:

```go
// In internal/process/manager.go or a new manager_logs.go
type LogSubscriber interface {
    Subscribe(filter string) (<-chan LogEntry, func())  // channel + unsubscribe func
}
```

- `filter` is a process name, or empty string for all processes
- Returns a buffered channel (buffer size ~256) and an unsubscribe function
- Manager broadcasts new log entries to all active subscribers
- Unsubscribe closes the channel and removes the subscriber

**SSE handler in server.go:**

```go
func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request) {
    flusher, ok := w.(http.Flusher)
    // ...
    processFilter := r.URL.Query().Get("process")
    ch, unsub := s.manager.Subscribe(processFilter)
    defer unsub()

    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    // Heartbeat ticker + event loop
    // Respect r.Context().Done() for clean disconnect
}
```

#### Client-side implementation

`apiclient.StreamLogs()` in `internal/apiclient/stream.go`:

```go
func (c *Client) StreamLogs(ctx context.Context, process string) (<-chan LogEntry, error) {
    // Open HTTP request with Accept: text/event-stream
    // Parse SSE events from response body in goroutine
    // Send LogEntry on returned channel
    // Close channel when ctx is cancelled or connection dies
    // Reconnect with backoff on unexpected disconnect
}
```

Reconnect strategy: 1s, 2s, 4s, 8s, max 30s backoff. Stops on context cancellation.

## File Changes

### Modified files
| File | Change |
|------|--------|
| `cmd/cbox-init/serve.go` | Start socket unconditionally, TCP gated behind `api_enabled` |
| `cmd/cbox-init/root.go` | Register new commands, remove commented-out lines, update help examples |
| `cmd/cbox-init/logs.go` | Rework to use apiclient + SSE streaming |
| `internal/api/server.go` | Socket-only start method, SSE log stream endpoint + handler |
| `internal/tui/*.go` | Update imports from local client to `internal/apiclient` |
| `internal/process/manager.go` | Add log subscriber interface and broadcast mechanism |

### New files
| File | Purpose |
|------|---------|
| `internal/apiclient/client.go` | Refactored API client (from tui/client.go) |
| `internal/apiclient/stream.go` | SSE parsing and StreamLogs implementation |
| `cmd/cbox-init/list.go` | `list` command |
| `cmd/cbox-init/status.go` | `status <name>` command |
| `cmd/cbox-init/start.go` | `start <name>` command |
| `cmd/cbox-init/stop.go` | `stop <name>` command |
| `cmd/cbox-init/restart.go` | `restart <name>` command |
| `cmd/cbox-init/scale.go` | `scale <name> <count>` command |
| `cmd/cbox-init/reload_config.go` | `reload-config` command |

## Not in Scope

- No new config fields — socket works without config changes
- No breaking changes — `api_enabled` still controls TCP as before
- TUI functionality unchanged — only import path changes
- No authentication changes — socket skips auth as it does today
- No changes to existing API endpoints — only additions (SSE stream)
