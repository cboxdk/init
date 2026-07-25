package snapshot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"
)

// Waker restores the workload. It is the agent in production and a fake in
// tests; Wake must be safe to call concurrently and must return only once the
// workload is actually running.
type Waker interface {
	Wake(ctx context.Context) error
}

// WakerFunc adapts a function to Waker.
type WakerFunc func(ctx context.Context) error

// Wake implements Waker.
func (f WakerFunc) Wake(ctx context.Context) error { return f(ctx) }

// Proxy holds the service port on behalf of a workload that may be checkpointed.
//
// Holding the port is what makes the warm tier possible without deceiving
// anyone. cbox-init is listening the whole time, so:
//
//   - The handshake completes immediately, even while the workload is asleep.
//     This is the first non-negotiable taken from the prior art: their initial
//     design let the SYN drop and paid a TCP retransmit — at least a second —
//     on every wake, dwarfing the restore itself. Accepting first and restoring
//     second turns that into nothing.
//   - Idleness is a fact rather than an inference, because every connection
//     passes through here.
//   - Probes are answered elsewhere, on cbox-init's own readiness port, so
//     nothing has to sniff traffic to tell a health check apart from a request.
type Proxy struct {
	upstream string
	waker    Waker
	activity *Activity
	log      *slog.Logger

	// dialTimeout bounds a single attempt to reach the workload.
	dialTimeout time.Duration
	// wakeTimeout bounds the whole restore.
	wakeTimeout time.Duration

	mu     sync.Mutex
	asleep bool

	ln net.Listener
	wg sync.WaitGroup
}

// ProxyOptions configures a Proxy. Only Upstream and Waker are required.
type ProxyOptions struct {
	// Upstream is the address the workload listens on inside the container.
	Upstream string
	Waker    Waker
	Activity *Activity
	Log      *slog.Logger

	DialTimeout time.Duration
	WakeTimeout time.Duration
}

// NewProxy builds a Proxy. It does not listen until Serve is called.
func NewProxy(opts ProxyOptions) *Proxy {
	p := &Proxy{
		upstream:    opts.Upstream,
		waker:       opts.Waker,
		activity:    opts.Activity,
		log:         opts.Log,
		dialTimeout: opts.DialTimeout,
		wakeTimeout: opts.WakeTimeout,
	}
	if p.activity == nil {
		p.activity = NewActivity()
	}
	if p.log == nil {
		p.log = slog.Default()
	}
	if p.dialTimeout <= 0 {
		p.dialTimeout = 2 * time.Second
	}
	if p.wakeTimeout <= 0 {
		p.wakeTimeout = 30 * time.Second
	}
	return p
}

// Activity exposes the counters the idle loop reads.
func (p *Proxy) Activity() *Activity { return p.activity }

// SetAsleep records whether the workload is currently checkpointed. The next
// connection to arrive will wake it.
func (p *Proxy) SetAsleep(asleep bool) {
	p.mu.Lock()
	p.asleep = asleep
	p.mu.Unlock()
}

// Asleep reports whether the workload is currently checkpointed.
func (p *Proxy) Asleep() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.asleep
}

// Serve accepts on ln until the context is cancelled or ln is closed.
func (p *Proxy) Serve(ctx context.Context, ln net.Listener) error {
	p.ln = ln

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			p.wg.Wait()
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accepting on %s: %w", ln.Addr(), err)
		}

		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.handle(ctx, conn)
		}()
	}
}

// Addr reports the address the proxy is listening on, once Serve has started.
func (p *Proxy) Addr() net.Addr {
	if p.ln == nil {
		return nil
	}
	return p.ln.Addr()
}

func (p *Proxy) handle(ctx context.Context, client net.Conn) {
	// The connection is already accepted — the client's handshake completed
	// before any of this ran. Everything below happens while the client waits
	// on a connection that exists, never on a retransmit.
	closed := p.activity.Opened()
	defer func() {
		closed()
		_ = client.Close()
	}()

	if p.Asleep() {
		wakeCtx, cancel := context.WithTimeout(ctx, p.wakeTimeout)
		err := p.waker.Wake(wakeCtx)
		cancel()

		if err != nil {
			// Fail loudly and leave the client to see a closed connection, but
			// do not pretend the workload is awake: a wrong "awake" would send
			// every subsequent request into a dial loop.
			p.log.Error("snapshot restore failed", "error", err)
			return
		}
		p.SetAsleep(false)
	}

	upstream, err := p.dialUpstream(ctx)
	if err != nil {
		p.log.Error("workload unreachable after restore", "upstream", p.upstream, "error", err)
		return
	}
	defer func() { _ = upstream.Close() }()

	done := p.activity.Started()
	defer done()

	p.splice(client, upstream)
}

// splice copies in both directions and tears both down as soon as either ends.
//
// The teardown is the part that matters. A proxy that only waits for the
// upstream to finish keeps a keep-alive connection open long after the client
// has gone, the request stays counted as in flight, and the workload never
// looks idle — so it never checkpoints at all. That failure is silent and
// fails in the safe direction, which is exactly why it survives review: the
// only symptom is a warm tier that quietly never engages.
func (p *Proxy) splice(client, upstream net.Conn) {
	done := make(chan struct{}, 2)

	go func() {
		_, _ = io.Copy(upstream, client)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, upstream)
		done <- struct{}{}
	}()

	<-done
}

// dialUpstream reaches the workload, retrying briefly. A freshly restored
// process is running before its listening socket has necessarily been observed
// as reachable, and a few hundred microseconds of retry is cheaper than any
// fixed delay.
func (p *Proxy) dialUpstream(ctx context.Context) (net.Conn, error) {
	deadline := time.Now().Add(p.dialTimeout)
	var lastErr error

	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		conn, err := net.DialTimeout("tcp", p.upstream, time.Until(deadline))
		if err == nil {
			return conn, nil
		}
		lastErr = err

		// Retry rather than classify. The only error worth distinguishing here
		// would be "connection refused", which is precisely the expected state
		// while a restored listener comes back — so every case either resolves
		// itself within the timeout or is reported when it expires.
		time.Sleep(500 * time.Microsecond)
	}

	if lastErr == nil {
		lastErr = errors.New("timed out")
	}
	return nil, lastErr
}
