package logger

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/cboxdk/init/internal/config"
)

// JSONParser parses JSON-formatted logs from processes
type JSONParser struct {
	enabled        bool
	detectAuto     bool
	extractLevel   bool
	extractMessage bool
	mergeFields    bool
}

// NewJSONParser creates a new JSONParser from configuration
func NewJSONParser(cfg *config.JSONConfig) *JSONParser {
	if cfg == nil || !cfg.Enabled {
		return &JSONParser{enabled: false}
	}

	return &JSONParser{
		enabled:        true,
		detectAuto:     cfg.DetectAuto,
		extractLevel:   cfg.ExtractLevel,
		extractMessage: cfg.ExtractMessage,
		mergeFields:    cfg.MergeFields,
	}
}

// Parse attempts to parse input as JSON
// Returns (true, data) if successfully parsed as JSON
// Returns (false, nil) if not JSON or parsing failed
// Fast-path: if !enabled, returns (false, nil) immediately
func (jp *JSONParser) Parse(input string) (isJSON bool, data map[string]interface{}) {
	// Fast-path: disabled parser
	if !jp.enabled {
		return false, nil
	}

	// Fast-path: empty input
	input = strings.TrimSpace(input)
	if input == "" {
		return false, nil
	}

	// Auto-detection: check if looks like JSON
	if jp.detectAuto {
		if !strings.HasPrefix(input, "{") {
			return false, nil
		}
	}

	// Try to parse as JSON
	data = make(map[string]interface{})
	if err := json.Unmarshal([]byte(input), &data); err != nil {
		return false, nil
	}

	return true, data
}

// ToLogAttrs converts parsed JSON data to slog attributes
// Extracts level and message fields if configured
// Returns (message, level, attrs)
func (jp *JSONParser) ToLogAttrs(data map[string]interface{}) (message string, level slog.Level, attrs []slog.Attr) {
	attrs = make([]slog.Attr, 0, len(data))

	// Default level
	level = slog.LevelInfo

	// Extract message field
	if jp.extractMessage {
		if msg, ok := data["message"].(string); ok {
			message = msg
			delete(data, "message") // Don't duplicate in attrs
		}
	}

	// Extract level field
	if jp.extractLevel {
		if levelStr, ok := data["level"].(string); ok {
			if parsedLevel, err := parseLevel(levelStr); err == nil {
				level = parsedLevel
			}
			delete(data, "level") // Don't duplicate in attrs
		}
	}

	// Merge remaining fields as attributes
	if jp.mergeFields {
		for key, value := range data {
			attrs = append(attrs, slog.Any(key, value))
		}
	}

	return message, level, attrs
}

// IsEnabled returns whether JSON parsing is enabled
func (jp *JSONParser) IsEnabled() bool {
	return jp.enabled
}
