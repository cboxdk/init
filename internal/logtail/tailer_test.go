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
			lc.buf.WriteString(line)
			break
		}
		lc.lines = append(lc.lines, line[:len(line)-1])
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
	_ = os.WriteFile(path, nil, 0644)

	collector := &lineCollector{}
	tailer := New(path, collector, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = tailer.Start(ctx) }()
	time.Sleep(200 * time.Millisecond)

	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString("line one\n")
	f.WriteString("line two\n")
	f.Close()

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
	_ = os.WriteFile(path, []byte("old line\n"), 0644)

	collector := &lineCollector{}
	tailer := New(path, collector, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = tailer.Start(ctx) }()
	time.Sleep(200 * time.Millisecond)

	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString("new line\n")
	f.Close()

	time.Sleep(500 * time.Millisecond)

	lines := collector.Lines()
	for _, l := range lines {
		if l == "old line" {
			t.Error("should not see pre-existing content")
		}
	}
	if len(lines) == 0 || lines[len(lines)-1] != "new line" {
		t.Errorf("expected 'new line', got %v", lines)
	}
}

func TestFileTailer_DetectsTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	_ = os.WriteFile(path, nil, 0644)

	collector := &lineCollector{}
	tailer := New(path, collector, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = tailer.Start(ctx) }()
	time.Sleep(200 * time.Millisecond)

	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString("before truncate\n")
	f.Close()
	time.Sleep(300 * time.Millisecond)

	os.Truncate(path, 0)
	time.Sleep(200 * time.Millisecond)

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

	go func() { _ = tailer.Start(ctx) }()
	time.Sleep(200 * time.Millisecond)

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
	_ = os.WriteFile(path, nil, 0644)

	collector := &lineCollector{}
	tailer := New(path, collector, nil)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- tailer.Start(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("tailer did not stop")
	}
}

func TestFileTailer_WithRotator(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	_ = os.WriteFile(path, nil, 0644)

	collector := &lineCollector{}
	rotator := NewFileRotator(100, 2)
	tailer := New(path, collector, rotator)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = tailer.Start(ctx) }()
	time.Sleep(200 * time.Millisecond)

	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	for i := 0; i < 20; i++ {
		f.WriteString("this is a log line that is fairly long to fill up space\n")
	}
	f.Close()

	time.Sleep(500 * time.Millisecond)

	if _, err := os.Stat(path + ".1"); os.IsNotExist(err) {
		t.Error("expected rotated file .1 to exist")
	}
}
