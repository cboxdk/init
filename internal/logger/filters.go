package logger

import (
	"fmt"
	"log/slog"
	"regexp"

	"github.com/cboxdk/init/internal/config"
)

// LogFilters handles include/exclude filtering and level filtering
type LogFilters struct {
	excludePatterns []*regexp.Regexp
	includePatterns []*regexp.Regexp
	minLevel        slog.Level
	hasFilters      bool
}

// NewLogFilters creates a new LogFilters from configuration
func NewLogFilters(cfg *config.FilterConfig, minLevel string) (*LogFilters, error) {
	filters := &LogFilters{
		excludePatterns: make([]*regexp.Regexp, 0),
		includePatterns: make([]*regexp.Regexp, 0),
		minLevel:        slog.LevelInfo,
	}

	// Parse minimum level
	if minLevel != "" {
		level, err := parseLevel(minLevel)
		if err != nil {
			return nil, fmt.Errorf("invalid min_level: %w", err)
		}
		filters.minLevel = level
	}

	// No filter config provided
	if cfg == nil {
		return filters, nil
	}

	// Compile exclude patterns
	for _, pattern := range cfg.Exclude {
		regex, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("failed to compile exclude pattern '%s': %w", pattern, err)
		}
		filters.excludePatterns = append(filters.excludePatterns, regex)
		filters.hasFilters = true
	}

	// Compile include patterns
	for _, pattern := range cfg.Include {
		regex, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("failed to compile include pattern '%s': %w", pattern, err)
		}
		filters.includePatterns = append(filters.includePatterns, regex)
		filters.hasFilters = true
	}

	return filters, nil
}

// ShouldLog determines if a log line should be logged based on filters
// Returns true if the log should be logged, false if it should be dropped
//
// Logic:
// 1. Check level: if level < minLevel, drop
// 2. Check exclude: if matches any exclude pattern, drop
// 3. Check include: if include patterns exist and doesn't match any, drop
// 4. Otherwise, log
func (lf *LogFilters) ShouldLog(input string, level slog.Level) bool {
	// Level check
	if level < lf.minLevel {
		return false
	}

	// Fast-path: no pattern filters configured
	if !lf.hasFilters {
		return true
	}

	// Exclude check (takes precedence)
	for _, pattern := range lf.excludePatterns {
		if pattern.MatchString(input) {
			return false // Drop if matches exclude pattern
		}
	}

	// Include check (if include patterns exist)
	if len(lf.includePatterns) > 0 {
		for _, pattern := range lf.includePatterns {
			if pattern.MatchString(input) {
				return true // Log if matches at least one include pattern
			}
		}
		return false // Drop if doesn't match any include pattern
	}

	// No include patterns, and doesn't match exclude patterns
	return true
}

// GetMinLevel returns the minimum log level
func (lf *LogFilters) GetMinLevel() slog.Level {
	return lf.minLevel
}

// HasFilters returns whether any filters are configured
func (lf *LogFilters) HasFilters() bool {
	return lf.hasFilters
}
