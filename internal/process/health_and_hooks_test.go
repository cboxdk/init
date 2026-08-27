package process

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHTTPHealthCheckHonorsConfiguredTimeout: the checker built its own
// http.Client with a hardcoded 5s Timeout, and whichever of that and the
// context deadline was shorter won. Any health_check.timeout above 5 was dead
// config — which made the documented remedy for a slow endpoint ("increase
// timeout") inert, so a /health that takes 6-10s under load flapped into a
// SIGKILL/restart loop no matter what the config said.
func TestHTTPHealthCheckHonorsConfiguredTimeout(t *testing.T) {
	// Structural first, so the invariant is asserted even in -short mode: a
	// client-level Timeout would silently outrank every configured value.
	if httpHealthClient.Timeout != 0 {
		t.Fatalf("the health-check client has its own %v Timeout; it overrides "+
			"health_check.timeout whenever it is shorter", httpHealthClient.Timeout)
	}

	if testing.Short() {
		t.Skip("the behavioural half has to outlast the old 5s cap")
	}

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(6 * time.Second):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()

	checker := &HTTPHealthChecker{url: slow.URL, expectedStatus: http.StatusOK}

	// A configured timeout ABOVE the old 5s cap must actually be waited out.
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	start := time.Now()
	err := checker.Check(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("check failed after %v with a 12s budget: %v", elapsed, err)
	}
	if elapsed < 5500*time.Millisecond {
		t.Errorf("check returned after %v; the endpoint takes 6s, so something "+
			"shorter than the configured timeout cut it off", elapsed)
	}
}

// TestHTTPHealthCheckStillHonorsAShortTimeout: the context remains the bound.
func TestHTTPHealthCheckStillHonorsAShortTimeout(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()

	checker := &HTTPHealthChecker{url: slow.URL, expectedStatus: http.StatusOK}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	if err := checker.Check(ctx); err == nil {
		t.Fatal("a 300ms budget against a 5s endpoint succeeded")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("check took %v to honour a 300ms timeout", elapsed)
	}
}

// TestPostStopHooksGetTheirOwnBudget: post-stop hooks ran on the context that
// had just bounded the process teardown. When stopping the workload consumed
// the global shutdown_timeout — exactly the case the timeout exists for — that
// context was already DeadlineExceeded, so every post-stop hook failed before
// fork and its cleanup was silently skipped. Shutdown still returned nil.
func TestPostStopHooksGetTheirOwnBudget(t *testing.T) {
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer cancel()

	if err := expired.Err(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("test setup: context is %v", err)
	}

	// This is the derivation the shutdown path performs.
	postStop, cancelPostStop := context.WithTimeout(context.WithoutCancel(expired), postStopHookBudget)
	defer cancelPostStop()

	if err := postStop.Err(); err != nil {
		t.Fatalf("the post-stop context inherited the expired deadline (%v); "+
			"hooks would fail before fork and their cleanup would be skipped", err)
	}

	deadline, ok := postStop.Deadline()
	if !ok {
		t.Fatal("the post-stop context has no deadline; a hung hook would block the exit")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > postStopHookBudget+time.Second {
		t.Errorf("post-stop budget is %v, want about %v", remaining, postStopHookBudget)
	}
}
