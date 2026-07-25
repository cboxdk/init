package snapshot

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The control protocol between cbox-init (in the container, unprivileged) and
// cortex-agent (on the node, privileged).
//
// It is one persistent connection, opened by cbox-init and held for the life of
// the container, carrying newline-delimited text. Three properties are
// deliberate:
//
//   - The agent listens and cbox-init dials. The agent is the long-lived node
//     service and the socket is bind-mounted in, so this is the direction that
//     works when a container restarts.
//   - Readiness after a restore is an explicit reply on a connection we already
//     hold, not something inferred by dialling the workload in a loop. Polling
//     would add its own latency to the one number this whole mechanism exists
//     to minimise.
//   - Registration carries the pipe's write end as an SCM_RIGHTS descriptor.
//     The agent is outside the container and has no other way to obtain it.
//
// Text rather than protobuf because the whole vocabulary is four verbs, and an
// operator being able to read the exchange with socat is worth more here than
// encoding efficiency.
const (
	// VerbRegister introduces the container: the child's PID as the container
	// sees it, and the stdout pipe's inode. Sent with the pipe's write end
	// attached.
	VerbRegister = "REGISTER"

	// VerbCheckpoint asks the agent to dump the child tree. The agent replies
	// only after the processes have stopped, so cbox-init knows exactly when to
	// start reaping.
	VerbCheckpoint = "CHECKPOINT"

	// VerbRestore asks the agent to restore the child tree. The reply is the
	// ready signal.
	VerbRestore = "RESTORE"

	// StatusOK prefixes a successful reply.
	StatusOK = "OK"

	// StatusError prefixes a failure. A failure is always reported; it never
	// silently disables anything. The workload keeps running, it is simply not
	// eligible for the warm tier until someone looks.
	StatusError = "ERR"
)

// Message is one line of the protocol: a verb and flat key=value fields.
type Message struct {
	Verb   string
	Fields map[string]string
}

// NewMessage builds a message from alternating key/value pairs.
func NewMessage(verb string, kv ...string) Message {
	m := Message{Verb: verb, Fields: map[string]string{}}
	for i := 0; i+1 < len(kv); i += 2 {
		m.Fields[kv[i]] = kv[i+1]
	}
	return m
}

// String renders the message as a single line, fields sorted so that the wire
// form of a given message is stable and testable.
func (m Message) String() string {
	keys := make([]string, 0, len(m.Fields))
	for k := range m.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString(m.Verb)
	for _, k := range keys {
		b.WriteString(" ")
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(m.Fields[k])
	}
	return b.String()
}

// Int reads a field as an integer, returning def when absent or unparseable.
// Absent-or-broken collapses to the default on purpose: a garbled policy field
// must not be able to stop a container from starting.
func (m Message) Int(key string, def int) int {
	v, ok := m.Fields[key]
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// Err returns a non-nil error when the message is a failure reply.
func (m Message) Err() error {
	if m.Verb != StatusError {
		return nil
	}
	if reason, ok := m.Fields["reason"]; ok {
		return fmt.Errorf("snapshot agent: %s", reason)
	}
	return fmt.Errorf("snapshot agent: refused")
}

// ParseMessage parses one protocol line. Unknown fields are kept rather than
// rejected, so one side can learn a new field without the other having to.
func ParseMessage(line string) (Message, error) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return Message{}, fmt.Errorf("empty message")
	}

	m := Message{Verb: fields[0], Fields: map[string]string{}}
	for _, f := range fields[1:] {
		k, v, found := strings.Cut(f, "=")
		if !found {
			return Message{}, fmt.Errorf("malformed field %q", f)
		}
		m.Fields[k] = v
	}
	return m, nil
}
