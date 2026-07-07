package readiness

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cboxdk/init/internal/config"
)

func testManager() *Manager {
	cfg := &config.ReadinessConfig{Enabled: true, Mode: "all_healthy"}
	return NewManager(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func serve(hs *HTTPServer, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	hs.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestReadyzReflectsAggregate(t *testing.T) {
	m := testManager()
	m.SetTrackedProcesses([]string{"web"})
	hs := NewHTTPServer(m, "", 0, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Not ready before the process reports healthy.
	if rec := serve(hs, "/readyz"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 while not ready, got %d", rec.Code)
	}

	// Ready once all tracked processes are healthy.
	m.UpdateProcessState("web", StateHealthy, "healthy")
	rec := serve(hs, "/readyz")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when ready, got %d", rec.Code)
	}
	var body struct {
		Ready     bool            `json:"ready"`
		Processes []ProcessStatus `json:"processes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if !body.Ready || len(body.Processes) != 1 || body.Processes[0].Name != "web" {
		t.Fatalf("unexpected body: %+v", body)
	}

	// Unready again when the process degrades — endpoint reflects it live.
	m.UpdateProcessState("web", StateUnhealthy, "unhealthy")
	if rec := serve(hs, "/readyz"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 after process went unhealthy, got %d", rec.Code)
	}
}

func TestLivezAlwaysOKWhenResponsive(t *testing.T) {
	hs := NewHTTPServer(testManager(), "", 0, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if rec := serve(hs, "/livez"); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from /livez, got %d", rec.Code)
	}
}
