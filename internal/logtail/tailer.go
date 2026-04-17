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
type FileTailer struct {
	path    string
	writer  io.Writer
	rotator *FileRotator
}

// New creates a new FileTailer. writer receives complete lines.
// rotator is optional (nil to disable rotation).
func New(path string, writer io.Writer, rotator *FileRotator) *FileTailer {
	return &FileTailer{path: path, writer: writer, rotator: rotator}
}

// Start begins tailing the file. Blocks until ctx is cancelled.
func (t *FileTailer) Start(ctx context.Context) error {
	existed := true
	if _, err := os.Stat(t.path); err != nil {
		existed = false
	}
	if err := t.waitForFile(ctx); err != nil {
		return err
	}
	return t.tailLoop(ctx, existed)
}

// Stop is a no-op — use context cancellation.
func (t *FileTailer) Stop() error {
	return nil
}

func (t *FileTailer) waitForFile(ctx context.Context) error {
	if _, err := os.Stat(t.path); err == nil {
		return nil
	}

	dir := filepath.Dir(t.path)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	defer watcher.Close()

	if err := watcher.Add(dir); err != nil {
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

func (t *FileTailer) tailLoop(ctx context.Context, seekToEnd bool) error {
	file, err := os.Open(t.path)
	if err != nil {
		return fmt.Errorf("open %s: %w", t.path, err)
	}
	defer file.Close()

	var offset int64
	if seekToEnd {
		offset, err = file.Seek(0, io.SeekEnd)
		if err != nil {
			return fmt.Errorf("seek to end: %w", err)
		}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	defer watcher.Close()

	if err := watcher.Add(t.path); err != nil {
		return fmt.Errorf("watch file: %w", err)
	}
	dir := filepath.Dir(t.path)
	_ = watcher.Add(dir)

	reader := bufio.NewReader(file)

	// readLines reads all available complete lines from the current position.
	readLines := func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if len(line) > 0 {
					// Incomplete line — seek back so we re-read it next time
					file.Seek(offset, io.SeekStart)
					reader.Reset(file)
				}
				break
			}
			offset += int64(len(line))
			t.writer.Write([]byte(line))
		}

		newOffset, _ := file.Seek(0, io.SeekCurrent)
		if newOffset > offset {
			offset = newOffset
		}
	}

	// If we didn't seek to end, read any existing content now
	if !seekToEnd {
		readLines()
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				info, err := os.Stat(t.path)
				if err != nil {
					continue
				}
				if info.Size() < offset {
					file.Seek(0, io.SeekStart)
					offset = 0
					reader.Reset(file)
				}

				readLines()

				if t.rotator != nil {
					t.rotator.CheckAndRotate(t.path)
				}
			}

			if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				file.Close()
				watcher.Remove(t.path)

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
		}
	}
}
