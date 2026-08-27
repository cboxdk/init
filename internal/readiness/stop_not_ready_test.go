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

// A readiness file removed from underneath us — a tmpfs cleaner, a sidecar, an
// operator — was never recreated, because the file was only written on a
// ready-state TRANSITION. The container then looked not-ready to a file-based
// probe for the rest of its life, with nothing in the logs to explain it.
func TestReadinessFileIsRecreatedIfRemoved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ready")
	lg := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(&config.ReadinessConfig{Enabled: true, Path: path, Mode: "all_running"}, lg)

	m.UpdateProcessState("web", StateRunning, "healthy")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("precondition: readiness file should exist: %v", err)
	}

	// Something deletes it.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	// The next evaluation must put it back.
	m.UpdateProcessState("web", StateRunning, "healthy")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("readiness file was not recreated after external removal: %v", err)
	}
}
