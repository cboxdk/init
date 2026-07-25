package snapshot

import (
	"sync"
	"time"
)

// Activity decides when a workload is idle enough to checkpoint.
//
// It counts what it actually knows — open connections and requests in flight —
// rather than inferring quiet from packet timestamps. cbox-init is the proxy,
// so this is exact information rather than a guess, and it is the one thing a
// transparent containerd shim structurally cannot have.
//
// Two rules taken from the prior art, both of which it paid for:
//
//   - Never use a structure whose absence means "no activity" but which can be
//     evicted. Their kernel LRU map silently dropped entries and pods
//     checkpointed while serving traffic. This is a counter and a timestamp;
//     nothing expires on its own.
//   - A connection that is open is activity, even if no bytes are moving. A
//     database replica or a long poll holds a connection open and must keep the
//     workload awake, not be mistaken for silence.
type Activity struct {
	mu       sync.Mutex
	open     int
	inFlight int
	last     time.Time
	now      func() time.Time
}

// NewActivity returns an Activity that starts counting from now.
func NewActivity() *Activity {
	return newActivity(time.Now)
}

func newActivity(now func() time.Time) *Activity {
	return &Activity{last: now(), now: now}
}

// Opened records a connection arriving. Returns a function to call when it
// closes; calling it more than once is safe.
func (a *Activity) Opened() (closed func()) {
	a.mu.Lock()
	a.open++
	a.last = a.now()
	a.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			a.open--
			a.last = a.now()
			a.mu.Unlock()
		})
	}
}

// Started records a request going upstream. Returns a function to call when it
// completes.
func (a *Activity) Started() (done func()) {
	a.mu.Lock()
	a.inFlight++
	a.last = a.now()
	a.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			a.inFlight--
			a.last = a.now()
			a.mu.Unlock()
		})
	}
}

// IdleFor reports whether nothing has been open or in flight for at least d.
// A zero or negative d means idling is disabled, never that everything is
// immediately idle — a misconfiguration must not checkpoint a busy workload.
func (a *Activity) IdleFor(d time.Duration) bool {
	if d <= 0 {
		return false
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.open > 0 || a.inFlight > 0 {
		return false
	}
	return a.now().Sub(a.last) >= d
}

// Counts reports open connections and in-flight requests, for status output.
func (a *Activity) Counts() (open, inFlight int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.open, a.inFlight
}
