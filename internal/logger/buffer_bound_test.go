package logger

import (
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
