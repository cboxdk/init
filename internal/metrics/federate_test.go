package metrics

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func federateOutput(t *testing.T, f *Federator) string {
	t.Helper()
	var b strings.Builder
	f.Append(context.Background(), &b)
	return b.String()
}

func TestFederatorAppendsUpSource(t *testing.T) {
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "phpfpm_listen_queue{pool=\"www\"} 3\n")
	}))
	defer src.Close()

	f := NewFederator([]FederateSource{{Name: "fpm-exporter", URL: src.URL}}, testLogger())
	out := federateOutput(t, f)

	if !strings.Contains(out, `phpfpm_listen_queue{pool="www"} 3`) {
		t.Fatalf("federated body missing from output:\n%s", out)
	}
	if !strings.Contains(out, `cbox_init_federate_up{name="fpm-exporter"} 1`) {
		t.Fatalf("federate_up 1 missing:\n%s", out)
	}
}

func TestFederatorDownSourceDegrades(t *testing.T) {
	f := NewFederator([]FederateSource{{
		Name:    "dead",
		URL:     "http://127.0.0.1:1/metrics", // nothing listens on port 1
		Timeout: 200 * time.Millisecond,
	}}, testLogger())
	out := federateOutput(t, f)

	if !strings.Contains(out, `cbox_init_federate_up{name="dead"} 0`) {
		t.Fatalf("down source must contribute federate_up 0:\n%s", out)
	}
}

func TestFederatorCachesWithinTTL(t *testing.T) {
	var hits atomic.Int64
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		io.WriteString(w, "x_total 1\n")
	}))
	defer src.Close()

	f := NewFederator([]FederateSource{{Name: "cached", URL: src.URL, CacheTTL: time.Hour}}, testLogger())
	for range 5 {
		federateOutput(t, f)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("expected exactly 1 upstream fetch within the TTL, got %d", got)
	}
}

func TestFederatorStripsOpenMetricsEOF(t *testing.T) {
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "y_total 2\n# EOF\n")
	}))
	defer src.Close()

	f := NewFederator([]FederateSource{{Name: "om", URL: src.URL}}, testLogger())
	out := federateOutput(t, f)

	if strings.Contains(out, "# EOF") {
		t.Fatalf("OpenMetrics terminator must be stripped:\n%s", out)
	}
	if !strings.Contains(out, "y_total 2") {
		t.Fatalf("body lost while stripping EOF:\n%s", out)
	}
}

func TestFederatorRejectsOversizedBody(t *testing.T) {
	big := strings.Repeat("a_metric 1\n", (federateMaxBodyBytes/11)+2)
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, big)
	}))
	defer src.Close()

	f := NewFederator([]FederateSource{{Name: "huge", URL: src.URL}}, testLogger())
	out := federateOutput(t, f)

	if !strings.Contains(out, `cbox_init_federate_up{name="huge"} 0`) {
		t.Fatalf("oversized body must degrade to down:\n%s", out)
	}
}

func TestMetricsHandlerMergesExtraGatherer(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := prometheus.NewGauge(prometheus.GaugeOpts{Name: "fpm_tune_test_gauge", Help: "test"})
	g.Set(42)
	reg.MustRegister(g)

	s := NewServer(0, "/metrics", nil, nil, testLogger())
	s.AddGatherer(reg) // after construction, as serve.go does after Start

	rr := httptest.NewRecorder()
	s.metricsHandler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if !strings.Contains(rr.Body.String(), "fpm_tune_test_gauge 42") {
		t.Fatalf("extra gatherer not merged into scrape:\n%s", rr.Body.String())
	}
}

func TestMetricsHandlerWithFederation(t *testing.T) {
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "laravel_queue_size{queue=\"default\"} 7\n")
	}))
	defer src.Close()

	reg := prometheus.NewRegistry()
	g := prometheus.NewGauge(prometheus.GaugeOpts{Name: "fpm_tune_other_gauge", Help: "test"})
	g.Set(1)
	reg.MustRegister(g)

	s := NewServer(0, "/metrics", nil, nil, testLogger())
	s.AddGatherer(reg)
	s.SetFederator(NewFederator([]FederateSource{{Name: "app", URL: src.URL}}, testLogger()))

	rr := httptest.NewRecorder()
	s.metricsHandler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rr.Body.String()

	for _, want := range []string{
		"fpm_tune_other_gauge 1",                // merged gatherer
		`laravel_queue_size{queue="default"} 7`, // federated body
		`cbox_init_federate_up{name="app"} 1`,   // health of the source
		"go_goroutines",                         // default registry still present
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in federated scrape:\n%s", want, body)
		}
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("federated response must be plain text, got %q", ct)
	}
}
