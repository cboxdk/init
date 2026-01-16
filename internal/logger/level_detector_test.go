package logger

import (
	"log/slog"
	"testing"

	"github.com/cboxdk/init/internal/config"
)

func TestNewLevelDetector_Disabled(t *testing.T) {
	tests := []struct {
		name   string
		config *config.LevelDetectionConfig
	}{
		{
			name:   "nil config",
			config: nil,
		},
		{
			name: "disabled config",
			config: &config.LevelDetectionConfig{
				Enabled: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ld, err := NewLevelDetector(tt.config)
			if err != nil {
				t.Fatalf("NewLevelDetector() error = %v", err)
			}
			if ld.enabled {
				t.Error("expected disabled level detector")
			}
		})
	}
}

func TestNewLevelDetector_InvalidLevel(t *testing.T) {
	cfg := &config.LevelDetectionConfig{
		Enabled: true,
		Patterns: map[string]string{
			"invalid_level": "pattern",
		},
	}

	_, err := NewLevelDetector(cfg)
	if err == nil {
		t.Fatal("expected error for invalid level")
	}
}

func TestNewLevelDetector_InvalidPattern(t *testing.T) {
	cfg := &config.LevelDetectionConfig{
		Enabled: true,
		Patterns: map[string]string{
			"error": "[invalid(regex",
		},
	}

	_, err := NewLevelDetector(cfg)
	if err == nil {
		t.Fatal("expected error for invalid regex pattern")
	}
}

func TestLevelDetector_Disabled_FastPath(t *testing.T) {
	ld, err := NewLevelDetector(nil)
	if err != nil {
		t.Fatalf("NewLevelDetector() error = %v", err)
	}

	input := "[ERROR] Something went wrong"
	level := ld.Detect(input)

	// Should return default level (Info) without pattern matching
	if level != slog.LevelInfo {
		t.Errorf("disabled detector should return default level, got: %v", level)
	}
}

func TestLevelDetector_ErrorPatterns(t *testing.T) {
	cfg := &config.LevelDetectionConfig{
		Enabled: true,
		Patterns: map[string]string{
			"error": `\[ERROR\]|ERROR:|Exception:|CRITICAL|Fatal`,
		},
		DefaultLevel: "info",
	}

	ld, err := NewLevelDetector(cfg)
	if err != nil {
		t.Fatalf("NewLevelDetector() error = %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected slog.Level
	}{
		{
			name:     "bracketed ERROR",
			input:    "[ERROR] Something went wrong",
			expected: slog.LevelError,
		},
		{
			name:     "ERROR with colon",
			input:    "ERROR: Failed to connect",
			expected: slog.LevelError,
		},
		{
			name:     "PHP Exception",
			input:    "Exception: Undefined variable in /path/file.php:123",
			expected: slog.LevelError,
		},
		{
			name:     "CRITICAL level",
			input:    "CRITICAL: Database connection lost",
			expected: slog.LevelError,
		},
		{
			name:     "Fatal error",
			input:    "Fatal error: Cannot allocate memory",
			expected: slog.LevelError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level := ld.Detect(tt.input)
			if level != tt.expected {
				t.Errorf("Detect() = %v, want %v", level, tt.expected)
			}
		})
	}
}

func TestLevelDetector_WarningPatterns(t *testing.T) {
	cfg := &config.LevelDetectionConfig{
		Enabled: true,
		Patterns: map[string]string{
			"warn": `\[WARNING\]|WARNING:|WARN:|Deprecated`,
		},
		DefaultLevel: "info",
	}

	ld, err := NewLevelDetector(cfg)
	if err != nil {
		t.Fatalf("NewLevelDetector() error = %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected slog.Level
	}{
		{
			name:     "bracketed WARNING",
			input:    "[WARNING] This feature is deprecated",
			expected: slog.LevelWarn,
		},
		{
			name:     "WARNING with colon",
			input:    "WARNING: Low disk space",
			expected: slog.LevelWarn,
		},
		{
			name:     "WARN abbreviation",
			input:    "WARN: Configuration missing",
			expected: slog.LevelWarn,
		},
		{
			name:     "PHP Deprecated",
			input:    "Deprecated: Function mysql_connect() is deprecated",
			expected: slog.LevelWarn,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level := ld.Detect(tt.input)
			if level != tt.expected {
				t.Errorf("Detect() = %v, want %v", level, tt.expected)
			}
		})
	}
}

func TestLevelDetector_MultiLevel(t *testing.T) {
	cfg := &config.LevelDetectionConfig{
		Enabled: true,
		Patterns: map[string]string{
			"error": `\[ERROR\]|ERROR:|Exception:`,
			"warn":  `\[WARNING\]|WARNING:`,
			"info":  `\[INFO\]|INFO:`,
			"debug": `\[DEBUG\]|DEBUG:`,
		},
		DefaultLevel: "info",
	}

	ld, err := NewLevelDetector(cfg)
	if err != nil {
		t.Fatalf("NewLevelDetector() error = %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected slog.Level
	}{
		{
			name:     "error level",
			input:    "[ERROR] Database connection failed",
			expected: slog.LevelError,
		},
		{
			name:     "warning level",
			input:    "[WARNING] Slow query detected",
			expected: slog.LevelWarn,
		},
		{
			name:     "info level",
			input:    "[INFO] Server started on port 8080",
			expected: slog.LevelInfo,
		},
		{
			name:     "debug level",
			input:    "[DEBUG] Request headers: {...}",
			expected: slog.LevelDebug,
		},
		{
			name:     "no match - default",
			input:    "Plain log message without level",
			expected: slog.LevelInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level := ld.Detect(tt.input)
			if level != tt.expected {
				t.Errorf("Detect() = %v, want %v", level, tt.expected)
			}
		})
	}
}

func TestLevelDetector_LaravelPatterns(t *testing.T) {
	cfg := &config.LevelDetectionConfig{
		Enabled: true,
		Patterns: map[string]string{
			"error": `local\.ERROR|production\.ERROR|Exception in`,
			"warn":  `local\.WARNING|production\.WARNING`,
			"info":  `local\.INFO|production\.INFO`,
		},
		DefaultLevel: "info",
	}

	ld, err := NewLevelDetector(cfg)
	if err != nil {
		t.Fatalf("NewLevelDetector() error = %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected slog.Level
	}{
		{
			name:     "Laravel local ERROR",
			input:    "[2024-01-01 12:00:00] local.ERROR: Query failed",
			expected: slog.LevelError,
		},
		{
			name:     "Laravel production ERROR",
			input:    "[2024-01-01 12:00:00] production.ERROR: Payment failed",
			expected: slog.LevelError,
		},
		{
			name:     "Laravel Exception",
			input:    "Exception in /app/Http/Controllers/UserController.php:42",
			expected: slog.LevelError,
		},
		{
			name:     "Laravel WARNING",
			input:    "[2024-01-01 12:00:00] local.WARNING: Cache miss",
			expected: slog.LevelWarn,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level := ld.Detect(tt.input)
			if level != tt.expected {
				t.Errorf("Detect() = %v, want %v", level, tt.expected)
			}
		})
	}
}

func TestLevelDetector_CustomDefaultLevel(t *testing.T) {
	cfg := &config.LevelDetectionConfig{
		Enabled: true,
		Patterns: map[string]string{
			"error": `\[ERROR\]`,
		},
		DefaultLevel: "debug",
	}

	ld, err := NewLevelDetector(cfg)
	if err != nil {
		t.Fatalf("NewLevelDetector() error = %v", err)
	}

	input := "Some random log message"
	level := ld.Detect(input)

	if level != slog.LevelDebug {
		t.Errorf("expected debug level (custom default), got: %v", level)
	}
}

func TestLevelDetector_PatternPriority(t *testing.T) {
	// When a log message matches multiple patterns,
	// the most severe level should be returned
	cfg := &config.LevelDetectionConfig{
		Enabled: true,
		Patterns: map[string]string{
			"error": `ERROR`,
			"warn":  `WARNING|ERROR`, // This also matches ERROR
		},
		DefaultLevel: "info",
	}

	ld, err := NewLevelDetector(cfg)
	if err != nil {
		t.Fatalf("NewLevelDetector() error = %v", err)
	}

	// Should match ERROR pattern (higher priority) not WARNING
	input := "ERROR: Something went wrong"
	level := ld.Detect(input)

	if level != slog.LevelError {
		t.Errorf("expected error level (higher priority), got: %v", level)
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected slog.Level
		wantErr  bool
	}{
		{
			name:     "debug lowercase",
			input:    "debug",
			expected: slog.LevelDebug,
			wantErr:  false,
		},
		{
			name:     "info mixed case",
			input:    "Info",
			expected: slog.LevelInfo,
			wantErr:  false,
		},
		{
			name:     "warn",
			input:    "warn",
			expected: slog.LevelWarn,
			wantErr:  false,
		},
		{
			name:     "warning",
			input:    "warning",
			expected: slog.LevelWarn,
			wantErr:  false,
		},
		{
			name:     "error uppercase",
			input:    "ERROR",
			expected: slog.LevelError,
			wantErr:  false,
		},
		{
			name:     "invalid level",
			input:    "invalid",
			expected: slog.LevelInfo,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, err := parseLevel(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseLevel() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && level != tt.expected {
				t.Errorf("parseLevel() = %v, want %v", level, tt.expected)
			}
		})
	}
}

func TestLevelDetector_IsEnabled(t *testing.T) {
	tests := []struct {
		name   string
		config *config.LevelDetectionConfig
		want   bool
	}{
		{
			name:   "nil config",
			config: nil,
			want:   false,
		},
		{
			name: "disabled config",
			config: &config.LevelDetectionConfig{
				Enabled: false,
			},
			want: false,
		},
		{
			name: "enabled config",
			config: &config.LevelDetectionConfig{
				Enabled: true,
				Patterns: map[string]string{
					"error": `ERROR`,
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ld, err := NewLevelDetector(tt.config)
			if err != nil {
				t.Fatalf("NewLevelDetector() error = %v", err)
			}
			if got := ld.IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLevelDetector_GetDefaultLevel(t *testing.T) {
	tests := []struct {
		name      string
		config    *config.LevelDetectionConfig
		wantLevel slog.Level
		wantErr   bool
	}{
		{
			name:      "nil config defaults to info",
			config:    nil,
			wantLevel: slog.LevelInfo,
			wantErr:   false,
		},
		{
			name: "disabled config defaults to info",
			config: &config.LevelDetectionConfig{
				Enabled: false,
			},
			wantLevel: slog.LevelInfo,
			wantErr:   false,
		},
		{
			name: "custom default level debug",
			config: &config.LevelDetectionConfig{
				Enabled:      true,
				DefaultLevel: "debug",
			},
			wantLevel: slog.LevelDebug,
			wantErr:   false,
		},
		{
			name: "custom default level warn",
			config: &config.LevelDetectionConfig{
				Enabled:      true,
				DefaultLevel: "warn",
			},
			wantLevel: slog.LevelWarn,
			wantErr:   false,
		},
		{
			name: "custom default level error",
			config: &config.LevelDetectionConfig{
				Enabled:      true,
				DefaultLevel: "error",
			},
			wantLevel: slog.LevelError,
			wantErr:   false,
		},
		{
			name: "empty default level uses info",
			config: &config.LevelDetectionConfig{
				Enabled:      true,
				DefaultLevel: "",
			},
			wantLevel: slog.LevelInfo,
			wantErr:   false,
		},
		{
			name: "invalid default level",
			config: &config.LevelDetectionConfig{
				Enabled:      true,
				DefaultLevel: "invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ld, err := NewLevelDetector(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewLevelDetector() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got := ld.GetDefaultLevel(); got != tt.wantLevel {
				t.Errorf("GetDefaultLevel() = %v, want %v", got, tt.wantLevel)
			}
		})
	}
}

func TestLevelDetector_NoPatterns_ReturnsDefault(t *testing.T) {
	cfg := &config.LevelDetectionConfig{
		Enabled:      true,
		Patterns:     map[string]string{}, // Empty patterns
		DefaultLevel: "warn",
	}

	ld, err := NewLevelDetector(cfg)
	if err != nil {
		t.Fatalf("NewLevelDetector() error = %v", err)
	}

	// Should return default level when no patterns configured
	input := "[ERROR] This won't match any pattern"
	level := ld.Detect(input)

	if level != slog.LevelWarn {
		t.Errorf("expected default level (warn), got %v", level)
	}
}
