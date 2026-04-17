package logger

import (
	"sync"
	"time"
)

// LogEntry represents a single log entry with metadata
type LogEntry struct {
	Timestamp   time.Time
	ProcessName string
	InstanceID  string
	Stream      string // stdout or stderr
	Message     string
	Level       string // debug, info, warn, error
}

// LogBuffer is a thread-safe ring buffer for storing recent log entries
type LogBuffer struct {
	mu      sync.RWMutex
	entries []LogEntry
	size    int
	index   int
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

// Add adds a log entry to the buffer
func (lb *LogBuffer) Add(entry LogEntry) {
	lb.mu.Lock()
	lb.entries[lb.index] = entry
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
