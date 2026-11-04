package metrics

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestCgroupCPUAccounting(t *testing.T) {
	for _, tt := range []struct {
		name, membership, stat string
		missing                string
		valid                  bool
		seconds                float64
	}{
		{name: "includes complete cgroup usage", membership: "0::/\n", stat: "usage_usec 1234567\nuser_usec 1000000\nsystem_usec 234567\n", valid: true, seconds: 1.234567},
		{name: "zero is valid", membership: "0::/", stat: "usage_usec 0", valid: true},
		{name: "host subtree not guessed", membership: "0::/system.slice/service", stat: "usage_usec 100"},
		{name: "v1 not guessed", membership: "2:cpu:/", stat: "usage_usec 100"},
		{name: "missing membership", missing: "/proc/self/cgroup"},
		{name: "not unified mount", membership: "0::/", stat: "usage_usec 100", missing: "/sys/fs/cgroup/cgroup.controllers"},
		{name: "missing accounting", membership: "0::/", missing: "/sys/fs/cgroup/cpu.stat"},
		{name: "missing usage", membership: "0::/", stat: "user_usec 1"},
		{name: "negative", membership: "0::/", stat: "usage_usec -1"},
		{name: "overflow", membership: "0::/", stat: "usage_usec 18446744073709551616"},
		{name: "fraction", membership: "0::/", stat: "usage_usec 1.5"},
		{name: "duplicate", membership: "0::/", stat: "usage_usec 1\nusage_usec 2"},
		{name: "truncated", membership: "0::/", stat: "usage_usec"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := newCgroupCollector()
			c.readFile = func(path string) ([]byte, error) {
				if path == tt.missing {
					return nil, os.ErrNotExist
				}
				switch path {
				case "/proc/self/cgroup":
					return []byte(tt.membership), nil
				case "/sys/fs/cgroup/cgroup.controllers":
					return []byte("cpu memory"), nil
				case "/sys/fs/cgroup/cpu.stat":
					return []byte(tt.stat), nil
				default:
					t.Fatalf("unexpected read: %s", path)
					return nil, os.ErrNotExist
				}
			}
			r := prometheus.NewRegistry()
			r.MustRegister(c)
			families, err := r.Gather()
			if err != nil {
				t.Fatal(err)
			}
			wantCount := 1
			if tt.valid {
				wantCount = 2
			}
			if len(families) != wantCount {
				t.Fatalf("got %d metric families, want %d", len(families), wantCount)
			}
			for _, family := range families {
				metric := family.Metric[0]
				switch family.GetName() {
				case "cbox_init_cgroup_cpu_accounting_up":
					want := float64(0)
					if tt.valid {
						want = 1
					}
					if metric.GetGauge().GetValue() != want {
						t.Fatalf("up = %v", metric.GetGauge().GetValue())
					}
				case "cbox_init_cgroup_cpu_usage_seconds_total":
					if metric.Counter == nil || metric.GetCounter().GetValue() != tt.seconds {
						t.Fatalf("CPU counter = %v", metric)
					}
				}
			}
		})
	}
}

func TestCgroupCPUAccountingReadsEveryScrape(t *testing.T) {
	c := newCgroupCollector()
	stat := "usage_usec 1000000"
	c.readFile = func(path string) ([]byte, error) {
		switch path {
		case "/proc/self/cgroup":
			return []byte("0::/"), nil
		case "/sys/fs/cgroup/cgroup.controllers":
			return []byte("cpu"), nil
		default:
			return []byte(stat), nil
		}
	}
	r := prometheus.NewRegistry()
	r.MustRegister(c)
	for _, next := range []struct {
		stat    string
		seconds float64
	}{
		{"usage_usec 1000000", 1},
		{"usage_usec 2500000", 2.5},
		{"usage_usec 0", 0}, // A new cgroup resets; never manufacture continuity.
	} {
		stat = next.stat
		families, err := r.Gather()
		if err != nil {
			t.Fatal(err)
		}
		if got := families[1].Metric[0].GetCounter().GetValue(); got != next.seconds {
			t.Fatalf("counter = %v, want %v", got, next.seconds)
		}
	}
}

func TestMetricsServerIncludesCgroupAccounting(t *testing.T) {
	server := NewServer(0, "/metrics", nil, nil, slog.Default())
	families, err := server.gatherer().Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() == "cbox_init_cgroup_cpu_accounting_up" {
			return
		}
	}
	t.Fatal("metrics server omitted cgroup accounting health")
}

// Run explicitly inside a private cgroup v2 container; never silently skip
// missing accounting once this acceptance gate has been requested.
func TestCgroupCPUAccountingKernel(t *testing.T) {
	if os.Getenv("CBOX_TEST_CGROUP_KERNEL") != "1" {
		t.Skip("requires explicit private cgroup v2 container test")
	}
	collector := newCgroupCollector()
	before, err := collector.readUsage()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(50 * time.Millisecond)
	for time.Now().Before(deadline) {
	}
	after, err := collector.readUsage()
	if err != nil {
		t.Fatal(err)
	}
	if after <= before {
		t.Fatalf("CPU accounting did not advance: %d -> %d", before, after)
	}
	t.Logf("kernel CPU delta: %d microseconds", after-before)
}
