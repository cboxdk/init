package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/snapshot"
)

type snapshotStub struct {
	state   string
	calls   int
	err     error
	onSleep func(context.Context) error
	status  *snapshot.Status
}

func (s *snapshotStub) Sleep(ctx context.Context) error {
	s.calls++
	if s.onSleep != nil {
		return s.onSleep(ctx)
	}
	s.state = "asleep"
	return s.err
}
func (s *snapshotStub) Wake(context.Context) error { s.calls++; s.state = "awake"; return s.err }
func (s *snapshotStub) Status() snapshot.Status {
	if s.status != nil {
		return *s.status
	}
	return snapshot.Status{Supported: true, State: s.state, IdleSeconds: 300}
}

// The status endpoint is where Stache reads why a workload cold started or
// stopped sleeping; it must never have to parse a log line for that.
func TestSnapshotStatusExposesCauseAndMessage(t *testing.T) {
	control := &snapshotStub{status: &snapshot.Status{
		Supported: true, State: "awake", IdleSeconds: 300,
		LastRestore: snapshot.LastRestoreColdStart,
		Cause:       snapshot.CauseCheckpointMissing, Level: snapshot.LevelInfo,
		Message: "no checkpoint images on this node to restore",
	}}
	s := createTestServer(t, "token", nil).SetSnapshotControl(control)
	r := httptest.NewRequest("GET", "/api/v1/snapshot", nil)
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	s.buildMux(true).ServeHTTP(w, r)

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"state": "awake", "last_restore": "cold_start", "cause": "checkpoint_missing",
		"level": "info", "message": "no checkpoint images on this node to restore",
	} {
		if got[key] != want {
			t.Errorf("%s = %v, want %q (%s)", key, got[key], want, w.Body.String())
		}
	}
	if _, ok := got["disabled_reason"]; ok {
		t.Error("a missing checkpoint must not report the workload disabled")
	}
}

// A failed action answers with a cause and the coordinator's explanation, never
// a bare "failed", and still keeps raw internal errors out of the body.
func TestSnapshotActionErrorsCarryCause(t *testing.T) {
	for _, tc := range []struct {
		name      string
		err       error
		status    snapshot.Status
		code      int
		cause     string
		mentioned string
	}{
		{"busy", fmt.Errorf("%w: 2 open connections, 0 requests in flight", snapshot.ErrWorkloadBusy), snapshot.Status{State: "awake"}, 409, "workload_busy", "2 open connections"},
		{"disabled", fmt.Errorf("%w: warm sleep is off for this container: dumps keep failing", snapshot.ErrSleepUnavailable),
			snapshot.Status{State: "disabled", DisabledCause: snapshot.CauseCheckpointFailed}, 409, "checkpoint_failed", "dumps keep failing"},
		{"restore failed", errors.New("private agent details"),
			snapshot.Status{State: "asleep", Cause: snapshot.CauseRestoreFailed, Level: snapshot.LevelError, Message: "restoring the checkpoint failed (CRIU exited 1)"}, 503, "restore_failed", "CRIU exited 1"},
		{"timeout", context.DeadlineExceeded, snapshot.Status{State: "transitioning"}, 504, "operation_timeout", "timed out"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := tc.status
			status.Supported = true
			control := &snapshotStub{err: tc.err, status: &status}
			s := createTestServer(t, "token", nil).SetSnapshotControl(control)
			r := httptest.NewRequest("POST", "/api/v1/snapshot/wake", nil)
			r.Header.Set("Authorization", "Bearer token")
			w := httptest.NewRecorder()
			s.buildMux(true).ServeHTTP(w, r)

			var body struct {
				Error    string          `json:"error"`
				Cause    string          `json:"cause"`
				Snapshot snapshot.Status `json:"snapshot"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.code || body.Cause != tc.cause || !strings.Contains(body.Error, tc.mentioned) || body.Snapshot.State != tc.status.State {
				t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "private agent details") {
				t.Fatal("leaked internal error")
			}
		})
	}
}

func TestSnapshotRoutesRespectAuthenticationMethodsAndRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body, auth string
		err                            error
		code, calls                    int
	}{
		{"unauthorized read", "GET", "/api/v1/snapshot", "", "", nil, 401, 0},
		{"unauthorized mutation", "POST", "/api/v1/snapshot/sleep", "", "", nil, 401, 0},
		{"status", "GET", "/api/v1/snapshot", "", "token", nil, 200, 0},
		{"sleep", "POST", "/api/v1/snapshot/sleep", "", "token", nil, 200, 1},
		{"wake", "POST", "/api/v1/snapshot/wake", "", "token", nil, 200, 1},
		{"wrong mutation method", "GET", "/api/v1/snapshot/sleep", "", "token", nil, 405, 0},
		{"wrong status method", "POST", "/api/v1/snapshot", "", "token", nil, 405, 0},
		{"force query", "POST", "/api/v1/snapshot/sleep?force=true", "", "token", nil, 400, 0},
		{"force body", "POST", "/api/v1/snapshot/sleep", `{"force":true}`, "token", nil, 400, 0},
		{"busy", "POST", "/api/v1/snapshot/sleep", "", "token", snapshot.ErrWorkloadBusy, 409, 1},
		{"disabled", "POST", "/api/v1/snapshot/sleep", "", "token", snapshot.ErrSleepUnavailable, 409, 1},
		{"timeout", "POST", "/api/v1/snapshot/wake", "", "token", context.DeadlineExceeded, 504, 1},
		{"canceled", "POST", "/api/v1/snapshot/wake", "", "token", context.Canceled, 504, 1},
		{"uncertain", "POST", "/api/v1/snapshot/sleep", "", "token", errors.New("private agent details"), 503, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			control := &snapshotStub{state: "awake", err: tc.err}
			s := createTestServer(t, "token", nil).SetSnapshotControl(control)
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.auth != "" {
				r.Header.Set("Authorization", "Bearer "+tc.auth)
			}
			w := httptest.NewRecorder()
			s.buildMux(true).ServeHTTP(w, r)
			if w.Code != tc.code || control.calls != tc.calls {
				t.Fatalf("HTTP %d, calls %d: %s", w.Code, control.calls, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "private agent details") {
				t.Fatal("leaked internal error")
			}
			if tc.code == 200 && w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("state response can be cached")
			}
			if tc.code == 200 {
				var got snapshot.Status
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.State != control.state {
					t.Fatalf("stale status: %s", w.Body.String())
				}
			}
		})
	}
}

func TestSnapshotUnavailableDoesNotPretendToSleep(t *testing.T) {
	s := createTestServer(t, "", nil)
	for _, action := range []string{"sleep", "wake"} {
		w := httptest.NewRecorder()
		s.buildMux(false).ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/snapshot/"+action, nil))
		if w.Code != 409 {
			t.Fatalf("HTTP %d", w.Code)
		}
	}
	w := httptest.NewRecorder()
	s.buildMux(false).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/snapshot", nil))
	var got snapshot.Status
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.Supported || got.State != "unavailable" {
		t.Fatalf("status = %s", w.Body.String())
	}
}

func TestSnapshotExtendsWriteDeadlineThroughAuditMiddleware(t *testing.T) {
	control := &snapshotStub{onSleep: func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > time.Minute || time.Until(deadline) < 50*time.Second {
			t.Error("unbounded or truncated snapshot context")
		}
		time.Sleep(80 * time.Millisecond)
		return nil
	}}
	s := createTestServer(t, "token", nil).SetSnapshotControl(control)
	srv := httptest.NewUnstartedServer(s.buildMux(true))
	srv.Config.WriteTimeout = 20 * time.Millisecond
	srv.Start()
	defer srv.Close()
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/snapshot/sleep", nil)
	req.Header.Set("Authorization", "Bearer token")
	response, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("HTTP %d", response.StatusCode)
	}
}
