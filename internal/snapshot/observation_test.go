package snapshot

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestObservationPinsWorkloadWithoutRefreshingIdleClock(t *testing.T) {
	upstream := echoUpstream(t)
	activity := NewActivity()
	activity.last = time.Now().Add(-time.Hour)
	var wakes atomic.Int32
	proxy := NewProxy(ProxyOptions{Upstream: upstream.Addr().String(), Activity: activity, Log: quietLogger(),
		Waker: WakerFunc(func(context.Context) error { wakes.Add(1); return nil })})
	control := &fakeControl{}
	coordinator := NewCoordinator(CoordinatorOptions{Proxy: proxy, Control: control, Workload: &fakeWorkload{}, Idle: time.Minute, Log: quietLogger()})
	client, accepted := net.Pipe()
	defer func() { _ = client.Close() }()
	finished := make(chan struct{})
	go func() { proxy.handleObservation(context.Background(), accepted); close(finished) }()
	if err := client.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 4)
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatal(err)
	}
	if string(reply) != "ping" {
		t.Fatalf("reply = %q", reply)
	}
	coordinator.maybeSleep(context.Background())
	if control.checkpoints != 0 {
		t.Fatal("checkpoint started during an observation")
	}
	_ = client.Close()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("observation did not finish")
	}
	if !activity.IdleFor(time.Minute) {
		t.Fatal("observation refreshed customer idle time")
	}
	coordinator.maybeSleep(context.Background())
	if control.checkpoints != 1 || !proxy.Asleep() {
		t.Fatal("workload did not sleep immediately after the probe closed")
	}
	if wakes.Load() != 0 {
		t.Fatal("observation woke the workload")
	}
}

func TestObservationDuringCheckpointIsRefusedWithoutWake(t *testing.T) {
	activity := NewActivity()
	activity.last = time.Now().Add(-time.Hour)
	var wakes atomic.Int32
	proxy := NewProxy(ProxyOptions{Upstream: "127.0.0.1:1", Activity: activity, Log: quietLogger(),
		Waker: WakerFunc(func(context.Context) error { wakes.Add(1); return nil })})
	started, release, slept := make(chan struct{}), make(chan struct{}), make(chan struct{})
	control := &fakeControl{checkpointer: func() error { close(started); <-release; return nil }}
	coordinator := NewCoordinator(CoordinatorOptions{Proxy: proxy, Control: control, Workload: &fakeWorkload{}, Idle: time.Minute, Log: quietLogger()})
	go func() { coordinator.maybeSleep(context.Background()); close(slept) }()
	defer func() { close(release); <-slept }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("checkpoint did not start")
	}
	client, accepted := net.Pipe()
	defer func() { _ = client.Close() }()
	finished := make(chan struct{})
	go func() { proxy.handleObservation(context.Background(), accepted); close(finished) }()
	if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("expected refused connection, got %v", err)
	}
	<-finished
	if wakes.Load() != 0 {
		t.Fatal("observation requested restore during checkpoint")
	}
	if open, inFlight := activity.Counts(); open != 0 || inFlight != 0 {
		t.Fatalf("observation leaked activity: %d/%d", open, inFlight)
	}
}

func TestObservationActivityCloseIsIdempotent(t *testing.T) {
	activity := NewActivity()
	activity.last = time.Now().Add(-time.Hour)
	done := activity.Observed()
	if activity.IdleFor(time.Minute) {
		t.Fatal("active observation considered idle")
	}
	done()
	done()
	if open, _ := activity.Counts(); open != 0 {
		t.Fatalf("open = %d", open)
	}
	if !activity.IdleFor(time.Minute) {
		t.Fatal("probe moved the idle clock")
	}
}

func TestObservationListenerClosesActiveProbeOnCancellation(t *testing.T) {
	upstream := echoUpstream(t)
	activity := NewActivity()
	proxy := NewProxy(ProxyOptions{Upstream: upstream.Addr().String(), Activity: activity, Log: quietLogger()})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- proxy.ServeObservations(ctx, listener) }()
	client, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(client, make([]byte, 4)); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("listener did not stop with active observation")
	}
	if open, inFlight := activity.Counts(); open != 0 || inFlight != 0 {
		t.Fatalf("cancelled probe leaked activity: %d/%d", open, inFlight)
	}
}
