package snapshot

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
	"time"
)

// ErrUnavailable means no agent is listening. It is not a failure of the
// workload: the container runs exactly as it always did, without the warm tier.
var ErrUnavailable = errors.New("snapshot agent unavailable")

// ErrControlLost means the control channel to the agent broke while an
// operation was in flight, or has broken since.
//
// It is a distinct error because it implies something the others do not: the
// agent is the only thing that can restore a checkpointed tree, so if the
// channel is gone while the workload is asleep, nothing will ever wake it. The
// caller's only recovery is a cold start.
var ErrControlLost = errors.New("snapshot control channel lost")

// DefaultRequestTimeout bounds one control operation when the caller supplies
// no deadline of its own.
//
// It has to exist. Without a deadline a client blocks in a read on the agent's
// socket forever, and an agent that is alive but stuck — as opposed to one that
// has died, which closes the socket and is noticed immediately — would hang
// cbox-init while it holds a customer's connection open. Generous rather than
// tight: a dump of a large resident set is legitimately slow, and a timeout
// that fires early costs a checkpoint, while one that fires late only costs the
// waiting request a little more of what it was already spending.
const DefaultRequestTimeout = 60 * time.Second

// Policy is what the agent tells cbox-init at registration. cbox-init does not
// read pod annotations — it cannot; it is inside the container and has no
// Kubernetes credentials — so the agent, which does read them, hands the policy
// down over the connection that already exists.
type Policy struct {
	// Port is the service port cbox-init should hold and proxy.
	Port int
	// IdleTimeout is how long the workload may be idle before checkpointing.
	IdleTimeout time.Duration
}

// Client is cbox-init's end of the control channel: one connection, held for
// the life of the container, serialised because the agent handles one operation
// per session at a time.
type Client struct {
	mu   sync.Mutex
	conn *net.UnixConn
	r    *bufio.Reader

	// Timeout bounds one request when the caller passes no deadline.
	Timeout time.Duration

	// broken records that the channel is no longer usable. Once a request has
	// timed out or failed mid-flight the stream is desynchronised — a reply
	// that arrives late would be read as the answer to the next request — so
	// the connection is closed and every later call fails immediately rather
	// than answering the wrong question.
	broken error
}

// Dial connects to the agent. A missing socket returns ErrUnavailable, which
// callers are expected to treat as "this cell does not offer the warm tier"
// rather than as an error to fail startup on.
func Dial(socketPath string) (*Client, error) {
	if socketPath == "" {
		return nil, ErrUnavailable
	}

	addr, err := net.ResolveUnixAddr("unix", socketPath)
	if err != nil {
		// nolint:errorlint // %v on the cause is deliberate: see the comment on
		// the dial below. Callers test for ErrUnavailable and nothing else.
		return nil, fmt.Errorf("%w: resolving %q: %v", ErrUnavailable, socketPath, err)
	}

	conn, err := net.DialUnix("unix", nil, addr)
	if err != nil {
		// Every dial failure means the same thing operationally — there is no
		// agent to talk to — and the kernel is not consistent about which errno
		// a missing socket produces. Wrap rather than classify, so the cause is
		// still visible in a log while callers only have to test for one thing.
		// nolint:errorlint // %v, not %w: wrapping the cause too would make it
		// matchable, and the whole point is that callers test for one thing.
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}

	return &Client{conn: conn, r: bufio.NewReader(conn), Timeout: DefaultRequestTimeout}, nil
}

// Register introduces the child and hands the agent the write end of its stdout
// pipe over SCM_RIGHTS.
//
// The descriptor is the whole reason registration is not just a line of text.
// On restore the agent passes it to CRIU as --inherit-fd, which is what puts
// the child's output back on the pipe cbox-init has been reading all along.
// Without it CRIU creates a new pipe, and the restored process dies on its
// first write with no reader on the other end.
func (c *Client) Register(ctx context.Context, childPID int, stream *Stream) (Policy, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.broken != nil {
		return Policy{}, c.broken
	}
	done := c.bound(ctx)
	defer done()

	msg := NewMessage(VerbRegister,
		"pid", fmt.Sprint(childPID),
		"inode", fmt.Sprint(stream.Inode()),
	)

	// net.UnixConn.File() dups the descriptor and, as a side effect, puts the
	// connection into blocking mode. That is why the raw send happens through
	// SyscallConn instead: the connection stays usable by the runtime poller
	// for every subsequent request.
	raw, err := c.conn.SyscallConn()
	if err != nil {
		return Policy{}, c.fail(fmt.Errorf("accessing agent socket: %w", err))
	}

	rights := syscall.UnixRights(int(stream.Writer().Fd()))
	var sendErr error
	if err := raw.Control(func(fd uintptr) {
		sendErr = syscall.Sendmsg(int(fd), []byte(msg.String()+"\n"), rights, nil, 0)
	}); err != nil {
		return Policy{}, c.fail(fmt.Errorf("accessing agent socket: %w", err))
	}
	if sendErr != nil {
		return Policy{}, c.fail(fmt.Errorf("sending stdout descriptor: %w", sendErr))
	}

	reply, err := c.readReply()
	if err != nil {
		return Policy{}, c.fail(err)
	}
	if err := reply.Err(); err != nil {
		return Policy{}, err
	}

	return Policy{
		Port:        reply.Int("port", 0),
		IdleTimeout: time.Duration(reply.Int("idle", 0)) * time.Second,
	}, nil
}

// Checkpoint asks the agent to dump the child tree. It returns only once the
// processes have actually stopped, so the caller knows precisely when to begin
// reaping — reaping early would race the dump, reaping late leaves zombies
// holding the PIDs the restore needs.
func (c *Client) Checkpoint(ctx context.Context) error {
	return c.request(ctx, NewMessage(VerbCheckpoint))
}

// Restore asks the agent to restore the child tree and returns when it is
// running. This reply is the ready signal.
func (c *Client) Restore(ctx context.Context) error {
	return c.request(ctx, NewMessage(VerbRestore))
}

func (c *Client) request(ctx context.Context, msg Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.broken != nil {
		return c.broken
	}
	// The caller's own deadline having expired is not a channel failure. Without
	// this check the write below fails immediately on the past deadline, fail()
	// closes the socket and latches broken forever — killing a healthy agent
	// connection for every future caller because one request queued too long.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s abandoned before send: %w", msg.Verb, err)
	}
	done := c.bound(ctx)
	defer done()

	if _, err := fmt.Fprintln(c.conn, msg.String()); err != nil {
		return c.fail(fmt.Errorf("sending %s: %w", msg.Verb, err))
	}

	reply, err := c.readReply()
	if err != nil {
		return c.fail(err)
	}
	// A refusal is the agent answering, not the channel failing. The connection
	// stays usable; the caller decides what the reason means.
	return reply.Err()
}

// bound applies a deadline to the connection for the duration of one request
// and watches ctx so a cancellation lands before the deadline does.
//
// The deadline is the load-bearing part. cbox-init holds a customer's
// connection open across a restore, so a control call that never returns is a
// request that never returns — and an agent that is stuck rather than dead does
// not close the socket for us to notice.
func (c *Client) bound(ctx context.Context) (done func()) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(timeout)
	}
	_ = c.conn.SetDeadline(deadline)

	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			// Unblock the read immediately rather than at the deadline.
			_ = c.conn.SetDeadline(time.Now())
		case <-stop:
		}
	}()

	return func() {
		close(stop)
		_ = c.conn.SetDeadline(time.Time{})
	}
}

// fail poisons the channel. Once a request has broken or timed out the stream
// is desynchronised — the agent may still be about to write the reply we gave
// up on, and reading it later would answer the wrong question — so the
// connection is closed and every subsequent call reports the same thing.
func (c *Client) fail(cause error) error {
	if c.broken == nil {
		// nolint:errorlint // Same reasoning as ErrUnavailable above.
		c.broken = fmt.Errorf("%w: %v", ErrControlLost, cause)
		_ = c.conn.Close()
	}
	return c.broken
}

// Lost reports whether the control channel has broken. A checkpointed workload
// whose channel is lost can never be restored: the agent is the only thing that
// holds its images and the descriptor its output goes back onto.
func (c *Client) Lost() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.broken != nil
}

func (c *Client) readReply() (Message, error) {
	line, err := c.r.ReadString('\n')
	if err != nil {
		return Message{}, fmt.Errorf("reading agent reply: %w", err)
	}
	return ParseMessage(line)
}

// Close releases the connection. The container keeps running; only the warm
// tier stops being available.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Close()
}

// SocketPath returns the agent socket cbox-init should use: whatever the
// compiled workload was given, falling back to the fixed path both sides agree
// on. A setting that has to match across a privilege boundary is a way to get
// it wrong, so there is a default rather than a requirement.
func SocketPath(configured string) string {
	if configured != "" {
		return configured
	}
	if env := os.Getenv("CBOX_SNAPSHOT_SOCKET"); env != "" {
		return env
	}
	return DefaultSocketPath
}

// DefaultSocketPath is where cortex-agent bind-mounts its control socket.
const DefaultSocketPath = "/run/cortex/snapshot.sock"
