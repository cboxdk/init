package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// populateInstance writes every per-instance series the supervisor produces for
// one instance: lifecycle (name=), resource (process=) and collection errors.
func populateInstance(processName, instanceID string) {
	RecordProcessStart(processName, instanceID, float64(time.Now().Unix()))
	RecordProcessStop(processName, instanceID, 137)
	UpdatePrometheusMetrics(processName, instanceID, &ResourceSample{
		CPUPercent:      12.5,
		MemoryRSSBytes:  64 << 20,
		MemoryVMSBytes:  256 << 20,
		MemoryPercent:   1.5,
		Threads:         4,
		FileDescriptors: 16,
	})
	ResourceCollectionErrors.WithLabelValues(processName, instanceID).Inc()
}

// populateProcess writes every per-process (not per-instance) series.
func populateProcess(processName string) {
	RecordProcessRestart(processName, "crash")
	SetDesiredScale(processName, 3)
	RecordHealthCheck(processName, "tcp", 0.01, true)
	RecordHealthCheckFailures(processName, 0)
}

// seriesWith returns the exposed series, across every cbox_init_ family in the
// default registry, whose labels include all of want. Matching is by label key,
// so a series labelled name="x" does not match {"process": "x"} — the check
// is exactly as strict as a PromQL selector.
func seriesWith(t *testing.T, want prometheus.Labels) []string {
	t.Helper()

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	var found []string
	for _, mf := range families {
		if !strings.HasPrefix(mf.GetName(), "cbox_init_") {
			continue
		}
		for _, m := range mf.GetMetric() {
			labels := make(map[string]string, len(m.GetLabel()))
			for _, lp := range m.GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}
			matched := true
			for k, v := range want {
				if labels[k] != v {
					matched = false
					break
				}
			}
			if matched {
				found = append(found, mf.GetName()+formatLabels(labels))
			}
		}
	}
	return found
}

func formatLabels(labels map[string]string) string {
	parts := make([]string, 0, len(labels))
	for k, v := range labels {
		parts = append(parts, k+"="+v)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// instanceSelectors covers both process-label keys in use, so an instance's
// series are found whichever key its family carries.
func instanceSelectors(processName, instanceID string) []prometheus.Labels {
	return []prometheus.Labels{
		{labelName: processName, labelInstance: instanceID},
		{labelProcess: processName, labelInstance: instanceID},
	}
}

// TestRemoveInstanceMetrics_ScaleDownLeavesNoSeries is the regression test for
// the label-key mismatch: RemoveInstanceMetrics matched every family on
// "process", so the lifecycle families (labelled "name") were never deleted and
// cbox_init_process_up{instance="php-fpm-9"} stayed at 0 after a scale-down.
func TestRemoveInstanceMetrics_ScaleDownLeavesNoSeries(t *testing.T) {
	const proc = "cleanup-scale-fpm"
	const other = "cleanup-scale-other"

	instances := []string{proc + "-0", proc + "-1", proc + "-2"}
	for _, id := range instances {
		populateInstance(proc, id)
	}
	populateProcess(proc)
	// A second process sharing an instance label value: removal must match on
	// process AND instance, never instance alone.
	populateInstance(other, instances[2])

	// Baseline totals per family, so the delta proves exactly two instances'
	// worth of series went away and nothing else did.
	before := make(map[*seriesFamily]int)
	for i := range instanceSeries {
		before[&instanceSeries[i]] = testutil.CollectAndCount(instanceSeries[i].vec.(prometheus.Collector))
	}

	// Every per-instance family must have been populated, otherwise the
	// assertions below would pass vacuously for the missing ones.
	for _, id := range instances {
		got := 0
		for _, sel := range instanceSelectors(proc, id) {
			got += len(seriesWith(t, sel))
		}
		// 9 families, ProcessMemoryBytes has two series (rss, vms).
		if want := len(instanceSeries) + 1; got != want {
			t.Fatalf("instance %s: populated %d series, want %d", id, got, want)
		}
	}

	// Scale 3 -> 1.
	for _, id := range instances[1:] {
		RemoveInstanceMetrics(proc, id)
	}

	for _, id := range instances[1:] {
		for _, sel := range instanceSelectors(proc, id) {
			if left := seriesWith(t, sel); len(left) > 0 {
				t.Errorf("series for removed instance %s survived: %v", id, left)
			}
		}
	}

	for i := range instanceSeries {
		f := &instanceSeries[i]
		perInstance := 1
		if f.vec == partialDeleter(ProcessMemoryBytes) {
			perInstance = 2
		}
		after := testutil.CollectAndCount(f.vec.(prometheus.Collector))
		if want := before[f] - 2*perInstance; after != want {
			t.Errorf("family %d (%s label): %d series after removal, want %d", i, f.processLabel, after, want)
		}
	}

	// The surviving instance, the other process and the per-process series are
	// untouched.
	if got := testutil.ToFloat64(ProcessUp.WithLabelValues(proc, instances[0])); got != 0 {
		t.Errorf("surviving instance process_up = %v, want 0 (it was stopped, not removed)", got)
	}
	for _, sel := range [][]prometheus.Labels{
		instanceSelectors(proc, instances[0]),
		instanceSelectors(other, instances[2]),
	} {
		got := len(seriesWith(t, sel[0])) + len(seriesWith(t, sel[1]))
		if want := len(instanceSeries) + 1; got != want {
			t.Errorf("series for %v: %d, want %d (removal over-matched)", sel, got, want)
		}
	}
	if left := seriesWith(t, prometheus.Labels{labelName: proc, "reason": "crash"}); len(left) != 1 {
		t.Errorf("per-process restarts series should survive a scale-down, got %v", left)
	}
}

// TestRemoveProcessMetrics_LeavesNoSeries covers removing a process from the
// config: every series carrying its name, under either label key, must go.
func TestRemoveProcessMetrics_LeavesNoSeries(t *testing.T) {
	const proc = "cleanup-removed-proc"
	const other = "cleanup-kept-proc"

	for _, p := range []string{proc, other} {
		populateInstance(p, p+"-0")
		populateInstance(p, p+"-1")
		populateProcess(p)
	}

	for _, key := range []string{labelName, labelProcess} {
		if len(seriesWith(t, prometheus.Labels{key: proc})) == 0 {
			t.Fatalf("no %s=%q series populated; test would pass vacuously", key, proc)
		}
	}

	RemoveProcessMetrics(proc)

	for _, key := range []string{labelName, labelProcess} {
		if left := seriesWith(t, prometheus.Labels{key: proc}); len(left) > 0 {
			t.Errorf("series for removed process survived under %q: %v", key, left)
		}
	}
	for _, key := range []string{labelName, labelProcess} {
		if len(seriesWith(t, prometheus.Labels{key: other})) == 0 {
			t.Errorf("series for %q under %q were deleted too (over-match)", other, key)
		}
	}
}
