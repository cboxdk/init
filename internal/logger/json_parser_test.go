package logger

import (
	"log/slog"
	"testing"

	"github.com/cboxdk/init/internal/config"
)

func TestNewJSONParser_Disabled(t *testing.T) {
	tests := []struct {
		name   string
		config *config.JSONConfig
	}{
		{
			name:   "nil config",
			config: nil,
		},
		{
			name: "disabled config",
			config: &config.JSONConfig{
				Enabled: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewJSONParser(tt.config)
			if parser.enabled {
				t.Error("expected disabled JSON parser")
			}
		})
	}
}

func TestNewJSONParser_Enabled(t *testing.T) {
	cfg := &config.JSONConfig{
		Enabled:        true,
		DetectAuto:     true,
		ExtractLevel:   true,
		ExtractMessage: true,
		MergeFields:    true,
	}

	parser := NewJSONParser(cfg)
	if !parser.enabled {
		t.Error("expected enabled JSON parser")
	}
	if !parser.detectAuto {
		t.Error("expected detectAuto to be true")
	}
	if !parser.extractLevel {
		t.Error("expected extractLevel to be true")
	}
	if !parser.extractMessage {
		t.Error("expected extractMessage to be true")
	}
	if !parser.mergeFields {
		t.Error("expected mergeFields to be true")
	}
}

func TestJSONParser_Parse_Disabled_FastPath(t *testing.T) {
	parser := NewJSONParser(nil)

	input := `{"message":"test","level":"info"}`
	isJSON, data := parser.Parse(input)

	if isJSON {
		t.Error("disabled parser should return false")
	}
	if data != nil {
		t.Error("disabled parser should return nil data")
	}
}

func TestJSONParser_Parse_EmptyInput(t *testing.T) {
	cfg := &config.JSONConfig{Enabled: true}
	parser := NewJSONParser(cfg)

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "empty string",
			input: "",
		},
		{
			name:  "whitespace only",
			input: "   \t\n  ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isJSON, data := parser.Parse(tt.input)
			if isJSON {
				t.Error("empty input should return false")
			}
			if data != nil {
				t.Error("empty input should return nil data")
			}
		})
	}
}

func TestJSONParser_Parse_AutoDetection(t *testing.T) {
	cfg := &config.JSONConfig{
		Enabled:    true,
		DetectAuto: true,
	}
	parser := NewJSONParser(cfg)

	tests := []struct {
		name    string
		input   string
		wantOK  bool
		wantNil bool
	}{
		{
			name:    "valid JSON object",
			input:   `{"message":"test"}`,
			wantOK:  true,
			wantNil: false,
		},
		{
			name:    "not JSON (no prefix)",
			input:   `plain text log`,
			wantOK:  false,
			wantNil: true,
		},
		{
			name:    "starts with { but invalid JSON",
			input:   `{invalid json}`,
			wantOK:  false,
			wantNil: true,
		},
		{
			name:    "array not object",
			input:   `["array","not","object"]`,
			wantOK:  false,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isJSON, data := parser.Parse(tt.input)
			if isJSON != tt.wantOK {
				t.Errorf("Parse() isJSON = %v, want %v", isJSON, tt.wantOK)
			}
			if tt.wantNil && data != nil {
				t.Error("expected nil data")
			}
			if !tt.wantNil && tt.wantOK && data == nil {
				t.Error("expected non-nil data")
			}
		})
	}
}

func TestJSONParser_Parse_NoAutoDetection(t *testing.T) {
	cfg := &config.JSONConfig{
		Enabled:    true,
		DetectAuto: false, // Disabled auto-detection
	}
	parser := NewJSONParser(cfg)

	tests := []struct {
		name   string
		input  string
		wantOK bool
	}{
		{
			name:   "valid JSON",
			input:  `{"message":"test"}`,
			wantOK: true,
		},
		{
			name:   "plain text (but tries to parse anyway)",
			input:  `plain text`,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isJSON, _ := parser.Parse(tt.input)
			if isJSON != tt.wantOK {
				t.Errorf("Parse() isJSON = %v, want %v", isJSON, tt.wantOK)
			}
		})
	}
}

func TestJSONParser_Parse_ValidJSON(t *testing.T) {
	cfg := &config.JSONConfig{Enabled: true}
	parser := NewJSONParser(cfg)

	input := `{"message":"Hello World","level":"info","user_id":123}`
	isJSON, data := parser.Parse(input)

	if !isJSON {
		t.Fatal("expected valid JSON to return true")
	}
	if data == nil {
		t.Fatal("expected non-nil data")
	}

	if msg, ok := data["message"].(string); !ok || msg != "Hello World" {
		t.Errorf("expected message='Hello World', got %v", data["message"])
	}
	if level, ok := data["level"].(string); !ok || level != "info" {
		t.Errorf("expected level='info', got %v", data["level"])
	}
}

func TestJSONParser_ToLogAttrs_ExtractMessage(t *testing.T) {
	cfg := &config.JSONConfig{
		Enabled:        true,
		ExtractMessage: true,
		ExtractLevel:   false,
		MergeFields:    false,
	}
	parser := NewJSONParser(cfg)

	data := map[string]interface{}{
		"message": "Test message",
		"level":   "info",
		"user_id": 123,
	}

	message, level, attrs := parser.ToLogAttrs(data)

	if message != "Test message" {
		t.Errorf("expected message='Test message', got %v", message)
	}
	if level != slog.LevelInfo {
		t.Errorf("expected default level Info, got %v", level)
	}
	if len(attrs) != 0 {
		t.Errorf("expected no attrs when MergeFields=false, got %d", len(attrs))
	}
	// Message should be deleted from data
	if _, exists := data["message"]; exists {
		t.Error("message should be removed from data")
	}
}

func TestJSONParser_ToLogAttrs_ExtractLevel(t *testing.T) {
	cfg := &config.JSONConfig{
		Enabled:        true,
		ExtractMessage: false,
		ExtractLevel:   true,
		MergeFields:    false,
	}
	parser := NewJSONParser(cfg)

	tests := []struct {
		name      string
		levelStr  string
		wantLevel slog.Level
	}{
		{
			name:      "debug level",
			levelStr:  "debug",
			wantLevel: slog.LevelDebug,
		},
		{
			name:      "info level",
			levelStr:  "info",
			wantLevel: slog.LevelInfo,
		},
		{
			name:      "warn level",
			levelStr:  "warn",
			wantLevel: slog.LevelWarn,
		},
		{
			name:      "warning level",
			levelStr:  "warning",
			wantLevel: slog.LevelWarn,
		},
		{
			name:      "error level",
			levelStr:  "error",
			wantLevel: slog.LevelError,
		},
		{
			name:      "invalid level defaults to info",
			levelStr:  "invalid",
			wantLevel: slog.LevelInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := map[string]interface{}{
				"level": tt.levelStr,
			}

			_, level, _ := parser.ToLogAttrs(data)
			if level != tt.wantLevel {
				t.Errorf("expected level %v, got %v", tt.wantLevel, level)
			}
			// Level should be deleted from data
			if _, exists := data["level"]; exists {
				t.Error("level should be removed from data")
			}
		})
	}
}

func TestJSONParser_ToLogAttrs_MergeFields(t *testing.T) {
	cfg := &config.JSONConfig{
		Enabled:        true,
		ExtractMessage: true,
		ExtractLevel:   true,
		MergeFields:    true,
	}
	parser := NewJSONParser(cfg)

	data := map[string]interface{}{
		"message":  "Test message",
		"level":    "error",
		"user_id":  123,
		"email":    "test@example.com",
		"duration": 45.5,
	}

	message, level, attrs := parser.ToLogAttrs(data)

	if message != "Test message" {
		t.Errorf("expected message='Test message', got %v", message)
	}
	if level != slog.LevelError {
		t.Errorf("expected level Error, got %v", level)
	}

	// Should have 3 attrs (user_id, email, duration) - message and level extracted
	if len(attrs) != 3 {
		t.Errorf("expected 3 attrs, got %d", len(attrs))
	}

	// Check attrs contain expected keys
	attrMap := make(map[string]interface{})
	for _, attr := range attrs {
		attrMap[attr.Key] = attr.Value.Any()
	}

	// Check user_id (could be float64 or int64 depending on slog internals)
	switch v := attrMap["user_id"].(type) {
	case float64:
		if v != 123.0 {
			t.Errorf("expected user_id=123.0, got %v", v)
		}
	case int64:
		if v != 123 {
			t.Errorf("expected user_id=123, got %v", v)
		}
	default:
		t.Errorf("expected user_id to be numeric, got %T", attrMap["user_id"])
	}

	if attrMap["email"] != "test@example.com" {
		t.Errorf("expected email='test@example.com', got %v", attrMap["email"])
	}

	// Check duration
	if duration, ok := attrMap["duration"].(float64); !ok || duration != 45.5 {
		t.Errorf("expected duration=45.5, got %v (type %T)", attrMap["duration"], attrMap["duration"])
	}
}

func TestJSONParser_ToLogAttrs_NoExtraction(t *testing.T) {
	cfg := &config.JSONConfig{
		Enabled:        true,
		ExtractMessage: false,
		ExtractLevel:   false,
		MergeFields:    true,
	}
	parser := NewJSONParser(cfg)

	data := map[string]interface{}{
		"message": "Test message",
		"level":   "error",
		"user_id": 123,
	}

	message, level, attrs := parser.ToLogAttrs(data)

	if message != "" {
		t.Errorf("expected empty message, got %v", message)
	}
	if level != slog.LevelInfo {
		t.Errorf("expected default level Info, got %v", level)
	}

	// All fields should be in attrs (nothing extracted)
	if len(attrs) != 3 {
		t.Errorf("expected 3 attrs, got %d", len(attrs))
	}
}

func TestJSONParser_ToLogAttrs_NonStringMessage(t *testing.T) {
	cfg := &config.JSONConfig{
		Enabled:        true,
		ExtractMessage: true,
		MergeFields:    false,
	}
	parser := NewJSONParser(cfg)

	// Message is not a string
	data := map[string]interface{}{
		"message": 123,
	}

	message, _, _ := parser.ToLogAttrs(data)

	if message != "" {
		t.Errorf("expected empty message for non-string, got %v", message)
	}
	// Non-string message should not be deleted
	if _, exists := data["message"]; !exists {
		t.Error("non-string message should remain in data")
	}
}

func TestJSONParser_ToLogAttrs_NonStringLevel(t *testing.T) {
	cfg := &config.JSONConfig{
		Enabled:      true,
		ExtractLevel: true,
		MergeFields:  false,
	}
	parser := NewJSONParser(cfg)

	// Level is not a string
	data := map[string]interface{}{
		"level": 123,
	}

	_, level, _ := parser.ToLogAttrs(data)

	if level != slog.LevelInfo {
		t.Errorf("expected default level Info for non-string, got %v", level)
	}
	// Non-string level should not be deleted
	if _, exists := data["level"]; !exists {
		t.Error("non-string level should remain in data")
	}
}

func TestJSONParser_IsEnabled(t *testing.T) {
	tests := []struct {
		name   string
		config *config.JSONConfig
		want   bool
	}{
		{
			name:   "nil config",
			config: nil,
			want:   false,
		},
		{
			name: "disabled config",
			config: &config.JSONConfig{
				Enabled: false,
			},
			want: false,
		},
		{
			name: "enabled config",
			config: &config.JSONConfig{
				Enabled: true,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewJSONParser(tt.config)
			if got := parser.IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJSONParser_ToLogAttrs_ComplexTypes(t *testing.T) {
	cfg := &config.JSONConfig{
		Enabled:        true,
		ExtractMessage: true,
		ExtractLevel:   true,
		MergeFields:    true,
	}
	parser := NewJSONParser(cfg)

	data := map[string]interface{}{
		"message": "Complex data",
		"level":   "info",
		"nested": map[string]interface{}{
			"key": "value",
		},
		"array": []interface{}{1, 2, 3},
		"bool":  true,
		"null":  nil,
	}

	message, level, attrs := parser.ToLogAttrs(data)

	if message != "Complex data" {
		t.Errorf("expected message='Complex data', got %v", message)
	}
	if level != slog.LevelInfo {
		t.Errorf("expected level Info, got %v", level)
	}

	// Should have 4 attrs (nested, array, bool, null)
	if len(attrs) != 4 {
		t.Errorf("expected 4 attrs, got %d", len(attrs))
	}
}
