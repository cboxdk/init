package snapshot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Cause codes. Every outcome of a checkpoint or restore that is not a plain
// success carries exactly one of these, in the status API as well as in the
// log, so a caller never has to parse a log line to learn what happened.
//
// They are stable identifiers: add new ones, never rename one.
const (
	// CauseCheckpointMissing: there were no checkpoint images on this node to
	// restore (node replaced or rebooted, images swept, pod rescheduled). The
	// workload cold started in place and the warm tier stays on. Expected, not
	// an error.
	CauseCheckpointMissing = "checkpoint_missing"

	// CauseCheckpointIncompatible: the images were written for another kernel,
	// CRIU or image build and can never load here. The workload cold started
	// and the warm tier stays on; the next checkpoint is taken on this node.
	CauseCheckpointIncompatible = "checkpoint_incompatible"

	// CauseRestoreFailed: one restore of a present checkpoint failed. The
	// images are kept, the workload stays asleep, and the next connection
	// retries.
	CauseRestoreFailed = "restore_failed"

	// CauseRestoreAbandoned: a present checkpoint failed to load and the agent
	// gave up on it. The workload cold started. The first time, the warm tier
	// stays on for one fresh checkpoint; a second time turns it off.
	CauseRestoreAbandoned = "restore_abandoned"

	// CauseCheckpointFailed: a dump failed; the workload kept running. After
	// maxCheckpointFailures in a row the warm tier turns off.
	CauseCheckpointFailed = "checkpoint_failed"

	// CauseAgentDisabled: the node agent refuses further checkpoints for this
	// container. Nothing cbox-init retries can change that.
	CauseAgentDisabled = "agent_disabled"

	// CauseControlLost: the channel to the node agent is gone.
	CauseControlLost = "control_lost"

	// CauseColdStartFailed: the workload could not be started again after its
	// checkpoint was lost.
	CauseColdStartFailed = "cold_start_failed"

	// CauseOperationUncertain: the outcome of a sent operation is unknown and
	// the container is being fenced.
	CauseOperationUncertain = "operation_uncertain"

	// CauseAgentRefused: the agent refused with a reason this version of
	// cbox-init does not know. The reason is in agent_reason.
	CauseAgentRefused = "agent_refused"

	// CauseIdleNotConfigured: no idle timeout was set, so the workload never
	// sleeps. Configuration, not a failure.
	CauseIdleNotConfigured = "idle_not_configured"
)

// Levels say how much attention an outcome deserves. They match the level it
// was logged at.
const (
	LevelInfo    = "info"
	LevelWarning = "warning"
	LevelError   = "error"
)

// Results of the most recent restore, as reported in Status.LastRestore.
const (
	LastRestoreRestored  = "restored"
	LastRestoreColdStart = "cold_start"
)

// Event is one non-success outcome of a warm-tier operation, with everything
// needed to understand it without the logs.
type Event struct {
	// Operation is "checkpoint" or "restore".
	Operation string `json:"operation"`
	// Cause is one of the Cause* codes.
	Cause string `json:"cause"`
	// Level is info, warning or error.
	Level string `json:"level"`
	// Message says, in a sentence, what happened, why, and what happens next.
	Message string `json:"message"`
	// AgentReason is the node agent's own reason token, when it sent one.
	AgentReason string `json:"agent_reason,omitempty"`
	// CRIUExit is CRIU's exit status, when the agent reported it.
	CRIUExit *int `json:"criu_exit,omitempty"`
	// CRIULog is the first relevant CRIU log line, when the agent reported it.
	CRIULog string `json:"criu_log,omitempty"`
	// Attempt counts consecutive failures of the same kind, when that matters.
	Attempt int `json:"attempt,omitempty"`
	// At is when it happened.
	At time.Time `json:"at"`
}

// newEvent builds an event, copying the agent's diagnosis when err carries it.
func newEvent(operation, cause, level, message string, err error) Event {
	e := Event{Operation: operation, Cause: cause, Level: level, Message: message, At: time.Now().UTC()}
	var agentErr *AgentError
	if errors.As(err, &agentErr) {
		e.AgentReason = agentErr.Reason
		e.CRIUExit = agentErr.CRIUExit
		e.CRIULog = agentErr.CRIULog
	}
	return e
}

// slogLevel maps an event level onto the logger.
func (e Event) slogLevel() slog.Level {
	switch e.Level {
	case LevelInfo:
		return slog.LevelInfo
	case LevelWarning:
		return slog.LevelWarn
	default:
		return slog.LevelError
	}
}

// log writes the event at its own level, with the cause as a field so log
// pipelines can match on it the same way the status API reports it.
func (e Event) log(log *slog.Logger) {
	attrs := []any{"operation", e.Operation, "cause", e.Cause}
	if e.AgentReason != "" {
		attrs = append(attrs, "agent_reason", e.AgentReason)
	}
	if e.CRIUExit != nil {
		attrs = append(attrs, "criu_exit", *e.CRIUExit)
	}
	if e.CRIULog != "" {
		attrs = append(attrs, "criu_log", e.CRIULog)
	}
	if e.Attempt > 0 {
		attrs = append(attrs, "attempt", e.Attempt)
	}
	log.Log(context.Background(), e.slogLevel(), e.Message, attrs...)
}

// agentDiagnosis renders what the agent said about a failure, for a message.
// When it said nothing beyond its reason, the message says where to look
// instead of pretending to know.
func agentDiagnosis(err error) string {
	var agentErr *AgentError
	if !errors.As(err, &agentErr) {
		if err == nil {
			return ""
		}
		return err.Error()
	}
	var parts []string
	if agentErr.CRIUExit != nil {
		parts = append(parts, fmt.Sprintf("CRIU exited %d", *agentErr.CRIUExit))
	}
	if agentErr.CRIULog != "" {
		parts = append(parts, "CRIU log: "+agentErr.CRIULog)
	}
	if agentErr.Detail != "" {
		parts = append(parts, agentErr.Detail)
	}
	if len(parts) == 0 {
		return fmt.Sprintf("the node agent answered %q and sent no CRIU diagnosis; its log for this container has it", agentErr.Reason)
	}
	return fmt.Sprintf("the node agent answered %q: %s", agentErr.Reason, strings.Join(parts, "; "))
}
