package logger

import (
	"encoding/json"
	"log/slog"
	"sort"
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

	// messageFields and levelFields are the keys tried, in order, when lifting
	// the message and the level out of a line.
	messageFields []string
	levelFields   []string
}

// defaultMessageFields covers both spellings in common use: "message" (Monolog,
// Winston, Bunyan-alikes) and "msg" (Go's log/slog, logrus, zap, pino) — the
// latter being what postgres_exporter and most Go daemons write.
var defaultMessageFields = []string{"message", "msg"}

var defaultLevelFields = []string{"level"}

// reservedLogKeys are the keys cbox-init writes itself on every entry it
// re-emits: slog's own time/level/msg, the process label its logger carries,
// and the instance_id/stream the ProcessWriter adds. A merged field with one of
// these names is renamed (see collisionPrefix) — merging it as-is wrote the key
// twice, and consumers that keep the last occurrence then showed the child's
// "msg" and "time" in place of cbox-init's.
var reservedLogKeys = map[string]bool{
	slog.TimeKey:    true,
	slog.LevelKey:   true,
	slog.MessageKey: true,
	"process":       true,
	"instance_id":   true,
	"stream":        true,
}

// collisionPrefix is prepended to a merged field whose key cbox-init already
// uses, so the child's value is kept next to cbox-init's instead of shadowing
// it: the child's "time" becomes "app_time".
const collisionPrefix = "app_"

// NewJSONParser creates a new JSONParser from configuration
func NewJSONParser(cfg *config.JSONConfig) *JSONParser {
	if cfg == nil || !cfg.Enabled {
		return &JSONParser{enabled: false}
	}

	messageFields := defaultMessageFields
	if cfg.MessageField != "" {
		messageFields = []string{cfg.MessageField}
	}
	levelFields := defaultLevelFields
	if cfg.LevelField != "" {
		levelFields = []string{cfg.LevelField}
	}

	return &JSONParser{
		enabled:        true,
		detectAuto:     cfg.DetectAuto,
		extractLevel:   cfg.ExtractLevel,
		extractMessage: cfg.ExtractMessage,
		mergeFields:    cfg.MergeFields,
		messageFields:  messageFields,
		levelFields:    levelFields,
	}
}

// Parse attempts to parse input as JSON
// Returns (true, data) if successfully parsed as JSON
// Returns (false, nil) if not JSON or parsing failed
// Fast-path: if !enabled, returns (false, nil) immediately
func (jp *JSONParser) Parse(input string) (isJSON bool, data map[string]any) {
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
	data = make(map[string]any)
	if err := json.Unmarshal([]byte(input), &data); err != nil {
		return false, nil
	}

	return true, data
}

// ToLogAttrs converts parsed JSON data to slog attributes.
//
// levelFound reports whether the line stated its own level. Callers need that
// to tell an explicit {"level":"info"} from the "assume info" fallback —
// otherwise pattern-based level detection overrides what the application said
// about its own log line.
//
// The message and level are lifted from the first configured key that holds a
// string (message_field / level_field; by default "message" then "msg", and
// "level"). A lifted key is removed so it is not repeated as an attribute. The
// remaining fields, when merged, come out sorted by key, and any whose name
// cbox-init already writes on the entry is renamed with collisionPrefix.
func (jp *JSONParser) ToLogAttrs(data map[string]any) (message string, level slog.Level, attrs []slog.Attr, levelFound bool) {
	// Default level
	level = slog.LevelInfo

	// Extract message field
	if jp.extractMessage {
		for _, key := range jp.messageFields {
			if msg, ok := data[key].(string); ok {
				message = msg
				delete(data, key) // Don't duplicate in attrs
				break
			}
		}
	}

	// Extract level field
	if jp.extractLevel {
		for _, key := range jp.levelFields {
			levelStr, ok := data[key].(string)
			if !ok {
				continue
			}
			if parsedLevel, err := parseLevel(levelStr); err == nil {
				level = parsedLevel
				levelFound = true
			}
			delete(data, key) // Don't duplicate in attrs
			break
		}
	}

	// Merge remaining fields as attributes
	if !jp.mergeFields {
		return message, level, nil, levelFound
	}

	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	attrs = make([]slog.Attr, 0, len(keys))
	for _, key := range keys {
		attrs = append(attrs, slog.Any(attrKey(key, data), data[key]))
	}

	return message, level, attrs, levelFound
}

// attrKey returns the name a merged field is emitted under: its own, unless
// cbox-init already writes that key, in which case it is prefixed — repeatedly,
// if the child also sent a field under the prefixed name.
func attrKey(key string, data map[string]any) string {
	if !reservedLogKeys[key] {
		return key
	}

	renamed := collisionPrefix + key
	for {
		if _, taken := data[renamed]; !taken && !reservedLogKeys[renamed] {
			return renamed
		}
		renamed = collisionPrefix + renamed
	}
}

// IsEnabled returns whether JSON parsing is enabled
func (jp *JSONParser) IsEnabled() bool {
	return jp.enabled
}
