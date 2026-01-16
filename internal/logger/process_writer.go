package logger

import (
	"bufio"
	"bytes"
	"fmt"
	"log/slog"
	"time"

	"github.com/cboxdk/init/internal/config"
)

// maxBufferSize is the maximum size for the line buffer to prevent OOM
// If a process outputs continuous data without newlines, flush partial content
const maxBufferSize = 64 * 1024 // 64KB

// ProcessWriter captures process output and logs it with structured metadata
// Implements the full logging pipeline: Multiline → Redaction → JSON → Level → Filters
type ProcessWriter struct {
	Logger      *slog.Logger
	ProcessName string
	InstanceID  string
	Stream      string // stdout or stderr

	// Advanced logging components
	redactor      *Redactor
	multiline     *MultilineBuffer
	jsonParser    *JSONParser
	levelDetector *LevelDetector
	filters       *LogFilters

	// Log buffer for TUI/API access
	logBuffer *LogBuffer

	buffer bytes.Buffer
}

// NewProcessWriter creates a new ProcessWriter with advanced logging features
func NewProcessWriter(logger *slog.Logger, processName, instanceID, stream string, cfg *config.LoggingConfig) (*ProcessWriter, error) {
	pw := &ProcessWriter{
		Logger:      logger,
		ProcessName: processName,
		InstanceID:  instanceID,
		Stream:      stream,
		logBuffer:   NewLogBuffer(1000), // Store last 1000 log entries
	}

	// Initialize components only if config provided
	if cfg == nil {
		return pw, nil
	}

	// Initialize Redactor (COMPLIANCE CRITICAL)
	var err error
	pw.redactor, err = NewRedactor(cfg.Redaction)
	if err != nil {
		return nil, fmt.Errorf("failed to create redactor: %w", err)
	}

	// Initialize MultilineBuffer
	pw.multiline, err = NewMultilineBuffer(cfg.Multiline)
	if err != nil {
		return nil, fmt.Errorf("failed to create multiline buffer: %w", err)
	}

	// Initialize JSONParser
	pw.jsonParser = NewJSONParser(cfg.JSON)

	// Initialize LevelDetector
	pw.levelDetector, err = NewLevelDetector(cfg.LevelDetection)
	if err != nil {
		return nil, fmt.Errorf("failed to create level detector: %w", err)
	}

	// Initialize LogFilters
	pw.filters, err = NewLogFilters(cfg.Filters, cfg.MinLevel)
	if err != nil {
		return nil, fmt.Errorf("failed to create log filters: %w", err)
	}

	return pw, nil
}

// Write implements io.Writer
// Processes incoming bytes through the full logging pipeline
func (pw *ProcessWriter) Write(p []byte) (n int, err error) {
	pw.buffer.Write(p)

	// Process complete lines
	scanner := bufio.NewScanner(&pw.buffer)
	var remaining bytes.Buffer

	for scanner.Scan() {
		line := scanner.Text()
		pw.processLine(line)
	}

	// Check if multiline buffer should be flushed due to timeout
	if pw.multiline != nil && pw.multiline.ShouldFlush() {
		if entry := pw.multiline.Flush(); entry != "" {
			pw.processEntry(entry)
		}
	}

	// Keep incomplete line in buffer
	if pw.buffer.Len() > 0 {
		remaining.Write(pw.buffer.Bytes())
	}
	pw.buffer = remaining

	// SAFETY: Flush buffer if it exceeds max size to prevent OOM
	// This handles pathological cases where process outputs no newlines
	if pw.buffer.Len() > maxBufferSize {
		partialLine := pw.buffer.String()
		pw.buffer.Reset()
		pw.Logger.Warn("Flushing oversized buffer (no newline)",
			"process", pw.ProcessName,
			"instance_id", pw.InstanceID,
			"buffer_size", len(partialLine),
		)
		pw.processLine(partialLine)
	}

	return len(p), nil
}

// processLine handles a single line from process output
func (pw *ProcessWriter) processLine(line string) {
	// Step 1: Multiline buffering
	if pw.multiline != nil && pw.multiline.IsEnabled() {
		if complete, entry := pw.multiline.Add(line); complete && entry != "" {
			// Buffer complete and has content, process it
			pw.processEntry(entry)
		}
		// Either still buffering or already processed
		return
	}

	// No multiline buffering, process line directly
	pw.processEntry(line)
}

// processEntry handles a complete log entry (single line or multiline)
// Applies: Redaction → JSON → Level → Filters → Log
func (pw *ProcessWriter) processEntry(entry string) {
	// Step 2: Redaction (ALWAYS FIRST for compliance)
	if pw.redactor != nil && pw.redactor.IsEnabled() {
		entry = pw.redactor.Redact(entry)
	}

	// Step 3: JSON parsing
	var message string
	var level slog.Level
	var attrs []slog.Attr

	if pw.jsonParser != nil && pw.jsonParser.IsEnabled() {
		isJSON, data := pw.jsonParser.Parse(entry)
		if isJSON {
			// Extract message, level, and attributes from JSON
			message, level, attrs = pw.jsonParser.ToLogAttrs(data)

			// If no message extracted, use original entry
			if message == "" {
				message = entry
			}
		} else {
			// Not JSON, treat as plain text
			message = entry
			level = slog.LevelInfo
		}
	} else {
		// JSON parsing disabled
		message = entry
		level = slog.LevelInfo
	}

	// Step 4: Level detection (if level not set by JSON)
	if pw.levelDetector != nil && pw.levelDetector.IsEnabled() && level == slog.LevelInfo {
		level = pw.levelDetector.Detect(entry)
	}

	// Step 5: Filter check
	if pw.filters != nil {
		if !pw.filters.ShouldLog(entry, level) {
			return // Drop log
		}
	}

	// Step 6: Log with structured metadata
	// Add instance_id and stream as base attributes
	baseAttrs := []any{
		"instance_id", pw.InstanceID,
		"stream", pw.Stream,
	}

	// Add JSON-extracted attributes
	for _, attr := range attrs {
		baseAttrs = append(baseAttrs, attr.Key, attr.Value)
	}

	// Log at appropriate level
	var levelStr string
	switch level {
	case slog.LevelDebug:
		pw.Logger.Debug(message, baseAttrs...)
		levelStr = "debug"
	case slog.LevelInfo:
		pw.Logger.Debug(message, baseAttrs...)
		levelStr = "info"
	case slog.LevelWarn:
		pw.Logger.Warn(message, baseAttrs...)
		levelStr = "warn"
	case slog.LevelError:
		pw.Logger.Error(message, baseAttrs...)
		levelStr = "error"
	default:
		pw.Logger.Debug(message, baseAttrs...)
		levelStr = "info"
	}

	// Step 7: Add to log buffer for TUI/API access
	if pw.logBuffer != nil {
		pw.logBuffer.Add(LogEntry{
			Timestamp:   time.Now(),
			ProcessName: pw.ProcessName,
			InstanceID:  pw.InstanceID,
			Stream:      pw.Stream,
			Message:     message,
			Level:       levelStr,
		})
	}
}

// Flush flushes any remaining buffered output
// CRITICAL: Must be called when process exits to avoid losing buffered output
func (pw *ProcessWriter) Flush() {
	// Flush incomplete line buffer
	if pw.buffer.Len() > 0 {
		line := pw.buffer.String()
		pw.buffer.Reset()
		if line != "" {
			pw.processLine(line)
		}
	}

	// Flush multiline buffer
	if pw.multiline != nil && pw.multiline.BufferSize() > 0 {
		if entry := pw.multiline.Flush(); entry != "" {
			pw.processEntry(entry)
		}
	}
}

// GetLogs returns all log entries from the buffer
func (pw *ProcessWriter) GetLogs() []LogEntry {
	if pw.logBuffer == nil {
		return []LogEntry{}
	}
	return pw.logBuffer.GetAll()
}

// GetRecentLogs returns the last n log entries
func (pw *ProcessWriter) GetRecentLogs(n int) []LogEntry {
	if pw.logBuffer == nil {
		return []LogEntry{}
	}
	return pw.logBuffer.GetRecent(n)
}

// AddEvent adds a lifecycle event to the log buffer
// Events use Level "event" to distinguish them from regular log output
// These are rendered as dividers in the TUI log view
func (pw *ProcessWriter) AddEvent(message string) {
	if pw.logBuffer == nil {
		return
	}
	pw.logBuffer.Add(LogEntry{
		Timestamp:   time.Now(),
		ProcessName: pw.ProcessName,
		InstanceID:  pw.InstanceID,
		Stream:      "event",
		Message:     message,
		Level:       "event",
	})
}
