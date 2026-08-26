package metrics

import (
	"testing"
	"time"
)

// fillBuffer populates a buffer with n evenly-spaced samples ending "now".
func fillBuffer(n int) *TimeSeriesBuffer {
	tsb := NewTimeSeriesBuffer(n)
	base := time.Unix(1_700_000_000, 0)
	for i := 0; i < n; i++ {
		tsb.Add(ResourceSample{
			Timestamp:      base.Add(time.Duration(i) * time.Second),
			CPUPercent:     float64(i % 100),
			MemoryRSSBytes: uint64(i) * 1024,
		})
	}
	return tsb
}

// BenchmarkTimeSeriesBuffer_GetRange guards the O(n) read of the full history
// (a full 720-sample hour). It used to be O(n^2) — a prepend-into-a-growing-slice
// loop — so this is the regression backstop for that fix.
func BenchmarkTimeSeriesBuffer_GetRange(b *testing.B) {
	tsb := fillBuffer(720)
	since := time.Unix(0, 0) // everything
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tsb.GetRange(since, 0)
	}
}

// BenchmarkTimeSeriesBuffer_Add measures the per-sample write cost into a full
// ring buffer (the steady state once the hour of history has filled).
func BenchmarkTimeSeriesBuffer_Add(b *testing.B) {
	tsb := fillBuffer(720)
	s := ResourceSample{Timestamp: time.Unix(1_700_100_000, 0), CPUPercent: 12.5}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tsb.Add(s)
	}
}
