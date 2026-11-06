//go:build integration

package snapshot

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cboxdk/fcgx"
	"github.com/cboxdk/phpfpm"
)

// This negative qualification case records why transport-idle alone cannot
// authorize checkpointing PHP. A successful FastCGI response can precede PHP
// shutdown work; production observation enablement still needs quiescence proof.
func TestObservationResponseDoesNotProveFPMQuiescence(t *testing.T) {
	upstream := os.Getenv("CBOX_TEST_FPM_UPSTREAM")
	if upstream == "" {
		t.Fatal("run tests/integration/e2e-fpm-observation.sh")
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(directory, "after-response.php")
	if err := os.WriteFile(script, []byte(`<?php
ignore_user_abort(true);
echo "response-complete";
fastcgi_finish_request();
usleep(5000000);
`), 0644); err != nil {
		t.Fatal(err)
	}
	activity := NewActivity()
	activity.last = time.Now().Add(-time.Hour)
	proxy := NewProxy(ProxyOptions{Upstream: upstream, Activity: activity, Log: quietLogger()})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- proxy.ServeObservations(ctx, listener) }()
	defer func() {
		cancel()
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	client, err := fcgx.DialContext(ctx, "tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	response, err := client.Get(ctx, map[string]string{"SCRIPT_FILENAME": script, "SCRIPT_NAME": "/after-response.php", "REMOTE_ADDR": "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	_ = client.Close()
	if err != nil || string(body) != "response-complete" {
		t.Fatalf("response = %q, error = %v", body, err)
	}
	deadline := time.Now().Add(time.Second)
	for !activity.IdleFor(time.Minute) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !activity.IdleFor(time.Minute) {
		t.Fatal("expected completed transport to be idle")
	}
	target := phpfpm.Target{Name: "www", Socket: "tcp://" + upstream, StatusPath: "/fpm-status", Timeout: time.Second}
	running := func() bool {
		t.Helper()
		pool, err := phpfpm.ReadStatus(ctx, target)
		if err != nil {
			t.Fatalf("FPM status: %v", err)
		}
		for _, process := range pool.Processes {
			if process.Script == script && process.State != "Idle" {
				return true
			}
		}
		return false
	}
	if !running() {
		t.Fatal("expected actual PHP work after the FastCGI response and transport close")
	}
	control := &fakeControl{}
	workload := &fakeWorkload{}
	guard, err := CboxFPMQuiescence("www", upstream)
	if err != nil {
		t.Fatal(err)
	}
	coordinator := NewCoordinator(CoordinatorOptions{Proxy: proxy, Control: control, Workload: workload, Idle: time.Minute, Log: quietLogger(), Quiesce: guard})
	coordinator.maybeSleep(ctx)
	if err := coordinator.Sleep(ctx); !errors.Is(err, ErrWorkloadBusy) {
		t.Fatalf("manual sleep must also refuse post-response PHP work: %v", err)
	}
	if control.checkpoints != 0 || workload.begun.Load() != 0 || proxy.Asleep() {
		t.Fatal("quiescence gate allowed checkpoint during post-response PHP work")
	}
	t.Log("transport idle while PHP continues; native FPM gate refuses checkpoint")
	// Let the actual request finish before the runner removes its fixture.
	deadline = time.Now().Add(8 * time.Second)
	for running() {
		if time.Now().After(deadline) {
			t.Fatal("post-response PHP did not finish")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Native status requests can briefly overlap while the serving worker returns
	// to Idle. The coordinator normally retries on its poll; do not require the
	// first observation after script completion to be quiescent.
	deadline = time.Now().Add(2 * time.Second)
	for control.checkpoints == 0 && time.Now().Before(deadline) {
		coordinator.maybeSleep(ctx)
		if control.checkpoints == 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if control.checkpoints != 1 || !proxy.Asleep() {
		t.Fatal("quiescence gate did not admit checkpoint after actual PHP finished")
	}
}
