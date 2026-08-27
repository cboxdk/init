package watcher

import (
	"log/slog"
	"os"
	"testing"
)

// TestReloadHandlerPanicDoesNotKillTheProcess: reload runs on a timer goroutine,
// so a panic in the handler is not recoverable by anything upstream — it takes
// the process down, and this process is PID 1. A container running happily for
// weeks died the moment somebody saved a config with a typo. A bad config must
// fail the reload, never the container.
func TestReloadHandlerPanicDoesNotKillTheProcess(t *testing.T) {
	w := &Watcher{
		logger:  slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		handler: func() error { panic("config blew up") },
	}

	err := w.runHandler()
	if err == nil {
		t.Fatal("a panicking handler reported success")
	}

	// The watcher stays usable: a later good reload still works.
	called := false
	w.handler = func() error { called = true; return nil }
	if err := w.runHandler(); err != nil {
		t.Fatalf("watcher unusable after a panic: %v", err)
	}
	if !called {
		t.Error("the recovered watcher did not run the next handler")
	}
}
