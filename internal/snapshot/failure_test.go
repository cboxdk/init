package snapshot

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The failure paths, from cbox-init's side.
//
// These matter more than the wake latency does. A fast mechanism that loses a
// customer's process state is worse than a slow one, and the two ways to lose
// it here are both quiet: a control call that never returns while a customer's
// connection is held open, and a restore that fails into a container nobody
// ever cold starts.

// --- the control channel ------------------------------------------------

// silentAgent accepts the connection and then says nothing at all. This is the
// failure that a dead agent does not produce: a dead agent closes its socket
// and is noticed immediately, while a stuck one leaves the reader blocked
// forever with no error to observe.
func silentAgent(t *testing.T) string {
	t.Helper()
	path := shortSocketPath(t)

	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var held []net.Conn
	var mu sync.Mutex
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		for _, c := range held {
			_ = c.Close()
		}
		mu.Unlock()
	})
	return path
}

func TestControlRequestTimesOutRatherThanHanging(t *testing.T) {
	client, err := Dial(silentAgent(t))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	client.Timeout = 150 * time.Millisecond

	start := time.Now()
	err = client.Checkpoint(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("a checkpoint against a silent agent must fail, not succeed")
	}
	if !errors.Is(err, ErrControlLost) {
		t.Errorf("error = %v, want the channel reported as lost", err)
	}
	if elapsed > time.Second {
		t.Errorf("the call took %s; cbox-init holds a customer's connection while this runs", elapsed)
	}
}

// After a timed-out request the stream is desynchronised: the agent may still
// be about to write the reply we gave up on, and reading it later would answer
// the wrong question. So the channel is poisoned rather than reused.
func TestControlChannelIsPoisonedAfterATimeout(t *testing.T) {
	client, err := Dial(silentAgent(t))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	client.Timeout = 100 * time.Millisecond

	if err := client.Checkpoint(context.Background()); err == nil {
		t.Fatal("expected the first call to fail")
	}
	if !client.Lost() {
		t.Error("a timed-out request must mark the channel lost")
	}

	start := time.Now()
	err = client.Restore(context.Background())
	if err == nil {
		t.Fatal("a later call on a poisoned channel must fail")
	}
	if !errors.Is(err, ErrControlLost) {
		t.Errorf("error = %v, want ErrControlLost", err)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Errorf("the second call waited %s; a poisoned channel must fail immediately", elapsed)
	}
}

// An agent that dies mid-flight closes its socket. That is the easy half — but
// it must still come back as a lost channel rather than as an ordinary refusal,
// because the two mean completely different things to the caller.
func TestAgentDeathMidRequestIsALostChannel(t *testing.T) {
	path := shortSocketPath(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		buf := make([]byte, 64)
		_, _ = c.Read(buf)
		// The agent is killed with the request in hand.
		_ = c.Close()
	}()

	client, err := Dial(path)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	err = client.Restore(context.Background())
	if !errors.Is(err, ErrControlLost) {
		t.Fatalf("error = %v, want ErrControlLost", err)
	}
}

// shortSocketPath keeps the socket inside the 104-byte sun_path limit, which
// the per-test temporary directory name alone can exceed.
func shortSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "cx")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "s.sock")
}

// --- the held connection ------------------------------------------------

// The proxy holds an accepted connection across the restore. If the waker never
// returns, the client must still get an error — the whole point of accepting
// first was that the client would not be left waiting on a handshake, and
// leaving it waiting on a splice instead would be the same failure one layer
// down.
func TestProxyFailsTheConnectionWhenTheWakerHangs(t *testing.T) {
	blocked := make(chan struct{})
	t.Cleanup(func() { close(blocked) })

	p := NewProxy(ProxyOptions{
		Upstream: "127.0.0.1:1",
		Waker: WakerFunc(func(ctx context.Context) error {
			<-blocked // ignores ctx entirely, as a stuck agent would
			return nil
		}),
		Log:         quietLogger(),
		WakeTimeout: 200 * time.Millisecond,
	})
	p.SetAsleep(true)

	addr, _ := startProxy(t, p)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// The handshake completed — that part is never allowed to wait. What must
	// not happen is the read never returning.
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 1)
	if _, err := conn.Read(buf); err == nil {
		t.Fatal("expected the connection to be failed, not served")
	} else if os.IsTimeout(err) {
		t.Fatal("the client was left hanging on a held connection")
	}
}

// --- the coordinator ----------------------------------------------------

type fakeControl struct {
	mu           sync.Mutex
	checkpointer func() error
	restorer     func() error
	checkpoints  int
	restores     int
}

func (f *fakeControl) Checkpoint(context.Context) error {
	f.mu.Lock()
	f.checkpoints++
	fn := f.checkpointer
	f.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn()
}

func (f *fakeControl) Restore(context.Context) error {
	f.mu.Lock()
	f.restores++
	fn := f.restorer
	f.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn()
}

type fakeWorkload struct {
	pids         []int
	begun        atomic.Int32
	aborted      atomic.Int32
	coldStarts   atomic.Int32
	ended        atomic.Int32
	completed    atomic.Int32
	coldStartErr error
}

func (f *fakeWorkload) BeginCheckpoint() []int {
	f.begun.Add(1)
	if f.pids == nil {
		return []int{42}
	}
	return f.pids
}
func (f *fakeWorkload) CompleteCheckpoint()  { f.completed.Add(1) }
func (f *fakeWorkload) AbortCheckpoint()     { f.aborted.Add(1) }
func (f *fakeWorkload) EndCheckpoint()       { f.ended.Add(1) }
func (f *fakeWorkload) IsCheckpointed() bool { return false }
func (f *fakeWorkload) ColdStart(context.Context) error {
	f.coldStarts.Add(1)
	return f.coldStartErr
}

// wakeAsleep establishes the state Wake is only ever called from in production —
// the proxy calls it exactly when it has marked the workload asleep — and then
// wakes. Wake short-circuits when the workload is not asleep (single-flight
// against the restore herd), so a test that skips this would exercise nothing.
func wakeAsleep(c *Coordinator, p *Proxy, ctx context.Context) error {
	p.SetAsleep(true)
	return c.Wake(ctx)
}

func newTestCoordinator(t *testing.T, control Control, workload Workload) (*Coordinator, *Proxy) {
	t.Helper()
	p := NewProxy(ProxyOptions{Upstream: "127.0.0.1:1", Log: quietLogger()})
	c := NewCoordinator(CoordinatorOptions{
		Control:  control,
		Workload: workload,
		Proxy:    p,
		Log:      quietLogger(),
		Idle:     time.Nanosecond,
		Poll:     time.Millisecond,
	})
	p.waker = c
	return c, p
}

// A dump that fails leaves the workload running and serving. Nothing about the
// processes changed — only the expectation that they were about to stop — so
// the supervisor is told to go back to ordinary supervision and the proxy goes
// back to proxying.
func TestFailedCheckpointLeavesTheWorkloadServing(t *testing.T) {
	control := &fakeControl{checkpointer: func() error { return &AgentError{Reason: "dump_failed"} }}
	workload := &fakeWorkload{}
	c, p := newTestCoordinator(t, control, workload)

	c.maybeSleep(context.Background())

	if p.Asleep() {
		t.Error("a failed dump must not leave the proxy believing the workload is asleep")
	}
	if workload.aborted.Load() != 1 {
		t.Error("the supervisor was never told the checkpoint was off; the next exit would be logged as a crash")
	}
	if c.Disabled() != "" {
		t.Error("one failure must not disable the warm tier")
	}
}

// Bounded, because the prior art's unbounded retry buried the real cause under
// its own log storm — and the real cause, in our own first run, was MPTCP.
func TestRepeatedCheckpointFailuresStopTrying(t *testing.T) {
	control := &fakeControl{checkpointer: func() error { return &AgentError{Reason: "dump_failed"} }}
	workload := &fakeWorkload{}
	c, _ := newTestCoordinator(t, control, workload)

	for i := 0; i < maxCheckpointFailures; i++ {
		c.maybeSleep(context.Background())
	}
	if c.Disabled() == "" {
		t.Fatal("the warm tier must give up rather than retry forever")
	}

	before := control.checkpoints
	c.maybeSleep(context.Background())
	if control.checkpoints != before {
		t.Error("a disabled warm tier must stop asking the agent")
	}
	if workload.aborted.Load() != int32(maxCheckpointFailures) {
		t.Error("every failed attempt must hand the processes back to ordinary supervision")
	}
}

// A restore that failed once is worth another try: memory pressure and a PID
// the reaper has not freed yet are both transient, and the images are the only
// way back to the customer's state. Throwing them away here would be the
// expensive mistake.
func TestTransientRestoreFailureDoesNotColdStart(t *testing.T) {
	control := &fakeControl{restorer: func() error { return &AgentError{Reason: "restore_failed"} }}
	workload := &fakeWorkload{}
	c, p := newTestCoordinator(t, control, workload)

	if err := wakeAsleep(c, p, context.Background()); err == nil {
		t.Fatal("the connection must be failed, not served")
	}
	if workload.coldStarts.Load() != 0 {
		t.Error("a retryable restore failure must not throw away the checkpoint")
	}
	if c.Disabled() != "" {
		t.Error("a retryable failure must not disable the warm tier")
	}
}

// When the agent says it has given up, the dumped processes are gone for good.
// Leaving the container asleep would mean every future connection fails against
// images that will never load, so it cold starts — and the state loss is stated
// rather than absorbed.
func TestAbandonedCheckpointColdStartsTheWorkload(t *testing.T) {
	for _, reason := range []string{ReasonRestoreAbandoned, ReasonImagesMissing} {
		t.Run(reason, func(t *testing.T) {
			control := &fakeControl{restorer: func() error { return &AgentError{Reason: reason} }}
			workload := &fakeWorkload{}
			c, p := newTestCoordinator(t, control, workload)

			if err := wakeAsleep(c, p, context.Background()); err != nil {
				t.Fatalf("after a cold start the connection can be served: %v", err)
			}
			if workload.coldStarts.Load() != 1 {
				t.Fatal("an abandoned checkpoint must be recovered by cold starting, not left asleep")
			}
			if c.Disabled() == "" {
				t.Error("a container that lost a checkpoint must come off the warm tier")
			}
		})
	}
}

// The other way to lose the processes for good: the agent is the only thing
// holding the images and the descriptor the workload's output goes back onto,
// so a channel that breaks while the workload is checkpointed means nothing can
// ever restore it.
func TestLostControlChannelColdStartsTheWorkload(t *testing.T) {
	control := &fakeControl{restorer: func() error { return ErrControlLost }}
	workload := &fakeWorkload{}
	c, p := newTestCoordinator(t, control, workload)

	if err := wakeAsleep(c, p, context.Background()); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	if workload.coldStarts.Load() != 1 {
		t.Error("a checkpointed workload whose agent is gone must cold start; nothing else can wake it")
	}
}

// And if even the cold start fails, the caller is told. This is the floor: an
// ordinary process failure, with the supervisor's restart policy and then the
// kubelet behind it.
func TestFailedColdStartIsReported(t *testing.T) {
	control := &fakeControl{restorer: func() error { return &AgentError{Reason: ReasonRestoreAbandoned} }}
	workload := &fakeWorkload{coldStartErr: errors.New("exec format error")}
	c, p := newTestCoordinator(t, control, workload)

	if err := wakeAsleep(c, p, context.Background()); err == nil {
		t.Fatal("a failed cold start must not be reported as a served connection")
	}
	if c.Disabled() == "" {
		t.Error("the warm tier must be off after a cold start failed")
	}
}

// A checkpoint failure caused by the channel itself gives up at once rather
// than after three tries: there is nothing left to retry against.
func TestLostChannelDisablesCheckpointingImmediately(t *testing.T) {
	control := &fakeControl{checkpointer: func() error { return ErrControlLost }}
	c, _ := newTestCoordinator(t, control, &fakeWorkload{})

	c.maybeSleep(context.Background())

	if c.Disabled() == "" {
		t.Error("with no channel to the agent there is nothing to retry; the tier must switch off")
	}
}

// The happy path, mostly here to pin down the ordering: the proxy is marked
// asleep before the dump, so a connection arriving mid-dump goes down the wake
// path instead of being proxied at a process CRIU is freezing.
func TestSleepMarksAsleepBeforeDumping(t *testing.T) {
	var asleepDuringDump bool
	workload := &fakeWorkload{}

	p := NewProxy(ProxyOptions{Upstream: "127.0.0.1:1", Log: quietLogger()})
	control := &fakeControl{checkpointer: func() error {
		asleepDuringDump = p.Asleep()
		return nil
	}}
	c := NewCoordinator(CoordinatorOptions{
		Control: control, Workload: workload, Proxy: p,
		Log: quietLogger(), Idle: time.Nanosecond, Poll: time.Millisecond,
	})
	p.waker = c

	c.maybeSleep(context.Background())

	if !asleepDuringDump {
		t.Error("a connection arriving mid-dump would have been proxied at a freezing process")
	}
	if !p.Asleep() {
		t.Error("the workload should be asleep after a successful dump")
	}
	if workload.aborted.Load() != 0 {
		t.Error("a successful dump must not abort anything")
	}
	if workload.completed.Load() != 1 {
		t.Error("the dumped tree was never reaped; its zombies hold the PIDs the restore needs")
	}
}

// Nothing running is not a failure and must not be treated as one — a container
// whose processes have all exited is the supervisor's problem, not the warm
// tier's.
func TestNothingToCheckpointIsNotAFailure(t *testing.T) {
	control := &fakeControl{}
	workload := &fakeWorkload{pids: []int{}}
	c, p := newTestCoordinator(t, control, workload)

	c.maybeSleep(context.Background())

	if control.checkpoints != 0 {
		t.Error("the agent was asked to dump an empty tree")
	}
	if p.Asleep() {
		t.Error("an empty container must not be marked asleep")
	}
	if c.Disabled() != "" {
		t.Error("nothing to dump must not count against the warm tier")
	}
}

// A connection that arrives between "the workload is idle" and "the dump has
// started" must not be spliced into a process CRIU is about to freeze. The
// sleep marks asleep first and then re-reads the counters, so a connection that
// got in just before the flag did cancels the sleep instead of being dumped
// mid-request.
func TestSleepBacksOutWhenAConnectionArrivesFirst(t *testing.T) {
	control := &fakeControl{}
	workload := &fakeWorkload{}
	c, p := newTestCoordinator(t, control, workload)

	// Standing in for the connection that slipped in: the activity counter is
	// the only thing the sleep path can see, and it is exact rather than
	// inferred precisely so this decision can be made on it.
	closed := p.Activity().Opened()
	defer closed()

	c.maybeSleep(context.Background())

	if control.checkpoints != 0 {
		t.Error("a workload with an open connection was dumped")
	}
	if p.Asleep() {
		t.Error("the proxy was left believing a running workload is asleep")
	}
	if workload.begun.Load() != 0 {
		t.Error("the supervisor was told to expect a checkpoint that never happened")
	}
}

// The warm tier has to work more than once. A restore leaves the tree back at
// the PIDs CRIU recorded but re-parented, with no monitoring goroutine and no
// exec.Cmd behind it, so unless the supervisor is told it is running again the
// workload can never sleep a second time and its death would go unnoticed.
func TestRestorePutsTheWorkloadBackUnderSupervision(t *testing.T) {
	workload := &fakeWorkload{}
	c, p := newTestCoordinator(t, &fakeControl{}, workload)

	if err := wakeAsleep(c, p, context.Background()); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	if workload.ended.Load() != 1 {
		t.Error("the supervisor was never told the workload is running again; it would sleep exactly once")
	}
}

// A cold start can legitimately finish after the caller has given up: a wedged
// CRIU can burn the whole wake budget before anyone stops waiting on it. If
// only the caller cleared the sleeping flag, the workload would be running with
// the proxy still convinced it is asleep, and every later connection would go
// into a restore that can no longer succeed. Observed exactly that way on a
// node before it was fixed.
func TestColdStartMarksTheProxyAwakeItself(t *testing.T) {
	control := &fakeControl{restorer: func() error { return ErrControlLost }}
	c, p := newTestCoordinator(t, control, &fakeWorkload{})
	p.SetAsleep(true)

	if err := wakeAsleep(c, p, context.Background()); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	if p.Asleep() {
		t.Error("the proxy was left asleep in front of a running workload")
	}
}

// And a successful restore does the same, for the same reason.
func TestSuccessfulRestoreMarksTheProxyAwake(t *testing.T) {
	c, p := newTestCoordinator(t, &fakeControl{}, &fakeWorkload{})
	p.SetAsleep(true)

	if err := wakeAsleep(c, p, context.Background()); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	if p.Asleep() {
		t.Error("the proxy stayed asleep after a successful restore")
	}
}

// Every connection that arrives while the workload is asleep calls Wake, so
// they queue on the same lock. Only the first may restore: asking the agent to
// restore an already-restored tree can be answered "images missing" (a restore
// consumes them), which counts as unrecoverable and cold starts a perfectly
// healthy workload — destroying exactly the state the warm tier exists to keep.
func TestConcurrentWakesRestoreOnce(t *testing.T) {
	control := &fakeControl{}
	workload := &fakeWorkload{}
	c, p := newTestCoordinator(t, control, workload)
	p.SetAsleep(true)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.Wake(context.Background()); err != nil {
				t.Errorf("wake: %v", err)
			}
		}()
	}
	wg.Wait()

	if control.restores != 1 {
		t.Errorf("restores = %d, want exactly 1 for a herd of concurrent wakes", control.restores)
	}
	if workload.coldStarts.Load() != 0 {
		t.Errorf("a healthy workload must never be cold started (%d cold starts)", workload.coldStarts.Load())
	}
}

// A wake whose caller already gave up must not touch the control channel: a
// write on an expired deadline fails, and that failure latches the channel
// broken for every future caller.
func TestWakeWithExpiredContextDoesNotPoisonTheChannel(t *testing.T) {
	control := &fakeControl{}
	c, p := newTestCoordinator(t, control, &fakeWorkload{})
	p.SetAsleep(true)

	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Wake(expired); err == nil {
		t.Fatal("a wake with an expired context must fail the caller")
	}
	if control.restores != 0 {
		t.Errorf("restores = %d, want 0 — the channel must not be used with a dead context", control.restores)
	}

	// The channel must still be usable for the next caller.
	if err := c.Wake(context.Background()); err != nil {
		t.Errorf("the control channel must survive an abandoned wake, got: %v", err)
	}
}
