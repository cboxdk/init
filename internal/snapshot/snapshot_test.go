package snapshot

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The property the whole mechanism rests on: the pipe belongs to us, not to
// os/exec, so we still hold the write end after the child is gone and have
// something to hand back on restore.
func TestStreamOutlivesItsChild(t *testing.T) {
	var mu sync.Mutex
	var got bytes.Buffer

	stream, err := NewStream(writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return got.Write(p)
	}))
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	if stream.Inode() == 0 {
		t.Fatal("stream has no inode; CRIU would have nothing to key --inherit-fd on")
	}

	cmd := exec.Command("sh", "-c", "echo hello")
	cmd.Stdout = stream.Writer()
	if err := cmd.Run(); err != nil {
		t.Fatalf("running child: %v", err)
	}

	// The child has exited. The pipe must still be open, because a checkpointed
	// child is also an exited child and the restore needs this exact pipe.
	if _, err := stream.Writer().Write([]byte("after\n")); err != nil {
		t.Fatalf("write end closed when the child exited: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		s := got.String()
		mu.Unlock()
		if strings.Contains(s, "hello") && strings.Contains(s, "after") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("output did not reach the log writer, got %q", got.String())
}

// The inode must stay stable: it is recorded at dump time and used at restore
// time, and a stream that renumbers itself in between breaks the restore.
func TestStreamInodeIsStable(t *testing.T) {
	stream, err := NewStream(&bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	first := stream.Inode()
	for i := 0; i < 10; i++ {
		if got := stream.Inode(); got != first {
			t.Fatalf("inode changed: %d then %d", first, got)
		}
	}
}

func TestActivity(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	advance := func(d time.Duration) { now = now.Add(d) }

	tests := []struct {
		name     string
		exercise func(a *Activity)
		idle     bool
		desc     string
	}{
		{
			name:     "quiet for long enough",
			exercise: func(a *Activity) { advance(time.Minute) },
			idle:     true,
			desc:     "nothing open, nothing in flight, past the timeout",
		},
		{
			name: "an open connection is activity even with no bytes moving",
			exercise: func(a *Activity) {
				a.Opened()
				advance(time.Minute)
			},
			idle: false,
			desc: "a long poll or a replica holds a connection open; that is not silence",
		},
		{
			name: "a request in flight is activity",
			exercise: func(a *Activity) {
				a.Started()
				advance(time.Minute)
			},
			idle: false,
			desc: "a slow upstream must not be checkpointed mid-request",
		},
		{
			name: "closing resets the clock rather than making it instantly idle",
			exercise: func(a *Activity) {
				closed := a.Opened()
				advance(time.Minute)
				closed()
			},
			idle: false,
			desc: "the idle period starts when the last connection went away",
		},
		{
			name: "idle again once the timeout passes after the last close",
			exercise: func(a *Activity) {
				closed := a.Opened()
				closed()
				advance(time.Minute)
			},
			idle: true,
			desc: "",
		},
		{
			name: "double close does not corrupt the count",
			exercise: func(a *Activity) {
				closed := a.Opened()
				closed()
				closed()
				a.Opened()
				advance(time.Minute)
			},
			idle: false,
			desc: "a decrement applied twice would drive the count negative and fake idleness",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now = time.Now()
			a := newActivity(clock)
			tt.exercise(a)

			if got := a.IdleFor(30 * time.Second); got != tt.idle {
				t.Errorf("IdleFor = %v, want %v. %s", got, tt.idle, tt.desc)
			}
		})
	}
}

// A misconfigured timeout must never mean "checkpoint immediately".
func TestActivityDisabledTimeoutNeverIdles(t *testing.T) {
	a := NewActivity()
	for _, d := range []time.Duration{0, -time.Second} {
		if a.IdleFor(d) {
			t.Errorf("IdleFor(%v) = true; a disabled timeout must not checkpoint a workload", d)
		}
	}
}

func TestProtocolRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		wire string
	}{
		{
			name: "register carries the child pid and the pipe inode",
			msg:  NewMessage(VerbRegister, "pid", "10", "inode", "2656132"),
			wire: "REGISTER inode=2656132 pid=10",
		},
		{
			name: "checkpoint takes no fields",
			msg:  NewMessage(VerbCheckpoint),
			wire: "CHECKPOINT",
		},
		{
			name: "policy comes back on the ok reply",
			msg:  NewMessage(StatusOK, "idle", "300", "port", "8080"),
			wire: "OK idle=300 port=8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.msg.String(); got != tt.wire {
				t.Fatalf("String() = %q, want %q", got, tt.wire)
			}

			parsed, err := ParseMessage(tt.wire + "\n")
			if err != nil {
				t.Fatalf("ParseMessage: %v", err)
			}
			if parsed.String() != tt.wire {
				t.Errorf("round trip changed the message: %q", parsed.String())
			}
		})
	}
}

func TestProtocolErrors(t *testing.T) {
	if _, err := ParseMessage("  \n"); err == nil {
		t.Error("empty message should be rejected")
	}
	if _, err := ParseMessage("REGISTER bare"); err == nil {
		t.Error("field without = should be rejected")
	}

	// Unknown fields are kept, so one side can add a field without the other
	// having to learn it first.
	m, err := ParseMessage("OK idle=30 somethingnew=1")
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if m.Fields["somethingnew"] != "1" {
		t.Error("unknown field was dropped")
	}

	// A garbled policy field falls back to the default rather than failing.
	if got := m.Int("port", 8080); got != 8080 {
		t.Errorf("Int fallback = %d, want 8080", got)
	}
	broken, _ := ParseMessage("OK port=notanumber")
	if got := broken.Int("port", 8080); got != 8080 {
		t.Errorf("unparseable field should fall back, got %d", got)
	}

	if err := (NewMessage(StatusError, "reason", "mptcp")).Err(); err == nil ||
		!strings.Contains(err.Error(), "mptcp") {
		t.Errorf("error reply should surface the reason, got %v", err)
	}
	if err := (NewMessage(StatusOK)).Err(); err != nil {
		t.Errorf("ok reply should not be an error, got %v", err)
	}
}

// The drain must collect the grandchild too, not just the process we started.
// An unreaped grandchild keeps its PID and the restore fails for that PID.
func TestDrainCheckpointedCollectsWholeTree(t *testing.T) {
	pending := []int{101, 102}
	var slept time.Duration

	reaped := DrainCheckpointed(DrainOptions{
		Wait: func(pid int, _ *syscall.WaitStatus, _ int, _ *syscall.Rusage) (int, error) {
			if len(pending) == 0 {
				return 0, nil
			}
			next := pending[0]
			pending = pending[1:]
			return next, nil
		},
		Sleep:   func(d time.Duration) { slept += d },
		Timeout: 200 * time.Millisecond,
		Settle:  20 * time.Millisecond,
	})

	if len(reaped) != 2 || reaped[0] != 101 || reaped[1] != 102 {
		t.Fatalf("reaped = %v, want the child and its grandchild", reaped)
	}
	if slept > 200*time.Millisecond {
		t.Errorf("drain slept %v; it should settle quickly once the tree is empty", slept)
	}
}

// ECHILD is a definitive answer: there is nothing left, so stop immediately
// rather than burning the whole timeout on every checkpoint.
func TestDrainCheckpointedStopsOnECHILD(t *testing.T) {
	calls := 0
	reaped := DrainCheckpointed(DrainOptions{
		Wait: func(int, *syscall.WaitStatus, int, *syscall.Rusage) (int, error) {
			calls++
			return -1, syscall.ECHILD
		},
		Sleep:   func(time.Duration) {},
		Timeout: time.Second,
	})

	if len(reaped) != 0 {
		t.Errorf("reaped = %v, want nothing", reaped)
	}
	if calls != 1 {
		t.Errorf("wait called %d times, want 1: ECHILD means stop", calls)
	}
}

func TestDialUnavailableIsNotAnError(t *testing.T) {
	if _, err := Dial(""); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Dial(\"\") = %v, want ErrUnavailable", err)
	}

	missing := t.TempDir() + "/nothing.sock"
	if _, err := Dial(missing); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Dial(missing) = %v, want ErrUnavailable: a cell without the warm tier is normal", err)
	}
}

func TestSocketPath(t *testing.T) {
	t.Setenv("CBOX_SNAPSHOT_SOCKET", "")
	if got := SocketPath(""); got != DefaultSocketPath {
		t.Errorf("SocketPath(\"\") = %q, want the default", got)
	}
	if got := SocketPath("/tmp/x.sock"); got != "/tmp/x.sock" {
		t.Errorf("configured path should win, got %q", got)
	}

	t.Setenv("CBOX_SNAPSHOT_SOCKET", "/from/env.sock")
	if got := SocketPath(""); got != "/from/env.sock" {
		t.Errorf("SocketPath should fall back to the environment the compiler set, got %q", got)
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

var _ = os.Stdout
