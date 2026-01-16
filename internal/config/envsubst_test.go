package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandEnv(t *testing.T) {
	// Set up test environment variables
	os.Setenv("TEST_VAR", "test_value")
	os.Setenv("TEST_PORT", "8080")
	defer func() {
		os.Unsetenv("TEST_VAR")
		os.Unsetenv("TEST_PORT")
	}()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple variable",
			input: "${TEST_VAR}",
			want:  "test_value",
		},
		{
			name:  "variable with default (var exists)",
			input: "${TEST_VAR:-default}",
			want:  "test_value",
		},
		{
			name:  "variable with default (var missing)",
			input: "${MISSING_VAR:-default_value}",
			want:  "default_value",
		},
		{
			name:  "variable in string",
			input: "port: ${TEST_PORT}",
			want:  "port: 8080",
		},
		{
			name:  "multiple variables",
			input: "${TEST_VAR} and ${TEST_PORT}",
			want:  "test_value and 8080",
		},
		{
			name:  "missing variable no default",
			input: "${MISSING_VAR}",
			want:  "",
		},
		{
			name:  "no variables",
			input: "plain text",
			want:  "plain text",
		},
		{
			name:  "empty default",
			input: "${MISSING_VAR:-}",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandEnv(tt.input)
			if got != tt.want {
				t.Errorf("ExpandEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadWithEnvExpansion(t *testing.T) {
	// Create temporary config file with environment variables
	tmpDir, err := os.MkdirTemp("", "cbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configContent := `version: "1.0"
global:
  shutdown_timeout: ${SHUTDOWN_TIMEOUT:-30}
  log_level: ${LOG_LEVEL:-info}

processes:
  test-process:
    enabled: true
    command: ["${TEST_COMMAND:-sleep}", "1"]
    scale: 1
    priority: 10
`

	configPath := filepath.Join(tmpDir, "test-config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Set environment variables
	os.Setenv("SHUTDOWN_TIMEOUT", "60")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("TEST_COMMAND", "echo")
	defer func() {
		os.Unsetenv("SHUTDOWN_TIMEOUT")
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("TEST_COMMAND")
	}()

	// Load config with expansion
	cfg, err := LoadWithEnvExpansion(configPath)
	if err != nil {
		t.Fatalf("LoadWithEnvExpansion() error = %v", err)
	}

	// Verify expansions
	if cfg.Global.ShutdownTimeout != 60 {
		t.Errorf("ShutdownTimeout = %v, want 60", cfg.Global.ShutdownTimeout)
	}

	if cfg.Global.LogLevel != "debug" {
		t.Errorf("LogLevel = %v, want debug", cfg.Global.LogLevel)
	}

	if proc, ok := cfg.Processes["test-process"]; ok {
		if len(proc.Command) == 0 || proc.Command[0] != "echo" {
			t.Errorf("Command[0] = %v, want echo", proc.Command[0])
		}
	} else {
		t.Error("test-process not found in config")
	}
}

func TestLoadWithEnvExpansion_WithDefaults(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configContent := `version: "1.0"
global:
  shutdown_timeout: ${SHUTDOWN_TIMEOUT:-45}
  log_level: ${LOG_LEVEL:-warn}

processes:
  test-process:
    enabled: true
    command: ["sleep", "1"]
    scale: 1
    priority: 10
`

	configPath := filepath.Join(tmpDir, "test-config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Don't set environment variables - should use defaults
	cfg, err := LoadWithEnvExpansion(configPath)
	if err != nil {
		t.Fatalf("LoadWithEnvExpansion() error = %v", err)
	}

	// Verify defaults were used
	if cfg.Global.ShutdownTimeout != 45 {
		t.Errorf("ShutdownTimeout = %v, want 45", cfg.Global.ShutdownTimeout)
	}

	if cfg.Global.LogLevel != "warn" {
		t.Errorf("LogLevel = %v, want warn", cfg.Global.LogLevel)
	}
}

func TestLoadWithEnvExpansion_InvalidFile(t *testing.T) {
	_, err := LoadWithEnvExpansion("/nonexistent/config.yaml")
	if err == nil {
		t.Error("LoadWithEnvExpansion() expected error for nonexistent file")
	}
}

func TestLoadWithEnvExpansion_InvalidYAML(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configContent := `invalid: yaml: content: [[[`
	configPath := filepath.Join(tmpDir, "invalid.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	_, err = LoadWithEnvExpansion(configPath)
	if err == nil {
		t.Error("LoadWithEnvExpansion() expected error for invalid YAML")
	}
}
