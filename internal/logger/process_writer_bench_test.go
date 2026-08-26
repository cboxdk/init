package logger

import (
	"io"
	"log/slog"
	"testing"
)

// BenchmarkProcessWriter_Write measures the per-write cost of the log hot path:
// a child's stdout arrives here in arbitrary chunks and must be split into lines
// (including assembling partial lines across writes) and turned into entries.
func BenchmarkProcessWriter_Write(b *testing.B) {
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	pw, err := NewProcessWriter(log, "bench", "0", "stdout", nil)
	if err != nil {
		b.Fatalf("NewProcessWriter: %v", err)
	}

	// A typical multi-line chunk, plus a trailing partial line to exercise the
	// cross-write assembly path.
	chunk := []byte("2026-08-26 INFO request handled in 12ms\n" +
		"2026-08-26 WARN slow query 210ms\n" +
		"2026-08-26 INFO cache hit ratio 0.97\n" +
		"partial line without newline")

	b.SetBytes(int64(len(chunk)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := pw.Write(chunk); err != nil {
			b.Fatalf("Write: %v", err)
		}
	}
}
