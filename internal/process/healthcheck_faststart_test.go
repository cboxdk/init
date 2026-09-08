package process

import (
	"context"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/config"
)

// A service that starts listening shortly after the monitor starts must be
// discovered within the fast-start cadence, not after a full period. This was
// the single largest component of container cold start: php-fpm listened 50ms
// after the immediate first probe, and readiness waited the full 5s period.
func TestHealthMonitor_FastStartDiscovery(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Reserve a port, close it, and re-listen 600ms later - after the
	// monitor's immediate first probe has already failed.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	cfg := &config.HealthCheck{
		Type:             "tcp",
		Address:          addr,
		Period:           5,
		Timeout:          1,
		FailureThreshold: 3,
	}
	monitor, err := NewHealthMonitor("fast-start", cfg, logger)
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		time.Sleep(600 * time.Millisecond)
		l2, err := net.Listen("tcp", addr)
		if err != nil {
			return
		}
		defer l2.Close()
		time.Sleep(5 * time.Second)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	statusCh := monitor.Start(ctx)
	for status := range statusCh {
		if status.LastCheckSucceeded {
			elapsed := time.Since(start)
			// Old behavior: first success at ~period (5s). Fast-start must
			// find it within ~600ms + a couple of probe intervals.
			if elapsed > 2*time.Second {
				t.Fatalf("first success took %v; fast-start should discover readiness well before the 5s period", elapsed)
			}
			return
		}
	}
	t.Fatal("monitor channel closed without a successful check")
}
