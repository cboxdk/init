package snapshot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// echoUpstream stands in for the workload. It keeps connections open after
// replying, exactly as an HTTP server with keep-alive does.
func echoUpstream(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("upstream listen: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				buf := make([]byte, 64)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					if _, err := c.Write(buf[:n]); err != nil {
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func startProxy(t *testing.T, p *Proxy) (addr string, cancel context.CancelFunc) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = p.Serve(ctx, ln) }()
	t.Cleanup(cancel)
	return ln.Addr().String(), cancel
}

// The whole point of holding the port: a connection arriving while the workload
// is checkpointed is accepted immediately and served after the restore, with
// the client seeing one ordinary connection and no retransmit stall.
func TestProxyHoldsTheConnectionAcrossAWake(t *testing.T) {
	upstream := echoUpstream(t)

	var wakes int32
	p := NewProxy(ProxyOptions{
		Upstream: upstream.Addr().String(),
		Log:      quietLogger(),
		Waker: WakerFunc(func(context.Context) error {
			atomic.AddInt32(&wakes, 1)
			// A real restore costs tens of milliseconds; the client is already
			// connected and simply waits.
			time.Sleep(20 * time.Millisecond)
			return nil
		}),
	})
	p.SetAsleep(true)

	addr, _ := startProxy(t, p)

	start := time.Now()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	accepted := time.Since(start)
	defer func() { _ = conn.Close() }()

	// The handshake must not have waited for the restore. If a SYN had been
	// dropped instead, this would be a TCP retransmit — at least a second.
	if accepted > 500*time.Millisecond {
		t.Errorf("accept took %v; the connection must be established before the restore", accepted)
	}

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 4)
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read through proxy: %v", err)
	}
	if string(buf) != "ping" {
		t.Errorf("got %q, want the upstream's reply", buf)
	}

	if got := atomic.LoadInt32(&wakes); got != 1 {
		t.Errorf("woke %d times, want exactly 1", got)
	}
	if p.Asleep() {
		t.Error("proxy still reports asleep after a successful restore")
	}
}

// The regression that made the whole mechanism silently never engage: the
// client goes away, but the upstream keep-alive connection stays open, so the
// request is counted as in flight forever and the workload never looks idle.
func TestProxyReleasesInFlightWhenTheClientLeaves(t *testing.T) {
	upstream := echoUpstream(t)

	p := NewProxy(ProxyOptions{
		Upstream: upstream.Addr().String(),
		Log:      quietLogger(),
		Waker:    WakerFunc(func(context.Context) error { return nil }),
	})
	addr, _ := startProxy(t, p)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}

	// The client hangs up. The upstream has not, and will not.
	_ = conn.Close()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		open, inFlight := p.Activity().Counts()
		if open == 0 && inFlight == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}

	open, inFlight := p.Activity().Counts()
	t.Fatalf("after the client left: open=%d in_flight=%d, want 0/0 — "+
		"a proxy that waits only on the upstream keeps the workload awake forever", open, inFlight)
}

// A failed restore must not be reported as success. Claiming the workload is
// awake would send every later request into a doomed dial.
func TestProxyStaysAsleepWhenTheRestoreFails(t *testing.T) {
	p := NewProxy(ProxyOptions{
		Upstream: "127.0.0.1:1", // never reachable; must not be dialled at all
		Log:      quietLogger(),
		Waker: WakerFunc(func(context.Context) error {
			return errors.New("criu: dump images missing")
		}),
		DialTimeout: 50 * time.Millisecond,
	})
	p.SetAsleep(true)

	addr, _ := startProxy(t, p)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// The proxy closes the connection rather than pretending to serve it.
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Error("expected the connection to be closed after a failed restore")
	}

	if !p.Asleep() {
		t.Error("a failed restore must leave the workload marked asleep, not awake")
	}
}

// An awake workload must not pay for the wake machinery at all.
func TestProxyDoesNotWakeAnAwakeWorkload(t *testing.T) {
	upstream := echoUpstream(t)

	var wakes int32
	p := NewProxy(ProxyOptions{
		Upstream: upstream.Addr().String(),
		Log:      quietLogger(),
		Waker: WakerFunc(func(context.Context) error {
			atomic.AddInt32(&wakes, 1)
			return nil
		}),
	})

	addr, _ := startProxy(t, p)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := io.ReadFull(conn, make([]byte, 4)); err != nil {
		t.Fatalf("read: %v", err)
	}

	if got := atomic.LoadInt32(&wakes); got != 0 {
		t.Errorf("woke %d times for a running workload, want 0", got)
	}
}
