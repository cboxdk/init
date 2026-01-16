package logger

import (
	"log/slog"
	"testing"

	"github.com/cboxdk/init/internal/config"
)

func TestNewLogFilters_Nil(t *testing.T) {
	filters, err := NewLogFilters(nil, "")
	if err != nil {
		t.Fatalf("NewLogFilters() error = %v", err)
	}
	if filters.hasFilters {
		t.Error("expected hasFilters to be false with nil config")
	}
	if filters.minLevel != slog.LevelInfo {
		t.Errorf("expected default minLevel to be Info, got %v", filters.minLevel)
	}
}

func TestNewLogFilters_MinLevel(t *testing.T) {
	tests := []struct {
		name     string
		minLevel string
		want     slog.Level
		wantErr  bool
	}{
		{
			name:     "debug level",
			minLevel: "debug",
			want:     slog.LevelDebug,
			wantErr:  false,
		},
		{
			name:     "info level",
			minLevel: "info",
			want:     slog.LevelInfo,
			wantErr:  false,
		},
		{
			name:     "warn level",
			minLevel: "warn",
			want:     slog.LevelWarn,
			wantErr:  false,
		},
		{
			name:     "warning level",
			minLevel: "warning",
			want:     slog.LevelWarn,
			wantErr:  false,
		},
		{
			name:     "error level",
			minLevel: "error",
			want:     slog.LevelError,
			wantErr:  false,
		},
		{
			name:     "invalid level",
			minLevel: "invalid",
			want:     slog.LevelInfo,
			wantErr:  true,
		},
		{
			name:     "empty string defaults to Info",
			minLevel: "",
			want:     slog.LevelInfo,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filters, err := NewLogFilters(nil, tt.minLevel)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewLogFilters() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && filters.minLevel != tt.want {
				t.Errorf("NewLogFilters() minLevel = %v, want %v", filters.minLevel, tt.want)
			}
		})
	}
}

func TestNewLogFilters_ExcludePatterns(t *testing.T) {
	cfg := &config.FilterConfig{
		Exclude: []string{`\[DEBUG\]`, `test.*pattern`},
	}

	filters, err := NewLogFilters(cfg, "info")
	if err != nil {
		t.Fatalf("NewLogFilters() error = %v", err)
	}

	if !filters.hasFilters {
		t.Error("expected hasFilters to be true")
	}
	if len(filters.excludePatterns) != 2 {
		t.Errorf("expected 2 exclude patterns, got %d", len(filters.excludePatterns))
	}
}

func TestNewLogFilters_IncludePatterns(t *testing.T) {
	cfg := &config.FilterConfig{
		Include: []string{`\[ERROR\]`, `\[WARN\]`},
	}

	filters, err := NewLogFilters(cfg, "info")
	if err != nil {
		t.Fatalf("NewLogFilters() error = %v", err)
	}

	if !filters.hasFilters {
		t.Error("expected hasFilters to be true")
	}
	if len(filters.includePatterns) != 2 {
		t.Errorf("expected 2 include patterns, got %d", len(filters.includePatterns))
	}
}

func TestNewLogFilters_InvalidExcludePattern(t *testing.T) {
	cfg := &config.FilterConfig{
		Exclude: []string{`[invalid(regex`},
	}

	_, err := NewLogFilters(cfg, "info")
	if err == nil {
		t.Fatal("expected error for invalid exclude pattern")
	}
}

func TestNewLogFilters_InvalidIncludePattern(t *testing.T) {
	cfg := &config.FilterConfig{
		Include: []string{`[invalid(regex`},
	}

	_, err := NewLogFilters(cfg, "info")
	if err == nil {
		t.Fatal("expected error for invalid include pattern")
	}
}

func TestLogFilters_ShouldLog_LevelCheck(t *testing.T) {
	filters, err := NewLogFilters(nil, "warn")
	if err != nil {
		t.Fatalf("NewLogFilters() error = %v", err)
	}

	tests := []struct {
		name  string
		input string
		level slog.Level
		want  bool
	}{
		{
			name:  "debug below min",
			input: "debug message",
			level: slog.LevelDebug,
			want:  false,
		},
		{
			name:  "info below min",
			input: "info message",
			level: slog.LevelInfo,
			want:  false,
		},
		{
			name:  "warn at min",
			input: "warn message",
			level: slog.LevelWarn,
			want:  true,
		},
		{
			name:  "error above min",
			input: "error message",
			level: slog.LevelError,
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filters.ShouldLog(tt.input, tt.level)
			if got != tt.want {
				t.Errorf("ShouldLog() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLogFilters_ShouldLog_NoFilters_FastPath(t *testing.T) {
	filters, err := NewLogFilters(nil, "info")
	if err != nil {
		t.Fatalf("NewLogFilters() error = %v", err)
	}

	// Fast-path: no pattern filters, only level check
	input := "any message"
	if !filters.ShouldLog(input, slog.LevelInfo) {
		t.Error("should log when no pattern filters configured")
	}
}

func TestLogFilters_ShouldLog_ExcludeMatches(t *testing.T) {
	cfg := &config.FilterConfig{
		Exclude: []string{`\[DEBUG\]`, `test pattern`},
	}

	filters, err := NewLogFilters(cfg, "info")
	if err != nil {
		t.Fatalf("NewLogFilters() error = %v", err)
	}

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "matches exclude pattern 1",
			input: "[DEBUG] Starting process",
			want:  false,
		},
		{
			name:  "matches exclude pattern 2",
			input: "Running test pattern validation",
			want:  false,
		},
		{
			name:  "does not match exclude",
			input: "[INFO] Normal log message",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filters.ShouldLog(tt.input, slog.LevelInfo)
			if got != tt.want {
				t.Errorf("ShouldLog() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLogFilters_ShouldLog_IncludeMatches(t *testing.T) {
	cfg := &config.FilterConfig{
		Include: []string{`\[ERROR\]`, `\[WARN\]`},
	}

	filters, err := NewLogFilters(cfg, "info")
	if err != nil {
		t.Fatalf("NewLogFilters() error = %v", err)
	}

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "matches include pattern ERROR",
			input: "[ERROR] Database connection failed",
			want:  true,
		},
		{
			name:  "matches include pattern WARN",
			input: "[WARN] Low disk space",
			want:  true,
		},
		{
			name:  "does not match include",
			input: "[INFO] Normal operation",
			want:  false,
		},
		{
			name:  "does not match include",
			input: "[DEBUG] Verbose output",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filters.ShouldLog(tt.input, slog.LevelInfo)
			if got != tt.want {
				t.Errorf("ShouldLog() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLogFilters_ShouldLog_ExcludeTakesPrecedence(t *testing.T) {
	cfg := &config.FilterConfig{
		Include: []string{`\[ERROR\]`},
		Exclude: []string{`sensitive`},
	}

	filters, err := NewLogFilters(cfg, "info")
	if err != nil {
		t.Fatalf("NewLogFilters() error = %v", err)
	}

	// Matches both include and exclude, exclude takes precedence
	input := "[ERROR] sensitive data leaked"
	if filters.ShouldLog(input, slog.LevelError) {
		t.Error("exclude should take precedence over include")
	}
}

func TestLogFilters_ShouldLog_CombinedFilters(t *testing.T) {
	cfg := &config.FilterConfig{
		Include: []string{`\[ERROR\]`, `\[WARN\]`},
		Exclude: []string{`deprecated`, `test`},
	}

	filters, err := NewLogFilters(cfg, "info")
	if err != nil {
		t.Fatalf("NewLogFilters() error = %v", err)
	}

	tests := []struct {
		name  string
		input string
		level slog.Level
		want  bool
	}{
		{
			name:  "matches include, not exclude",
			input: "[ERROR] Database error",
			level: slog.LevelError,
			want:  true,
		},
		{
			name:  "matches include and exclude",
			input: "[WARN] deprecated function called",
			level: slog.LevelWarn,
			want:  false,
		},
		{
			name:  "does not match include",
			input: "[INFO] Normal operation",
			level: slog.LevelInfo,
			want:  false,
		},
		{
			name:  "below min level",
			input: "[ERROR] Important error",
			level: slog.LevelDebug,
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filters.ShouldLog(tt.input, tt.level)
			if got != tt.want {
				t.Errorf("ShouldLog() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLogFilters_GetMinLevel(t *testing.T) {
	filters, err := NewLogFilters(nil, "error")
	if err != nil {
		t.Fatalf("NewLogFilters() error = %v", err)
	}

	if filters.GetMinLevel() != slog.LevelError {
		t.Errorf("GetMinLevel() = %v, want %v", filters.GetMinLevel(), slog.LevelError)
	}
}

func TestLogFilters_HasFilters(t *testing.T) {
	tests := []struct {
		name   string
		config *config.FilterConfig
		want   bool
	}{
		{
			name:   "nil config",
			config: nil,
			want:   false,
		},
		{
			name: "with exclude patterns",
			config: &config.FilterConfig{
				Exclude: []string{`pattern`},
			},
			want: true,
		},
		{
			name: "with include patterns",
			config: &config.FilterConfig{
				Include: []string{`pattern`},
			},
			want: true,
		},
		{
			name: "with both",
			config: &config.FilterConfig{
				Include: []string{`include`},
				Exclude: []string{`exclude`},
			},
			want: true,
		},
		{
			name:   "empty config",
			config: &config.FilterConfig{},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filters, err := NewLogFilters(tt.config, "info")
			if err != nil {
				t.Fatalf("NewLogFilters() error = %v", err)
			}
			if got := filters.HasFilters(); got != tt.want {
				t.Errorf("HasFilters() = %v, want %v", got, tt.want)
			}
		})
	}
}
