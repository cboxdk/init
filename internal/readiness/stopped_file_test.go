package readiness

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/cboxdk/init/internal/config"
)

// TestStoppedManagerDoesNotRecreateTheReadinessFile: IsReady and Snapshot
// already refuse to report ready once stopped, but setReady still wrote the
// FILE. A late evaluation racing shutdown therefore recreated the readiness file
// after Stop had removed it, and a file-based probe went on reporting the
// container ready while it was tearing itself down — so traffic kept arriving.
func TestStoppedManagerDoesNotRecreateTheReadinessFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ready")

	m := NewManager(&config.ReadinessConfig{
		Enabled: true,
		Path:    path,
		Mode:    "all_healthy",
	}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	m.mu.Lock()
	m.setReady(true)
	m.mu.Unlock()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("readiness file was not created while running: %v", err)
	}

	if err := m.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Stop did not remove the readiness file: %v", err)
	}

	// A late evaluation lands after Stop.
	m.mu.Lock()
	m.setReady(true)
	m.mu.Unlock()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the readiness file was recreated after Stop; a file-based probe " +
			"keeps routing traffic to a container that is shutting down")
	}
	if m.IsReady() {
		t.Error("a stopped manager reported ready")
	}
}
