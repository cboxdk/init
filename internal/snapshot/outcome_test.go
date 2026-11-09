package snapshot

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// Every non-success outcome of a checkpoint or restore must say what happened
// and why, in the status as well as the log, and only a real failure may turn
// warm sleep off. These tests pin that down case by case.

// ColdStart leaves nothing checkpointed, as the manager's does.
func (w *statefulWorkload) ColdStart(ctx context.Context) error {
	if err := w.fakeWorkload.ColdStart(ctx); err != nil {
		return err
	}
	w.checkpointed.Store(false)
	return nil
}

// recordingHandler keeps every log record so a test can assert the level an
// outcome was logged at.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordingHandler) maxLevel() slog.Level {
	h.mu.Lock()
	defer h.mu.Unlock()
	max := slog.LevelDebug
	for _, r := range h.records {
		if r.Level > max {
			max = r.Level
		}
	}
	return max
}

func newRecordingCoordinator(t *testing.T, control Control, workload Workload) (*Coordinator, *Proxy, *recordingHandler) {
	t.Helper()
	c, p := newTestCoordinator(t, control, workload)
	h := &recordingHandler{}
	c.log = slog.New(h)
	return c, p, h
}

// sleepThenWake takes a real checkpoint and then wakes it, the order the
// coordinator sees in production.
func sleepThenWake(t *testing.T, c *Coordinator) error {
	t.Helper()
	if err := c.Sleep(context.Background()); err != nil {
		t.Fatalf("sleep: %v", err)
	}
	return c.Wake(context.Background())
}

func exitCode(n int) *int { return &n }

// A pod that lands on another node, or a node that was replaced, has no
// checkpoint to restore. That is normal: cold start, stay on, say so at info.
func TestMissingCheckpointColdStartsAndKeepsWarmSleep(t *testing.T) {
	control := &fakeControl{}
	w := &statefulWorkload{}
	c, _, logs := newRecordingCoordinator(t, control, w)

	control.restorer = func() error { return &AgentError{Reason: ReasonImagesMissing} }
	if err := sleepThenWake(t, c); err != nil {
		t.Fatalf("a missing checkpoint must still serve the connection: %v", err)
	}

	status := c.Status()
	if status.State != "awake" || status.LastRestore != LastRestoreColdStart ||
		status.Cause != CauseCheckpointMissing || status.Level != LevelInfo {
		t.Fatalf("status = %+v", status)
	}
	if status.DisabledReason != "" || status.DisabledCause != "" {
		t.Fatalf("a missing checkpoint must not disable warm sleep: %+v", status)
	}
	if !strings.Contains(status.Message, "fresh checkpoint") {
		t.Errorf("the message must say what happens next: %q", status.Message)
	}
	if logs.maxLevel() > slog.LevelInfo {
		t.Errorf("a missing checkpoint was logged at %s; it is not a warning or an error", logs.maxLevel())
	}

	// The next sleep takes a fresh checkpoint, and its success clears the cause.
	control.restorer = nil
	if err := c.Sleep(context.Background()); err != nil {
		t.Fatalf("warm sleep must still work after a missing checkpoint: %v", err)
	}
	if control.checkpoints != 2 {
		t.Fatalf("checkpoints = %d, want a fresh one", control.checkpoints)
	}
	status = c.Status()
	if status.Cause != "" || status.LastEvent == nil || status.LastEvent.Cause != CauseCheckpointMissing {
		t.Fatalf("after a success the current cause clears and history stays: %+v", status)
	}
}

// Images written for another kernel or CRIU can never load here, but a fresh
// checkpoint on this node will. Cold start, stay on, and carry the diagnosis.
func TestIncompatibleCheckpointColdStartsAndKeepsWarmSleep(t *testing.T) {
	control := &fakeControl{}
	w := &statefulWorkload{}
	c, _, logs := newRecordingCoordinator(t, control, w)

	control.restorer = func() error {
		return &AgentError{Reason: ReasonCheckpointIncompatible, CRIUExit: exitCode(1), CRIULog: "Error (criu/cr-restore.c:1234): kernel version mismatch"}
	}
	if err := sleepThenWake(t, c); err != nil {
		t.Fatalf("Wake: %v", err)
	}

	status := c.Status()
	if status.Cause != CauseCheckpointIncompatible || status.Level != LevelWarning || status.DisabledReason != "" {
		t.Fatalf("status = %+v", status)
	}
	if status.LastRestore != LastRestoreColdStart || w.coldStarts.Load() != 1 {
		t.Fatalf("an incompatible checkpoint must cold start: %+v", status)
	}
	ev := status.LastEvent
	if ev == nil || ev.CRIUExit == nil || *ev.CRIUExit != 1 || !strings.Contains(ev.CRIULog, "kernel version mismatch") {
		t.Fatalf("CRIU's diagnosis must reach the status: %+v", ev)
	}
	if !strings.Contains(status.Message, "kernel version mismatch") {
		t.Errorf("the message must say why: %q", status.Message)
	}
	if logs.maxLevel() != slog.LevelWarn {
		t.Errorf("logged at %s, want WARN", logs.maxLevel())
	}
}

// One failed restore of a present checkpoint keeps the images and the workload
// asleep for a retry, and says why with CRIU's own words.
func TestRestoreFailureOfPresentCheckpointExplainsAndRetries(t *testing.T) {
	control := &fakeControl{}
	w := &statefulWorkload{}
	c, p, _ := newRecordingCoordinator(t, control, w)

	control.restorer = func() error {
		return &AgentError{Reason: ReasonRestoreFailed, CRIUExit: exitCode(1), CRIULog: "Error (criu/files-reg.c:1876): Can't open file"}
	}
	if err := sleepThenWake(t, c); err == nil {
		t.Fatal("a failed restore must fail the connection")
	}
	status := c.Status()
	if status.Cause != CauseRestoreFailed || status.Level != LevelError || status.DisabledReason != "" {
		t.Fatalf("status = %+v", status)
	}
	if !p.Asleep() || w.coldStarts.Load() != 0 {
		t.Fatal("a retryable failure must keep the checkpoint")
	}
	if !strings.Contains(status.Message, "CRIU exited 1") || !strings.Contains(status.Message, "Can't open file") {
		t.Errorf("the message must carry CRIU's exit and log line: %q", status.Message)
	}

	control.restorer = nil
	if err := c.Wake(context.Background()); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if status := c.Status(); status.Cause != "" || status.LastRestore != LastRestoreRestored {
		t.Fatalf("after a successful retry: %+v", status)
	}
}

// A checkpoint that was present and still would not load is the one outcome
// that may turn warm sleep off — but only after one fresh checkpoint failed the
// same way. A successful restore in between starts the count again.
func TestAbandonedRestoreRetriesAFreshCheckpointOnceBeforeDisabling(t *testing.T) {
	control := &fakeControl{}
	w := &statefulWorkload{}
	c, _, _ := newRecordingCoordinator(t, control, w)
	abandon := func() error { return &AgentError{Reason: ReasonRestoreAbandoned} }

	control.restorer = abandon
	if err := sleepThenWake(t, c); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	status := c.Status()
	if status.Cause != CauseRestoreAbandoned || status.DisabledReason != "" || status.LastEvent.Attempt != 1 {
		t.Fatalf("first abandon must keep warm sleep for one fresh checkpoint: %+v", status)
	}
	if !strings.Contains(status.Message, "one fresh checkpoint") {
		t.Errorf("the message must say what happens next: %q", status.Message)
	}

	// A good cycle resets the count.
	control.restorer = nil
	if err := sleepThenWake(t, c); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	control.restorer = abandon
	if err := sleepThenWake(t, c); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	if c.Disabled() != "" {
		t.Fatal("a success in between must reset the abandon count")
	}

	if err := sleepThenWake(t, c); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	status = c.Status()
	if status.State != "disabled" || status.DisabledCause != CauseRestoreAbandoned || status.DisabledReason == "" {
		t.Fatalf("a second abandon in a row must turn warm sleep off and say why: %+v", status)
	}
	if status.LastRestore != LastRestoreColdStart || w.coldStarts.Load() != 3 {
		t.Fatalf("the workload must still be cold started: %+v", status)
	}
}

// A failed dump explains itself with its attempt count, and the bounded retry
// turns warm sleep off with a cause after the last one.
func TestCheckpointFailureExplainsAndDisablesAfterRepeatedFailures(t *testing.T) {
	control := &fakeControl{checkpointer: func() error {
		return &AgentError{Reason: ReasonDumpFailed, CRIUExit: exitCode(1), CRIULog: "Error (criu/sk-inet.c:188): Unsupported socket"}
	}}
	c, _, _ := newRecordingCoordinator(t, control, &statefulWorkload{})

	err := c.Sleep(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Unsupported socket") {
		t.Fatalf("the error must carry the cause: %v", err)
	}
	status := c.Status()
	if status.Cause != CauseCheckpointFailed || status.LastEvent.Attempt != 1 || status.DisabledReason != "" {
		t.Fatalf("status = %+v", status)
	}
	if !strings.Contains(status.Message, "Unsupported socket") || !strings.Contains(status.Message, "attempt 1 of 3") {
		t.Errorf("message = %q", status.Message)
	}

	for range maxCheckpointFailures - 1 {
		_ = c.Sleep(context.Background())
	}
	status = c.Status()
	if status.DisabledCause != CauseCheckpointFailed || !strings.Contains(status.DisabledReason, "Unsupported socket") {
		t.Fatalf("status = %+v", status)
	}
	if err := c.Sleep(context.Background()); !errors.Is(err, ErrSleepUnavailable) || !strings.Contains(err.Error(), "Unsupported socket") {
		t.Fatalf("a refused sleep must say why: %v", err)
	}
}

// An agent that stopped accepting checkpoints answers every later request the
// same way. Stop at once, with a cause, rather than burn the retry budget.
func TestAgentDisabledCheckpointStopsAtOnceAndSaysWhy(t *testing.T) {
	control := &fakeControl{}
	c, _, _ := newRecordingCoordinator(t, control, &statefulWorkload{})

	control.restorer = func() error { return &AgentError{Reason: ReasonImagesMissing} }
	if err := sleepThenWake(t, c); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	control.checkpointer = func() error { return &AgentError{Reason: ReasonCheckpointDisabled} }
	_ = c.Sleep(context.Background())

	status := c.Status()
	if status.DisabledCause != CauseAgentDisabled || control.checkpoints != 2 {
		t.Fatalf("status = %+v, checkpoints = %d", status, control.checkpoints)
	}
	if !strings.Contains(status.DisabledReason, "needs updating") {
		t.Errorf("after a missing checkpoint the reason must point at the agent: %q", status.DisabledReason)
	}
}

// Losing the channel before a restore is sent cold starts and turns warm sleep
// off, with a cause.
func TestLostControlChannelOnRestoreReportsCause(t *testing.T) {
	control := &fakeControl{}
	c, _, _ := newRecordingCoordinator(t, control, &statefulWorkload{})
	control.restorer = func() error { return ErrControlLost }

	if err := sleepThenWake(t, c); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	status := c.Status()
	if status.DisabledCause != CauseControlLost || status.Cause != CauseControlLost || status.LastRestore != LastRestoreColdStart {
		t.Fatalf("status = %+v", status)
	}
}

// A cold start that fails is reported as such, not as the lost checkpoint.
func TestFailedColdStartReportsItsOwnCause(t *testing.T) {
	control := &fakeControl{restorer: func() error { return &AgentError{Reason: ReasonImagesMissing} }}
	w := &fakeWorkload{coldStartErr: errors.New("exec format error")}
	c, p, _ := newRecordingCoordinator(t, control, w)

	if err := wakeAsleep(c, p, context.Background()); err == nil {
		t.Fatal("a failed cold start must fail the connection")
	}
	status := c.Status()
	if status.DisabledCause != CauseColdStartFailed || !strings.Contains(status.DisabledReason, "exec format error") {
		t.Fatalf("status = %+v", status)
	}
}

// A caller that gives up before the dump is sent did not cause a failed dump.
func TestCheckpointAbandonedBeforeSendIsNotAFailure(t *testing.T) {
	control := &fakeControl{checkpointer: func() error {
		return errors.Join(errors.New("CHECKPOINT abandoned before send"), context.Canceled)
	}}
	c, _, logs := newRecordingCoordinator(t, control, &statefulWorkload{})

	_ = c.Sleep(context.Background())
	if status := c.Status(); status.Cause != "" || status.DisabledReason != "" {
		t.Fatalf("status = %+v", status)
	}
	if logs.maxLevel() > slog.LevelInfo {
		t.Errorf("logged at %s", logs.maxLevel())
	}
}

// Refusals say why, so a caller never sees a bare "busy" or "unavailable".
func TestSleepRefusalsCarryTheirReason(t *testing.T) {
	c, p, _ := newRecordingCoordinator(t, &fakeControl{}, &statefulWorkload{})
	closed := p.Activity().Opened()
	err := c.Sleep(context.Background())
	closed()
	if !errors.Is(err, ErrWorkloadBusy) || !strings.Contains(err.Error(), "1 open connections") {
		t.Fatalf("busy refusal = %v", err)
	}

	c.idle = 0
	if err := c.Sleep(context.Background()); !errors.Is(err, ErrSleepUnavailable) || !strings.Contains(err.Error(), "idle timeout") {
		t.Fatalf("unconfigured refusal = %v", err)
	}
	if status := c.Status(); status.State != "disabled" || status.DisabledCause != CauseIdleNotConfigured {
		t.Fatalf("status = %+v", status)
	}
}

// The status serialises its new fields under stable names and leaves them out
// when there is nothing to say, so older readers see exactly what they did.
func TestStatusJSONIsAdditive(t *testing.T) {
	c, _, _ := newRecordingCoordinator(t, &fakeControl{}, &statefulWorkload{})
	raw, err := json.Marshal(c.Status())
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"supported":true,"state":"awake","idle_seconds":0}` {
		t.Fatalf("healthy status changed shape: %s", raw)
	}

	control := &fakeControl{restorer: func() error { return &AgentError{Reason: ReasonImagesMissing} }}
	c, _, _ = newRecordingCoordinator(t, control, &statefulWorkload{})
	if err := sleepThenWake(t, c); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(c.Status())
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"state": "awake", "last_restore": "cold_start", "cause": "checkpoint_missing", "level": "info"} {
		if got[key] != want {
			t.Errorf("%s = %v, want %s (%s)", key, got[key], want, raw)
		}
	}
	event, ok := got["last_event"].(map[string]any)
	if !ok || event["operation"] != "restore" || event["agent_reason"] != ReasonImagesMissing || event["message"] == "" {
		t.Errorf("last_event = %v", got["last_event"])
	}
}

// The wire format: optional diagnosis fields are percent-encoded single tokens,
// and an agent that sends none still works.
func TestAgentErrorCarriesOptionalCRIUDiagnosis(t *testing.T) {
	msg, err := ParseMessage("ERR reason=restore_failed criu_exit=1 criu_log=Error%20(criu/cr-restore.c:42):%20boom detail=pid%20busy")
	if err != nil {
		t.Fatal(err)
	}
	var agentErr *AgentError
	if !errors.As(msg.Err(), &agentErr) {
		t.Fatal("expected an agent error")
	}
	if agentErr.Reason != ReasonRestoreFailed || agentErr.CRIUExit == nil || *agentErr.CRIUExit != 1 ||
		agentErr.CRIULog != "Error (criu/cr-restore.c:42): boom" || agentErr.Detail != "pid busy" {
		t.Fatalf("parsed %+v", agentErr)
	}
	if !strings.Contains(agentErr.Error(), "criu exit 1") {
		t.Errorf("Error() = %q", agentErr.Error())
	}

	plain, _ := ParseMessage("ERR reason=images_missing")
	if !errors.As(plain.Err(), &agentErr) || agentErr.CRIUExit != nil || agentErr.CRIULog != "" {
		t.Fatalf("plain = %+v", agentErr)
	}
	if got := agentDiagnosis(agentErr); !strings.Contains(got, "sent no CRIU diagnosis") {
		t.Errorf("a missing diagnosis must say where to look: %q", got)
	}

	broken, _ := ParseMessage("ERR reason=dump_failed criu_exit=x criu_log=%zz")
	if !errors.As(broken.Err(), &agentErr) || agentErr.CRIUExit != nil || agentErr.CRIULog != "%zz" {
		t.Fatalf("garbled fields must not drop the reason: %+v", agentErr)
	}
}
