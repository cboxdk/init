package snapshot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cboxdk/phpfpm"
)

func TestFPMQuiescenceRequiresCompleteIdleNativeStatus(t *testing.T) {
	target := phpfpm.Target{Name: "www", StatusPath: "/status"}
	valid := func() phpfpm.Pool {
		return phpfpm.Pool{Name: "www", TotalProcesses: 2, ActiveProcesses: 1, IdleProcesses: 1, Processes: []phpfpm.PoolProcess{
			{PID: 1, State: "Running", RequestMethod: "GET", RequestURI: "/status?json&full"}, {PID: 2, State: "Idle"},
		}}
	}
	if err := validateIdleFPMPool(valid(), target); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*phpfpm.Pool){
		"foreign pool":        func(p *phpfpm.Pool) { p.Name = "WWW" },
		"missing workers":     func(p *phpfpm.Pool) { p.Processes = p.Processes[:1] },
		"queued work":         func(p *phpfpm.Pool) { p.ListenQueue = 1 },
		"invalid queue":       func(p *phpfpm.Pool) { p.ListenQueue = -1 },
		"empty pool":          func(p *phpfpm.Pool) { p.TotalProcesses = 0 },
		"unknown worker":      func(p *phpfpm.Pool) { p.Processes[1].State = "Unknown" },
		"post response work":  func(p *phpfpm.Pool) { p.Processes[1].State = "Finishing" },
		"two running workers": func(p *phpfpm.Pool) { p.Processes[1] = p.Processes[0]; p.Processes[1].PID = 2 },
		"active mismatch":     func(p *phpfpm.Pool) { p.ActiveProcesses = 0 },
		"idle mismatch":       func(p *phpfpm.Pool) { p.IdleProcesses = 0 },
		"duplicate pid":       func(p *phpfpm.Pool) { p.Processes[1].PID = 1 },
		"missing pid":         func(p *phpfpm.Pool) { p.Processes[1].PID = 0 },
		"application path":    func(p *phpfpm.Pool) { p.Processes[0].RequestURI = "/status-other?json&full" },
		"wrong method":        func(p *phpfpm.Pool) { p.Processes[0].RequestMethod = "POST" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			pool := valid()
			mutate(&pool)
			if err := validateIdleFPMPool(pool, target); err == nil {
				t.Fatal("unsafe status accepted")
			}
		})
	}
	if err := CheckFPMQuiescence(context.Background(), nil); err == nil {
		t.Fatal("empty inventory accepted")
	}
	if err := CheckFPMQuiescence(context.Background(), []phpfpm.Target{{}}); err == nil {
		t.Fatal("unnamed pool accepted")
	}
}

func TestCheckpointQuiescenceRefusalAndRecovery(t *testing.T) {
	activity := NewActivity()
	activity.last = time.Now().Add(-time.Hour)
	proxy := NewProxy(ProxyOptions{Activity: activity, Log: quietLogger()})
	control, workload := &fakeControl{}, &fakeWorkload{}
	busy := true
	c := NewCoordinator(CoordinatorOptions{Proxy: proxy, Control: control, Workload: workload, Idle: time.Minute, Log: quietLogger(), Quiesce: func(ctx context.Context) error {
		if !proxy.Asleep() {
			t.Fatal("admission was open during check")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("check has no deadline")
		}
		if busy {
			return errors.New("still finishing")
		}
		return nil
	}})
	c.maybeSleep(context.Background())
	if proxy.Asleep() || control.checkpoints != 0 || workload.begun.Load() != 0 || c.Disabled() != "" {
		t.Fatal("refused check changed workload lifecycle")
	}
	busy = false
	c.maybeSleep(context.Background())
	if !proxy.Asleep() || control.checkpoints != 1 {
		t.Fatal("idle workload failed to checkpoint")
	}
}

func TestCheckpointRechecksCustomerArrivalAndCancellationAfterQuiescence(t *testing.T) {
	for _, arrival := range []bool{true, false} {
		t.Run(map[bool]string{true: "arrival", false: "cancellation"}[arrival], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			activity := NewActivity()
			activity.last = time.Now().Add(-time.Hour)
			proxy := NewProxy(ProxyOptions{Activity: activity, Log: quietLogger()})
			control, workload := &fakeControl{}, &fakeWorkload{}
			var closeConnection func()
			c := NewCoordinator(CoordinatorOptions{Proxy: proxy, Control: control, Workload: workload, Idle: time.Minute, Log: quietLogger(), Quiesce: func(context.Context) error {
				if arrival {
					closeConnection = activity.Opened()
				} else {
					cancel()
				}
				return nil
			}})
			c.maybeSleep(ctx)
			if closeConnection != nil {
				closeConnection()
			}
			if proxy.Asleep() || control.checkpoints != 0 || workload.begun.Load() != 0 {
				t.Fatal("checkpoint proceeded after admission changed")
			}
		})
	}
}

func TestCboxFPMQuiescenceRejectsUnqualifiedTargets(t *testing.T) {
	for _, target := range []struct{ pool, upstream string }{
		{"", "127.0.0.1:9000"}, {"other", "127.0.0.1:9000"}, {"www", "localhost:9000"},
		{"www", "10.0.0.1:9000"}, {"www", "127.0.0.1:0"}, {"www", "/run/php.sock"},
	} {
		if guard, err := CboxFPMQuiescence(target.pool, target.upstream); err == nil || guard != nil {
			t.Errorf("accepted unqualified target %+v", target)
		}
	}
}
