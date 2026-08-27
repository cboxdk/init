package logger

import (
	"github.com/cboxdk/init/internal/config"
	"strings"
	"testing"
	"time"
)

// The ring buffer is capped by entry COUNT, and a single entry can be large:
// newline-free output flushes at a 64KB threshold and multiline buffering can
// hold an entire stack trace. At 1000 entries that reached hundreds of megabytes
// per instance — PID 1 grows to it and the cgroup OOM-kills the container, from
// nothing worse than an app dumping a base64 blob. Retained messages are now
// truncated (the full line still reaches stdout and live subscribers).
func TestLogBuffer_BoundsRetainedBytes(t *testing.T) {
	lb := NewLogBuffer(1000)
	huge := strings.Repeat("x", 96*1024) // the size a single entry can reach
	for i := 0; i < 1000; i++ {
		lb.Add(LogEntry{Timestamp: time.Now(), Message: huge})
	}
	total := 0
	for _, e := range lb.GetAll() {
		total += len(e.Message)
	}
	t.Logf("retained %d bytes across %d entries", total, len(lb.GetAll()))
	if total > 16*1024*1024 {
		t.Errorf("retained %d bytes; unbounded entry size can OOM PID 1", total)
	}
}

// Pattern-based level detection must not override a level the application
// stated itself: an explicit {"level":"info"} was indistinguishable from the
// "assume info" fallback, so a line containing a word like "error" anywhere was
// re-labelled against the app's own classification.
func TestJSONParser_ReportsWhetherLevelWasStated(t *testing.T) {
	p := NewJSONParser(&config.JSONConfig{Enabled: true, ExtractLevel: true, ExtractMessage: true})

	isJSON, data := p.Parse(`{"level":"info","message":"connection error handled"}`)
	if !isJSON {
		t.Fatal("expected JSON to parse")
	}
	_, _, _, levelFound := p.ToLogAttrs(data)
	if !levelFound {
		t.Error("an explicit level must be reported as stated by the source")
	}

	isJSON, data = p.Parse(`{"message":"no level here"}`)
	if !isJSON {
		t.Fatal("expected JSON to parse")
	}
	if _, _, _, levelFound = p.ToLogAttrs(data); levelFound {
		t.Error("a line with no level must not be reported as stating one")
	}
}

// A negative request must not panic (make() would).
func TestLogBuffer_GetRecentNegative(t *testing.T) {
	lb := NewLogBuffer(4)
	lb.Add(LogEntry{Message: "x"})
	if got := lb.GetRecent(-1); len(got) != 0 {
		t.Errorf("GetRecent(-1) = %d entries, want 0", len(got))
	}
}
