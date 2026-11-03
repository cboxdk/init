package snapshot

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
)

func TestClosedChannelBeforeSendIsNotAnUncertainOperation(t *testing.T) {
	client, err := Dial(silentAgent(t))
	if err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	err = client.Restore(context.Background())
	if !errors.Is(err, ErrControlLost) || errors.Is(err, ErrOperationUncertain) {
		t.Fatalf("zero-byte send should permit ordinary lost-agent recovery: %v", err)
	}
}

func TestUncertainCheckpointFencesWithoutAbortingOrColdStarting(t *testing.T) {
	workload := &fakeWorkload{}
	control := &fakeControl{checkpointer: func() error { return &AgentError{Reason: "operation_uncertain"} }}
	coordinator, proxy := newTestCoordinator(t, control, workload)
	var fences atomic.Int32
	coordinator.fence = func(error) { fences.Add(1) }
	coordinator.maybeSleep(context.Background())
	if fences.Load() != 1 || !proxy.Asleep() || coordinator.Disabled() == "" {
		t.Fatal("uncertain dump did not fence and retain the traffic gate")
	}
	if workload.aborted.Load() != 0 || workload.completed.Load() != 0 || workload.coldStarts.Load() != 0 {
		t.Fatal("unknown dump outcome was treated as an ordinary failure or completion")
	}
	if err := coordinator.Wake(context.Background()); !errors.Is(err, ErrOperationUncertain) {
		t.Fatalf("later wake must remain fenced: %v", err)
	}
	if fences.Load() != 1 || control.restores != 0 {
		t.Fatal("fenced workload attempted another lifecycle operation")
	}
}

func TestUncertainRestoreFencesOnceAcrossConcurrentWakeRequests(t *testing.T) {
	workload := &fakeWorkload{}
	control := &fakeControl{restorer: func() error { return ErrOperationUncertain }}
	coordinator, proxy := newTestCoordinator(t, control, workload)
	proxy.SetAsleep(true)
	var fences atomic.Int32
	coordinator.fence = func(error) { fences.Add(1) }
	var callers sync.WaitGroup
	for range 16 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			if err := coordinator.Wake(context.Background()); !errors.Is(err, ErrOperationUncertain) {
				t.Errorf("wake outcome = %v, want uncertain", err)
			}
		}()
	}
	callers.Wait()
	if fences.Load() != 1 || control.restores != 1 || !proxy.Asleep() {
		t.Fatal("concurrent wakes did not retain one fencing decision")
	}
	if workload.coldStarts.Load() != 0 || workload.ended.Load() != 0 {
		t.Fatal("uncertain restore started or adopted a process tree")
	}
}

func TestMissingFenceCallbackStillCannotStartADuplicateTree(t *testing.T) {
	workload := &fakeWorkload{}
	coordinator, proxy := newTestCoordinator(t, &fakeControl{
		restorer: func() error { return ErrOperationUncertain },
	}, workload)
	if err := wakeAsleep(coordinator, proxy, context.Background()); !errors.Is(err, ErrOperationUncertain) {
		t.Fatal(err)
	}
	if workload.coldStarts.Load() != 0 || !proxy.Asleep() {
		t.Fatal("absent fencing callback must fail closed")
	}
}

func TestUnexpectedReplyCannotAcknowledgeASentRestore(t *testing.T) {
	path := shortSocketPath(t)
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = bufio.NewReader(conn).ReadString('\n')
		_, _ = fmt.Fprintln(conn, "UNKNOWN state=restored")
	}()
	client, err := Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.Restore(context.Background()); !errors.Is(err, ErrOperationUncertain) {
		t.Fatalf("unexpected verb was treated as a definitive result: %v", err)
	}
}
