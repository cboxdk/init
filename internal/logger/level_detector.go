package logger

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/cboxdk/init/internal/config"
)

// LevelDetector detects log severity from log content
type LevelDetector struct {
	enabled      bool
	patterns     map[slog.Level]*regexp.Regexp
	defaultLevel slog.Level
}

// NewLevelDetector creates a new LevelDetector from configuration
func NewLevelDetector(cfg *config.LevelDetectionConfig) (*LevelDetector, error) {
	if cfg == nil || !cfg.Enabled {
		return &LevelDetector{enabled: false}, nil
	}

	patterns := make(map[slog.Level]*regexp.Regexp)

	// Compile patterns for each level
	for levelStr, patternStr := range cfg.Patterns {
		// Parse level
		level, err := parseLevel(levelStr)
		if err != nil {
			return nil, fmt.Errorf("invalid level '%s': %w", levelStr, err)
		}

		// Compile regex
		regex, err := regexp.Compile(patternStr)
		if err != nil {
			return nil, fmt.Errorf("failed to compile pattern for level '%s': %w", levelStr, err)
		}

		patterns[level] = regex
	}

	// Parse default level
	defaultLevel := slog.LevelInfo
	if cfg.DefaultLevel != "" {
		level, err := parseLevel(cfg.DefaultLevel)
		if err != nil {
			return nil, fmt.Errorf("invalid default level '%s': %w", cfg.DefaultLevel, err)
		}
		defaultLevel = level
	}

	return &LevelDetector{
		enabled:      true,
		patterns:     patterns,
		defaultLevel: defaultLevel,
	}, nil
}

// Detect determines the log level from the input string
// Returns the detected level or the default level if no pattern matches
// Fast-path: if !enabled, returns defaultLevel immediately
func (ld *LevelDetector) Detect(input string) slog.Level {
	// Fast-path: disabled detector
	if !ld.enabled {
		return ld.defaultLevel
	}

	// Fast-path: no patterns configured
	if len(ld.patterns) == 0 {
		return ld.defaultLevel
	}

	// Check patterns in order of severity (most severe first)
	// This ensures we detect ERROR before WARNING if both patterns match
	levels := []slog.Level{slog.LevelError, slog.LevelWarn, slog.LevelInfo, slog.LevelDebug}
	for _, level := range levels {
		if regex, ok := ld.patterns[level]; ok {
			if regex.MatchString(input) {
				return level
			}
		}
	}

	// No pattern matched, return default
	return ld.defaultLevel
}

// IsEnabled returns whether level detection is enabled
func (ld *LevelDetector) IsEnabled() bool {
	return ld.enabled
}

// GetDefaultLevel returns the default log level
func (ld *LevelDetector) GetDefaultLevel() slog.Level {
	return ld.defaultLevel
}

// parseLevel converts a string level to slog.Level
func parseLevel(levelStr string) (slog.Level, error) {
	switch strings.ToLower(levelStr) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown level: %s", levelStr)
	}
}
