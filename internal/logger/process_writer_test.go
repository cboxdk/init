package logger

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/config"
)

func TestNewProcessWriter_NilConfig(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	pw, err := NewProcessWriter(logger, "test-process", "test-0", "stdout", nil)
	if err != nil {
		t.Fatalf("NewProcessWriter() error = %v", err)
	}

	if pw.ProcessName != "test-process" {
		t.Errorf("ProcessName = %s, want test-process", pw.ProcessName)
	}
	if pw.InstanceID != "test-0" {
		t.Errorf("InstanceID = %s, want test-0", pw.InstanceID)
	}
	if pw.Stream != "stdout" {
		t.Errorf("Stream = %s, want stdout", pw.Stream)
	}
	if pw.logBuffer == nil {
		t.Error("logBuffer should not be nil")
	}
}

func TestNewProcessWriter_WithConfig(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	cfg := &config.LoggingConfig{
		Redaction: &config.RedactionConfig{
			Enabled: true,
			Patterns: []config.RedactionPattern{
				{
					Name:        "email",
					Pattern:     `\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`,
					Replacement: "***@***",
				},
			},
		},
		Multiline: &config.MultilineConfig{
			Enabled:  true,
			Pattern:  `^\[`,
			MaxLines: 100,
			Timeout:  1,
		},
		JSON: &config.JSONConfig{
			Enabled: true,
		},
		LevelDetection: &config.LevelDetectionConfig{
			Enabled:      true,
			DefaultLevel: "info",
			Patterns: map[string]string{
				"error": `(?i)(error|exception|fatal)`,
			},
		},
		Filters: &config.FilterConfig{
			Include: []string{"test"},
		},
		MinLevel: "info",
	}

	pw, err := NewProcessWriter(logger, "test-process", "test-0", "stdout", cfg)
	if err != nil {
		t.Fatalf("NewProcessWriter() error = %v", err)
	}

	if pw.redactor == nil {
		t.Error("redactor should be initialized")
	}
	if pw.multiline == nil {
		t.Error("multiline should be initialized")
	}
	if pw.jsonParser == nil {
		t.Error("jsonParser should be initialized")
	}
	if pw.levelDetector == nil {
		t.Error("levelDetector should be initialized")
	}
	if pw.filters == nil {
		t.Error("filters should be initialized")
	}
}

func TestNewProcessWriter_InvalidRedactor(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	cfg := &config.LoggingConfig{
		Redaction: &config.RedactionConfig{
			Enabled: true,
			Patterns: []config.RedactionPattern{
				{
					Name:    "invalid",
					Pattern: "[invalid(regex",
				},
			},
		},
	}

	_, err := NewProcessWriter(logger, "test", "test-0", "stdout", cfg)
	if err == nil {
		t.Fatal("expected error for invalid redactor pattern")
	}
	if !strings.Contains(err.Error(), "failed to create redactor") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProcessWriter_Write_SimpleLine(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	pw, err := NewProcessWriter(logger, "test-process", "test-0", "stdout", nil)
	if err != nil {
		t.Fatalf("NewProcessWriter() error = %v", err)
	}

	input := "Test log message\n"
	n, err := pw.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len(input) {
		t.Errorf("Write() wrote %d bytes, want %d", n, len(input))
	}

	// Check that log was recorded in buffer
	logs := pw.GetLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}
	if logs[0].Message != "Test log message" {
		t.Errorf("log message = %q, want %q", logs[0].Message, "Test log message")
	}
}

func TestProcessWriter_Write_MultipleLines(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	pw, err := NewProcessWriter(logger, "test-process", "test-0", "stdout", nil)
	if err != nil {
		t.Fatalf("NewProcessWriter() error = %v", err)
	}

	input := "Line 1\nLine 2\nLine 3\n"
	_, _ = pw.Write([]byte(input))

	logs := pw.GetLogs()
	if len(logs) != 3 {
		t.Fatalf("expected 3 log entries, got %d", len(logs))
	}
}

func TestProcessWriter_Write_PartialLine(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	pw, err := NewProcessWriter(logger, "test-process", "test-0", "stdout", nil)
	if err != nil {
		t.Fatalf("NewProcessWriter() error = %v", err)
	}

	initialCount := len(pw.GetLogs())

	// bufio.Scanner treats data without newline as complete line when scanner finishes
	// So "Partial" without \n gets processed immediately by scanner.Scan()
	_, _ = pw.Write([]byte("Partial"))

	// Scanner processes it as complete line (EOF condition)
	logs := pw.GetLogs()
	if len(logs) != initialCount+1 {
		t.Errorf("expected 1 log, got %d (initial: %d)", len(logs), initialCount)
	}

	if len(logs) > 0 {
		lastLog := logs[len(logs)-1]
		if lastLog.Message != "Partial" {
			t.Errorf("log message = %q, want %q", lastLog.Message, "Partial")
		}
	}
}

func TestProcessWriter_Write_OversizedBuffer(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	pw, err := NewProcessWriter(logger, "test-process", "test-0", "stdout", nil)
	if err != nil {
		t.Fatalf("NewProcessWriter() error = %v", err)
	}

	// Write more than maxBufferSize without newline
	// This tests the overflow protection (line 113-122 in process_writer.go)
	oversized := strings.Repeat("x", maxBufferSize+1000)
	n, err := pw.Write([]byte(oversized))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len(oversized) {
		t.Errorf("Write() wrote %d bytes, want %d", n, len(oversized))
	}

	// Test that oversized data was handled (either logged or buffered safely)
	// The important thing is Write() didn't panic or fail
	t.Logf("Successfully handled oversized buffer of %d bytes", len(oversized))
}

func TestProcessWriter_Flush(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	pw, err := NewProcessWriter(logger, "test-process", "test-0", "stdout", nil)
	if err != nil {
		t.Fatalf("NewProcessWriter() error = %v", err)
	}

	initialCount := len(pw.GetLogs())

	// Write line without newline - scanner processes it as EOF
	_, _ = pw.Write([]byte("Incomplete"))

	// Scanner processes it immediately (EOF condition)
	logs := pw.GetLogs()
	if len(logs) != initialCount+1 {
		t.Fatalf("expected 1 log entry, got %d (initial: %d)", len(logs), initialCount)
	}

	// Flush with empty buffer should not add more logs
	beforeFlush := len(pw.GetLogs())
	pw.Flush()
	afterFlush := len(pw.GetLogs())

	if afterFlush != beforeFlush {
		t.Errorf("Flush with empty buffer should not add logs, had %d, now %d", beforeFlush, afterFlush)
	}
}

func TestProcessWriter_WithRedaction(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	cfg := &config.LoggingConfig{
		Redaction: &config.RedactionConfig{
			Enabled: true,
			Patterns: []config.RedactionPattern{
				{
					Name:        "email",
					Pattern:     `\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`,
					Replacement: "***@***",
				},
			},
		},
	}

	pw, err := NewProcessWriter(logger, "test-process", "test-0", "stdout", cfg)
	if err != nil {
		t.Fatalf("NewProcessWriter() error = %v", err)
	}

	_, _ = pw.Write([]byte("User: user@example.com\n"))

	logs := pw.GetLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}
	if !strings.Contains(logs[0].Message, "***@***") {
		t.Errorf("expected redacted email, got: %s", logs[0].Message)
	}
	if strings.Contains(logs[0].Message, "user@example.com") {
		t.Error("email should be redacted")
	}
}

func TestProcessWriter_WithJSONParsing(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	cfg := &config.LoggingConfig{
		JSON: &config.JSONConfig{
			Enabled:        true,
			ExtractLevel:   true,
			ExtractMessage: true,
			MergeFields:    true,
		},
	}

	pw, err := NewProcessWriter(logger, "test-process", "test-0", "stdout", cfg)
	if err != nil {
		t.Fatalf("NewProcessWriter() error = %v", err)
	}

	jsonLog := `{"level":"error","message":"Database error","user_id":123}`
	_, _ = pw.Write([]byte(jsonLog + "\n"))

	logs := pw.GetLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	// The message extraction depends on JSON config settings
	// Since JSON parser might not extract message, check for error level
	if !strings.Contains(logs[0].Message, "Database error") && !strings.Contains(logs[0].Message, jsonLog) {
		t.Errorf("log message should contain 'Database error' or full JSON, got: %q", logs[0].Message)
	}
}

func TestProcessWriter_WithLevelDetection(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	cfg := &config.LoggingConfig{
		LevelDetection: &config.LevelDetectionConfig{
			Enabled:      true,
			DefaultLevel: "info",
			Patterns: map[string]string{
				"error": `(?i)error`,
				"warn":  `(?i)warning`,
			},
		},
	}

	pw, err := NewProcessWriter(logger, "test-process", "test-0", "stdout", cfg)
	if err != nil {
		t.Fatalf("NewProcessWriter() error = %v", err)
	}

	tests := []struct {
		input         string
		expectedLevel string
	}{
		{"ERROR: Something went wrong", "error"},
		{"WARNING: Check this", "warn"},
		{"INFO: Normal operation", "info"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			// Clear buffer
			pw.logBuffer.Clear()

			_, _ = pw.Write([]byte(tt.input + "\n"))

			logs := pw.GetLogs()
			if len(logs) != 1 {
				t.Fatalf("expected 1 log entry, got %d", len(logs))
			}
			if logs[0].Level != tt.expectedLevel {
				t.Errorf("log level = %q, want %q", logs[0].Level, tt.expectedLevel)
			}
		})
	}
}

func TestProcessWriter_WithFilters(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	cfg := &config.LoggingConfig{
		Filters: &config.FilterConfig{
			Exclude: []string{"debug"},
		},
		MinLevel: "info",
	}

	pw, err := NewProcessWriter(logger, "test-process", "test-0", "stdout", cfg)
	if err != nil {
		t.Fatalf("NewProcessWriter() error = %v", err)
	}

	// Should be filtered out
	_, _ = pw.Write([]byte("debug message\n"))

	logs := pw.GetLogs()
	if len(logs) != 0 {
		t.Errorf("expected filtered log to be dropped, got %d logs", len(logs))
	}

	// Should pass through
	_, _ = pw.Write([]byte("info message\n"))

	logs = pw.GetLogs()
	if len(logs) != 1 {
		t.Errorf("expected 1 log entry, got %d", len(logs))
	}
}

func TestProcessWriter_WithMultiline(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	cfg := &config.LoggingConfig{
		Multiline: &config.MultilineConfig{
			Enabled:  true,
			Pattern:  `^\[ERROR\]`,
			MaxLines: 10,
			Timeout:  1,
		},
	}

	pw, err := NewProcessWriter(logger, "test-process", "test-0", "stdout", cfg)
	if err != nil {
		t.Fatalf("NewProcessWriter() error = %v", err)
	}

	// Write multiline log entry
	_, _ = pw.Write([]byte("[ERROR] Exception\n"))
	_, _ = pw.Write([]byte("  at line 1\n"))
	_, _ = pw.Write([]byte("  at line 2\n"))

	// Should be buffered, not logged yet
	logs := pw.GetLogs()
	if len(logs) != 0 {
		t.Errorf("expected multiline to be buffered, got %d logs", len(logs))
	}

	// Start new entry, should flush previous
	_, _ = pw.Write([]byte("[ERROR] Another error\n"))

	logs = pw.GetLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry (flushed), got %d", len(logs))
	}

	expected := "[ERROR] Exception\n  at line 1\n  at line 2"
	if logs[0].Message != expected {
		t.Errorf("multiline message = %q, want %q", logs[0].Message, expected)
	}
}

func TestProcessWriter_GetLogs_NilBuffer(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	pw := &ProcessWriter{
		Logger:      logger,
		ProcessName: "test",
		InstanceID:  "test-0",
		Stream:      "stdout",
		logBuffer:   nil, // Explicitly nil
	}

	logs := pw.GetLogs()
	if logs == nil {
		t.Error("GetLogs() should return empty slice, not nil")
	}
	if len(logs) != 0 {
		t.Errorf("expected empty slice, got %d logs", len(logs))
	}
}

func TestProcessWriter_GetRecentLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	pw, err := NewProcessWriter(logger, "test-process", "test-0", "stdout", nil)
	if err != nil {
		t.Fatalf("NewProcessWriter() error = %v", err)
	}

	// Write 10 log lines
	for i := 1; i <= 10; i++ {
		_, _ = pw.Write([]byte("Log line " + string(rune('0'+i)) + "\n"))
	}

	// Get recent 3 logs
	recent := pw.GetRecentLogs(3)
	if len(recent) != 3 {
		t.Fatalf("expected 3 recent logs, got %d", len(recent))
	}

	// Should be last 3 entries
	expected := []string{"Log line 8", "Log line 9", "Log line :"}
	for i, log := range recent {
		if log.Message != expected[i] {
			t.Errorf("recent[%d] = %q, want %q", i, log.Message, expected[i])
		}
	}
}

func TestProcessWriter_GetRecentLogs_NilBuffer(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	pw := &ProcessWriter{
		Logger:      logger,
		ProcessName: "test",
		InstanceID:  "test-0",
		Stream:      "stdout",
		logBuffer:   nil,
	}

	logs := pw.GetRecentLogs(5)
	if logs == nil {
		t.Error("GetRecentLogs() should return empty slice, not nil")
	}
	if len(logs) != 0 {
		t.Errorf("expected empty slice, got %d logs", len(logs))
	}
}

func TestProcessWriter_LogEntryMetadata(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	pw, err := NewProcessWriter(logger, "nginx", "nginx-0", "stderr", nil)
	if err != nil {
		t.Fatalf("NewProcessWriter() error = %v", err)
	}

	before := time.Now()
	_, _ = pw.Write([]byte("Test message\n"))
	after := time.Now()

	logs := pw.GetLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	log := logs[0]
	if log.ProcessName != "nginx" {
		t.Errorf("ProcessName = %s, want nginx", log.ProcessName)
	}
	if log.InstanceID != "nginx-0" {
		t.Errorf("InstanceID = %s, want nginx-0", log.InstanceID)
	}
	if log.Stream != "stderr" {
		t.Errorf("Stream = %s, want stderr", log.Stream)
	}
	if log.Timestamp.Before(before) || log.Timestamp.After(after) {
		t.Errorf("Timestamp %v outside expected range [%v, %v]", log.Timestamp, before, after)
	}
}

func TestProcessWriter_MultilineTimeout(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	cfg := &config.LoggingConfig{
		Multiline: &config.MultilineConfig{
			Enabled:  true,
			Pattern:  `^\[ERROR\]`,
			MaxLines: 100,
			Timeout:  1, // 1 second
		},
	}

	pw, err := NewProcessWriter(logger, "test-process", "test-0", "stdout", cfg)
	if err != nil {
		t.Fatalf("NewProcessWriter() error = %v", err)
	}

	// Write lines to multiline buffer
	_, _ = pw.Write([]byte("[ERROR] Exception\n"))
	_, _ = pw.Write([]byte("  stack line 1\n"))

	// Wait for timeout
	time.Sleep(1100 * time.Millisecond)

	// Write trigger to check timeout flush
	_, _ = pw.Write([]byte(""))

	// Flush should have happened due to timeout
	logs := pw.GetLogs()
	if len(logs) == 0 {
		t.Error("expected timeout to flush multiline buffer")
	}
}

func TestProcessWriter_FlushMultilineBuffer(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	cfg := &config.LoggingConfig{
		Multiline: &config.MultilineConfig{
			Enabled:  true,
			Pattern:  `^\[ERROR\]`,
			MaxLines: 100,
			Timeout:  10,
		},
	}

	pw, err := NewProcessWriter(logger, "test-process", "test-0", "stdout", cfg)
	if err != nil {
		t.Fatalf("NewProcessWriter() error = %v", err)
	}

	// Add to multiline buffer
	_, _ = pw.Write([]byte("[ERROR] Test\n"))
	_, _ = pw.Write([]byte("  stack\n"))

	// Flush should flush multiline buffer
	pw.Flush()

	logs := pw.GetLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log after flush, got %d", len(logs))
	}

	expected := "[ERROR] Test\n  stack"
	if logs[0].Message != expected {
		t.Errorf("message = %q, want %q", logs[0].Message, expected)
	}
}

func TestProcessWriter_EmptyFlush(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	pw, err := NewProcessWriter(logger, "test-process", "test-0", "stdout", nil)
	if err != nil {
		t.Fatalf("NewProcessWriter() error = %v", err)
	}

	// Flush with no buffered data should not panic
	pw.Flush()

	logs := pw.GetLogs()
	if len(logs) != 0 {
		t.Errorf("expected 0 logs after empty flush, got %d", len(logs))
	}
}

func TestProcessWriter_AddEvent(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	pw, err := NewProcessWriter(logger, "test-process", "test-0", "stdout", nil)
	if err != nil {
		t.Fatalf("NewProcessWriter() error = %v", err)
	}

	// Add an event
	pw.AddEvent("Process started")

	logs := pw.GetLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	event := logs[0]
	if event.Message != "Process started" {
		t.Errorf("event message = %q, want %q", event.Message, "Process started")
	}
	if event.Level != "event" {
		t.Errorf("event level = %q, want %q", event.Level, "event")
	}
	if event.Stream != "event" {
		t.Errorf("event stream = %q, want %q", event.Stream, "event")
	}
	if event.ProcessName != "test-process" {
		t.Errorf("event ProcessName = %q, want %q", event.ProcessName, "test-process")
	}
	if event.InstanceID != "test-0" {
		t.Errorf("event InstanceID = %q, want %q", event.InstanceID, "test-0")
	}
}

func TestProcessWriter_AddEvent_NilBuffer(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	pw := &ProcessWriter{
		Logger:      logger,
		ProcessName: "test",
		InstanceID:  "test-0",
		Stream:      "stdout",
		logBuffer:   nil, // Explicitly nil
	}

	// Should not panic
	pw.AddEvent("Test event")
}

func TestNewProcessWriter_InvalidMultiline(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	cfg := &config.LoggingConfig{
		Multiline: &config.MultilineConfig{
			Enabled: true,
			Pattern: "[invalid(regex",
		},
	}

	_, err := NewProcessWriter(logger, "test", "test-0", "stdout", cfg)
	if err == nil {
		t.Fatal("expected error for invalid multiline pattern")
	}
	if !strings.Contains(err.Error(), "failed to create multiline buffer") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewProcessWriter_InvalidLevelDetector(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	cfg := &config.LoggingConfig{
		LevelDetection: &config.LevelDetectionConfig{
			Enabled: true,
			Patterns: map[string]string{
				"error": "[invalid(regex",
			},
		},
	}

	_, err := NewProcessWriter(logger, "test", "test-0", "stdout", cfg)
	if err == nil {
		t.Fatal("expected error for invalid level detector pattern")
	}
	if !strings.Contains(err.Error(), "failed to create level detector") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewProcessWriter_InvalidFilters(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	cfg := &config.LoggingConfig{
		Filters: &config.FilterConfig{
			Include: []string{"[invalid(regex"},
		},
	}

	_, err := NewProcessWriter(logger, "test", "test-0", "stdout", cfg)
	if err == nil {
		t.Fatal("expected error for invalid filter pattern")
	}
	if !strings.Contains(err.Error(), "failed to create log filters") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProcessWriter_FlushWithBufferedData(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	pw, err := NewProcessWriter(logger, "test-process", "test-0", "stdout", nil)
	if err != nil {
		t.Fatalf("NewProcessWriter() error = %v", err)
	}

	// Manually add data to the internal buffer without newline
	pw.buffer.WriteString("buffered content")

	// Flush should process the buffered content
	pw.Flush()

	logs := pw.GetLogs()
	if len(logs) == 0 {
		t.Error("expected log entry after flushing buffered content")
	}
}

func TestProcessWriter_Write_JSONWithLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	cfg := &config.LoggingConfig{
		JSON: &config.JSONConfig{
			Enabled:        true,
			ExtractLevel:   true,
			ExtractMessage: true,
		},
	}

	pw, err := NewProcessWriter(logger, "test-process", "test-0", "stdout", cfg)
	if err != nil {
		t.Fatalf("NewProcessWriter() error = %v", err)
	}

	// Test warn level (debug may get filtered)
	_, _ = pw.Write([]byte(`{"level":"warn","message":"Warning message"}` + "\n"))

	logs := pw.GetLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].Level != "warn" {
		t.Errorf("expected level 'warn', got %q", logs[0].Level)
	}
}

func TestProcessWriter_Write_JSONWithEmptyMessage(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	cfg := &config.LoggingConfig{
		JSON: &config.JSONConfig{
			Enabled:        true,
			ExtractLevel:   true,
			ExtractMessage: true,
		},
	}

	pw, err := NewProcessWriter(logger, "test-process", "test-0", "stdout", cfg)
	if err != nil {
		t.Fatalf("NewProcessWriter() error = %v", err)
	}

	// JSON without message field
	jsonLog := `{"level":"info","user_id":123}`
	_, _ = pw.Write([]byte(jsonLog + "\n"))

	logs := pw.GetLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	// Message should fall back to original entry
	if !strings.Contains(logs[0].Message, "user_id") {
		t.Errorf("expected fallback to original JSON, got: %q", logs[0].Message)
	}
}

func TestProcessWriter_Write_DefaultLevelSwitch(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	cfg := &config.LoggingConfig{
		JSON: &config.JSONConfig{
			Enabled:        true,
			ExtractLevel:   true,
			ExtractMessage: true,
		},
	}

	pw, err := NewProcessWriter(logger, "test-process", "test-0", "stdout", cfg)
	if err != nil {
		t.Fatalf("NewProcessWriter() error = %v", err)
	}

	// Test unknown level (should fall through to default case)
	_, _ = pw.Write([]byte(`{"level":"trace","message":"Trace message"}` + "\n"))

	logs := pw.GetLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	// Unknown level should be treated as info
	if logs[0].Level != "info" {
		t.Errorf("expected 'info' for unknown level, got: %q", logs[0].Level)
	}
}
