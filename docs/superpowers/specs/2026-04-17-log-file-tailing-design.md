# Log File Tailing

**Date:** 2026-04-17
**Status:** Draft

## Problem

Applications write logs to local files (e.g., Laravel's `storage/logs/laravel.log`) that are invisible to cbox-init. Users want these file-based logs piped through the same logging pipeline as process stdout/stderr — with JSON normalization, level detection, severity filtering, redaction, and real-time streaming via TUI/CLI/SSE.

## Design

### 1. Configuration

Log files are configured under `logging.files` on a process. Each file has its own format/filtering config. Redaction is inherited from the process-level `logging.redaction` — it applies globally to stdout, stderr, and all log files.

```yaml
processes:
  laravel:
    command: ["php-fpm", "-F"]
    logging:
      redaction:                              # Global — applies to stdout, stderr, AND all files
        enabled: true
        patterns:
          - pattern: "password=\\S+"
            replacement: "password=***"
      files:
        laravel-log:
          path: /var/www/html/storage/logs/laravel.log
          rotate:
            max_size: 50MB
            max_files: 7
          json: { auto_detect: true }
          level_detection:
            patterns:
              error: "\\[ERROR\\]"
              warn: "\\[WARNING\\]"
          min_level: info
        horizon-log:
          path: /var/www/html/storage/logs/horizon.log
          json: { auto_detect: true }
```

**Config structs:**

New field on `LoggingConfig`:
```go
Files map[string]*LogFileConfig `yaml:"files" json:"files"`
```

New structs:
```go
type LogFileConfig struct {
    Path           string                `yaml:"path" json:"path"`
    Rotate         *RotateConfig         `yaml:"rotate" json:"rotate"`
    MinLevel       string                `yaml:"min_level" json:"min_level"`
    JSON           *JSONConfig           `yaml:"json" json:"json"`
    LevelDetection *LevelDetectionConfig `yaml:"level_detection" json:"level_detection"`
    Multiline      *MultilineConfig      `yaml:"multiline" json:"multiline"`
    Filters        *FilterConfig         `yaml:"filters" json:"filters"`
}

type RotateConfig struct {
    MaxSize  string `yaml:"max_size" json:"max_size"`    // e.g., "50MB", "100KB", "1GB"
    MaxFiles int    `yaml:"max_files" json:"max_files"`  // Number of rotated files to keep
}
```

Per-file fields (`json`, `level_detection`, `min_level`, `multiline`, `filters`) map directly to the corresponding fields in `LoggingConfig`. Redaction is NOT on `LogFileConfig` — it is always inherited from the parent process's `logging.redaction`.

### 2. FileTailer

Pure Go implementation of `tail -F` semantics. No shell commands, no external `tail` binary — works in any container.

**Behavior:**
- **Start:** Open file, seek to end (only new content). If file doesn't exist yet, watch parent directory and wait for it to appear.
- **Follow:** Use `fsnotify` to detect writes. Read new bytes, buffer into lines.
- **Truncation detection:** If file size shrinks (app's log rotation via `copytruncate`), seek to beginning and continue.
- **Replacement detection:** If file is deleted and recreated (rename-based rotation), close old handle, reopen, continue from start of new file.
- **Output:** Feed complete lines into a `ProcessWriter`, which runs the full logging pipeline (redaction, JSON parsing, level detection, filtering, broadcast).

```go
// Package: internal/logtail

type FileTailer struct {
    path    string
    writer  *logger.ProcessWriter
    rotator *FileRotator            // Optional, nil if no rotate config
    watcher *fsnotify.Watcher
}

func New(path string, writer *logger.ProcessWriter, rotator *FileRotator) *FileTailer
func (t *FileTailer) Start(ctx context.Context) error  // Blocks until ctx cancelled
func (t *FileTailer) Stop() error
```

### 3. FileRotator

Simple size-based log rotation. Runs as part of the tailing loop — checks file size after each read cycle.

**Behavior:**
- When file reaches `max_size`: rename `app.log` → `app.log.1`, shift `app.log.1` → `app.log.2`, etc.
- Delete files beyond `max_files` count.
- Truncate original file after rename (copytruncate style — the app holds the file handle open).
- The FileTailer detects the truncation and continues seamlessly.

```go
type FileRotator struct {
    maxSize  int64
    maxFiles int
}

func NewFileRotator(maxSize int64, maxFiles int) *FileRotator
func (r *FileRotator) CheckAndRotate(path string) error
```

`max_size` parsing: supports human-readable sizes — `"50MB"`, `"100KB"`, `"1GB"`. Parsed during config validation.

### 4. Integration in Supervisor

Each configured log file gets its own `ProcessWriter` + `FileTailer`, managed by the Supervisor.

**ProcessWriter per file:**
- `ProcessName`: the process name (scoping)
- `InstanceID`: the file config key (e.g., `"laravel-log"`)
- `Stream`: `"file"` (distinguishes from `"stdout"`/`"stderr"`)
- `LoggingConfig`: built from file-specific config with redaction inherited from process

**Lifecycle:**

FileTailers are bound to **configuration**, not process state:

1. **Process configured** → FileTailers start, seek to end of each file
2. **Process stop/restart** → FileTailers keep running (capture shutdown logs, maintain file position)
3. **Process removed from config** → FileTailers stop

This means log entries written during graceful shutdown are captured, and no data is lost during restarts. The FileTailer doesn't need to track or persist file offsets across restarts.

**Supervisor struct changes:**
```go
// New field:
fileTailers map[string]*logtail.FileTailer  // keyed by file config name
```

FileTailers are created when the supervisor starts (or when config is reloaded with new files). Each runs in its own goroutine. They share the process's `LogBroadcaster` for real-time streaming.

### 5. Data Flow

```
log file → FileTailer → ProcessWriter → [Redaction → JSON → LevelDetect → Filter] → LogBuffer → Broadcaster
                                                                                         ↓           ↓
                                                                                       TUI/API    SSE stream
```

Identical pipeline to stdout/stderr. The only difference is the input source. Log entries from files appear in TUI, `cbox-init logs`, and SSE streaming with `stream=file` and `instance_id=<file-key>` metadata.

## File Changes

### New files
| File | Purpose |
|------|---------|
| `internal/logtail/tailer.go` | FileTailer — pure Go tail -F with fsnotify |
| `internal/logtail/tailer_test.go` | Tests: follow, truncation, replacement, missing file |
| `internal/logtail/rotator.go` | FileRotator — size-based rotation |
| `internal/logtail/rotator_test.go` | Tests: rotation trigger, max_files cleanup, size parsing |

### Modified files
| File | Change |
|------|--------|
| `internal/config/types.go` | Add `Files` field to `LoggingConfig`, new `LogFileConfig` + `RotateConfig` structs |
| `internal/config/validation.go` | Validate log file config: path required, max_size parsing, max_files > 0 |
| `internal/config/defaults.go` | No default rotation unless explicitly configured |
| `internal/process/supervisor.go` | Create/manage FileTailers alongside process lifecycle |

## Not in Scope

- Glob patterns for file paths
- Log rotation scheduling (time-based) — only size-based
- Compression of rotated files
- Tailing files not associated with a process
- Persisting file offsets across daemon restarts
