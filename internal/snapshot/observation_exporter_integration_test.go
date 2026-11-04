//go:build integration

package snapshot

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// The optional binary is built separately, so init does not acquire an exporter
// dependency or silently test a different revision than the operator selected.
func startObservationExporter(t *testing.T, upstream string) func(bool) {
	t.Helper()
	binary := os.Getenv("CBOX_TEST_FPM_EXPORTER_BINARY")
	if binary == "" {
		t.Log("exporter binary not selected; only shared-library transport is verified")
		return func(bool) {}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "exporter.yaml")
	contents := fmt.Sprintf(`phpfpm:
  enabled: true
  autodiscover: false
  pools:
    - name: www
      socket: tcp://%s
      status_path: /status
      timeout: 1s
monitor:
  listen_addr: %s
  scrape_timeout: 3s
`, upstream, address)
	if err := os.WriteFile(config, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(dir, "exporter.log"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(ctx, binary, "serve", "--config", config)
	command.Stdout, command.Stderr = log, log
	if err := command.Start(); err != nil {
		cancel()
		_ = log.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = command.Wait()
		_ = log.Close()
		if t.Failed() {
			content, _ := os.ReadFile(log.Name())
			t.Logf("exporter output: %s", content)
		}
	})
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}}
	t.Cleanup(client.CloseIdleConnections)
	// This reserved-port handoff can race another local process; an occupied port
	// fails the test rather than widening the listening address.
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("exporter did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}
	return func(awake bool) {
		t.Helper()
		response, err := client.Get("http://" + address + "/metrics")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("exporter HTTP status: %d", response.StatusCode)
		}
		parser := expfmt.NewTextParser(model.UTF8Validation)
		families, err := parser.TextToMetricFamilies(io.LimitReader(response.Body, 1<<20))
		if err != nil {
			t.Fatal(err)
		}
		gauge := func(name string, expected float64) {
			t.Helper()
			family := families[name]
			if family == nil || len(family.Metric) != 1 || family.Metric[0].Gauge == nil || family.Metric[0].Gauge.GetValue() != expected {
				t.Fatalf("awake=%t: expected %s=%v, got %v", awake, name, expected, family)
			}
		}
		if awake {
			gauge("phpfpm_scrape_success", 1)
			gauge("phpfpm_opcache_scrape_success", 1)
			gauge("phpfpm_opcache_enabled", 1)
			gauge("phpfpm_total_processes", 2)
			for _, name := range []string{"phpfpm_listen_queue", "phpfpm_opcache_used_memory_bytes", "phpfpm_opcache_hits_total"} {
				if families[name] == nil {
					t.Fatalf("missing real exporter metric %s", name)
				}
			}
		} else {
			gauge("phpfpm_scrape_success", 0)
			for _, name := range []string{"phpfpm_total_processes", "phpfpm_opcache_enabled", "phpfpm_opcache_used_memory_bytes", "phpfpm_opcache_hits_total"} {
				if families[name] != nil {
					t.Fatalf("sleeping exporter emitted unavailable measurement %s", name)
				}
			}
		}
	}
}
