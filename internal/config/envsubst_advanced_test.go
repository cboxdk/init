package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMatchFieldPath(t *testing.T) {
	tests := []struct {
		name   string
		tokens []string
		wantOk bool
	}{
		{
			name:   "empty tokens",
			tokens: []string{},
			wantOk: true,
		},
		{
			name:   "valid single token",
			tokens: []string{"shutdown_timeout"},
			wantOk: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := matchFieldPath(globalFieldTree, tt.tokens)
			if ok != tt.wantOk {
				t.Errorf("matchFieldPath() ok = %v, want %v", ok, tt.wantOk)
			}
		})
	}
}

func TestSetNestedValue(t *testing.T) {
	tests := []struct {
		name  string
		path  []string
		value interface{}
	}{
		{
			name:  "empty path",
			path:  []string{},
			value: "ignored",
		},
		{
			name:  "single level",
			path:  []string{"key"},
			value: "value",
		},
		{
			name:  "nested path",
			path:  []string{"level1", "level2", "level3"},
			value: 42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := make(map[string]interface{})
			setNestedValue(root, tt.path, tt.value)

			// For non-empty paths, verify the value was set
			if len(tt.path) > 0 {
				current := root
				for i := 0; i < len(tt.path)-1; i++ {
					next, ok := current[tt.path[i]].(map[string]interface{})
					if !ok {
						t.Fatalf("Path segment %d not found or not a map", i)
					}
					current = next
				}
				if current[tt.path[len(tt.path)-1]] != tt.value {
					t.Errorf("Value not set correctly")
				}
			}
		})
	}
}

func TestParseEnvValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  interface{}
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "whitespace only",
			input: "   ",
			want:  "",
		},
		{
			name:  "JSON array",
			input: `["item1", "item2"]`,
			want:  []interface{}{"item1", "item2"},
		},
		{
			name:  "JSON object",
			input: `{"key": "value"}`,
			want:  map[string]interface{}{"key": "value"},
		},
		{
			name:  "boolean true",
			input: "true",
			want:  true,
		},
		{
			name:  "boolean false",
			input: "false",
			want:  false,
		},
		{
			name:  "integer",
			input: "42",
			want:  int64(42),
		},
		{
			name:  "negative integer",
			input: "-100",
			want:  int64(-100),
		},
		{
			name:  "float",
			input: "3.14",
			want:  float64(3.14),
		},
		{
			name:  "string",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "invalid JSON array (returns as string)",
			input: "[incomplete",
			want:  "[incomplete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseEnvValue(tt.input)
			if !equalValues(got, tt.want) {
				t.Errorf("parseEnvValue() = %v (%T), want %v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}

// Helper function to compare values of different types
func equalValues(a, b interface{}) bool {
	switch v := a.(type) {
	case []interface{}:
		arr, ok := b.([]interface{})
		if !ok || len(v) != len(arr) {
			return false
		}
		for i := range v {
			if !equalValues(v[i], arr[i]) {
				return false
			}
		}
		return true
	case map[string]interface{}:
		m, ok := b.(map[string]interface{})
		if !ok || len(v) != len(m) {
			return false
		}
		for k := range v {
			if !equalValues(v[k], m[k]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}

func TestBuildPathFromKey(t *testing.T) {
	tests := []struct {
		name    string
		segment string
		wantLen int
	}{
		{
			name:    "shutdown_timeout",
			segment: "SHUTDOWN_TIMEOUT",
			wantLen: 1,
		},
		{
			name:    "log_level",
			segment: "LOG_LEVEL",
			wantLen: 1,
		},
		{
			name:    "unknown field",
			segment: "UNKNOWN_FIELD_NAME",
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := buildPathFromKey(tt.segment, globalFieldTree)
			if len(path) != tt.wantLen {
				t.Errorf("buildPathFromKey() path length = %v, want %v", len(path), tt.wantLen)
			}
		})
	}
}

func TestApplyProcessEnvOverride(t *testing.T) {
	tests := []struct {
		name     string
		segment  string
		value    string
		checkKey string
		checkVal string
	}{
		{
			name:     "empty segment",
			segment:  "",
			value:    "value",
			checkKey: "",
		},
		{
			name:     "process env variable",
			segment:  "PHPFPM_ENV_DB_HOST",
			value:    "localhost",
			checkKey: "DB_HOST",
			checkVal: "localhost",
		},
		{
			name:     "process field",
			segment:  "PHPFPM_SCALE",
			value:    "5",
			checkKey: "scale",
			checkVal: "5",
		},
		{
			name:     "env with empty key",
			segment:  "PHPFPM_ENV_",
			value:    "value",
			checkKey: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processes := make(map[string]interface{})
			applyProcessEnvOverride(processes, tt.segment, tt.value)

			if tt.checkKey != "" {
				// Check if the value was set correctly
				if strings.Contains(tt.segment, "_ENV_") {
					// Check environment variable
					procName := "phpfpm"
					if proc, ok := processes[procName].(map[string]interface{}); ok {
						if env, ok := proc["env"].(map[string]interface{}); ok {
							if env[tt.checkKey] != tt.checkVal {
								t.Errorf("Env var not set correctly: got %v, want %v", env[tt.checkKey], tt.checkVal)
							}
						} else {
							t.Error("Env map not found")
						}
					}
				}
			}
		})
	}
}

func TestDecodeProcessName(t *testing.T) {
	tests := []struct {
		name      string
		processes map[string]interface{}
		encoded   string
		want      string
	}{
		{
			name: "exact match",
			processes: map[string]interface{}{
				"php-fpm": map[string]interface{}{},
			},
			encoded: "PHP_FPM",
			want:    "php-fpm",
		},
		{
			name: "underscore to dash",
			processes: map[string]interface{}{
				"my-process": map[string]interface{}{},
			},
			encoded: "MY_PROCESS",
			want:    "my-process",
		},
		{
			name:      "no match",
			processes: map[string]interface{}{},
			encoded:   "NONEXISTENT",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeProcessName(tt.processes, tt.encoded)
			if got != tt.want {
				t.Errorf("decodeProcessName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeProcessName(t *testing.T) {
	tests := []struct {
		name    string
		encoded string
		want    string
	}{
		{
			name:    "uppercase with underscores",
			encoded: "PHP_FPM",
			want:    "php-fpm",
		},
		{
			name:    "mixed case",
			encoded: "My_Process_Name",
			want:    "my-process-name",
		},
		{
			name:    "already lowercase with dashes",
			encoded: "already-good",
			want:    "already-good",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeProcessName(tt.encoded)
			if got != tt.want {
				t.Errorf("normalizeProcessName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyEnvOverridesMap_GlobalOverrides(t *testing.T) {
	// Set up environment variables
	os.Setenv("CBOX_INIT_GLOBAL_SHUTDOWN_TIMEOUT", "60")
	os.Setenv("CBOX_INIT_GLOBAL_LOG_LEVEL", "debug")
	os.Setenv("CBOX_INIT_GLOBAL_METRICS_PORT", "9999")
	defer func() {
		os.Unsetenv("CBOX_INIT_GLOBAL_SHUTDOWN_TIMEOUT")
		os.Unsetenv("CBOX_INIT_GLOBAL_LOG_LEVEL")
		os.Unsetenv("CBOX_INIT_GLOBAL_METRICS_PORT")
	}()

	raw := make(map[string]interface{})
	err := applyEnvOverridesMap(raw)
	if err != nil {
		t.Fatalf("applyEnvOverridesMap() error = %v", err)
	}

	global, ok := raw["global"].(map[string]interface{})
	if !ok {
		t.Fatal("global map not created")
	}

	if global["shutdown_timeout"] != int64(60) {
		t.Errorf("shutdown_timeout = %v, want 60", global["shutdown_timeout"])
	}
	if global["log_level"] != "debug" {
		t.Errorf("log_level = %v, want debug", global["log_level"])
	}
	if global["metrics_port"] != int64(9999) {
		t.Errorf("metrics_port = %v, want 9999", global["metrics_port"])
	}
}

func TestApplyEnvOverridesMap_ProcessOverrides(t *testing.T) {
	// Set up environment variables
	os.Setenv("CBOX_INIT_PROCESS_PHPFPM_SCALE", "10")
	os.Setenv("CBOX_INIT_PROCESS_PHPFPM_ENV_DB_HOST", "postgres")
	defer func() {
		os.Unsetenv("CBOX_INIT_PROCESS_PHPFPM_SCALE")
		os.Unsetenv("CBOX_INIT_PROCESS_PHPFPM_ENV_DB_HOST")
	}()

	raw := make(map[string]interface{})
	err := applyEnvOverridesMap(raw)
	if err != nil {
		t.Fatalf("applyEnvOverridesMap() error = %v", err)
	}

	processes, ok := raw["processes"].(map[string]interface{})
	if !ok {
		t.Fatal("processes map not created")
	}

	phpfpm, ok := processes["phpfpm"].(map[string]interface{})
	if !ok {
		t.Fatal("phpfpm process not created")
	}

	if phpfpm["scale"] != int64(10) {
		t.Errorf("scale = %v, want 10", phpfpm["scale"])
	}

	env, ok := phpfpm["env"].(map[string]interface{})
	if !ok {
		t.Fatal("env map not created")
	}

	if env["DB_HOST"] != "postgres" {
		t.Errorf("DB_HOST = %v, want postgres", env["DB_HOST"])
	}
}

func TestLoadWithEnvExpansion_NoFile(t *testing.T) {
	// Test loading when file doesn't exist (should use env vars only)
	tmpDir, err := os.MkdirTemp("", "cbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	nonexistentPath := filepath.Join(tmpDir, "nonexistent.yaml")

	// Set required env vars to create a valid config
	os.Setenv("CBOX_INIT_GLOBAL_SHUTDOWN_TIMEOUT", "30")
	os.Setenv("CBOX_INIT_GLOBAL_LOG_LEVEL", "info")
	os.Setenv("CBOX_INIT_GLOBAL_LOG_FORMAT", "json")
	os.Setenv("CBOX_INIT_PROCESS_TEST_COMMAND", `["sleep", "1"]`)
	defer func() {
		os.Unsetenv("CBOX_INIT_GLOBAL_SHUTDOWN_TIMEOUT")
		os.Unsetenv("CBOX_INIT_GLOBAL_LOG_LEVEL")
		os.Unsetenv("CBOX_INIT_GLOBAL_LOG_FORMAT")
		os.Unsetenv("CBOX_INIT_PROCESS_TEST_COMMAND")
	}()

	cfg, err := LoadWithEnvExpansion(nonexistentPath)
	if err != nil {
		t.Fatalf("LoadWithEnvExpansion() error = %v", err)
	}

	if cfg == nil {
		t.Fatal("Config should not be nil")
	}

	if cfg.Global.ShutdownTimeout != 30 {
		t.Errorf("ShutdownTimeout = %v, want 30", cfg.Global.ShutdownTimeout)
	}
}
