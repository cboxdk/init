package logger

import (
	"fmt"
	"sync"
	"time"
)

// LogEntry represents a single log entry with metadata
// The JSON names are the API's wire contract for a log entry and match the SSE
// stream exactly, so a consumer of GET /api/v1/logs and of /api/v1/logs/stream
// parses one shape. (Before 3.0 the REST endpoints serialized the Go field names
// — Timestamp/ProcessName/InstanceID — while the stream used these; the two
// needed separate parsers.)
type LogEntry struct {
	Timestamp   time.Time `json:"timestamp"`
	ProcessName string    `json:"process"`
	InstanceID  string    `json:"instance"`
	Stream      string    `json:"stream"` // stdout or stderr
	Message     string    `json:"message"`
	Level       string    `json:"level"` // debug, info, warn, error
}

// LogBuffer is a thread-safe ring buffer for storing recent log entries
type LogBuffer struct {
	mu          sync.RWMutex
	entries     []LogEntry
	size        int
	index       int
	full        bool
	broadcaster *LogBroadcaster // optional, for real-time subscribers
}

// NewLogBuffer creates a new log buffer with the specified capacity
func NewLogBuffer(size int) *LogBuffer {
	if size <= 0 {
		size = 1000 // Default to 1000 entries
	}
	return &LogBuffer{
		entries: make([]LogEntry, size),
		size:    size,
		index:   0,
		full:    false,
	}
}

// MaxStoredMessageBytes bounds the size of a single message RETAINED in the ring
// buffer. The buffer is capped by entry count, not bytes, and a single entry can
// be large: output with no newline is flushed at a 64KB threshold, and multiline
// buffering can hold a whole stack trace. At 1000 entries that reaches hundreds
// of megabytes per instance — PID 1 grows to it and the cgroup OOM-kills the
// container, which is precisely the failure this project exists to prevent, from
// nothing worse than an app dumping a base64 blob.
//
// Truncation applies only to the retained copy. The full line still goes to
// stdout and to live subscribers; it is just not accumulated.
const MaxStoredMessageBytes = 8 * 1024

// truncateAtRuneBoundary cuts s to at most n bytes without splitting a UTF-8
// rune. Slicing on a byte index alone left a partial rune at the end, which
// renders as a replacement character and — because the API serialises these
// entries as JSON — produces invalid UTF-8 that a strict client rejects.
func truncateAtRuneBoundary(s string, n int) string {
	if len(s) <= n {
		return s
	}

	// Walk back to the start of the rune that straddles the cut. A UTF-8
	// continuation byte is 10xxxxxx; the start of a sequence is anything else.
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}

	return s[:n]
}

// Add adds a log entry to the buffer
func (lb *LogBuffer) Add(entry LogEntry) {
	// Broadcast the entry in full: streaming is transient, so it costs no
	// retained memory.
	stored := entry
	if len(stored.Message) > MaxStoredMessageBytes {
		truncated := len(stored.Message) - MaxStoredMessageBytes
		stored.Message = truncateAtRuneBoundary(stored.Message, MaxStoredMessageBytes) +
			fmt.Sprintf("… [truncated %d bytes]", truncated)
	}

	lb.mu.Lock()
	lb.entries[lb.index] = stored
	lb.index++
	if lb.index >= lb.size {
		lb.index = 0
		lb.full = true
	}
	b := lb.broadcaster
	lb.mu.Unlock()

	if b != nil {
		b.Broadcast(entry)
	}
}

// GetAll returns all log entries in chronological order
func (lb *LogBuffer) GetAll() []LogEntry {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	if !lb.full {
		// Buffer not full yet, return entries from 0 to index
		result := make([]LogEntry, lb.index)
		copy(result, lb.entries[:lb.index])
		return result
	}

	// Buffer is full, return entries in correct chronological order
	result := make([]LogEntry, lb.size)

	// Copy from index to end (oldest entries)
	copy(result, lb.entries[lb.index:])

	// Copy from start to index (newest entries)
	copy(result[lb.size-lb.index:], lb.entries[:lb.index])

	return result
}

// GetRecent returns the last n log entries
func (lb *LogBuffer) GetRecent(n int) []LogEntry {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	count := lb.index
	if lb.full {
		count = lb.size
	}

	if n < 0 {
		n = 0 // a negative request is not a panic (make() would panic on it)
	}
	if n > count {
		n = count
	}

	if !lb.full {
		// Simple case: just copy the last n entries
		start := lb.index - n
		if start < 0 {
			start = 0
			n = lb.index
		}
		result := make([]LogEntry, n)
		copy(result, lb.entries[start:lb.index])
		return result
	}

	// Buffer is full, need to wrap around
	result := make([]LogEntry, n)
	if n <= lb.index {
		// All entries are before index, no wrap needed
		copy(result, lb.entries[lb.index-n:lb.index])
	} else {
		// Need to wrap: take from end and beginning
		fromEnd := n - lb.index
		copy(result, lb.entries[lb.size-fromEnd:])
		copy(result[fromEnd:], lb.entries[:lb.index])
	}

	return result
}

// Clear clears all entries from the buffer
func (lb *LogBuffer) Clear() {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.index = 0
	lb.full = false
}

// Size returns the current number of entries in the buffer
func (lb *LogBuffer) Size() int {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	if lb.full {
		return lb.size
	}
	return lb.index
}

// SetBroadcaster sets the broadcaster for real-time log subscriptions.
func (lb *LogBuffer) SetBroadcaster(b *LogBroadcaster) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.broadcaster = b
}
