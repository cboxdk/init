package logger

import (
	"sync"
)

// LogBroadcaster delivers log entries to multiple subscribers in real-time.
// Subscribers receive entries on a buffered channel. If a subscriber can't
// keep up, entries are dropped (non-blocking send).
type LogBroadcaster struct {
	mu          sync.RWMutex
	subscribers map[uint64]*subscription
	nextID      uint64
}

type subscription struct {
	ch     chan LogEntry
	filter string // process name filter, empty = all
}

// NewLogBroadcaster creates a new broadcaster.
func NewLogBroadcaster() *LogBroadcaster {
	return &LogBroadcaster{
		subscribers: make(map[uint64]*subscription),
	}
}

// Subscribe registers a new subscriber.
// filter is a process name — empty string receives all processes.
// Returns a read channel and an unsubscribe function.
// The channel is closed when unsubscribe is called.
func (b *LogBroadcaster) Subscribe(filter string) (<-chan LogEntry, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.nextID
	b.nextID++

	ch := make(chan LogEntry, 256)
	b.subscribers[id] = &subscription{ch: ch, filter: filter}

	unsub := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if sub, ok := b.subscribers[id]; ok {
			close(sub.ch)
			delete(b.subscribers, id)
		}
	}

	return ch, unsub
}

// Broadcast sends a log entry to all matching subscribers.
// Non-blocking: if a subscriber's buffer is full, the entry is dropped.
func (b *LogBroadcaster) Broadcast(entry LogEntry) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, sub := range b.subscribers {
		if sub.filter != "" && sub.filter != entry.ProcessName {
			continue
		}
		select {
		case sub.ch <- entry:
		default:
		}
	}
}
