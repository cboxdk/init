package logger

import (
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/config"
)

type syncSink struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (s *syncSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// TestMultilineTimeoutFiresWithoutFurtherOutput: the multiline timeout was only
// evaluated inside Write, so it was not a timeout at all. A worker that logged a
// fatal error and its stack trace and then went quiet kept that entry buffered
// indefinitely — invisible in docker logs, /api/v1/logs and the TUI until the
// process happened to write again, or exited. Which is to say: exactly when you
// most need to see it, you could not.
func TestMultilineTimeoutFiresWithoutFurtherOutput(t *testing.T) {
	sink := &syncSink{}
	pw, err := NewProcessWriter(
		slog.New(slog.NewTextHandler(sink, &slog.HandlerOptions{Level: slog.LevelInfo})),
		"worker", "worker-0", "stdout",
		&config.LoggingConfig{
			Stdout: true,
			Multiline: &config.MultilineConfig{
				Enabled:  true,
				Pattern:  `^\S`,
				Timeout:  1,
				MaxLines: 50,
			},
		},
	)
	if err != nil {
		t.Fatalf("NewProcessWriter: %v", err)
	}
	defer pw.Flush()

	// A fatal followed by an indented stack trace — then silence.
	if _, err := pw.Write([]byte("FATAL: connection pool exhausted\n    at Pool.acquire\n    at Worker.run\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got := sink.String(); strings.Contains(got, "connection pool exhausted") {
		t.Fatal("test setup: the entry was emitted immediately, so the timeout is not exercised")
	}

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(sink.String(), "connection pool exhausted") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Errorf("with timeout: 1 the fatal is still buffered after 4s of quiet; "+
		"it is invisible to docker logs, the API and the TUI:\n%s", sink.String())
}
