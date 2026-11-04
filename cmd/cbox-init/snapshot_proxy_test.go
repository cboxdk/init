package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotProxyServesWithoutAgent(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "awake") }))
	defer upstream.Close()
	reserve, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reserve.Addr().String()
	_ = reserve.Close()
	t.Setenv(upstreamEnv, upstream.Listener.Addr().String())
	t.Setenv(listenEnv, address)
	t.Setenv("CBOX_SNAPSHOT_SOCKET", filepath.Join(t.TempDir(), "missing.sock"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if c := startWarmTier(ctx, nil, slog.New(slog.NewTextHandler(io.Discard, nil))); c != nil {
		t.Fatal("missing agent enabled sleep")
	}
	client := &http.Client{Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Get("http://" + address)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "awake" {
		t.Fatalf("fallback response %q: %v", body, err)
	}
	cancel()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, 50*time.Millisecond)
		if err != nil {
			return
		}
		_ = conn.Close()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("proxy listener survived cancellation")
}

func TestObservationProxyRejectsNonLoopbackAddresses(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, address := range []string{":9002", "0.0.0.0:9002", "[::]:9002", "192.0.2.1:9002", "localhost:9002", "invalid"} {
		if err := startObservationProxy(context.Background(), nil, address, log); err == nil {
			t.Errorf("unsafe observation address %q accepted", address)
		}
	}
}
