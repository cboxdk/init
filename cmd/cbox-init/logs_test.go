package main

import (
	"testing"

	"github.com/cboxdk/init/internal/logger"
)

func TestParseLogLevelFilter(t *testing.T) {
	tests := []struct {
		level    string
		want     int
		wantErr  bool
	}{
		{level: "all", want: -1},
		{level: "debug", want: 0},
		{level: "info", want: 1},
		{level: "warn", want: 2},
		{level: "warning", want: 2},
		{level: "error", want: 3},
		{level: "TRACE", wantErr: true},
	}

	for _, tt := range tests {
		got, err := parseLogLevelFilter(tt.level)
		if (err != nil) != tt.wantErr {
			t.Fatalf("parseLogLevelFilter(%q) error = %v, wantErr %v", tt.level, err, tt.wantErr)
		}
		if !tt.wantErr && got != tt.want {
			t.Fatalf("parseLogLevelFilter(%q) = %d, want %d", tt.level, got, tt.want)
		}
	}
}

func TestShouldPrintLogEntry(t *testing.T) {
	tests := []struct {
		name     string
		entry    logger.LogEntry
		minLevel int
		want     bool
	}{
		{
			name:     "all passes",
			entry:    logger.LogEntry{Level: "debug"},
			minLevel: -1,
			want:     true,
		},
		{
			name:     "warn hides info",
			entry:    logger.LogEntry{Level: "info"},
			minLevel: 2,
			want:     false,
		},
		{
			name:     "warn shows error",
			entry:    logger.LogEntry{Level: "error"},
			minLevel: 2,
			want:     true,
		},
		{
			name:     "unknown level falls back to info",
			entry:    logger.LogEntry{Level: "custom"},
			minLevel: 1,
			want:     true,
		},
	}

	for _, tt := range tests {
		if got := shouldPrintLogEntry(tt.entry, tt.minLevel); got != tt.want {
			t.Fatalf("%s: shouldPrintLogEntry() = %v, want %v", tt.name, got, tt.want)
		}
	}
}
