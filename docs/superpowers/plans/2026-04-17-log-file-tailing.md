# Log File Tailing — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow users to configure local log files on processes so cbox-init tails them through the same logging pipeline (redaction, JSON parsing, level detection, filtering, broadcasting) as process stdout/stderr.

**Architecture:** New `internal/logtail/` package with `FileTailer` (pure Go tail -F using fsnotify) and `FileRotator` (size-based rotation). Config extends `LoggingConfig` with a `Files` map. Supervisor creates and manages FileTailers alongside process instances, with config-bound lifecycle (tailers outlive process restarts).

**Tech Stack:** Go 1.24, fsnotify (already in go.mod), existing logger.ProcessWriter pipeline

**Spec:** `docs/superpowers/specs/2026-04-17-log-file-tailing-design.md`

---

## File Structure

### New files
| File | Responsibility |
|------|---------------|
| `internal/logtail/rotator.go` | `FileRotator` — size-based log rotation |
| `internal/logtail/rotator_test.go` | Rotator tests |
| `internal/logtail/tailer.go` | `FileTailer` — pure Go tail -F with fsnotify |
| `internal/logtail/tailer_test.go` | Tailer tests: follow, truncation, replacement, missing file |

### Modified files
| File | Change |
|------|--------|
| `internal/config/types.go` | Add `Files` field to `LoggingConfig`, new `LogFileConfig` + `RotateConfig` structs |
| `internal/config/validation.go` | Validate log file config in `validateProcessLoggingConfig` |
| `internal/config/types.go` | Add defaults for log file multiline/level detection in `setProcessLoggingAdvancedDefaults` |
| `internal/process/supervisor.go` | Create/manage FileTailers in Start/Stop lifecycle |

---

## Task 1: Config Structs

Add the configuration types for log file tailing.

**Files:**
- Modify: `internal/config/types.go`

- [ ] **Step 1: Add `LogFileConfig` and `RotateConfig` structs**

Add after `FilterConfig` (after line 193) in `internal/config/types.go`:

```go
// LogFileConfig configures tailing of a local log file
type LogFileConfig struct {
	Path           string                `yaml:"path" json:"path"`
	Rotate         *RotateConfig         `yaml:"rotate" json:"rotate"`
	MinLevel       string                `yaml:"min_level" json:"min_level"`
	JSON           *JSONConfig           `yaml:"json" json:"json"`
	LevelDetection *LevelDetectionConfig `yaml:"level_detection" json:"level_detection"`
	Multiline      *MultilineConfig      `yaml:"multiline" json:"multiline"`
	Filters        *FilterConfig         `yaml:"filters" json:"filters"`
}

// RotateConfig configures size-based log file rotation
type RotateConfig struct {
	MaxSize  string `yaml:"max_size" json:"max_size"`   // Human-readable size: "50MB", "100KB", "1GB"
	MaxFiles int    `yaml:"max_files" json:"max_files"` // Number of rotated files to keep
}
```

- [ ] **Step 2: Add `Files` field to `LoggingConfig`**

Add to the `LoggingConfig` struct (after `Filters` on line 149):

```go
	Files          map[string]*LogFileConfig `yaml:"files" json:"files"`                   // Log file tailing
```

- [ ] **Step 3: Add `ParseSize` helper for human-readable size strings**

Add after the new structs:

```go
// ParseSize parses a human-readable size string (e.g., "50MB", "100KB", "1GB") into bytes.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size string")
	}

	s = strings.ToUpper(s)

	multipliers := []struct {
		suffix string
		mult   int64
	}{
		{"GB", 1024 * 1024 * 1024},
		{"MB", 1024 * 1024},
		{"KB", 1024},
		{"B", 1},
	}

	for _, m := range multipliers {
		if strings.HasSuffix(s, m.suffix) {
			numStr := strings.TrimSuffix(s, m.suffix)
			numStr = strings.TrimSpace(numStr)
			val, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid size number %q: %w", numStr, err)
			}
			if val <= 0 {
				return 0, fmt.Errorf("size must be positive, got %s", s)
			}
			return int64(val * float64(m.mult)), nil
		}
	}

	return 0, fmt.Errorf("invalid size format %q (use KB, MB, or GB suffix)", s)
}
```

Add `"strconv"` and `"strings"` to imports if not already present.

- [ ] **Step 4: Build to verify**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && go build ./...`
Expected: Build succeeds

- [ ] **Step 5: Commit**

```bash
git add internal/config/types.go
git commit -m "feat: add log file tailing config structs (LogFileConfig, RotateConfig)"
```

---

## Task 2: Config Validation and Defaults

Validate log file configuration and set defaults.

**Files:**
- Modify: `internal/config/validation.go`
- Modify: `internal/config/types.go` (defaults section)

- [ ] **Step 1: Add log file validation to `validateProcessLoggingConfig`**

In `internal/config/validation.go`, expand `validateProcessLoggingConfig` (currently lines 440-448). After the existing stdout/stderr check, add:

```go
	// Validate log file tailing configuration
	if proc.Logging.Files != nil {
		for fileName, fileCfg := range proc.Logging.Files {
			if fileCfg.Path == "" {
				result.AddProcessError(name, fmt.Sprintf("logging.files.%s.path", fileName), "Path is required", "Specify the file path to tail")
			}
			if fileCfg.Rotate != nil {
				if fileCfg.Rotate.MaxSize != "" {
					if _, err := ParseSize(fileCfg.Rotate.MaxSize); err != nil {
						result.AddProcessError(name, fmt.Sprintf("logging.files.%s.rotate.max_size", fileName), fmt.Sprintf("Invalid size: %v", err), "Use format like '50MB', '1GB'")
					}
				}
				if fileCfg.Rotate.MaxFiles < 0 {
					result.AddProcessError(name, fmt.Sprintf("logging.files.%s.rotate.max_files", fileName), "max_files must be non-negative", "Set max_files >= 0")
				}
			}
		}
	}
```

- [ ] **Step 2: Add log file defaults to `setProcessLoggingAdvancedDefaults`**

In `internal/config/types.go`, expand `setProcessLoggingAdvancedDefaults` (currently lines 498-516). After the existing MinLevel default, add:

```go
	// Set defaults for log file tailing configs
	for _, fileCfg := range proc.Logging.Files {
		if fileCfg.Multiline != nil {
			if fileCfg.Multiline.MaxLines == 0 {
				fileCfg.Multiline.MaxLines = 100
			}
			if fileCfg.Multiline.Timeout == 0 {
				fileCfg.Multiline.Timeout = 1
			}
		}
		if fileCfg.LevelDetection != nil && fileCfg.LevelDetection.DefaultLevel == "" {
			fileCfg.LevelDetection.DefaultLevel = "info"
		}
		if fileCfg.Rotate != nil && fileCfg.Rotate.MaxFiles == 0 {
			fileCfg.Rotate.MaxFiles = 5
		}
	}
```

- [ ] **Step 3: Run tests**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && go test ./internal/config/ -v -count=1`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add internal/config/validation.go internal/config/types.go
git commit -m "feat: add validation and defaults for log file tailing config"
```

---

## Task 3: FileRotator

Implement size-based log rotation.

**Files:**
- Create: `internal/logtail/rotator.go`
- Create: `internal/logtail/rotator_test.go`

- [ ] **Step 1: Write failing tests for FileRotator**

Create `internal/logtail/rotator_test.go`:

```go
package logtail

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileRotator_NoRotationNeeded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	os.WriteFile(path, []byte("small"), 0644)

	r := NewFileRotator(1024, 3) // 1KB max
	if err := r.CheckAndRotate(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// File should still exist unchanged
	data, _ := os.ReadFile(path)
	if string(data) != "small" {
		t.Errorf("file was modified: %q", data)
	}
}

func TestFileRotator_RotatesWhenOverSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	// Write 2KB of data
	data := make([]byte, 2048)
	for i := range data {
		data[i] = 'x'
	}
	os.WriteFile(path, data, 0644)

	r := NewFileRotator(1024, 3) // 1KB max, keep 3
	if err := r.CheckAndRotate(path); err != nil {
		t.Fatalf("rotation failed: %v", err)
	}

	// Original file should be truncated
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("original file missing: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("expected truncated file, got size %d", info.Size())
	}

	// .1 file should exist with original data
	rotated := path + ".1"
	rData, err := os.ReadFile(rotated)
	if err != nil {
		t.Fatalf("rotated file missing: %v", err)
	}
	if len(rData) != 2048 {
		t.Errorf("rotated file size %d, expected 2048", len(rData))
	}
}

func TestFileRotator_ShiftsExistingFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	// Create existing rotated files
	os.WriteFile(path+".1", []byte("old-1"), 0644)
	os.WriteFile(path+".2", []byte("old-2"), 0644)

	// Write oversized main file
	os.WriteFile(path, make([]byte, 2048), 0644)

	r := NewFileRotator(1024, 3)
	if err := r.CheckAndRotate(path); err != nil {
		t.Fatalf("rotation failed: %v", err)
	}

	// .1 should have main file data (2048 bytes)
	info, _ := os.Stat(path + ".1")
	if info.Size() != 2048 {
		t.Errorf(".1 size %d, expected 2048", info.Size())
	}

	// .2 should have old .1 data
	data, _ := os.ReadFile(path + ".2")
	if string(data) != "old-1" {
		t.Errorf(".2 content %q, expected 'old-1'", data)
	}

	// .3 should have old .2 data
	data, _ = os.ReadFile(path + ".3")
	if string(data) != "old-2" {
		t.Errorf(".3 content %q, expected 'old-2'", data)
	}
}

func TestFileRotator_DeletesBeyondMaxFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	// Create max rotated files already
	os.WriteFile(path+".1", []byte("one"), 0644)
	os.WriteFile(path+".2", []byte("two"), 0644)

	// Write oversized main file
	os.WriteFile(path, make([]byte, 2048), 0644)

	r := NewFileRotator(1024, 2) // keep only 2
	if err := r.CheckAndRotate(path); err != nil {
		t.Fatalf("rotation failed: %v", err)
	}

	// .1 and .2 should exist
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Error(".1 should exist")
	}
	if _, err := os.Stat(path + ".2"); err != nil {
		t.Error(".2 should exist")
	}

	// .3 should NOT exist (was old .2, shifted beyond max)
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Error(".3 should have been deleted")
	}
}

func TestFileRotator_MissingFile(t *testing.T) {
	r := NewFileRotator(1024, 3)
	// Should not error on missing file
	if err := r.CheckAndRotate("/tmp/nonexistent-test-file.log"); err != nil {
		t.Fatalf("unexpected error on missing file: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && go test ./internal/logtail/ -run TestFileRotator -v -count=1`
Expected: FAIL — package not found

- [ ] **Step 3: Implement `internal/logtail/rotator.go`**

```go
package logtail

import (
	"fmt"
	"os"
)

// FileRotator performs size-based log file rotation.
// When a file exceeds MaxSize, it is renamed with a numeric suffix
// (app.log → app.log.1, app.log.1 → app.log.2, etc.) and the
// original is truncated. Files beyond MaxFiles are deleted.
type FileRotator struct {
	MaxSize  int64
	MaxFiles int
}

// NewFileRotator creates a new FileRotator.
// maxSize is the maximum file size in bytes before rotation.
// maxFiles is the number of rotated files to keep.
func NewFileRotator(maxSize int64, maxFiles int) *FileRotator {
	return &FileRotator{
		MaxSize:  maxSize,
		MaxFiles: maxFiles,
	}
}

// CheckAndRotate checks if the file exceeds MaxSize and rotates if needed.
// Safe to call frequently — returns immediately if file is under the limit.
// Returns nil if file doesn't exist (file may not have been created yet).
func (r *FileRotator) CheckAndRotate(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", path, err)
	}

	if info.Size() < r.MaxSize {
		return nil
	}

	// Shift existing rotated files: .N → .N+1, starting from highest
	// Delete any that exceed MaxFiles
	for i := r.MaxFiles; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", path, i)
		dst := fmt.Sprintf("%s.%d", path, i+1)

		if i == r.MaxFiles {
			// Delete the oldest file beyond max
			os.Remove(dst)
		}

		if _, err := os.Stat(src); err == nil {
			if i >= r.MaxFiles {
				os.Remove(src)
			} else {
				os.Rename(src, dst)
			}
		}
	}

	// Rename current file to .1
	rotatedPath := fmt.Sprintf("%s.1", path)
	if err := os.Rename(path, rotatedPath); err != nil {
		return fmt.Errorf("rename %s → %s: %w", path, rotatedPath, err)
	}

	// Create new empty file (copytruncate style)
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	f.Close()

	return nil
}
```

- [ ] **Step 4: Run rotator tests**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && go test ./internal/logtail/ -run TestFileRotator -v -count=1`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/logtail/
git commit -m "feat: add FileRotator for size-based log file rotation"
```

---

## Task 4: FileTailer

Implement pure Go tail -F with fsnotify.

**Files:**
- Create: `internal/logtail/tailer.go`
- Create: `internal/logtail/tailer_test.go`

- [ ] **Step 1: Write failing tests for FileTailer**

Create `internal/logtail/tailer_test.go`:

```go
package logtail

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// lineCollector is a simple io.Writer that collects lines for testing
type lineCollector struct {
	mu    sync.Mutex
	lines []string
	buf   bytes.Buffer
}

func (lc *lineCollector) Write(p []byte) (int, error) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.buf.Write(p)
	for {
		line, err := lc.buf.ReadString('\n')
		if err != nil {
			// Put incomplete line back
			lc.buf.WriteString(line)
			break
		}
		lc.lines = append(lc.lines, line[:len(line)-1]) // strip newline
	}
	return len(p), nil
}

func (lc *lineCollector) Lines() []string {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	result := make([]string, len(lc.lines))
	copy(result, lc.lines)
	return result
}

func TestFileTailer_FollowNewLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	// Create empty file
	os.WriteFile(path, nil, 0644)

	collector := &lineCollector{}
	tailer := New(path, collector, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go tailer.Start(ctx)
	time.Sleep(200 * time.Millisecond) // Let tailer initialize

	// Write lines
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString("line one\n")
	f.WriteString("line two\n")
	f.Close()

	// Wait for tailer to pick up
	time.Sleep(500 * time.Millisecond)

	lines := collector.Lines()
	if len(lines) < 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "line one" {
		t.Errorf("line 0: %q", lines[0])
	}
	if lines[1] != "line two" {
		t.Errorf("line 1: %q", lines[1])
	}
}

func TestFileTailer_SeeksToEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	// Write existing content BEFORE starting tailer
	os.WriteFile(path, []byte("old line\n"), 0644)

	collector := &lineCollector{}
	tailer := New(path, collector, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go tailer.Start(ctx)
	time.Sleep(200 * time.Millisecond)

	// Write new content
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString("new line\n")
	f.Close()

	time.Sleep(500 * time.Millisecond)

	lines := collector.Lines()
	// Should only see "new line", not "old line"
	for _, l := range lines {
		if l == "old line" {
			t.Error("should not see content that existed before tailer started")
		}
	}
	if len(lines) == 0 || lines[len(lines)-1] != "new line" {
		t.Errorf("expected 'new line', got %v", lines)
	}
}

func TestFileTailer_DetectsTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	os.WriteFile(path, nil, 0644)

	collector := &lineCollector{}
	tailer := New(path, collector, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go tailer.Start(ctx)
	time.Sleep(200 * time.Millisecond)

	// Write first line
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString("before truncate\n")
	f.Close()
	time.Sleep(300 * time.Millisecond)

	// Truncate file (simulates copytruncate rotation)
	os.Truncate(path, 0)
	time.Sleep(200 * time.Millisecond)

	// Write after truncation
	f, _ = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString("after truncate\n")
	f.Close()
	time.Sleep(500 * time.Millisecond)

	lines := collector.Lines()
	found := false
	for _, l := range lines {
		if l == "after truncate" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'after truncate' in lines: %v", lines)
	}
}

func TestFileTailer_WaitsForMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-yet.log")

	collector := &lineCollector{}
	tailer := New(path, collector, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go tailer.Start(ctx)
	time.Sleep(200 * time.Millisecond)

	// Create file after tailer started
	f, _ := os.Create(path)
	f.WriteString("appeared\n")
	f.Close()

	time.Sleep(500 * time.Millisecond)

	lines := collector.Lines()
	if len(lines) == 0 || lines[0] != "appeared" {
		t.Errorf("expected 'appeared', got %v", lines)
	}
}

func TestFileTailer_StopsOnContextCancel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	os.WriteFile(path, nil, 0644)

	collector := &lineCollector{}
	tailer := New(path, collector, nil)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- tailer.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Expected: Start returned
	case <-time.After(2 * time.Second):
		t.Fatal("tailer did not stop after context cancel")
	}
}

func TestFileTailer_WithRotator(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	os.WriteFile(path, nil, 0644)

	collector := &lineCollector{}
	rotator := NewFileRotator(100, 2) // 100 bytes, keep 2
	tailer := New(path, collector, rotator)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go tailer.Start(ctx)
	time.Sleep(200 * time.Millisecond)

	// Write enough to trigger rotation
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	for i := 0; i < 20; i++ {
		f.WriteString("this is a log line that is fairly long to fill up space\n")
	}
	f.Close()

	time.Sleep(500 * time.Millisecond)

	// Rotated file should exist
	if _, err := os.Stat(path + ".1"); os.IsNotExist(err) {
		t.Error("expected rotated file .1 to exist")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && go test ./internal/logtail/ -run TestFileTailer -v -count=1`
Expected: FAIL — `New` function not found

- [ ] **Step 3: Implement `internal/logtail/tailer.go`**

```go
package logtail

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// FileTailer implements tail -F semantics in pure Go.
// It follows a file, detects truncation and replacement,
// and waits for missing files to appear.
// Output is written to an io.Writer (typically a ProcessWriter).
type FileTailer struct {
	path    string
	writer  io.Writer
	rotator *FileRotator // optional
}

// New creates a new FileTailer.
// writer receives complete lines (including newline).
// rotator is optional (nil to disable rotation).
func New(path string, writer io.Writer, rotator *FileRotator) *FileTailer {
	return &FileTailer{
		path:    path,
		writer:  writer,
		rotator: rotator,
	}
}

// Start begins tailing the file. Blocks until ctx is cancelled.
// If the file doesn't exist, waits for it to appear.
// Detects truncation (seek to start) and replacement (reopen).
func (t *FileTailer) Start(ctx context.Context) error {
	// Wait for file to exist
	if err := t.waitForFile(ctx); err != nil {
		return err
	}

	return t.tailLoop(ctx)
}

// Stop is a no-op — use context cancellation to stop the tailer.
func (t *FileTailer) Stop() error {
	return nil
}

// waitForFile blocks until the target file exists or ctx is cancelled.
// Uses fsnotify to watch the parent directory for file creation.
func (t *FileTailer) waitForFile(ctx context.Context) error {
	if _, err := os.Stat(t.path); err == nil {
		return nil // File exists
	}

	// Watch parent directory for file creation
	dir := filepath.Dir(t.path)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	defer watcher.Close()

	if err := watcher.Add(dir); err != nil {
		// Directory doesn't exist either — fall back to polling
		return t.pollForFile(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-watcher.Events:
			if !ok {
				return fmt.Errorf("watcher closed")
			}
			if event.Name == t.path && (event.Has(fsnotify.Create) || event.Has(fsnotify.Write)) {
				return nil
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return fmt.Errorf("watcher error channel closed")
			}
			return fmt.Errorf("watcher error: %w", err)
		}
	}
}

// pollForFile polls for file existence as a fallback when fsnotify can't watch the directory.
func (t *FileTailer) pollForFile(ctx context.Context) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := os.Stat(t.path); err == nil {
				return nil
			}
		}
	}
}

// tailLoop is the main tailing loop. Opens the file, seeks to end,
// and reads new lines as they're written.
func (t *FileTailer) tailLoop(ctx context.Context) error {
	file, err := os.Open(t.path)
	if err != nil {
		return fmt.Errorf("open %s: %w", t.path, err)
	}
	defer file.Close()

	// Seek to end — we only want new content
	offset, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("seek to end: %w", err)
	}

	// Setup fsnotify watcher on the file
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	defer watcher.Close()

	// Watch both the file and its parent directory (for rename/create detection)
	if err := watcher.Add(t.path); err != nil {
		return fmt.Errorf("watch file: %w", err)
	}
	dir := filepath.Dir(t.path)
	_ = watcher.Add(dir) // Best-effort, may fail if dir is unreadable

	reader := bufio.NewReader(file)

	for {
		select {
		case <-ctx.Done():
			return nil

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				// Check for truncation
				info, err := os.Stat(t.path)
				if err != nil {
					continue
				}

				if info.Size() < offset {
					// File was truncated — seek to start
					file.Seek(0, io.SeekStart)
					offset = 0
					reader.Reset(file)
				}

				// Read new lines
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						if len(line) > 0 {
							// Incomplete line — put it back by seeking back
							file.Seek(offset, io.SeekStart)
							reader.Reset(file)
						}
						break
					}
					offset += int64(len(line))
					// Write to output (ProcessWriter implements io.Writer)
					t.writer.Write([]byte(line))
				}

				// Update offset to current position
				newOffset, _ := file.Seek(0, io.SeekCurrent)
				if newOffset > offset {
					offset = newOffset
				}

				// Check rotation if configured
				if t.rotator != nil {
					if err := t.rotator.CheckAndRotate(t.path); err != nil {
						// Log but don't fail — rotation is best-effort
						continue
					}
				}
			}

			if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				// File was removed/renamed — try to reopen
				file.Close()
				watcher.Remove(t.path)

				// Wait for file to reappear
				if err := t.waitForFile(ctx); err != nil {
					return err
				}

				file, err = os.Open(t.path)
				if err != nil {
					return fmt.Errorf("reopen %s: %w", t.path, err)
				}
				defer file.Close()

				offset = 0
				reader = bufio.NewReader(file)
				_ = watcher.Add(t.path)
			}

		case _, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			// Continue on watcher errors — best effort
		}
	}
}
```

- [ ] **Step 4: Run all tailer tests**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && go test ./internal/logtail/ -v -count=1`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/logtail/tailer.go internal/logtail/tailer_test.go
git commit -m "feat: add FileTailer with pure Go tail -F semantics"
```

---

## Task 5: Supervisor Integration

Wire FileTailers into the Supervisor lifecycle.

**Files:**
- Modify: `internal/process/supervisor.go`

- [ ] **Step 1: Add `fileTailers` field and imports**

Add to the Supervisor struct (after `logBroadcaster` field, around line 108):

```go
	fileTailers    map[string]context.CancelFunc // active file tailers, keyed by config name
```

Add import for logtail:
```go
	"github.com/cboxdk/init/internal/logtail"
```

Also add `"github.com/cboxdk/init/internal/config"` if not already in imports (it likely is via the `config.Process` field).

- [ ] **Step 2: Add `startFileTailers` method**

Add after the `startInstance` method:

```go
// startFileTailers starts file tailers for all configured log files.
// Each tailer gets its own ProcessWriter with the file's logging config
// plus the process-level redaction config.
// Tailers are bound to configuration, not process state — they survive restarts.
func (s *Supervisor) startFileTailers(ctx context.Context) {
	if s.config.Logging == nil || len(s.config.Logging.Files) == 0 {
		return
	}

	if s.fileTailers == nil {
		s.fileTailers = make(map[string]context.CancelFunc)
	}

	for name, fileCfg := range s.config.Logging.Files {
		// Build a LoggingConfig for this file, inheriting process-level redaction
		fileLogging := &config.LoggingConfig{
			Redaction:      s.config.Logging.Redaction, // Inherited from process
			MinLevel:       fileCfg.MinLevel,
			JSON:           fileCfg.JSON,
			LevelDetection: fileCfg.LevelDetection,
			Multiline:      fileCfg.Multiline,
			Filters:        fileCfg.Filters,
		}

		// Create a ProcessWriter for this file
		pw, err := logger.NewProcessWriter(s.logger, s.name, name, "file", fileLogging)
		if err != nil {
			s.logger.Error("Failed to create process writer for log file",
				"file", name, "path", fileCfg.Path, "error", err)
			continue
		}

		// Wire broadcaster for real-time subscriptions
		if s.logBroadcaster != nil {
			pw.SetBroadcaster(s.logBroadcaster)
		}

		// Create optional rotator
		var rotator *logtail.FileRotator
		if fileCfg.Rotate != nil && fileCfg.Rotate.MaxSize != "" {
			maxBytes, err := config.ParseSize(fileCfg.Rotate.MaxSize)
			if err != nil {
				s.logger.Error("Invalid rotate max_size for log file",
					"file", name, "error", err)
				continue
			}
			rotator = logtail.NewFileRotator(maxBytes, fileCfg.Rotate.MaxFiles)
		}

		// Create and start tailer
		tailer := logtail.New(fileCfg.Path, pw, rotator)

		tailerCtx, tailerCancel := context.WithCancel(ctx)
		s.fileTailers[name] = tailerCancel

		s.goroutines.Add(1)
		go func(n, p string) {
			defer s.goroutines.Done()
			if err := tailer.Start(tailerCtx); err != nil && tailerCtx.Err() == nil {
				s.logger.Error("File tailer error", "file", n, "path", p, "error", err)
			}
		}(name, fileCfg.Path)

		s.logger.Info("Started file tailer", "file", name, "path", fileCfg.Path)
	}
}

// stopFileTailers stops all active file tailers.
func (s *Supervisor) stopFileTailers() {
	for name, cancel := range s.fileTailers {
		cancel()
		s.logger.Debug("Stopped file tailer", "file", name)
	}
	s.fileTailers = nil
}
```

- [ ] **Step 3: Hook into Supervisor.Start**

In the `Start` method (around line 319), after the instance startup loop and before releasing the lock, add a call to start file tailers. Find the line after the instance loop where `s.state` is set to `StateRunning` and add before it:

```go
	// Start file tailers (config-bound, independent of process instances)
	s.startFileTailers(s.ctx)
```

- [ ] **Step 4: Hook into Supervisor.Stop**

In the `Stop` method (around line 712), after cancelling the context (the `s.cancel()` call), add:

```go
	// Stop file tailers
	s.stopFileTailers()
```

- [ ] **Step 5: Build and run tests**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && go build ./... && go test ./internal/process/ -v -count=1`
Expected: Build succeeds, all tests PASS

- [ ] **Step 6: Run full test suite**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && go test ./... -count=1`
Expected: All tests PASS

- [ ] **Step 7: Commit**

```bash
git add internal/process/supervisor.go
git commit -m "feat: integrate FileTailers into Supervisor lifecycle"
```

---

## Task 6: Full Verification

Run everything, verify the feature works end-to-end.

**Files:** None (verification only)

- [ ] **Step 1: Run full test suite**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && go test ./... -count=1`
Expected: All tests PASS

- [ ] **Step 2: Build**

Run: `cd /home/cortex/.polyscope/clones/c3ce9287/sleek-tiger && make build`
Expected: Build succeeds

- [ ] **Step 3: Verify config loading with log files**

Create a temporary test config and run check-config:

```bash
cat > /tmp/test-logtail.yaml << 'EOF'
version: "1.0"
global:
  shutdown_timeout: 30
  log_level: info

processes:
  laravel:
    enabled: true
    command: ["php-fpm", "-F"]
    logging:
      redaction:
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
          json:
            enabled: true
            detect_auto: true
          min_level: info
        horizon-log:
          path: /var/www/html/storage/logs/horizon.log
          json:
            enabled: true
            detect_auto: true
EOF
./build/cbox-init check-config --config /tmp/test-logtail.yaml
```
Expected: Config validation passes (may have warnings about missing health check etc., but no errors about log_files)

- [ ] **Step 4: Clean up and final commit if needed**

```bash
rm /tmp/test-logtail.yaml
```

If any fixes were needed:
```bash
git add -A
git commit -m "fix: address issues from log file tailing verification"
```
