package logger

import (
	"strings"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/config"
)

func TestNewMultilineBuffer_Disabled(t *testing.T) {
	tests := []struct {
		name   string
		config *config.MultilineConfig
	}{
		{
			name:   "nil config",
			config: nil,
		},
		{
			name: "disabled config",
			config: &config.MultilineConfig{
				Enabled: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mb, err := NewMultilineBuffer(tt.config)
			if err != nil {
				t.Fatalf("NewMultilineBuffer() error = %v", err)
			}
			if mb.enabled {
				t.Error("expected disabled multiline buffer")
			}
		})
	}
}

func TestNewMultilineBuffer_InvalidPattern(t *testing.T) {
	cfg := &config.MultilineConfig{
		Enabled: true,
		Pattern: "[invalid(regex",
	}

	_, err := NewMultilineBuffer(cfg)
	if err == nil {
		t.Fatal("expected error for invalid regex pattern")
	}
}

func TestMultilineBuffer_Disabled_FastPath(t *testing.T) {
	mb, err := NewMultilineBuffer(nil)
	if err != nil {
		t.Fatalf("NewMultilineBuffer() error = %v", err)
	}

	// Should return line immediately without buffering
	complete, entry := mb.Add("Test log line")
	if !complete {
		t.Error("disabled buffer should return lines immediately")
	}
	if entry != "Test log line" {
		t.Errorf("expected unchanged line, got: %s", entry)
	}
}

func TestMultilineBuffer_SingleLine(t *testing.T) {
	cfg := &config.MultilineConfig{
		Enabled:  true,
		Pattern:  `^\[`, // Lines starting with [
		MaxLines: 100,
		Timeout:  1,
	}

	mb, err := NewMultilineBuffer(cfg)
	if err != nil {
		t.Fatalf("NewMultilineBuffer() error = %v", err)
	}

	// First line starts new entry
	complete, entry := mb.Add("[INFO] Starting server")
	if complete {
		t.Error("first line should not be complete")
	}
	if entry != "" {
		t.Error("first line should not return entry")
	}

	// Second line starts new entry, should flush first
	complete, entry = mb.Add("[INFO] Server started")
	if !complete {
		t.Error("new entry should flush previous")
	}
	if entry != "[INFO] Starting server" {
		t.Errorf("expected flushed entry, got: %s", entry)
	}
}

func TestMultilineBuffer_StackTrace(t *testing.T) {
	cfg := &config.MultilineConfig{
		Enabled:  true,
		Pattern:  `^\[`, // Lines starting with [
		MaxLines: 100,
		Timeout:  1,
	}

	mb, err := NewMultilineBuffer(cfg)
	if err != nil {
		t.Fatalf("NewMultilineBuffer() error = %v", err)
	}

	// Log entry with stack trace
	lines := []string{
		"[ERROR] Exception occurred",
		"  at Controller.php:123",
		"  at Middleware.php:45",
		"  at Router.php:67",
	}

	// Add first line (starts new entry)
	complete, _ := mb.Add(lines[0])
	if complete {
		t.Error("first line should not be complete")
	}

	// Add stack trace lines
	for i := 1; i < len(lines); i++ {
		complete, _ = mb.Add(lines[i])
		if complete {
			t.Errorf("stack trace line %d should not trigger flush", i)
		}
	}

	// Manual flush
	entry := mb.Flush()
	expected := strings.Join(lines, "\n")
	if entry != expected {
		t.Errorf("Flush() = %q, want %q", entry, expected)
	}
}

func TestMultilineBuffer_LaravelStackTrace(t *testing.T) {
	cfg := &config.MultilineConfig{
		Enabled:  true,
		Pattern:  `^\[\d{4}-\d{2}-\d{2}`, // Laravel timestamp pattern
		MaxLines: 100,
		Timeout:  1,
	}

	mb, err := NewMultilineBuffer(cfg)
	if err != nil {
		t.Fatalf("NewMultilineBuffer() error = %v", err)
	}

	lines := []string{
		"[2024-01-01 12:00:00] local.ERROR: Undefined variable",
		"Stack trace:",
		"#0 /app/Http/Controllers/UserController.php(42): getValue()",
		"#1 /app/Http/Kernel.php(123): handle()",
		"#2 /public/index.php(52): run()",
	}

	// Add all lines
	var lastEntry string
	for _, line := range lines {
		if complete, entry := mb.Add(line); complete && entry != "" {
			lastEntry = entry
		}
	}

	// Flush remaining buffer
	entry := mb.Flush()
	if entry == "" && lastEntry != "" {
		entry = lastEntry
	}

	// Should have all lines combined
	expected := strings.Join(lines, "\n")
	if entry != expected {
		t.Errorf("Flush() = %q, want %q", entry, expected)
	}
}

func TestMultilineBuffer_MaxLinesProtection(t *testing.T) {
	cfg := &config.MultilineConfig{
		Enabled:  true,
		Pattern:  `^\[ERROR\]`,
		MaxLines: 3, // Very small limit
		Timeout:  1,
	}

	mb, err := NewMultilineBuffer(cfg)
	if err != nil {
		t.Fatalf("NewMultilineBuffer() error = %v", err)
	}

	// Add lines until max is reached
	mb.Add("[ERROR] Exception")
	mb.Add("  line 1")
	complete, entry := mb.Add("  line 2")

	// Should force flush at max lines
	if !complete {
		t.Error("should flush when max lines reached")
	}

	lines := strings.Split(entry, "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
}

func TestMultilineBuffer_TimeoutFlush(t *testing.T) {
	cfg := &config.MultilineConfig{
		Enabled:  true,
		Pattern:  `^\[ERROR\]`,
		MaxLines: 100,
		Timeout:  1, // 1 second
	}

	mb, err := NewMultilineBuffer(cfg)
	if err != nil {
		t.Fatalf("NewMultilineBuffer() error = %v", err)
	}

	// Add line
	mb.Add("[ERROR] Exception")
	mb.Add("  stack trace line")

	// Should not flush immediately
	if mb.ShouldFlush() {
		t.Error("should not flush immediately")
	}

	// Wait for timeout
	time.Sleep(1100 * time.Millisecond)

	// Should flush after timeout
	if !mb.ShouldFlush() {
		t.Error("should flush after timeout")
	}

	entry := mb.Flush()
	if entry == "" {
		t.Error("flush should return buffered entry")
	}
}

func TestMultilineBuffer_MultipleEntries(t *testing.T) {
	cfg := &config.MultilineConfig{
		Enabled:  true,
		Pattern:  `^\[`,
		MaxLines: 100,
		Timeout:  1,
	}

	mb, err := NewMultilineBuffer(cfg)
	if err != nil {
		t.Fatalf("NewMultilineBuffer() error = %v", err)
	}

	entries := [][]string{
		{
			"[ERROR] First error",
			"  stack line 1",
			"  stack line 2",
		},
		{
			"[INFO] Second log",
			"  extra info",
		},
		{
			"[WARN] Third warning",
		},
	}

	var results []string

	for i, entryLines := range entries {
		for j, line := range entryLines {
			// On new entry start (except first line of first entry), previous entry flushes
			if complete, entry := mb.Add(line); complete && entry != "" {
				results = append(results, entry)
			}

			// Last line of last entry needs manual flush
			if i == len(entries)-1 && j == len(entryLines)-1 {
				finalEntry := mb.Flush()
				if finalEntry != "" {
					results = append(results, finalEntry)
				}
			}
		}
	}

	// Should have 3 complete entries
	if len(results) != 3 {
		t.Errorf("expected 3 entries, got %d", len(results))
	}

	// Verify first entry content
	expected := strings.Join(entries[0], "\n")
	if results[0] != expected {
		t.Errorf("first entry = %q, want %q", results[0], expected)
	}
}

func TestMultilineBuffer_EmptyBuffer(t *testing.T) {
	cfg := &config.MultilineConfig{
		Enabled:  true,
		Pattern:  `^\[`,
		MaxLines: 100,
		Timeout:  1,
	}

	mb, err := NewMultilineBuffer(cfg)
	if err != nil {
		t.Fatalf("NewMultilineBuffer() error = %v", err)
	}

	// Flush empty buffer should return empty string
	entry := mb.Flush()
	if entry != "" {
		t.Errorf("flush empty buffer should return empty string, got: %s", entry)
	}

	// ShouldFlush on empty buffer should return false
	if mb.ShouldFlush() {
		t.Error("ShouldFlush on empty buffer should return false")
	}
}

func TestMultilineBuffer_NoPattern(t *testing.T) {
	cfg := &config.MultilineConfig{
		Enabled:  true,
		Pattern:  "", // No pattern
		MaxLines: 100,
		Timeout:  1,
	}

	mb, err := NewMultilineBuffer(cfg)
	if err != nil {
		t.Fatalf("NewMultilineBuffer() error = %v", err)
	}

	// Without pattern, should return lines immediately
	complete, entry := mb.Add("Test log line")
	if !complete {
		t.Error("buffer without pattern should return lines immediately")
	}
	if entry != "Test log line" {
		t.Errorf("expected unchanged line, got: %s", entry)
	}
}

func TestMultilineBuffer_PHPException(t *testing.T) {
	cfg := &config.MultilineConfig{
		Enabled:  true,
		Pattern:  `^(PHP Fatal|PHP Warning|Exception)`,
		MaxLines: 100,
		Timeout:  1,
	}

	mb, err := NewMultilineBuffer(cfg)
	if err != nil {
		t.Fatalf("NewMultilineBuffer() error = %v", err)
	}

	lines := []string{
		"PHP Fatal error:  Uncaught Exception: Database connection failed",
		"Stack trace:",
		"#0 /app/Database/Connection.php(234): connect()",
		"#1 /app/Database/Query.php(45): execute()",
		"  thrown in /app/Database/Connection.php on line 234",
	}

	// Add all lines
	for i, line := range lines {
		complete, _ := mb.Add(line)
		// Only first line should not trigger completion
		if i > 0 && complete {
			t.Errorf("continuation line %d should not trigger flush", i)
		}
	}

	// Flush buffer
	entry := mb.Flush()
	expected := strings.Join(lines, "\n")
	if entry != expected {
		t.Errorf("Flush() = %q, want %q", entry, expected)
	}
}

func TestMultilineBuffer_BufferSize(t *testing.T) {
	cfg := &config.MultilineConfig{
		Enabled:  true,
		Pattern:  `^\[`,
		MaxLines: 100,
		Timeout:  1,
	}

	mb, err := NewMultilineBuffer(cfg)
	if err != nil {
		t.Fatalf("NewMultilineBuffer() error = %v", err)
	}

	if mb.BufferSize() != 0 {
		t.Errorf("initial buffer size should be 0, got: %d", mb.BufferSize())
	}

	mb.Add("[ERROR] Test")
	if mb.BufferSize() != 1 {
		t.Errorf("buffer size should be 1, got: %d", mb.BufferSize())
	}

	mb.Add("  stack line")
	if mb.BufferSize() != 2 {
		t.Errorf("buffer size should be 2, got: %d", mb.BufferSize())
	}

	mb.Flush()
	if mb.BufferSize() != 0 {
		t.Errorf("buffer size after flush should be 0, got: %d", mb.BufferSize())
	}
}

func TestMultilineBuffer_IsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		config  *config.MultilineConfig
		enabled bool
	}{
		{
			name:    "nil config",
			config:  nil,
			enabled: false,
		},
		{
			name: "disabled config",
			config: &config.MultilineConfig{
				Enabled: false,
			},
			enabled: false,
		},
		{
			name: "enabled config",
			config: &config.MultilineConfig{
				Enabled:  true,
				Pattern:  `^\[`,
				MaxLines: 100,
				Timeout:  1,
			},
			enabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mb, err := NewMultilineBuffer(tt.config)
			if err != nil {
				t.Fatalf("NewMultilineBuffer() error = %v", err)
			}

			if mb.IsEnabled() != tt.enabled {
				t.Errorf("IsEnabled() = %v, want %v", mb.IsEnabled(), tt.enabled)
			}
		})
	}
}
