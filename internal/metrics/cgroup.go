package metrics

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// cgroupCollector reads the kernel's cumulative CPU accounting at scrape time.
// It supports a cgroup v2 namespace rooted at this process's cgroup, as used
// by containers. It deliberately does not guess paths in a host cgroup tree.
type cgroupCollector struct {
	readFile func(string) ([]byte, error)
	usage    *prometheus.Desc
	up       *prometheus.Desc
}

func newCgroupCollector() *cgroupCollector {
	return &cgroupCollector{
		readFile: os.ReadFile,
		usage: prometheus.NewDesc("cbox_init_cgroup_cpu_usage_seconds_total",
			"Cumulative CPU seconds consumed by the rooted cgroup v2 subtree", nil, nil),
		up: prometheus.NewDesc("cbox_init_cgroup_cpu_accounting_up",
			"Whether rooted cgroup v2 CPU accounting was read successfully", nil, nil),
	}
}

func (c *cgroupCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.usage
	ch <- c.up
}

func (c *cgroupCollector) Collect(ch chan<- prometheus.Metric) {
	usage, err := c.readUsage()
	if err != nil {
		ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0)
		return
	}
	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 1)
	ch <- prometheus.MustNewConstMetric(c.usage, prometheus.CounterValue, float64(usage)/1e6)
}

func (c *cgroupCollector) readUsage() (uint64, error) {
	membership, err := c.readFile("/proc/self/cgroup")
	if err != nil {
		return 0, fmt.Errorf("read cgroup membership: %w", err)
	}
	if strings.TrimSpace(string(membership)) != "0::/" {
		return 0, fmt.Errorf("CPU accounting requires a rooted cgroup v2 namespace")
	}
	// The controllers file identifies a unified hierarchy. A cpu.stat file
	// alone is not evidence that this is the expected cgroup v2 mount.
	if _, err := c.readFile("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		return 0, fmt.Errorf("read cgroup v2 controllers: %w", err)
	}
	stat, err := c.readFile("/sys/fs/cgroup/cpu.stat")
	if err != nil {
		return 0, fmt.Errorf("read cgroup CPU accounting: %w", err)
	}
	var usage uint64
	found := false
	for _, line := range strings.Split(string(stat), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "usage_usec" {
			continue
		}
		if found || len(fields) != 2 {
			return 0, fmt.Errorf("invalid usage_usec field")
		}
		usage, err = strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse usage_usec: %w", err)
		}
		found = true
	}
	if !found {
		return 0, fmt.Errorf("missing usage_usec field")
	}
	return usage, nil
}
