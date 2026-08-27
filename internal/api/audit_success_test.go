package api

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// syncBuffer is an io.Writer safe for the slog handler to use from any
// goroutine, so these tests stay clean under -race.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// createTestServerWithAuditSink builds a server with audit logging on and its
// log output captured, so a test can assert what did and did not reach the
// audit trail.
func createTestServerWithAuditSink(t *testing.T, auth string, sink *syncBuffer) *Server {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(sink, &slog.HandlerOptions{Level: slog.LevelInfo}))
	return NewServer(9180, "", auth, nil, nil, true, 0, createTestManager(t), logger)
}

// TestSuccessfulPrivilegedActionsAreAudited: the audit trail recorded only
// refusals — ACL denials, rate limits, auth failures. Everything that got
// through was invisible, so a caller holding a valid bearer token could stop
// every process in the container and the log showed nothing at all.
func TestSuccessfulPrivilegedActionsAreAudited(t *testing.T) {
	var buf syncBuffer
	server := createTestServerWithAuditSink(t, "test-token", &buf)

	handler := server.wrapHandler(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}, true)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/processes/php-fpm/stop", nil)
	req.RemoteAddr = "10.0.0.5:41000"
	req.Header.Set("Authorization", "Bearer test-token")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("request failed with %d", w.Code)
	}

	logged := buf.String()
	if !strings.Contains(logged, "/api/v1/processes/php-fpm/stop") {
		t.Errorf("a successful stop left no audit trail:\n%s", logged)
	}
	if !strings.Contains(logged, "10.0.0.5") {
		t.Errorf("audit entry does not record who did it:\n%s", logged)
	}

	// The bearer token must never reach the log — audit output is shipped
	// off-host and retained far longer than the token's own lifetime.
	if strings.Contains(logged, "test-token") {
		t.Errorf("the bearer token was written into the audit log:\n%s", logged)
	}
}

// TestReadOnlyRequestsAreNotAudited keeps the signal usable: a GET-heavy TUI
// polling loop must not drown the record of actual changes.
func TestReadOnlyRequestsAreNotAudited(t *testing.T) {
	var buf syncBuffer
	server := createTestServerWithAuditSink(t, "test-token", &buf)

	handler := server.wrapHandler(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}, true)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/processes", nil)
	req.RemoteAddr = "10.0.0.5:41000"
	req.Header.Set("Authorization", "Bearer test-token")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if strings.Contains(buf.String(), "privileged API action") {
		t.Errorf("a read-only request was recorded as a privileged action:\n%s", buf.String())
	}
}

// TestRejectedPrivilegedActionIsNotLoggedAsSuccess: a refused request must not
// appear in the trail as something that happened.
func TestRejectedPrivilegedActionIsNotLoggedAsSuccess(t *testing.T) {
	var buf syncBuffer
	server := createTestServerWithAuditSink(t, "test-token", &buf)

	handler := server.wrapHandler(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}, true)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/processes/php-fpm/stop", nil)
	req.RemoteAddr = "10.0.0.5:41000"
	req.Header.Set("Authorization", "Bearer wrong-token")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatal("a bad token was accepted")
	}
	if strings.Contains(buf.String(), "privileged API action performed") {
		t.Errorf("a rejected request was recorded as performed:\n%s", buf.String())
	}
}
