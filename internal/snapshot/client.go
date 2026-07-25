package snapshot

import (
	"bufio"
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
		return nil, fmt.Errorf("%w: resolving %q: %v", ErrUnavailable, socketPath, err)
	}

	conn, err := net.DialUnix("unix", nil, addr)
	if err != nil {
		// Every dial failure means the same thing operationally — there is no
		// agent to talk to — and the kernel is not consistent about which errno
		// a missing socket produces. Wrap rather than classify, so the cause is
		// still visible in a log while callers only have to test for one thing.
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}

	return &Client{conn: conn, r: bufio.NewReader(conn)}, nil
}

// Register introduces the child and hands the agent the write end of its stdout
// pipe over SCM_RIGHTS.
//
// The descriptor is the whole reason registration is not just a line of text.
// On restore the agent passes it to CRIU as --inherit-fd, which is what puts
// the child's output back on the pipe cbox-init has been reading all along.
// Without it CRIU creates a new pipe, and the restored process dies on its
// first write with no reader on the other end.
func (c *Client) Register(childPID int, stream *Stream) (Policy, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

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
		return Policy{}, fmt.Errorf("accessing agent socket: %w", err)
	}

	rights := syscall.UnixRights(int(stream.Writer().Fd()))
	var sendErr error
	if err := raw.Control(func(fd uintptr) {
		sendErr = syscall.Sendmsg(int(fd), []byte(msg.String()+"\n"), rights, nil, 0)
	}); err != nil {
		return Policy{}, fmt.Errorf("accessing agent socket: %w", err)
	}
	if sendErr != nil {
		return Policy{}, fmt.Errorf("sending stdout descriptor: %w", sendErr)
	}

	reply, err := c.readReply()
	if err != nil {
		return Policy{}, err
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
func (c *Client) Checkpoint() error {
	return c.request(NewMessage(VerbCheckpoint))
}

// Restore asks the agent to restore the child tree and returns when it is
// running. This reply is the ready signal.
func (c *Client) Restore() error {
	return c.request(NewMessage(VerbRestore))
}

func (c *Client) request(msg Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, err := fmt.Fprintln(c.conn, msg.String()); err != nil {
		return fmt.Errorf("sending %s: %w", msg.Verb, err)
	}

	reply, err := c.readReply()
	if err != nil {
		return err
	}
	return reply.Err()
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
