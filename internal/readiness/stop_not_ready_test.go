package readiness

import (
	"log/slog"

	"github.com/cboxdk/init/internal/config"
	"os"
	"path/filepath"
	"testing"
)

// Stop removed the readiness file but left isReady true, and the /readyz HTTP
// server outlives Stop (it is torn down only after the process manager has shut
// down). So a file-based probe correctly went not-ready while an httpGet probe
// kept answering ready for the whole graceful-shutdown window — keeping the pod
// in the Service endpoints and routing traffic into a container whose processes
// were being SIGTERMed.
func TestNotReadyAfterStop(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(&config.ReadinessConfig{Enabled: true, Path: filepath.Join(dir, "ready"), Mode: "all_running"}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	m.mu.Lock()
	m.isReady = true
	m.mu.Unlock()

	if ready, _ := m.Snapshot(); !ready {
		t.Fatal("precondition: should be ready")
	}
	if err := m.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}

	if ready, _ := m.Snapshot(); ready {
		t.Error("/readyz still reports ready during shutdown — traffic keeps being routed to a dying container")
	}
	if m.IsReady() {
		t.Error("IsReady still true after Stop")
	}
	if _, err := os.Stat(filepath.Join(dir, "ready")); err == nil {
		t.Error("readiness file still present after Stop")
	}
}
