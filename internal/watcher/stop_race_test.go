package watcher

import (
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// TestStoppedWatcherDoesNotFireAPendingReload: Stop stopped the debounce timer,
// but time.AfterFunc has already launched its goroutine by then, and that
// goroutine may be blocked on the mutex inside reload — Stop releases the lock
// and the reload proceeds against a watcher the caller has shut down.
func TestStoppedWatcherDoesNotFireAPendingReload(t *testing.T) {
	var reloads atomic.Int32

	w := &Watcher{
		logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		handler:  func() error { reloads.Add(1); return nil },
		debounce: 10 * time.Millisecond,
	}

	// Arm a debounced reload, then stop before it fires.
	w.mu.Lock()
	w.stopped = true
	w.mu.Unlock()

	// Simulate the timer goroutine that was already in flight when Stop ran.
	w.reload()

	if got := reloads.Load(); got != 0 {
		t.Errorf("a stopped watcher ran %d reload(s); the config of a shut-down "+
			"watcher was reloaded", got)
	}
}
