package metrics

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// CollectProcessMetrics collects resource metrics for a single process using a
// fresh gopsutil handle. NOTE: gopsutil computes CPUPercent as the busy time
// since the handle was created, so a fresh handle every call yields the
// process's *lifetime average* CPU, not the recent interval. For repeated
// sampling use ResourceCollector.Collect, which reuses the handle across ticks
// so CPUPercent reflects recent usage (PERF-3).
func CollectProcessMetrics(pid int, processName, instanceID string) (*ResourceSample, error) {
	// Validate PID is within int32 range (process IDs are always positive and fit in int32)
	if pid <= 0 || pid > 0x7FFFFFFF {
		return nil, fmt.Errorf("invalid PID: %d", pid)
	}
	proc, err := process.NewProcess(int32(pid)) // #nosec G115 -- bounds checked above
	if err != nil {
		return nil, err
	}
	return sampleFromProc(proc)
}

// sampleFromProc reads the resource sample from an existing gopsutil handle.
func sampleFromProc(proc *process.Process) (*ResourceSample, error) {
	sample := &ResourceSample{
		Timestamp:       time.Now(),
		FileDescriptors: -1, // Default for non-Linux
	}

	// CPU Percent (delta since the previous call on this handle)
	if cpu, err := proc.CPUPercent(); err == nil {
		sample.CPUPercent = cpu
	}

	// Memory Info
	if memInfo, err := proc.MemoryInfo(); err == nil {
		sample.MemoryRSSBytes = memInfo.RSS
		sample.MemoryVMSBytes = memInfo.VMS
	}

	// Memory Percent
	if memPct, err := proc.MemoryPercent(); err == nil {
		sample.MemoryPercent = memPct
	}

	// Thread Count
	if threads, err := proc.NumThreads(); err == nil {
		sample.Threads = threads
	}

	// File Descriptors (Linux only)
	if fds, err := proc.NumFDs(); err == nil {
		sample.FileDescriptors = int32(fds)
	}

	return sample, nil
}

// UpdatePrometheusMetrics updates Prometheus gauges with resource sample
func UpdatePrometheusMetrics(processName, instanceID string, sample *ResourceSample) {
	ProcessCPUPercent.WithLabelValues(processName, instanceID).Set(sample.CPUPercent)

	ProcessMemoryBytes.WithLabelValues(processName, instanceID, "rss").Set(float64(sample.MemoryRSSBytes))
	ProcessMemoryBytes.WithLabelValues(processName, instanceID, "vms").Set(float64(sample.MemoryVMSBytes))

	ProcessMemoryPercent.WithLabelValues(processName, instanceID).Set(float64(sample.MemoryPercent))

	ProcessThreads.WithLabelValues(processName, instanceID).Set(float64(sample.Threads))

	if sample.FileDescriptors >= 0 {
		ProcessFileDescriptors.WithLabelValues(processName, instanceID).Set(float64(sample.FileDescriptors))
	}
}

// procHandle caches a gopsutil process handle and the PID it was opened for, so
// a restarted instance (new PID under the same key) gets a fresh handle. A
// gopsutil Process caches internal state and is NOT safe for concurrent use, so
// mu serializes samples taken through this handle.
type procHandle struct {
	pid  int
	proc *process.Process
	mu   sync.Mutex
}

// ResourceCollector manages resource metric collection
type ResourceCollector struct {
	interval   time.Duration
	maxSamples int
	buffers    map[string]*TimeSeriesBuffer // key: "process-instance"
	handles    map[string]*procHandle       // key: "process-instance"; reused across ticks
	mu         sync.RWMutex
	logger     *slog.Logger
}

// NewResourceCollector creates a new resource collector
func NewResourceCollector(interval time.Duration, maxSamples int, logger *slog.Logger) *ResourceCollector {
	return &ResourceCollector{
		interval:   interval,
		maxSamples: maxSamples,
		buffers:    make(map[string]*TimeSeriesBuffer),
		handles:    make(map[string]*procHandle),
		logger:     logger.With("component", "resource_collector"),
	}
}

// Collect samples a process's resources, reusing the gopsutil handle across
// calls for the same instance so CPUPercent reflects usage since the previous
// tick rather than the process's lifetime average. If the PID changed (the
// instance restarted), the handle is recreated. (PERF-3)
func (rc *ResourceCollector) Collect(pid int, processName, instanceID string) (*ResourceSample, error) {
	if pid <= 0 || pid > 0x7FFFFFFF {
		return nil, fmt.Errorf("invalid PID: %d", pid)
	}
	key := processName + "-" + instanceID

	rc.mu.Lock()
	h := rc.handles[key]
	if h == nil || h.pid != pid {
		proc, err := process.NewProcess(int32(pid)) // #nosec G115 -- bounds checked above
		if err != nil {
			rc.mu.Unlock()
			return nil, err
		}
		h = &procHandle{pid: pid, proc: proc}
		rc.handles[key] = h
	}
	rc.mu.Unlock()

	// Serialize reads through this handle: gopsutil's Process is not safe for
	// concurrent use, and the handle is now shared/cached across ticks. Held
	// outside rc.mu so sampling one instance does not block collection of others.
	h.mu.Lock()
	defer h.mu.Unlock()
	return sampleFromProc(h.proc)
}

// GetHistory returns time series for a process instance
func (rc *ResourceCollector) GetHistory(processName, instanceID string, since time.Time, limit int) []ResourceSample {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	key := processName + "-" + instanceID
	buffer, exists := rc.buffers[key]
	if !exists {
		return []ResourceSample{}
	}

	return buffer.GetRange(since, limit)
}

// AddSample adds a sample to the buffer for a process instance
func (rc *ResourceCollector) AddSample(processName, instanceID string, sample ResourceSample) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	key := processName + "-" + instanceID

	// Lazy initialization of buffer
	if _, exists := rc.buffers[key]; !exists {
		rc.buffers[key] = NewTimeSeriesBuffer(rc.maxSamples)
	}

	rc.buffers[key].Add(sample)
}

// RemoveBuffer removes buffer for stopped process
func (rc *ResourceCollector) RemoveBuffer(processName, instanceID string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	key := processName + "-" + instanceID
	delete(rc.buffers, key)
	delete(rc.handles, key)
}

// GetBufferSizes returns memory usage info
func (rc *ResourceCollector) GetBufferSizes() map[string]int {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	sizes := make(map[string]int, len(rc.buffers))
	for key, buffer := range rc.buffers {
		sizes[key] = buffer.Size()
	}

	return sizes
}

// GetInterval returns the collection interval
func (rc *ResourceCollector) GetInterval() time.Duration {
	return rc.interval
}

// GetLatest returns the latest sample for a process instance if available
func (rc *ResourceCollector) GetLatest(processName, instanceID string) (ResourceSample, bool) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	key := processName + "-" + instanceID
	buffer, exists := rc.buffers[key]
	if !exists {
		return ResourceSample{}, false
	}

	return buffer.Latest()
}
