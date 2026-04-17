package logger

import (
	"testing"
	"time"
)

func TestLogBroadcaster_SubscribeReceivesEntries(t *testing.T) {
	b := NewLogBroadcaster()
	ch, unsub := b.Subscribe("")
	defer unsub()

	entry := LogEntry{
		Timestamp:   time.Now(),
		ProcessName: "nginx",
		InstanceID:  "nginx-0",
		Stream:      "stdout",
		Message:     "hello",
		Level:       "info",
	}
	b.Broadcast(entry)

	select {
	case got := <-ch:
		if got.Message != "hello" || got.ProcessName != "nginx" {
			t.Errorf("unexpected entry: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for entry")
	}
}

func TestLogBroadcaster_FilterByProcess(t *testing.T) {
	b := NewLogBroadcaster()
	ch, unsub := b.Subscribe("nginx")
	defer unsub()

	b.Broadcast(LogEntry{ProcessName: "php-fpm", Message: "wrong"})
	b.Broadcast(LogEntry{ProcessName: "nginx", Message: "correct"})

	select {
	case got := <-ch:
		if got.Message != "correct" {
			t.Errorf("expected 'correct', got %q", got.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for filtered entry")
	}
}

func TestLogBroadcaster_UnsubscribeStopsDelivery(t *testing.T) {
	b := NewLogBroadcaster()
	ch, unsub := b.Subscribe("")
	unsub()

	b.Broadcast(LogEntry{Message: "after-unsub"})

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("received entry after unsubscribe")
		}
	case <-time.After(100 * time.Millisecond):
	}
}

func TestLogBroadcaster_MultipleSubscribers(t *testing.T) {
	b := NewLogBroadcaster()
	ch1, unsub1 := b.Subscribe("")
	defer unsub1()
	ch2, unsub2 := b.Subscribe("")
	defer unsub2()

	b.Broadcast(LogEntry{Message: "both"})

	for _, ch := range []<-chan LogEntry{ch1, ch2} {
		select {
		case got := <-ch:
			if got.Message != "both" {
				t.Errorf("unexpected: %q", got.Message)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out")
		}
	}
}

func TestLogBroadcaster_SlowSubscriberDropsEntries(t *testing.T) {
	b := NewLogBroadcaster()
	ch, unsub := b.Subscribe("")
	defer unsub()

	for i := 0; i < 300; i++ {
		b.Broadcast(LogEntry{Message: "flood"})
	}

	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	if count > 256 {
		t.Errorf("got %d entries, expected at most 256", count)
	}
}
