package watcher

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWatchFollowsSymlinkedConfig: filepath.Abs does not resolve symlinks, so a
// config reached through a link — /etc/cbox-init/cbox-init.yaml pointing at
// /data/config/app.yaml, or a Kubernetes ConfigMap's ..data indirection — put
// the directory watch on the LINK's directory, where nothing ever changes.
// --watch became a silent no-op on Linux. (On macOS the file watch happened to
// survive, which is why it never showed up locally.)
func TestWatchFollowsSymlinkedConfig(t *testing.T) {
	root := t.TempDir()

	// The real file lives in one directory...
	dataDir := filepath.Join(root, "data")
	if err := os.Mkdir(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dataDir, "app.yaml")
	if err := os.WriteFile(target, []byte("version: \"1.0\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// ...and is reached through a link in another.
	linkDir := filepath.Join(root, "etc")
	if err := os.Mkdir(linkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(linkDir, "cbox-init.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	reloaded := make(chan struct{}, 4)
	w, err := New(Config{
		ConfigPath: link,
		Debounce:   30 * time.Millisecond,
		Logger:     slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		Handler: func() error {
			select {
			case reloaded <- struct{}{}:
			default:
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = w.Stop() }()

	// Give the watch a moment to be established.
	time.Sleep(100 * time.Millisecond)

	// Write through the TARGET, as an editor or a config generator would.
	if err := os.WriteFile(target, []byte("version: \"1.0\"\n# edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case <-reloaded:
	case <-time.After(3 * time.Second):
		t.Fatal("editing the symlink's target triggered no reload; " +
			"--watch is a no-op for a linked config")
	}

	// The behavioural check above only fails on Linux: macOS's kqueue backend
	// follows the symlink when it registers the directory's entries, so the bug
	// is invisible there. Assert the structure directly as well, so this test
	// means something on every platform.
	watched := w.watcher.WatchList()
	if !containsPath(watched, dataDir) {
		t.Errorf("the resolved target's directory %s is not watched (watching %v); "+
			"on Linux inotify this makes --watch a no-op", dataDir, watched)
	}
	if !containsPath(watched, linkDir) {
		t.Errorf("the link's own directory %s is not watched (watching %v); "+
			"a Kubernetes ConfigMap update swaps the ..data link there", linkDir, watched)
	}
}

func containsPath(list []string, want string) bool {
	want, err := filepath.EvalSymlinks(want)
	if err != nil {
		return false
	}
	for _, got := range list {
		if resolved, err := filepath.EvalSymlinks(got); err == nil && resolved == want {
			return true
		}
	}
	return false
}
