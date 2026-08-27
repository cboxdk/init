package watcher

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestNew_MissingConfigPath(t *testing.T) {
	_, err := New(Config{
		Handler: func() error { return nil },
	})
	if err == nil {
		t.Error("Expected error for missing config path, got nil")
	}
}

func TestNew_MissingHandler(t *testing.T) {
	// Create a temporary file
	tmpfile, err := os.CreateTemp(t.TempDir(), "test-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	_, err = New(Config{
		ConfigPath: tmpfile.Name(),
	})
	if err == nil {
		t.Error("Expected error for missing handler, got nil")
	}
}

func TestNew_DefaultLogger(t *testing.T) {
	// Create a temporary file
	tmpfile, err := os.CreateTemp(t.TempDir(), "test-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	w, err := New(Config{
		ConfigPath: tmpfile.Name(),
		Handler:    func() error { return nil },
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer func() { _ = w.Stop() }()

	if w.logger == nil {
		t.Error("Logger should be set to default")
	}
}

func TestNew_DefaultDebounce(t *testing.T) {
	// Create a temporary file
	tmpfile, err := os.CreateTemp(t.TempDir(), "test-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	w, err := New(Config{
		ConfigPath: tmpfile.Name(),
		Handler:    func() error { return nil },
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer func() { _ = w.Stop() }()

	expected := 1 * time.Second
	if w.debounce != expected {
		t.Errorf("Expected default debounce %v, got %v", expected, w.debounce)
	}
}

func TestNew_CustomDebounce(t *testing.T) {
	// Create a temporary file
	tmpfile, err := os.CreateTemp(t.TempDir(), "test-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	customDebounce := 5 * time.Second
	w, err := New(Config{
		ConfigPath: tmpfile.Name(),
		Handler:    func() error { return nil },
		Debounce:   customDebounce,
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer func() { _ = w.Stop() }()

	if w.debounce != customDebounce {
		t.Errorf("Expected debounce %v, got %v", customDebounce, w.debounce)
	}
}

func TestNew_AbsolutePath(t *testing.T) {
	// Create a temporary file
	tmpfile, err := os.CreateTemp(t.TempDir(), "test-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	w, err := New(Config{
		ConfigPath: tmpfile.Name(),
		Handler:    func() error { return nil },
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer func() { _ = w.Stop() }()

	// Verify the path is absolute
	if !filepath.IsAbs(w.configPath) {
		t.Errorf("Expected absolute path, got: %s", w.configPath)
	}
}

func TestWatcher_Start(t *testing.T) {
	// Create a temporary file
	tmpfile, err := os.CreateTemp(t.TempDir(), "test-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	w, err := New(Config{
		ConfigPath: tmpfile.Name(),
		Handler:    func() error { return nil },
		Logger:     slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer func() { _ = w.Stop() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = w.Start(ctx)
	if err != nil {
		t.Errorf("Start returned error: %v", err)
	}
}

func TestWatcher_StartNonExistentFile(t *testing.T) {
	// Create a temp dir, then use a non-existent file path within it
	tmpdir, err := os.MkdirTemp("", "test-watcher-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpdir)

	nonExistentPath := filepath.Join(tmpdir, "does-not-exist.yaml")

	w, err := New(Config{
		ConfigPath: nonExistentPath,
		Handler:    func() error { return nil },
		Logger:     slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
	})
	if err != nil {
		t.Fatalf("Unexpected error creating watcher: %v", err)
	}
	defer func() { _ = w.Stop() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = w.Start(ctx)
	if err == nil {
		t.Error("Expected error when watching non-existent file, got nil")
	}
}

func TestWatcher_Stop(t *testing.T) {
	// Create a temporary file
	tmpfile, err := os.CreateTemp(t.TempDir(), "test-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	w, err := New(Config{
		ConfigPath: tmpfile.Name(),
		Handler:    func() error { return nil },
		Logger:     slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	err = w.Stop()
	if err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
}

func TestWatcher_FileChange(t *testing.T) {
	// Create a temporary file
	tmpfile, err := os.CreateTemp(t.TempDir(), "test-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	// Write initial content
	_, err = tmpfile.WriteString("version: 1.0\n")
	if err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpfile.Close()

	// Track handler calls
	var handlerCalls int32
	handler := func() error {
		atomic.AddInt32(&handlerCalls, 1)
		return nil
	}

	w, err := New(Config{
		ConfigPath: tmpfile.Name(),
		Handler:    handler,
		Debounce:   100 * time.Millisecond, // Short debounce for testing
		Logger:     slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer func() { _ = w.Stop() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = w.Start(ctx)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	// Give the watcher time to start
	time.Sleep(50 * time.Millisecond)

	// Modify the file
	err = os.WriteFile(tmpfile.Name(), []byte("version: 2.0\n"), 0644)
	if err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}

	// Wait for the handler to be called
	time.Sleep(300 * time.Millisecond)

	calls := atomic.LoadInt32(&handlerCalls)
	if calls == 0 {
		t.Error("Handler was not called after file change")
	}
}

func TestWatcher_Debounce(t *testing.T) {
	// Create a temporary file
	tmpfile, err := os.CreateTemp(t.TempDir(), "test-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	// Track handler calls
	var handlerCalls int32
	handler := func() error {
		atomic.AddInt32(&handlerCalls, 1)
		return nil
	}

	w, err := New(Config{
		ConfigPath: tmpfile.Name(),
		Handler:    handler,
		Debounce:   500 * time.Millisecond, // 500ms debounce
		Logger:     slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer func() { _ = w.Stop() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = w.Start(ctx)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	// Give the watcher time to start
	time.Sleep(50 * time.Millisecond)

	// Rapidly modify the file multiple times
	for i := 0; i < 5; i++ {
		err = os.WriteFile(tmpfile.Name(), []byte("version: "+string(rune('0'+i))+"\n"), 0644)
		if err != nil {
			t.Fatalf("Failed to write to temp file: %v", err)
		}
		time.Sleep(50 * time.Millisecond) // Within debounce period
	}

	// Wait for potential handler calls
	time.Sleep(700 * time.Millisecond)

	calls := atomic.LoadInt32(&handlerCalls)
	// Due to debounce, we should have at most 2 calls (first call + possible one after debounce)
	if calls > 2 {
		t.Errorf("Expected at most 2 handler calls due to debounce, got %d", calls)
	}
}

func TestWatcher_ContextCancellation(t *testing.T) {
	// Create a temporary file
	tmpfile, err := os.CreateTemp(t.TempDir(), "test-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	w, err := New(Config{
		ConfigPath: tmpfile.Name(),
		Handler:    func() error { return nil },
		Logger:     slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer func() { _ = w.Stop() }()

	ctx, cancel := context.WithCancel(context.Background())

	err = w.Start(ctx)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	// Cancel the context
	cancel()

	// Give the watcher time to stop
	time.Sleep(100 * time.Millisecond)

	// Test passes if no panic or deadlock occurs
}

func TestWatcher_HandlerError(t *testing.T) {
	// Create a temporary file
	tmpfile, err := os.CreateTemp(t.TempDir(), "test-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	// Track handler calls
	var handlerCalls int32
	handler := func() error {
		atomic.AddInt32(&handlerCalls, 1)
		return os.ErrInvalid // Return an error
	}

	w, err := New(Config{
		ConfigPath: tmpfile.Name(),
		Handler:    handler,
		Debounce:   50 * time.Millisecond,
		Logger:     slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer func() { _ = w.Stop() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = w.Start(ctx)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	// Give the watcher time to start
	time.Sleep(50 * time.Millisecond)

	// Modify the file
	err = os.WriteFile(tmpfile.Name(), []byte("version: 2.0\n"), 0644)
	if err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}

	// Wait for the handler to be called
	time.Sleep(200 * time.Millisecond)

	calls := atomic.LoadInt32(&handlerCalls)
	if calls == 0 {
		t.Error("Handler was not called after file change")
	}

	// When handler returns error, lastReload should NOT be updated,
	// allowing retry on next change. Modify file again:
	err = os.WriteFile(tmpfile.Name(), []byte("version: 3.0\n"), 0644)
	if err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}

	// Wait for another handler call
	time.Sleep(200 * time.Millisecond)

	newCalls := atomic.LoadInt32(&handlerCalls)
	if newCalls <= calls {
		t.Error("Handler should be called again after error (retry)")
	}
}
