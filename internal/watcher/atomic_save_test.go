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

// Almost every way of editing a config replaces the file rather than writing in
// place — vim, sed -i, helm upgrade, a Kubernetes ConfigMap update. inotify
// watches an inode, so a watch on the file itself dies on the first such save
// and --watch silently stops working. Watching the directory survives it.
func TestWatcherSurvivesAtomicSaves(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cbox-init.yaml")
	os.WriteFile(p, []byte("version: \"1.0\"\n"), 0600)

	var reloads atomic.Int32
	w, err := New(Config{
		ConfigPath: p,
		Debounce:   10 * time.Millisecond,
		Logger:     slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		Handler:    func() error { reloads.Add(1); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	atomicSave := func(content string) {
		tmp := p + ".tmp"
		os.WriteFile(tmp, []byte(content), 0600)
		os.Rename(tmp, p) // atomic replace, as editors/ConfigMaps do
	}

	atomicSave("version: \"1.0\"\n# one\n")
	time.Sleep(300 * time.Millisecond)
	first := reloads.Load()
	atomicSave("version: \"1.0\"\n# two\n")
	time.Sleep(300 * time.Millisecond)
	second := reloads.Load()

	t.Logf("reloads after first atomic save: %d, after second: %d", first, second)
	if second <= first {
		t.Error("watching stopped after the first atomic save — --watch silently became a no-op")
	}
}
