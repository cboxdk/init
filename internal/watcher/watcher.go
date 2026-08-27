package watcher

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ReloadHandler is called when a configuration file change is detected
type ReloadHandler func() error

// Watcher watches configuration files for changes and triggers reload
type Watcher struct {
	configPath    string
	debounceTimer *time.Timer
	handler       ReloadHandler
	logger        *slog.Logger
	watcher       *fsnotify.Watcher
	mu            sync.Mutex
	lastReload    time.Time
	debounce      time.Duration
	// stopped is checked inside reload under mu. Stopping the timer is not
	// enough on its own: time.AfterFunc has already launched its goroutine by
	// then, and that goroutine may be blocked on mu inside reload — Stop
	// releases the lock and the reload proceeds against a watcher the caller has
	// shut down.
	stopped bool
}

// Config holds watcher configuration
type Config struct {
	ConfigPath string
	Handler    ReloadHandler
	Logger     *slog.Logger
	Debounce   time.Duration // Debounce period to avoid multiple rapid reloads
}

// New creates a new configuration file watcher
func New(cfg Config) (*Watcher, error) {
	if cfg.ConfigPath == "" {
		return nil, fmt.Errorf("config path is required")
	}
	if cfg.Handler == nil {
		return nil, fmt.Errorf("reload handler is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Debounce == 0 {
		cfg.Debounce = 1 * time.Second // Default 1 second debounce
	}

	// Create fsnotify watcher
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	// Get absolute path
	absPath, err := filepath.Abs(cfg.ConfigPath)
	if err != nil {
		fsWatcher.Close()
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	w := &Watcher{
		configPath: absPath,
		handler:    cfg.Handler,
		logger:     cfg.Logger,
		watcher:    fsWatcher,
		debounce:   cfg.Debounce,
	}

	return w, nil
}

// Start begins watching the configuration file for changes
func (w *Watcher) Start(ctx context.Context) error {
	// Watch the config file's DIRECTORY, not the file itself.
	//
	// inotify watches an inode. Every common way of editing a config file
	// replaces it rather than writing in place — vim, `sed -i`, `helm upgrade`,
	// a Kubernetes ConfigMap update, `kubectl cp` — so a watch on the file is
	// destroyed by the first save and `--watch` silently becomes a no-op. (On
	// macOS/kqueue it happens to survive, which is why this never shows up in
	// local testing.) Watching the directory survives replacement; events for
	// other files in it are filtered out below.
	// Watching the directory means a missing config file would no longer be
	// reported, so check for it explicitly — starting a watch on a config that
	// is not there is a mistake worth surfacing immediately.
	if _, err := os.Stat(w.configPath); err != nil {
		return fmt.Errorf("failed to watch config file: %w", err)
	}
	if err := w.watcher.Add(filepath.Dir(w.configPath)); err != nil {
		return fmt.Errorf("failed to watch config directory: %w", err)
	}

	w.logger.Info("Config watcher started",
		"path", w.configPath,
		"debounce", w.debounce)

	go w.watchLoop(ctx)

	return nil
}

// watchLoop is the main event loop for file watching
func (w *Watcher) watchLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			w.logger.Debug("Config watcher stopped")
			return

		case event, ok := <-w.watcher.Events:
			if !ok {
				w.logger.Warn("Config watcher events channel closed")
				return
			}

			// The watch is on the directory, so filter to our own file.
			if filepath.Clean(event.Name) != w.configPath {
				continue
			}

			// Create and Rename both land here when the file is replaced
			// atomically (write temp + rename over), which is what an editor or
			// a ConfigMap update does.
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
				w.handleFileChange(event)
			}

		case err, ok := <-w.watcher.Errors:
			if !ok {
				w.logger.Warn("Config watcher errors channel closed")
				return
			}
			w.logger.Warn("Config watcher error", "error", err)
		}
	}
}

// handleFileChange processes a file change event with trailing-edge debouncing.
//
// Trailing edge matters: reloading on the FIRST event of a burst and ignoring
// the rest means the config that actually gets loaded is the intermediate state,
// and the final write of the burst — the one the operator meant — is never
// loaded at all. Editors and generators routinely write a file in several steps.
// Restarting the timer on each event and firing once it settles always loads the
// last version.
func (w *Watcher) handleFileChange(event fsnotify.Event) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stopped {
		return
	}

	w.logger.Debug("Config change observed; waiting for writes to settle",
		"path", event.Name,
		"event", event.Op.String(),
		"debounce", w.debounce)

	if w.debounceTimer != nil {
		w.debounceTimer.Stop()
	}
	w.debounceTimer = time.AfterFunc(w.debounce, w.reload)
}

// reload runs the handler once a burst of writes has settled.
func (w *Watcher) reload() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stopped {
		w.logger.Debug("Ignoring debounced reload after stop")

		return
	}

	w.logger.Info("Config file changed, triggering reload")

	if err := w.runHandler(); err != nil {
		w.logger.Error("Config reload failed", "error", err)
		// Don't update lastReload on failure to allow retry
		return
	}

	w.lastReload = time.Now()
	w.logger.Info("Config reload successful")
}

// runHandler calls the reload handler, converting a panic into an error.
//
// This runs on a timer goroutine, so a panic here is not recoverable by
// anything upstream: it takes the whole process down, and this process is PID 1.
// A container that had been running happily for weeks died the moment somebody
// saved a config with a typo in it — no graceful shutdown, no pre-stop hooks,
// in-flight work lost. A bad config must fail the reload, never the container.
func (w *Watcher) runHandler() (err error) {
	defer func() {
		if r := recover(); r != nil {
			w.logger.Error("Config reload handler panicked; keeping the previous configuration",
				"panic", r,
				"stack", string(debug.Stack()),
			)
			err = fmt.Errorf("reload handler panicked: %v", r)
		}
	}()

	return w.handler()
}

// Stop stops the file watcher
func (w *Watcher) Stop() error {
	w.logger.Debug("Stopping config watcher")

	// Cancel a pending debounced reload: firing it after Stop would reload the
	// config of a watcher the caller has already shut down.
	w.mu.Lock()
	w.stopped = true
	if w.debounceTimer != nil {
		w.debounceTimer.Stop()
		w.debounceTimer = nil
	}
	w.mu.Unlock()

	return w.watcher.Close()
}
