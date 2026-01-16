package logger

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cboxdk/init/internal/config"
)

// MultilineBuffer buffers multiline log entries (e.g., stack traces)
type MultilineBuffer struct {
	enabled      bool
	startPattern *regexp.Regexp
	maxLines     int
	timeout      time.Duration

	lines     []string
	startTime time.Time
}

// NewMultilineBuffer creates a new MultilineBuffer from configuration
func NewMultilineBuffer(cfg *config.MultilineConfig) (*MultilineBuffer, error) {
	if cfg == nil || !cfg.Enabled {
		return &MultilineBuffer{enabled: false}, nil
	}

	// Compile start pattern
	var startPattern *regexp.Regexp
	var err error
	if cfg.Pattern != "" {
		startPattern, err = regexp.Compile(cfg.Pattern)
		if err != nil {
			return nil, fmt.Errorf("failed to compile multiline pattern: %w", err)
		}
	}

	// Set defaults
	maxLines := cfg.MaxLines
	if maxLines == 0 {
		maxLines = 100
	}

	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout == 0 {
		timeout = 1 * time.Second
	}

	return &MultilineBuffer{
		enabled:      true,
		startPattern: startPattern,
		maxLines:     maxLines,
		timeout:      timeout,
		lines:        make([]string, 0, 10),
	}, nil
}

// Add adds a line to the buffer
// Returns (true, entry) if the buffer is complete and should be flushed
// Returns (false, "") if the line was buffered and more lines are expected
//
// Logic:
// - If line matches startPattern and buffer is not empty: flush buffer, start new entry
// - If line matches startPattern and buffer is empty: start new entry
// - If line doesn't match startPattern: add to current buffer
// - If buffer exceeds maxLines: force flush
// - If timeout exceeded: force flush (checked externally via ShouldFlush)
func (mb *MultilineBuffer) Add(line string) (complete bool, entry string) {
	// Fast-path: disabled buffer
	if !mb.enabled {
		return true, line
	}

	// Fast-path: no start pattern configured
	if mb.startPattern == nil {
		return true, line
	}

	// Check if this line starts a new log entry
	isStart := mb.startPattern.MatchString(line)

	// If this is a new entry and we have buffered lines, flush the buffer
	if isStart && len(mb.lines) > 0 {
		flushed := mb.flush()
		// Start new buffer with current line
		mb.lines = []string{line}
		mb.startTime = time.Now()
		return true, flushed
	}

	// Add line to buffer
	mb.lines = append(mb.lines, line)

	// Initialize start time if this is the first line
	if len(mb.lines) == 1 {
		mb.startTime = time.Now()
	}

	// Check if buffer exceeded max lines (force flush)
	if len(mb.lines) >= mb.maxLines {
		return true, mb.flush()
	}

	// Buffer not complete yet
	return false, ""
}

// ShouldFlush returns true if the buffer should be flushed due to timeout
func (mb *MultilineBuffer) ShouldFlush() bool {
	if !mb.enabled || len(mb.lines) == 0 {
		return false
	}

	return time.Since(mb.startTime) >= mb.timeout
}

// Flush returns the buffered entry and clears the buffer
// Returns empty string if buffer is empty
func (mb *MultilineBuffer) Flush() string {
	if !mb.enabled || len(mb.lines) == 0 {
		return ""
	}

	return mb.flush()
}

// flush is the internal flush implementation
func (mb *MultilineBuffer) flush() string {
	if len(mb.lines) == 0 {
		return ""
	}

	entry := strings.Join(mb.lines, "\n")
	mb.lines = mb.lines[:0] // Clear buffer but keep capacity
	mb.startTime = time.Time{}
	return entry
}

// IsEnabled returns whether multiline buffering is enabled
func (mb *MultilineBuffer) IsEnabled() bool {
	return mb.enabled
}

// BufferSize returns the current number of buffered lines
func (mb *MultilineBuffer) BufferSize() int {
	return len(mb.lines)
}
