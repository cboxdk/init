package metrics

import (
	"sync"
	"time"
)

// ResourceSample represents a single resource metrics sample
type ResourceSample struct {
	Timestamp       time.Time `json:"timestamp"`
	CPUPercent      float64   `json:"cpu_percent"`
	MemoryRSSBytes  uint64    `json:"memory_rss_bytes"`
	MemoryVMSBytes  uint64    `json:"memory_vms_bytes"`
	MemoryPercent   float32   `json:"memory_percent"`
	Threads         int32     `json:"threads"`
	FileDescriptors int32     `json:"file_descriptors,omitempty"` // -1 if unavailable
}

// TimeSeriesBuffer stores resource metrics in a ring buffer
type TimeSeriesBuffer struct {
	samples    []ResourceSample
	head       int
	size       int
	maxSamples int
	mu         sync.RWMutex
}

// NewTimeSeriesBuffer creates a new time series ring buffer
func NewTimeSeriesBuffer(maxSamples int) *TimeSeriesBuffer {
	if maxSamples < 1 {
		maxSamples = 720 // Default: 1 hour at 5s interval
	}

	return &TimeSeriesBuffer{
		samples:    make([]ResourceSample, maxSamples),
		maxSamples: maxSamples,
		head:       0,
		size:       0,
	}
}

// Add adds a sample to the ring buffer
func (tsb *TimeSeriesBuffer) Add(sample ResourceSample) {
	tsb.mu.Lock()
	defer tsb.mu.Unlock()

	tsb.samples[tsb.head] = sample
	tsb.head = (tsb.head + 1) % tsb.maxSamples

	if tsb.size < tsb.maxSamples {
		tsb.size++
	}
}

// GetRange returns samples within time range, up to limit
func (tsb *TimeSeriesBuffer) GetRange(since time.Time, limit int) []ResourceSample {
	tsb.mu.RLock()
	defer tsb.mu.RUnlock()

	if tsb.size == 0 {
		return []ResourceSample{}
	}

	if limit <= 0 || limit > tsb.size {
		limit = tsb.size
	}

	result := make([]ResourceSample, 0, limit)

	// Walk backwards from head (newest to oldest), appending as we go — an O(1)
	// amortized operation. The previous code prepended each sample, which
	// reallocates and copies the whole slice every iteration: O(n²), ~260k
	// element copies for a full 720-sample read.
	for i := 0; i < tsb.size && len(result) < limit; i++ {
		idx := (tsb.head - 1 - i + tsb.maxSamples) % tsb.maxSamples
		sample := tsb.samples[idx]

		if sample.Timestamp.After(since) || sample.Timestamp.Equal(since) {
			result = append(result, sample)
		}
	}

	// Reverse once into chronological order (oldest first).
	for l, r := 0, len(result)-1; l < r; l, r = l+1, r-1 {
		result[l], result[r] = result[r], result[l]
	}

	return result
}

// GetLast returns the last N samples
func (tsb *TimeSeriesBuffer) GetLast(n int) []ResourceSample {
	since := time.Time{} // Beginning of time - gets all
	return tsb.GetRange(since, n)
}

// GetSince returns all samples since a specific time
func (tsb *TimeSeriesBuffer) GetSince(since time.Time) []ResourceSample {
	return tsb.GetRange(since, tsb.maxSamples)
}

// Size returns current number of samples stored
func (tsb *TimeSeriesBuffer) Size() int {
	tsb.mu.RLock()
	defer tsb.mu.RUnlock()
	return tsb.size
}

// Latest returns the most recent sample
func (tsb *TimeSeriesBuffer) Latest() (ResourceSample, bool) {
	tsb.mu.RLock()
	defer tsb.mu.RUnlock()

	if tsb.size == 0 {
		return ResourceSample{}, false
	}

	idx := (tsb.head - 1 + tsb.maxSamples) % tsb.maxSamples
	return tsb.samples[idx], true
}

// Clear empties the buffer
func (tsb *TimeSeriesBuffer) Clear() {
	tsb.mu.Lock()
	defer tsb.mu.Unlock()

	tsb.head = 0
	tsb.size = 0
	// Keep allocated memory, just reset pointers
}
