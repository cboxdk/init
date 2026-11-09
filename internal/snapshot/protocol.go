package snapshot

import (
	"fmt"
	"net/url"
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

// AgentError is a refusal from the agent, carrying the single-token reason it
// sent. It is a type rather than a string because the caller has to act
// differently on different reasons: a dump that failed leaves a running
// workload alone, while a checkpoint the agent has abandoned means the
// processes are gone and only a cold start brings the service back.
//
// The reason is the only field an agent must send. An agent may add CRIU's own
// diagnosis as optional fields, which cbox-init carries into its status so the
// cause of a failed dump or restore is never only in a node log:
//
//	criu_exit=<int>      CRIU's exit status
//	criu_log=<escaped>   the first relevant line of CRIU's log, percent-encoded
//	detail=<escaped>     any other human explanation, percent-encoded
//
// Percent-encoding (RFC 3986, %20 for a space) keeps each value one token on
// the line-oriented wire. Agents that send none of these still interoperate.
type AgentError struct {
	Reason string
	// CRIUExit is CRIU's exit status, when the agent reported it.
	CRIUExit *int
	// CRIULog is the first relevant CRIU log line, when the agent reported it.
	CRIULog string
	// Detail is the agent's own explanation, when it sent one.
	Detail string
}

// Is exposes explicit uncertain outcomes through the same fencing path as a
// lost reply. Acknowledging uncertainty is not an ordinary safe refusal.
func (e *AgentError) Is(target error) bool {
	return target == ErrOperationUncertain && e.Reason == ReasonOperationUncertain
}

func (e *AgentError) Error() string {
	var b strings.Builder
	b.WriteString("snapshot agent: ")
	b.WriteString(e.Reason)
	if e.CRIUExit != nil {
		fmt.Fprintf(&b, " (criu exit %d)", *e.CRIUExit)
	}
	if e.CRIULog != "" {
		b.WriteString(": ")
		b.WriteString(e.CRIULog)
	}
	if e.Detail != "" {
		b.WriteString(": ")
		b.WriteString(e.Detail)
	}
	return b.String()
}

// Reasons the agent sends that the container must act on rather than merely
// log. The remaining reasons all mean "not this time", and the workload is
// running throughout.
const (
	// ReasonRestoreAbandoned means the agent gave up on a checkpoint it could
	// not load. The dumped processes are gone for good.
	ReasonRestoreAbandoned = "restore_abandoned"

	// ReasonImagesMissing means there is nothing left to restore from. That is
	// a normal event, not a failure: the node was replaced or rebooted, the
	// images were swept, or they were never on this node. The container cold
	// starts and keeps the warm tier.
	ReasonImagesMissing = "images_missing"

	// ReasonCheckpointIncompatible means the images exist but were written for
	// a different kernel, CRIU or image build, so they can never load here. A
	// fresh checkpoint on this node will, so the warm tier stays on.
	ReasonCheckpointIncompatible = "checkpoint_incompatible"

	// ReasonRestoreFailed means one restore of present images failed. The
	// images are kept and the next connection retries.
	ReasonRestoreFailed = "restore_failed"

	// ReasonDumpFailed means a dump failed and the workload kept running.
	ReasonDumpFailed = "dump_failed"

	// ReasonCheckpointDisabled means the agent no longer accepts checkpoints
	// for this container. It answers every later request the same way.
	ReasonCheckpointDisabled = "checkpoint_disabled"

	// ReasonOperationUncertain means the agent cannot say whether an operation
	// ran. Handled by fencing, never by a cold start.
	ReasonOperationUncertain = "operation_uncertain"
)

// Lost reports whether a reason means the checkpointed processes can never come
// back, which is the only case where losing state is the right answer.
func (e *AgentError) Lost() bool {
	switch e.Reason {
	case ReasonRestoreAbandoned, ReasonImagesMissing, ReasonCheckpointIncompatible:
		return true
	}
	return false
}

// Err returns a non-nil error when the message is a failure reply.
func (m Message) Err() error {
	if m.Verb != StatusError {
		return nil
	}
	agentErr := &AgentError{Reason: "refused"}
	if reason, ok := m.Fields["reason"]; ok && reason != "" {
		agentErr.Reason = reason
	}
	if v, ok := m.Fields["criu_exit"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			agentErr.CRIUExit = &n
		}
	}
	agentErr.CRIULog = unescapeField(m.Fields["criu_log"])
	agentErr.Detail = unescapeField(m.Fields["detail"])
	return agentErr
}

// unescapeField decodes an optional percent-encoded value. A value that does
// not decode is shown as sent rather than dropped: a garbled explanation is
// still more useful to an operator than none.
func unescapeField(v string) string {
	if v == "" {
		return ""
	}
	decoded, err := url.PathUnescape(v)
	if err != nil {
		return v
	}
	return strings.TrimSpace(decoded)
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
