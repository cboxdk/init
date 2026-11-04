//go:build integration

package snapshot

import (
	"context"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cboxdk/phpfpm"
)

// Runs against the real Cbox PHP image started by e2e-fpm-observation.sh.
// The coordinator control is fake: this verifies transport/idle behavior, not CRIU.
func TestObservationWithRealFPM(t *testing.T) {
	upstream := os.Getenv("CBOX_TEST_FPM_UPSTREAM")
	if upstream == "" {
		t.Fatal("run tests/integration/e2e-fpm-observation.sh")
	}
	activity := NewActivity()
	activity.last = time.Now().Add(-time.Hour)
	var wakes atomic.Int32
	proxy := NewProxy(ProxyOptions{Upstream: upstream, Activity: activity, Log: quietLogger(), Waker: WakerFunc(func(context.Context) error { wakes.Add(1); return nil })})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- proxy.ServeObservations(ctx, ln) }()
	defer func() {
		cancel()
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	defer phpfpm.RemoveOpcacheScript()
	target := phpfpm.Target{Name: "www", Socket: "tcp://" + ln.Addr().String(), StatusPath: "/status", Timeout: time.Second}
	// FPM startup can trail the container's published port.
	deadline := time.Now().Add(15 * time.Second)
	for {
		result := phpfpm.Scrape(ctx, target, quietLogger())
		if result.Err == nil && result.Result != nil {
			pool := result.Result.Pools["www"]
			if pool.TotalProcesses < 1 || pool.AcceptedConnections < 1 {
				t.Fatalf("invalid live pool counters: %+v", pool)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("FPM not ready: %v", result.Err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	probeCtx, stop := context.WithTimeout(ctx, 3*time.Second)
	cache, err := phpfpm.GetOpcacheStatus(probeCtx, target)
	stop()
	if err != nil {
		t.Fatal(err)
	}
	if cache == nil || !cache.Enabled {
		t.Fatal("did not observe enabled FPM OPcache")
	}
	checkExporter := startObservationExporter(t, ln.Addr().String())
	checkExporter(true)
	// The client receives its reply before the server necessarily drops its pin.
	deadline = time.Now().Add(time.Second)
	for !activity.IdleFor(time.Minute) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !activity.IdleFor(time.Minute) {
		t.Fatal("real FPM probes refreshed customer idle time or leaked activity")
	}
	control := &fakeControl{}
	coordinator := NewCoordinator(CoordinatorOptions{Proxy: proxy, Control: control, Workload: &fakeWorkload{}, Idle: time.Minute, Log: quietLogger()})
	coordinator.maybeSleep(ctx)
	if control.checkpoints != 1 || !proxy.Asleep() {
		t.Fatal("probes prevented idle transition")
	}
	// FPM deliberately stays running: a successful response would prove bypass.
	result := phpfpm.Scrape(ctx, target, quietLogger())
	if result.Err == nil {
		t.Fatal("sleeping proxy allowed an FPM scrape")
	}
	probeCtx, stop = context.WithTimeout(ctx, time.Second)
	_, err = phpfpm.GetOpcacheStatus(probeCtx, target)
	stop()
	if err == nil {
		t.Fatal("sleeping proxy allowed an OPcache probe")
	}
	checkExporter(false)
	if wakes.Load() != 0 {
		t.Fatal("monitoring requested a wake")
	}
	proxy.SetAsleep(false)
	result = phpfpm.Scrape(ctx, target, quietLogger())
	if result.Err != nil {
		t.Fatalf("observation did not recover after awake transition: %v", result.Err)
	}
	checkExporter(true)
}
