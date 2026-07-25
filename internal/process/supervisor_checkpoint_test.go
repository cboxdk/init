package process

import (
	"context"
	"log/slog"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/snapshot"
	"github.com/cboxdk/init/internal/testutil"
)

// snapshotStreamFor reaches into the supervisor's instance list; the streams
// are deliberately unexported, and asserting on them is the only way to pin
// down which side owns the pipe.
func (s *Supervisor) snapshotStreamFor(index int) *snapshot.Stream {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if index >= len(s.instances) {
		return nil
	}
	inst := s.instances[index]
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	return inst.stdoutStream
}

func checkpointSupervisor(t *testing.T, restart string) *Supervisor {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	sup := NewSupervisor(
		"snapshotted",
		&config.Process{
			Enabled: true,
			Command: []string{"sh", "-c", "while :; do sleep 0.05; done"},
			Restart: restart,
			Scale:   1,
		},
		&config.GlobalConfig{LogLevel: "error", MaxRestartAttempts: 3, RestartBackoff: 1},
		logger,
		audit.NewLogger(logger, false),
		nil,
	)
	sup.SetSnapshotStreams(true)
	return sup
}

// The state the warm tier needs from the supervisor: the process really exits —
// CRIU stops it as part of the dump — but the exit is the intended outcome, so
// it must be reaped without being treated as a crash and without being
// restarted. Restarting it would throw away the very memory image that was just
// written.
func TestSupervisorCheckpointedExitIsNotACrash(t *testing.T) {
	sup := checkpointSupervisor(t, "always")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := sup.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = sup.Stop(context.Background()) }()

	testutil.Eventually(t, func() bool {
		return len(sup.GetInstances()) == 1 && sup.GetState() == StateRunning
	}, "process reaches running", 10*time.Second)

	originalPID := sup.GetInstances()[0].PID

	// What the agent is told to checkpoint.
	pids := sup.BeginCheckpoint()
	if len(pids) != 1 || pids[0] != originalPID {
		t.Fatalf("BeginCheckpoint returned %v, want the running pid %d", pids, originalPID)
	}

	// Stand in for the dump: CRIU stops the process once the image is written.
	if err := syscall.Kill(originalPID, syscall.SIGKILL); err != nil {
		t.Fatalf("simulating the dump: %v", err)
	}

	testutil.Eventually(t, func() bool {
		return sup.IsCheckpointed()
	}, "instance reports checkpointed", 10*time.Second)

	// A restart policy of "always" would normally bring it straight back, and
	// with it a brand-new process that has none of the checkpointed state.
	time.Sleep(500 * time.Millisecond)

	if !sup.IsCheckpointed() {
		t.Fatal("instance left the checkpointed state; a checkpoint must not be restarted")
	}
	instances := sup.GetInstances()
	if len(instances) != 1 {
		t.Fatalf("instance count = %d, want 1", len(instances))
	}
	if got := instances[0].PID; got != originalPID {
		t.Errorf("pid changed from %d to %d; the process was restarted rather than checkpointed",
			originalPID, got)
	}
}

// A failed dump must leave the workload running as an ordinary supervised
// process — not disabled, not force-killed on the next attempt.
func TestSupervisorAbortCheckpointRestoresOrdinarySupervision(t *testing.T) {
	sup := checkpointSupervisor(t, "always")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := sup.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = sup.Stop(context.Background()) }()

	testutil.Eventually(t, func() bool {
		return len(sup.GetInstances()) == 1 && sup.GetState() == StateRunning
	}, "process reaches running", 10*time.Second)

	originalPID := sup.GetInstances()[0].PID

	sup.BeginCheckpoint()
	sup.AbortCheckpoint() // the agent reported the dump failed

	// The process is untouched and still supervised, so a real crash now
	// restarts it exactly as it would have before the attempt.
	if err := syscall.Kill(originalPID, syscall.SIGKILL); err != nil {
		t.Fatalf("killing process: %v", err)
	}

	testutil.Eventually(t, func() bool {
		instances := sup.GetInstances()
		return len(instances) == 1 && instances[0].PID != originalPID
	}, "process restarts after an aborted checkpoint", 15*time.Second)

	if sup.IsCheckpointed() {
		t.Error("an aborted checkpoint must not leave the instance marked checkpointed")
	}
}

// The enabling change: with snapshot streams on, the supervisor owns the pipe
// rather than os/exec, which is the only arrangement where a write end still
// exists to hand back at restore.
func TestSupervisorOwnsChildStdioWhenSnapshotting(t *testing.T) {
	sup := checkpointSupervisor(t, "never")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := sup.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = sup.Stop(context.Background()) }()

	testutil.Eventually(t, func() bool {
		return len(sup.GetInstances()) == 1
	}, "instance starts", 10*time.Second)

	stream := sup.snapshotStreamFor(0)

	if stream == nil {
		t.Fatal("no supervisor-owned stdout stream; os/exec would own the pipe and the write end would be unreachable")
	}
	if stream.Inode() == 0 {
		t.Error("stream has no inode; CRIU's --inherit-fd has nothing to key on")
	}
}

// Without snapshotting, nothing changes: the ordinary path must stay exactly as
// it was, because every image that never sleeps still runs through it.
func TestSupervisorLeavesStdioAloneWithoutSnapshotting(t *testing.T) {
	sup := checkpointSupervisor(t, "never")
	sup.SetSnapshotStreams(false)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := sup.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = sup.Stop(context.Background()) }()

	testutil.Eventually(t, func() bool {
		return len(sup.GetInstances()) == 1
	}, "instance starts", 10*time.Second)

	if stream := sup.snapshotStreamFor(0); stream != nil {
		t.Error("snapshot streams should only be created when snapshotting is enabled")
	}
}

// What a lost checkpoint costs, made explicit. The processes were dumped, so
// they are genuinely gone; if the images cannot be loaded there is nothing to
// bring back and the only way to have a working service again is to start one.
// The container is not recreated and the pod is not deleted — cbox-init is
// still PID 1 and the pod still holds its IP, its volumes and its identity, so
// only the memory image is lost.
func TestSupervisorColdStartsAfterALostCheckpoint(t *testing.T) {
	sup := checkpointSupervisor(t, "always")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := sup.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = sup.Stop(context.Background()) }()

	testutil.Eventually(t, func() bool {
		return len(sup.GetInstances()) == 1 && sup.GetState() == StateRunning
	}, "process reaches running", 10*time.Second)

	originalPID := sup.GetInstances()[0].PID

	sup.BeginCheckpoint()
	if err := syscall.Kill(originalPID, syscall.SIGKILL); err != nil {
		t.Fatalf("simulating the dump: %v", err)
	}
	testutil.Eventually(t, func() bool { return sup.IsCheckpointed() }, "instance reports checkpointed", 10*time.Second)

	recovered, err := sup.RecoverCheckpointed(ctx)
	if err != nil {
		t.Fatalf("RecoverCheckpointed: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}

	testutil.Eventually(t, func() bool {
		instances := sup.GetInstances()
		return len(instances) == 1 && instances[0].PID != originalPID && instances[0].State == string(StateRunning)
	}, "a fresh process replaces the lost one", 10*time.Second)

	if sup.IsCheckpointed() {
		t.Error("a cold-started instance must not still look checkpointed")
	}

	// And it is an ordinary supervised process again: the next exit is a crash,
	// not another intended checkpoint.
	freshPID := sup.GetInstances()[0].PID
	if err := syscall.Kill(freshPID, syscall.SIGKILL); err != nil {
		t.Fatalf("killing the fresh process: %v", err)
	}
	testutil.Eventually(t, func() bool {
		instances := sup.GetInstances()
		return len(instances) == 1 && instances[0].PID != freshPID
	}, "the cold-started process is supervised normally", 15*time.Second)
}

// Nothing checkpointed means nothing to cold start. Saying so beats quietly
// restarting a healthy workload that was never asleep.
func TestSupervisorColdStartIgnoresRunningInstances(t *testing.T) {
	sup := checkpointSupervisor(t, "never")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := sup.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = sup.Stop(context.Background()) }()

	testutil.Eventually(t, func() bool {
		return len(sup.GetInstances()) == 1 && sup.GetState() == StateRunning
	}, "process reaches running", 10*time.Second)

	pid := sup.GetInstances()[0].PID

	recovered, err := sup.RecoverCheckpointed(ctx)
	if err != nil {
		t.Fatalf("RecoverCheckpointed: %v", err)
	}
	if recovered != 0 {
		t.Errorf("recovered = %d, want nothing touched", recovered)
	}
	if got := sup.GetInstances()[0].PID; got != pid {
		t.Errorf("a running process was restarted: pid %d became %d", pid, got)
	}
}

// The session is the missing half of the process group. Without it CRIU
// classifies the dump as a shell job and demands --shell-job, and a shell-job
// restore does not outlive the process that performed it — the workload comes
// back and immediately disappears.
func TestSnapshottedChildGetsItsOwnSession(t *testing.T) {
	sup := checkpointSupervisor(t, "never")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := sup.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = sup.Stop(context.Background()) }()

	testutil.Eventually(t, func() bool { return len(sup.GetInstances()) == 1 }, "instance starts", 10*time.Second)

	pid := sup.GetInstances()[0].PID
	sid, err := syscall.Getsid(pid)
	if err != nil {
		t.Fatalf("Getsid: %v", err)
	}
	if sid != pid {
		t.Errorf("session id = %d for pid %d; a snapshotted child must lead its own session", sid, pid)
	}
}
