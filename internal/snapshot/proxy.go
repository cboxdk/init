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

// SetWaker installs the waker after construction.
//
// The proxy and the coordinator each need the other: the coordinator decides
// when to sleep by reading the proxy's activity counters, and the proxy decides
// when to wake by calling the coordinator. One of the two has to be wired
// second, and it is this one.
func (p *Proxy) SetWaker(w Waker) {
	p.mu.Lock()
	p.waker = w
	p.mu.Unlock()
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
	return p.serve(ctx, ln, p.handle)
}

// ServeObservations exposes the same upstream only while awake. It never wakes
// the workload and observations do not extend its idle timer. Loopback only:
// this is a privileged local monitoring path, not another customer ingress.
func (p *Proxy) ServeObservations(ctx context.Context, ln net.Listener) error {
	address, ok := ln.Addr().(*net.TCPAddr)
	if !ok || !address.IP.IsLoopback() {
		return errors.New("observation listener must use a loopback TCP address")
	}
	return p.serve(ctx, ln, p.handleObservation)
}

func (p *Proxy) serve(ctx context.Context, ln net.Listener, handle func(context.Context, net.Conn)) error {
	var wg sync.WaitGroup
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			wg.Wait()
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accepting on %s: %w", ln.Addr(), err)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			handle(ctx, conn)
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

	woken := false
	if p.Asleep() {
		wakeCtx, cancel := context.WithTimeout(ctx, p.wakeTimeout)
		err := p.wake(wakeCtx)
		cancel()

		if err != nil {
			// Fail loudly and close, so the client gets an error instead of
			// waiting on a connection that will never carry anything. Do not
			// pretend the workload is awake either: a wrong "awake" would send
			// every subsequent request into a dial loop.
			p.log.Error("snapshot restore failed; failing the connection", "error", err)
			return
		}
		p.SetAsleep(false)
		woken = true
	}

	upstream, err := p.dialUpstream(ctx)
	if err != nil {
		// Only say "after restore" when there was one: an awake workload that
		// refuses connections is an ordinary outage, not a warm-tier failure.
		msg := "workload unreachable; failing the connection"
		if woken {
			msg = "workload unreachable after waking it; failing the connection"
		}
		p.log.Error(msg, "upstream", p.upstream, "error", err)
		return
	}
	defer func() { _ = upstream.Close() }()

	done := p.activity.Started()
	defer done()

	p.splice(client, upstream)
}

// wake bounds the restore even when the waker itself does not honour the
// context.
//
// The proxy is holding a customer's accepted connection while this runs, so
// this timeout is the last thing standing between a stuck node agent and a
// request that never returns. It does not trust the waker to enforce its own
// deadline, because the failure being defended against is precisely an agent
// that is alive but not answering — which closes no socket and returns no
// error. The goroutine outlives the timeout and that is intended: it is blocked
// on a control channel that poisons itself, and letting it finish is cheaper
// than leaving the connection open until it does.
func (p *Proxy) wake(ctx context.Context) error {
	p.mu.Lock()
	waker := p.waker
	p.mu.Unlock()

	if waker == nil {
		return errors.New("no waker configured; the workload cannot be restored")
	}

	done := make(chan error, 1)
	go func() { done <- waker.Wake(ctx) }()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("restore did not complete within %s: %w", p.wakeTimeout, ctx.Err())
	}
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

// handleObservation holds an open-connection pin before checking the sleep flag.
// A transition already in progress refuses the probe; a probe already admitted
// makes the coordinator's count recheck abort the transition. No wake is called.
func (p *Proxy) handleObservation(ctx context.Context, client net.Conn) {
	done := p.activity.Observed()
	defer done()
	defer func() { _ = client.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stop()
	if ctx.Err() != nil || p.Asleep() {
		return
	}
	// Bound monitoring independently of the caller so an idle exporter cannot
	// hold a workload awake forever with a keep-alive socket.
	deadline := time.Now().Add(5 * time.Second)
	if err := client.SetDeadline(deadline); err != nil {
		return
	}
	probeCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	upstream, err := (&net.Dialer{}).DialContext(probeCtx, "tcp", p.upstream)
	if err != nil {
		return
	}
	defer func() { _ = upstream.Close() }()
	if err := upstream.SetDeadline(deadline); err != nil {
		return
	}
	p.splice(client, upstream)
}
