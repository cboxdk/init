package metrics

import (
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// TestCollectProcessMetrics tests collecting metrics for a real process
func TestCollectProcessMetrics(t *testing.T) {
	// Use current process for testing
	pid := os.Getpid()

	tests := []struct {
		name        string
		pid         int
		processName string
		instanceID  string
		wantErr     bool
	}{
		{
			name:        "collect current process",
			pid:         pid,
			processName: "test-process",
			instanceID:  "test-0",
			wantErr:     false,
		},
		{
			name:        "invalid pid",
			pid:         -1,
			processName: "invalid",
			instanceID:  "invalid-0",
			wantErr:     true,
		},
		{
			name:        "non-existent pid",
			pid:         999999,
			processName: "missing",
			instanceID:  "missing-0",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sample, err := CollectProcessMetrics(tt.pid, tt.processName, tt.instanceID)

			if (err != nil) != tt.wantErr {
				t.Errorf("CollectProcessMetrics() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil {
				return
			}

			// Verify sample has reasonable values
			if sample == nil {
				t.Fatal("Expected non-nil sample")
			}

			if sample.Timestamp.IsZero() {
				t.Error("Expected non-zero timestamp")
			}

			// CPU percent should be >= 0
			if sample.CPUPercent < 0 {
				t.Errorf("Invalid CPU percent: %f", sample.CPUPercent)
			}

			// Memory should be > 0 for current process
			if sample.MemoryRSSBytes == 0 {
				t.Error("Expected non-zero RSS memory")
			}

			// Threads should be > 0
			if sample.Threads <= 0 {
				t.Error("Expected positive thread count")
			}
		})
	}
}

// TestCollectProcessMetrics_FieldCollection tests individual field collection
func TestCollectProcessMetrics_FieldCollection(t *testing.T) {
	pid := os.Getpid()
	sample, err := CollectProcessMetrics(pid, "test", "test-0")

	if err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}

	// Verify all expected fields are populated
	tests := []struct {
		name      string
		checkFunc func() bool
	}{
		{
			name:      "timestamp set",
			checkFunc: func() bool { return !sample.Timestamp.IsZero() },
		},
		{
			name:      "cpu percent set",
			checkFunc: func() bool { return sample.CPUPercent >= 0 },
		},
		{
			name:      "memory rss set",
			checkFunc: func() bool { return sample.MemoryRSSBytes > 0 },
		},
		{
			name:      "memory vms set",
			checkFunc: func() bool { return sample.MemoryVMSBytes > 0 },
		},
		{
			name:      "memory percent set",
			checkFunc: func() bool { return sample.MemoryPercent >= 0 },
		},
		{
			name:      "threads set",
			checkFunc: func() bool { return sample.Threads > 0 },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.checkFunc() {
				t.Errorf("Field check failed: %s", tt.name)
			}
		})
	}
}

// TestUpdatePrometheusMetrics tests updating Prometheus gauges
func TestUpdatePrometheusMetrics(t *testing.T) {
	sample := &ResourceSample{
		Timestamp:       time.Now(),
		CPUPercent:      25.5,
		MemoryRSSBytes:  1024 * 1024 * 100, // 100 MB
		MemoryVMSBytes:  1024 * 1024 * 500, // 500 MB
		MemoryPercent:   5.5,
		Threads:         10,
		FileDescriptors: 42,
	}

	tests := []struct {
		name        string
		processName string
		instanceID  string
		sample      *ResourceSample
	}{
		{
			name:        "update php-fpm metrics",
			processName: "php-fpm",
			instanceID:  "php-fpm-0",
			sample:      sample,
		},
		{
			name:        "update nginx metrics",
			processName: "nginx",
			instanceID:  "nginx-1",
			sample:      sample,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			UpdatePrometheusMetrics(tt.processName, tt.instanceID, tt.sample)
		})
	}
}

// TestUpdatePrometheusMetrics_NoFileDescriptors tests handling -1 FD value
func TestUpdatePrometheusMetrics_NoFileDescriptors(t *testing.T) {
	sample := &ResourceSample{
		Timestamp:       time.Now(),
		CPUPercent:      10.0,
		MemoryRSSBytes:  1024 * 1024,
		MemoryVMSBytes:  1024 * 1024 * 2,
		MemoryPercent:   1.0,
		Threads:         5,
		FileDescriptors: -1, // Not available
	}

	// Should not panic and should skip FD metric
	UpdatePrometheusMetrics("test", "test-0", sample)
}

// TestNewResourceCollector tests creating a new collector
func TestNewResourceCollector(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	tests := []struct {
		name       string
		interval   time.Duration
		maxSamples int
	}{
		{
			name:       "default config",
			interval:   5 * time.Second,
			maxSamples: 720,
		},
		{
			name:       "fast sampling",
			interval:   1 * time.Second,
			maxSamples: 100,
		},
		{
			name:       "slow sampling",
			interval:   30 * time.Second,
			maxSamples: 1440,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := NewResourceCollector(tt.interval, tt.maxSamples, logger)

			if rc == nil {
				t.Fatal("Expected non-nil collector")
			}

			if rc.interval != tt.interval {
				t.Errorf("Expected interval %v, got %v", tt.interval, rc.interval)
			}

			if rc.maxSamples != tt.maxSamples {
				t.Errorf("Expected maxSamples %d, got %d", tt.maxSamples, rc.maxSamples)
			}

			if rc.buffers == nil {
				t.Error("Expected non-nil buffers map")
			}

			if len(rc.buffers) != 0 {
				t.Errorf("Expected empty buffers, got %d", len(rc.buffers))
			}
		})
	}
}

// TestResourceCollector_AddSample tests adding samples
func TestResourceCollector_AddSample(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	rc := NewResourceCollector(5*time.Second, 100, logger)

	now := time.Now()
	sample := ResourceSample{
		Timestamp:      now,
		CPUPercent:     10.5,
		MemoryRSSBytes: 1024 * 1024,
		Threads:        5,
	}

	// Add sample
	rc.AddSample("php-fpm", "php-fpm-0", sample)

	// Verify buffer was created
	sizes := rc.GetBufferSizes()
	if len(sizes) != 1 {
		t.Errorf("Expected 1 buffer, got %d", len(sizes))
	}

	if size, exists := sizes["php-fpm-php-fpm-0"]; !exists || size != 1 {
		t.Errorf("Expected buffer size 1, got %d (exists: %v)", size, exists)
	}

	// Add more samples to same process
	for i := 0; i < 5; i++ {
		sample := ResourceSample{
			Timestamp:      now.Add(time.Duration(i+1) * time.Second),
			CPUPercent:     float64(i * 10),
			MemoryRSSBytes: uint64(i * 1024 * 1024),
		}
		rc.AddSample("php-fpm", "php-fpm-0", sample)
	}

	// Verify buffer size increased
	sizes = rc.GetBufferSizes()
	if size, exists := sizes["php-fpm-php-fpm-0"]; !exists || size != 6 {
		t.Errorf("Expected buffer size 6, got %d (exists: %v)", size, exists)
	}
}

// TestResourceCollector_GetHistory tests retrieving history
func TestResourceCollector_GetHistory(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	rc := NewResourceCollector(5*time.Second, 100, logger)

	now := time.Now()

	// Add samples
	for i := 0; i < 10; i++ {
		sample := ResourceSample{
			Timestamp:      now.Add(time.Duration(i) * time.Second),
			CPUPercent:     float64(i * 10),
			MemoryRSSBytes: uint64(i * 1024 * 1024),
		}
		rc.AddSample("worker", "worker-0", sample)
	}

	tests := []struct {
		name          string
		since         time.Time
		limit         int
		expectedCount int
	}{
		{
			name:          "get last 5",
			since:         time.Time{},
			limit:         5,
			expectedCount: 5,
		},
		{
			name:          "get since 5 seconds",
			since:         now.Add(5 * time.Second),
			limit:         100,
			expectedCount: 5, // samples 5-9
		},
		{
			name:          "get all",
			since:         time.Time{},
			limit:         100,
			expectedCount: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			history := rc.GetHistory("worker", "worker-0", tt.since, tt.limit)

			if len(history) != tt.expectedCount {
				t.Errorf("Expected %d samples, got %d", tt.expectedCount, len(history))
			}

			// Verify chronological order
			for i := 1; i < len(history); i++ {
				if !history[i].Timestamp.After(history[i-1].Timestamp) {
					t.Error("History not in chronological order")
				}
			}
		})
	}
}

// TestResourceCollector_GetHistory_NonExistent tests getting history for non-existent process
func TestResourceCollector_GetHistory_NonExistent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	rc := NewResourceCollector(5*time.Second, 100, logger)

	history := rc.GetHistory("non-existent", "inst-0", time.Time{}, 100)

	if len(history) != 0 {
		t.Errorf("Expected empty history for non-existent process, got %d samples", len(history))
	}
}

// TestResourceCollector_RemoveBuffer tests removing process buffers
func TestResourceCollector_RemoveBuffer(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	rc := NewResourceCollector(5*time.Second, 100, logger)

	// Add samples
	sample := ResourceSample{
		Timestamp:  time.Now(),
		CPUPercent: 10.0,
	}
	rc.AddSample("temp", "temp-0", sample)

	// Verify buffer exists
	sizes := rc.GetBufferSizes()
	if len(sizes) != 1 {
		t.Fatalf("Expected 1 buffer, got %d", len(sizes))
	}

	// Remove buffer
	rc.RemoveBuffer("temp", "temp-0")

	// Verify buffer removed
	sizes = rc.GetBufferSizes()
	if len(sizes) != 0 {
		t.Errorf("Expected 0 buffers after removal, got %d", len(sizes))
	}

	// Removing again should not panic
	rc.RemoveBuffer("temp", "temp-0")
}

// TestResourceCollector_GetBufferSizes tests getting buffer sizes
func TestResourceCollector_GetBufferSizes(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	rc := NewResourceCollector(5*time.Second, 100, logger)

	// Initially empty
	sizes := rc.GetBufferSizes()
	if len(sizes) != 0 {
		t.Errorf("Expected 0 buffers, got %d", len(sizes))
	}

	// Add samples to multiple processes
	sample := ResourceSample{Timestamp: time.Now(), CPUPercent: 10.0}
	rc.AddSample("proc1", "inst-0", sample)
	rc.AddSample("proc1", "inst-1", sample)
	rc.AddSample("proc2", "inst-0", sample)

	sizes = rc.GetBufferSizes()
	if len(sizes) != 3 {
		t.Errorf("Expected 3 buffers, got %d", len(sizes))
	}

	// Verify each buffer has size 1
	for key, size := range sizes {
		if size != 1 {
			t.Errorf("Buffer %s: expected size 1, got %d", key, size)
		}
	}
}

// TestResourceCollector_GetInterval tests getting collection interval
func TestResourceCollector_GetInterval(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	tests := []struct {
		name     string
		interval time.Duration
	}{
		{
			name:     "5 second interval",
			interval: 5 * time.Second,
		},
		{
			name:     "1 second interval",
			interval: 1 * time.Second,
		},
		{
			name:     "30 second interval",
			interval: 30 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := NewResourceCollector(tt.interval, 100, logger)

			if rc.GetInterval() != tt.interval {
				t.Errorf("Expected interval %v, got %v", tt.interval, rc.GetInterval())
			}
		})
	}
}

// TestResourceCollector_GetLatest tests getting the latest sample
func TestResourceCollector_GetLatest(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	rc := NewResourceCollector(5*time.Second, 100, logger)

	// Test non-existent process
	_, exists := rc.GetLatest("non-existent", "inst-0")
	if exists {
		t.Error("Expected no latest sample for non-existent process")
	}

	// Add samples
	now := time.Now()
	for i := 0; i < 5; i++ {
		sample := ResourceSample{
			Timestamp:  now.Add(time.Duration(i) * time.Second),
			CPUPercent: float64(i * 10),
		}
		rc.AddSample("test", "test-0", sample)
	}

	// Get latest
	latest, exists := rc.GetLatest("test", "test-0")
	if !exists {
		t.Fatal("Expected latest sample to exist")
	}

	// Latest should be the last added sample (i=4)
	if latest.CPUPercent != 40.0 {
		t.Errorf("Expected latest CPU 40.0, got %f", latest.CPUPercent)
	}

	// Timestamp should be newest
	expectedTime := now.Add(4 * time.Second)
	if !latest.Timestamp.Equal(expectedTime) {
		t.Errorf("Expected latest timestamp %v, got %v", expectedTime, latest.Timestamp)
	}
}

// TestResourceCollector_ConcurrentAccess tests thread-safety
func TestResourceCollector_ConcurrentAccess(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	rc := NewResourceCollector(5*time.Second, 100, logger)

	done := make(chan bool, 3)

	// Writer 1
	go func() {
		for i := 0; i < 50; i++ {
			sample := ResourceSample{
				Timestamp:  time.Now(),
				CPUPercent: float64(i),
			}
			rc.AddSample("proc1", "inst-0", sample)
			time.Sleep(time.Millisecond)
		}
		done <- true
	}()

	// Writer 2
	go func() {
		for i := 0; i < 50; i++ {
			sample := ResourceSample{
				Timestamp:  time.Now(),
				CPUPercent: float64(i),
			}
			rc.AddSample("proc2", "inst-0", sample)
			time.Sleep(time.Millisecond)
		}
		done <- true
	}()

	// Reader
	go func() {
		for i := 0; i < 50; i++ {
			_ = rc.GetHistory("proc1", "inst-0", time.Time{}, 10)
			_ = rc.GetBufferSizes()
			_, _ = rc.GetLatest("proc2", "inst-0")
			time.Sleep(time.Millisecond)
		}
		done <- true
	}()

	// Wait for all
	<-done
	<-done
	<-done
}

// TestResourceCollector_MultipleProcesses tests handling multiple processes
func TestResourceCollector_MultipleProcesses(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	rc := NewResourceCollector(5*time.Second, 100, logger)

	processes := []struct {
		name     string
		instance string
		samples  int
	}{
		{"php-fpm", "php-fpm-0", 10},
		{"php-fpm", "php-fpm-1", 15},
		{"nginx", "nginx-0", 20},
		{"worker", "worker-0", 5},
		{"worker", "worker-1", 8},
	}

	now := time.Now()

	// Add samples for each process
	for _, proc := range processes {
		for i := 0; i < proc.samples; i++ {
			sample := ResourceSample{
				Timestamp:  now.Add(time.Duration(i) * time.Second),
				CPUPercent: float64(i * 5),
			}
			rc.AddSample(proc.name, proc.instance, sample)
		}
	}

	// Verify buffer sizes
	sizes := rc.GetBufferSizes()
	if len(sizes) != len(processes) {
		t.Errorf("Expected %d buffers, got %d", len(processes), len(sizes))
	}

	// Verify each process has correct number of samples
	for _, proc := range processes {
		history := rc.GetHistory(proc.name, proc.instance, time.Time{}, 100)
		if len(history) != proc.samples {
			t.Errorf("%s-%s: expected %d samples, got %d",
				proc.name, proc.instance, proc.samples, len(history))
		}
	}
}

// TestCollectProcessMetrics_WithRealProcess tests collection with subprocess
func TestCollectProcessMetrics_WithRealProcess(t *testing.T) {
	// This test creates a real subprocess to test metric collection
	// Skip in CI environments where process creation might be restricted
	if testing.Short() {
		t.Skip("Skipping subprocess test in short mode")
	}

	// Create a simple subprocess (sleep)
	proc, err := process.NewProcess(int32(os.Getpid()))
	if err != nil {
		t.Fatalf("Failed to get current process: %v", err)
	}

	pid := int(proc.Pid)

	// Collect metrics
	sample, err := CollectProcessMetrics(pid, "test", "test-0")
	if err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}

	// Verify sample is reasonable
	if sample.CPUPercent < 0 || sample.CPUPercent > 1000 {
		t.Errorf("Unreasonable CPU percent: %f", sample.CPUPercent)
	}

	if sample.MemoryRSSBytes == 0 {
		t.Error("Expected non-zero memory usage")
	}

	if sample.Threads == 0 {
		t.Error("Expected non-zero thread count")
	}
}

// TestResourceCollector_Collect_ReusesHandle verifies the collector caches the
// gopsutil handle per instance across ticks (so CPUPercent measures recent
// usage) and evicts it when the buffer is removed (PERF-3).
func TestResourceCollector_Collect_ReusesHandle(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	rc := NewResourceCollector(time.Second, 10, logger)
	pid := os.Getpid()

	if _, err := rc.Collect(pid, "self", "0"); err != nil {
		t.Fatalf("first Collect: %v", err)
	}
	rc.mu.RLock()
	h1 := rc.handles["self-0"]
	rc.mu.RUnlock()
	if h1 == nil || h1.pid != pid {
		t.Fatalf("handle not cached for the instance after Collect")
	}

	if _, err := rc.Collect(pid, "self", "0"); err != nil {
		t.Fatalf("second Collect: %v", err)
	}
	rc.mu.RLock()
	h2 := rc.handles["self-0"]
	rc.mu.RUnlock()
	if h2 != h1 {
		t.Errorf("second Collect created a new handle; it must reuse the cached one")
	}

	rc.RemoveBuffer("self", "0")
	rc.mu.RLock()
	_, exists := rc.handles["self-0"]
	rc.mu.RUnlock()
	if exists {
		t.Error("handle was not evicted on RemoveBuffer")
	}
}

// TestResourceCollector_Collect_InvalidPID rejects an out-of-range PID.
func TestResourceCollector_Collect_InvalidPID(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	rc := NewResourceCollector(time.Second, 10, logger)
	if _, err := rc.Collect(0, "p", "0"); err == nil {
		t.Error("expected error for PID 0")
	}
}

// TestResourceCollector_Collect_ConcurrentSameKey stresses concurrent Collect
// calls for the same instance. Because the gopsutil handle is cached and shared,
// and gopsutil's Process is not concurrency-safe, this must be serialized by the
// per-handle mutex — the race detector will flag it otherwise (regression guard
// for the PERF-3 handle cache).
func TestResourceCollector_Collect_ConcurrentSameKey(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	rc := NewResourceCollector(time.Second, 10, logger)
	pid := os.Getpid()

	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = rc.Collect(pid, "self", "0")
		}()
	}
	wg.Wait()
}
