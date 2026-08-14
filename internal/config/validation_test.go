package config

import (
	"strings"
	"testing"
	"time"
)

func TestNewValidationResult(t *testing.T) {
	result := NewValidationResult()

	if result == nil {
		t.Fatal("NewValidationResult returned nil")
	}

	if len(result.Errors) != 0 {
		t.Errorf("Expected empty Errors, got %d", len(result.Errors))
	}
	if len(result.Warnings) != 0 {
		t.Errorf("Expected empty Warnings, got %d", len(result.Warnings))
	}
	if len(result.Suggestions) != 0 {
		t.Errorf("Expected empty Suggestions, got %d", len(result.Suggestions))
	}
}

func TestValidationResult_AddError(t *testing.T) {
	result := NewValidationResult()

	result.AddError("test.field", "test message", "test suggestion")

	if len(result.Errors) != 1 {
		t.Fatalf("Expected 1 error, got %d", len(result.Errors))
	}

	err := result.Errors[0]
	if err.Severity != SeverityError {
		t.Errorf("Expected severity %s, got %s", SeverityError, err.Severity)
	}
	if err.Field != "test.field" {
		t.Errorf("Expected field 'test.field', got '%s'", err.Field)
	}
	if err.Message != "test message" {
		t.Errorf("Expected message 'test message', got '%s'", err.Message)
	}
	if err.Suggestion != "test suggestion" {
		t.Errorf("Expected suggestion 'test suggestion', got '%s'", err.Suggestion)
	}
}

func TestValidationResult_AddWarning(t *testing.T) {
	result := NewValidationResult()

	result.AddWarning("test.field", "warning message", "warning suggestion")

	if len(result.Warnings) != 1 {
		t.Fatalf("Expected 1 warning, got %d", len(result.Warnings))
	}

	warn := result.Warnings[0]
	if warn.Severity != SeverityWarning {
		t.Errorf("Expected severity %s, got %s", SeverityWarning, warn.Severity)
	}
}

func TestValidationResult_AddSuggestion(t *testing.T) {
	result := NewValidationResult()

	result.AddSuggestion("test.field", "suggestion message", "how to fix")

	if len(result.Suggestions) != 1 {
		t.Fatalf("Expected 1 suggestion, got %d", len(result.Suggestions))
	}

	sugg := result.Suggestions[0]
	if sugg.Severity != SeveritySuggestion {
		t.Errorf("Expected severity %s, got %s", SeveritySuggestion, sugg.Severity)
	}
}

func TestValidationResult_AddProcessError(t *testing.T) {
	result := NewValidationResult()

	result.AddProcessError("php-fpm", "command", "missing command", "add command")

	if len(result.Errors) != 1 {
		t.Fatalf("Expected 1 error, got %d", len(result.Errors))
	}

	err := result.Errors[0]
	if err.Field != "processes.php-fpm.command" {
		t.Errorf("Expected field 'processes.php-fpm.command', got '%s'", err.Field)
	}
	if err.ProcessName != "php-fpm" {
		t.Errorf("Expected ProcessName 'php-fpm', got '%s'", err.ProcessName)
	}
}

func TestValidationResult_AddProcessWarning(t *testing.T) {
	result := NewValidationResult()

	result.AddProcessWarning("nginx", "scale", "high scale", "reduce scale")

	if len(result.Warnings) != 1 {
		t.Fatalf("Expected 1 warning, got %d", len(result.Warnings))
	}

	warn := result.Warnings[0]
	if warn.Field != "processes.nginx.scale" {
		t.Errorf("Expected field 'processes.nginx.scale', got '%s'", warn.Field)
	}
	if warn.ProcessName != "nginx" {
		t.Errorf("Expected ProcessName 'nginx', got '%s'", warn.ProcessName)
	}
}

func TestValidationResult_AddProcessSuggestion(t *testing.T) {
	result := NewValidationResult()

	result.AddProcessSuggestion("worker", "logging", "enable logging", "set stdout: true")

	if len(result.Suggestions) != 1 {
		t.Fatalf("Expected 1 suggestion, got %d", len(result.Suggestions))
	}

	sugg := result.Suggestions[0]
	if sugg.Field != "processes.worker.logging" {
		t.Errorf("Expected field 'processes.worker.logging', got '%s'", sugg.Field)
	}
}

func TestValidationResult_HasErrors(t *testing.T) {
	result := NewValidationResult()

	if result.HasErrors() {
		t.Error("Expected HasErrors() false for empty result")
	}

	result.AddError("field", "message", "suggestion")

	if !result.HasErrors() {
		t.Error("Expected HasErrors() true after AddError")
	}
}

func TestValidationResult_HasWarnings(t *testing.T) {
	result := NewValidationResult()

	if result.HasWarnings() {
		t.Error("Expected HasWarnings() false for empty result")
	}

	result.AddWarning("field", "message", "suggestion")

	if !result.HasWarnings() {
		t.Error("Expected HasWarnings() true after AddWarning")
	}
}

func TestValidationResult_HasSuggestions(t *testing.T) {
	result := NewValidationResult()

	if result.HasSuggestions() {
		t.Error("Expected HasSuggestions() false for empty result")
	}

	result.AddSuggestion("field", "message", "suggestion")

	if !result.HasSuggestions() {
		t.Error("Expected HasSuggestions() true after AddSuggestion")
	}
}

func TestValidationResult_TotalIssues(t *testing.T) {
	result := NewValidationResult()

	if result.TotalIssues() != 0 {
		t.Errorf("Expected 0 total issues, got %d", result.TotalIssues())
	}

	result.AddError("e1", "m", "s")
	result.AddError("e2", "m", "s")
	result.AddWarning("w1", "m", "s")
	result.AddSuggestion("s1", "m", "s")

	if result.TotalIssues() != 4 {
		t.Errorf("Expected 4 total issues, got %d", result.TotalIssues())
	}
}

func TestValidationResult_ToError(t *testing.T) {
	// Test no errors
	result := NewValidationResult()
	result.AddWarning("field", "warning", "suggestion")

	err := result.ToError()
	if err != nil {
		t.Errorf("Expected nil error for no errors, got %v", err)
	}

	// Test with errors
	result.AddError("test.field", "test error message", "how to fix it")

	err = result.ToError()
	if err == nil {
		t.Fatal("Expected non-nil error when errors exist")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "1 error(s)") {
		t.Errorf("Expected error message to contain '1 error(s)', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "test.field") {
		t.Errorf("Expected error message to contain field name, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "test error message") {
		t.Errorf("Expected error message to contain message, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "how to fix it") {
		t.Errorf("Expected error message to contain suggestion, got: %s", errMsg)
	}
}

func TestValidateComprehensive_ValidConfig(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "info",
			LogFormat:          "json",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
			APIPort:            8080, // Non-privileged port
			MetricsPort:        9090, // Non-privileged port
		},
		Processes: map[string]*Process{
			"test-proc": {
				Enabled:      true,
				Type:         "longrun",
				InitialState: "running",
				Command:      []string{"sleep", "60"},
				Restart:      "always",
				Scale:        1,
			},
		},
	}

	result, err := cfg.ValidateComprehensive()

	if err != nil {
		t.Errorf("Expected no error for valid config, got: %v", err)
	}

	if result.HasErrors() {
		t.Errorf("Expected no errors, got: %d", len(result.Errors))
		for _, e := range result.Errors {
			t.Logf("  Error: [%s] %s", e.Field, e.Message)
		}
	}
}

func TestValidateComprehensive_NoProcesses(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "info",
			LogFormat:          "json",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*Process{},
	}

	result, err := cfg.ValidateComprehensive()

	if err == nil {
		t.Error("Expected error for config with no processes")
	}

	if !result.HasErrors() {
		t.Error("Expected HasErrors() true for no processes")
	}

	// Check for specific error
	found := false
	for _, e := range result.Errors {
		if e.Field == "processes" && strings.Contains(e.Message, "No processes defined") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected 'No processes defined' error")
	}
}

func TestValidateComprehensive_InvalidLogLevel(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "invalid-level",
			LogFormat:          "json",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*Process{
			"test": {
				Enabled:      true,
				Type:         "longrun",
				InitialState: "running",
				Command:      []string{"sleep", "60"},
				Restart:      "always",
				Scale:        1,
			},
		},
	}

	result, err := cfg.ValidateComprehensive()

	if err == nil {
		t.Error("Expected error for invalid log level")
	}

	found := false
	for _, e := range result.Errors {
		if e.Field == "global.log_level" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected error for global.log_level")
	}
}

func TestValidateComprehensive_NegativeShutdownTimeout(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{
			ShutdownTimeout:    -1,
			LogLevel:           "info",
			LogFormat:          "json",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*Process{
			"test": {
				Enabled:      true,
				Type:         "longrun",
				InitialState: "running",
				Command:      []string{"sleep", "60"},
				Restart:      "always",
				Scale:        1,
			},
		},
	}

	result, err := cfg.ValidateComprehensive()

	if err == nil {
		t.Error("Expected error for negative shutdown timeout")
	}

	found := false
	for _, e := range result.Errors {
		if e.Field == "global.shutdown_timeout" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected error for global.shutdown_timeout")
	}
}

func TestValidateComprehensive_ProcessNoCommand(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "info",
			LogFormat:          "json",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*Process{
			"no-command": {
				Enabled:      true,
				Type:         "longrun",
				InitialState: "running",
				Command:      []string{}, // Empty command
				Restart:      "always",
				Scale:        1,
			},
		},
	}

	result, err := cfg.ValidateComprehensive()

	if err == nil {
		t.Error("Expected error for process with no command")
	}

	found := false
	for _, e := range result.Errors {
		if strings.Contains(e.Field, "no-command") && strings.Contains(e.Field, "command") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected error for process command")
	}
}

func TestValidateComprehensive_InvalidRestartPolicy(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "info",
			LogFormat:          "json",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*Process{
			"test": {
				Enabled:      true,
				Type:         "longrun",
				InitialState: "running",
				Command:      []string{"sleep", "60"},
				Restart:      "invalid-policy",
				Scale:        1,
			},
		},
	}

	result, err := cfg.ValidateComprehensive()

	if err == nil {
		t.Error("Expected error for invalid restart policy")
	}

	found := false
	for _, e := range result.Errors {
		if strings.Contains(e.Field, "restart") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected error for restart policy")
	}
}

func TestValidateComprehensive_OneshotWithAlwaysRestart(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "info",
			LogFormat:          "json",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*Process{
			"oneshot-proc": {
				Enabled:      true,
				Type:         "oneshot",
				InitialState: "running",
				Command:      []string{"echo", "hello"},
				Restart:      "always", // Invalid for oneshot
				Scale:        1,
			},
		},
	}

	result, err := cfg.ValidateComprehensive()

	if err == nil {
		t.Error("Expected error for oneshot with always restart")
	}

	found := false
	for _, e := range result.Errors {
		if strings.Contains(e.Message, "Oneshot cannot have restart: always") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected error for oneshot restart policy")
	}
}

func TestValidateComprehensive_CircularDependency(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "info",
			LogFormat:          "json",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*Process{
			"proc-a": {
				Enabled:      true,
				Type:         "longrun",
				InitialState: "running",
				Command:      []string{"sleep", "60"},
				Restart:      "always",
				Scale:        1,
				DependsOn:    []string{"proc-b"},
			},
			"proc-b": {
				Enabled:      true,
				Type:         "longrun",
				InitialState: "running",
				Command:      []string{"sleep", "60"},
				Restart:      "always",
				Scale:        1,
				DependsOn:    []string{"proc-a"}, // Circular!
			},
		},
	}

	result, err := cfg.ValidateComprehensive()

	if err == nil {
		t.Error("Expected error for circular dependency")
	}

	found := false
	for _, e := range result.Errors {
		if strings.Contains(e.Message, "Circular dependency") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected circular dependency error")
	}
}

func TestValidateComprehensive_MissingDependency(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{
			ShutdownTimeout:    30,
			LogLevel:           "info",
			LogFormat:          "json",
			MaxRestartAttempts: 3,
			RestartBackoff:     5,
		},
		Processes: map[string]*Process{
			"proc-a": {
				Enabled:      true,
				Type:         "longrun",
				InitialState: "running",
				Command:      []string{"sleep", "60"},
				Restart:      "always",
				Scale:        1,
				DependsOn:    []string{"non-existent"},
			},
		},
	}

	result, err := cfg.ValidateComprehensive()

	if err == nil {
		t.Error("Expected error for missing dependency")
	}

	found := false
	for _, e := range result.Errors {
		if strings.Contains(e.Message, "non-existent") && strings.Contains(e.Message, "not defined") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected missing dependency error")
	}
}

func TestValidateComprehensive_HealthCheckValidation(t *testing.T) {
	tests := []struct {
		name        string
		healthCheck *HealthCheck
		expectError bool
		errorField  string
	}{
		{
			name: "TCP without address",
			healthCheck: &HealthCheck{
				Type:    "tcp",
				Address: "", // Missing!
				Period:  10,
				Timeout: 5,
			},
			expectError: true,
			errorField:  "health_check.address",
		},
		{
			name: "HTTP without URL",
			healthCheck: &HealthCheck{
				Type:    "http",
				URL:     "", // Missing!
				Period:  10,
				Timeout: 5,
			},
			expectError: true,
			errorField:  "health_check.url",
		},
		{
			name: "Exec without command",
			healthCheck: &HealthCheck{
				Type:    "exec",
				Command: []string{}, // Missing!
				Period:  10,
				Timeout: 5,
			},
			expectError: true,
			errorField:  "health_check.command",
		},
		{
			name: "Invalid type",
			healthCheck: &HealthCheck{
				Type:   "invalid",
				Period: 10,
			},
			expectError: true,
			errorField:  "health_check.type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Global: GlobalConfig{
					ShutdownTimeout:    30,
					LogLevel:           "info",
					LogFormat:          "json",
					MaxRestartAttempts: 3,
					RestartBackoff:     5,
				},
				Processes: map[string]*Process{
					"test": {
						Enabled:      true,
						Type:         "longrun",
						InitialState: "running",
						Command:      []string{"sleep", "60"},
						Restart:      "always",
						Scale:        1,
						HealthCheck:  tt.healthCheck,
					},
				},
			}

			result, err := cfg.ValidateComprehensive()

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for %s", tt.name)
				}
				found := false
				for _, e := range result.Errors {
					if strings.Contains(e.Field, tt.errorField) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected error field containing '%s'", tt.errorField)
				}
			}
		})
	}
}

func TestValidateComprehensive_Warnings(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{
			ShutdownTimeout:    5,       // Short timeout - warning
			LogLevel:           "debug", // Debug level - warning
			LogFormat:          "json",
			MaxRestartAttempts: 3,
			RestartBackoff:     0, // Short backoff - warning
			APIPort:            8080,
			MetricsPort:        9090,
		},
		Processes: map[string]*Process{
			"test": {
				Enabled:      true,
				Type:         "longrun",
				InitialState: "running",
				Command:      []string{"sleep", "60"},
				Restart:      "always",
				Scale:        25, // High scale - warning
			},
		},
	}

	result, err := cfg.ValidateComprehensive()

	// Should pass but with warnings
	if err != nil {
		t.Errorf("Expected no blocking error, got: %v", err)
	}

	if !result.HasWarnings() {
		t.Error("Expected warnings for this config")
	}

	// Check for specific warnings
	warningFields := []string{}
	for _, w := range result.Warnings {
		warningFields = append(warningFields, w.Field)
	}
	t.Logf("Warnings found: %v", warningFields)
}

func TestValidateComprehensive_Suggestions(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{
			ShutdownTimeout:    200, // Long timeout - suggestion
			LogLevel:           "info",
			LogFormat:          "text", // Text format - suggestion
			MaxRestartAttempts: 15,     // High attempts - suggestion
			RestartBackoff:     5,
			APIPort:            8080,
			MetricsPort:        9090,
		},
		Processes: map[string]*Process{
			"test": {
				Enabled:      true,
				Type:         "longrun",
				InitialState: "running",
				Command:      []string{"sleep", "60"},
				Restart:      "always",
				Scale:        1,
			},
		},
	}

	result, err := cfg.ValidateComprehensive()

	if err != nil {
		t.Errorf("Expected no blocking error, got: %v", err)
	}

	if !result.HasSuggestions() {
		t.Error("Expected suggestions for this config")
	}

	// Check for text format suggestion
	found := false
	for _, s := range result.Suggestions {
		if s.Field == "global.log_format" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected suggestion for log_format")
	}
}

func TestContains(t *testing.T) {
	slice := []string{"a", "b", "c"}

	if !contains(slice, "a") {
		t.Error("Expected contains(slice, 'a') to be true")
	}

	if !contains(slice, "b") {
		t.Error("Expected contains(slice, 'b') to be true")
	}

	if contains(slice, "d") {
		t.Error("Expected contains(slice, 'd') to be false")
	}

	if contains([]string{}, "a") {
		t.Error("Expected contains(empty, 'a') to be false")
	}
}

// TestValidateComprehensive_UpperBounds tests upper bound validation for configurable values
func TestValidateComprehensive_UpperBounds(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorField  string
	}{
		{
			name: "shutdown_timeout exceeds max",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout:    MaxShutdownTimeout + 1, // Exceeds max
					LogLevel:           "info",
					LogFormat:          "json",
					MaxRestartAttempts: 3,
					RestartBackoff:     5,
				},
				Processes: map[string]*Process{
					"test": {
						Enabled:      true,
						Type:         "longrun",
						InitialState: "running",
						Command:      []string{"sleep", "60"},
						Restart:      "always",
						Scale:        1,
					},
				},
			},
			expectError: true,
			errorField:  "global.shutdown_timeout",
		},
		{
			name: "max_restart_attempts exceeds max",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout:    30,
					LogLevel:           "info",
					LogFormat:          "json",
					MaxRestartAttempts: MaxRestartAttemptsLimit + 1, // Exceeds max
					RestartBackoff:     5,
				},
				Processes: map[string]*Process{
					"test": {
						Enabled:      true,
						Type:         "longrun",
						InitialState: "running",
						Command:      []string{"sleep", "60"},
						Restart:      "always",
						Scale:        1,
					},
				},
			},
			expectError: true,
			errorField:  "global.max_restart_attempts",
		},
		{
			name: "process scale exceeds max",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout:    30,
					LogLevel:           "info",
					LogFormat:          "json",
					MaxRestartAttempts: 3,
					RestartBackoff:     5,
				},
				Processes: map[string]*Process{
					"test": {
						Enabled:      true,
						Type:         "longrun",
						InitialState: "running",
						Command:      []string{"sleep", "60"},
						Restart:      "always",
						Scale:        MaxProcessScaleLimit + 1, // Exceeds max
					},
				},
			},
			expectError: true,
			errorField:  "processes.test.scale",
		},
		{
			name: "process max_scale exceeds max",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout:    30,
					LogLevel:           "info",
					LogFormat:          "json",
					MaxRestartAttempts: 3,
					RestartBackoff:     5,
				},
				Processes: map[string]*Process{
					"test": {
						Enabled:      true,
						Type:         "longrun",
						InitialState: "running",
						Command:      []string{"sleep", "60"},
						Restart:      "always",
						Scale:        1,
						MaxScale:     MaxProcessScaleLimit + 1, // Exceeds max
					},
				},
			},
			expectError: true,
			errorField:  "processes.test.max_scale",
		},
		{
			name: "schedule_history_size exceeds max",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout:     30,
					LogLevel:            "info",
					LogFormat:           "json",
					MaxRestartAttempts:  3,
					RestartBackoff:      5,
					ScheduleHistorySize: MaxScheduleHistorySize + 1, // Exceeds max
				},
				Processes: map[string]*Process{
					"test": {
						Enabled:      true,
						Type:         "longrun",
						InitialState: "running",
						Command:      []string{"sleep", "60"},
						Restart:      "always",
						Scale:        1,
					},
				},
			},
			expectError: true,
			errorField:  "global.schedule_history_size",
		},
		{
			name: "oneshot_history_max_entries exceeds max",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout:          30,
					LogLevel:                 "info",
					LogFormat:                "json",
					MaxRestartAttempts:       3,
					RestartBackoff:           5,
					OneshotHistoryMaxEntries: MaxOneshotHistoryEntries + 1, // Exceeds max
				},
				Processes: map[string]*Process{
					"test": {
						Enabled:      true,
						Type:         "longrun",
						InitialState: "running",
						Command:      []string{"sleep", "60"},
						Restart:      "always",
						Scale:        1,
					},
				},
			},
			expectError: true,
			errorField:  "global.oneshot_history_max_entries",
		},
		{
			name: "max_process_scale exceeds max",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout:    30,
					LogLevel:           "info",
					LogFormat:          "json",
					MaxRestartAttempts: 3,
					RestartBackoff:     5,
					MaxProcessScale:    MaxProcessScaleLimit + 1, // Exceeds max
				},
				Processes: map[string]*Process{
					"test": {
						Enabled:      true,
						Type:         "longrun",
						InitialState: "running",
						Command:      []string{"sleep", "60"},
						Restart:      "always",
						Scale:        1,
					},
				},
			},
			expectError: true,
			errorField:  "global.max_process_scale",
		},
		{
			name: "api_max_request_body exceeds max",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout:    30,
					LogLevel:           "info",
					LogFormat:          "json",
					MaxRestartAttempts: 3,
					RestartBackoff:     5,
					APIMaxRequestBody:  MaxAPIRequestBodySize + 1, // Exceeds max
				},
				Processes: map[string]*Process{
					"test": {
						Enabled:      true,
						Type:         "longrun",
						InitialState: "running",
						Command:      []string{"sleep", "60"},
						Restart:      "always",
						Scale:        1,
					},
				},
			},
			expectError: true,
			errorField:  "global.api_max_request_body",
		},
		{
			name: "all values within limits",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout:    MaxShutdownTimeout, // At max, not over
					LogLevel:           "info",
					LogFormat:          "json",
					MaxRestartAttempts: MaxRestartAttemptsLimit, // At max, not over
					RestartBackoff:     5,
					APIPort:            9180, // Non-privileged port
					MetricsPort:        9181, // Non-privileged port
				},
				Processes: map[string]*Process{
					"test": {
						Enabled:      true,
						Type:         "longrun",
						InitialState: "running",
						Command:      []string{"sleep", "60"},
						Restart:      "always",
						Scale:        MaxProcessScaleLimit, // At max, not over
					},
				},
			},
			expectError: false,
			errorField:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.config.ValidateComprehensive()

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for field %s, got none", tt.errorField)
				} else {
					// Verify the error field is correct
					found := false
					for _, e := range result.Errors {
						if e.Field == tt.errorField {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Expected error for field %s, but found errors: %v", tt.errorField, result.Errors)
					}
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
			}
		})
	}
}

// TestValidateComprehensive_ResourceMetricsUpperBounds tests upper bounds for resource metrics
func TestValidateComprehensive_ResourceMetricsUpperBounds(t *testing.T) {
	boolTrue := true

	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorField  string
	}{
		{
			name: "resource_metrics_interval exceeds max",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout:         30,
					LogLevel:                "info",
					LogFormat:               "json",
					MaxRestartAttempts:      3,
					RestartBackoff:          5,
					ResourceMetricsEnabled:  &boolTrue,
					ResourceMetricsInterval: MaxResourceMetricsInterval + 1, // Exceeds max
				},
				Processes: map[string]*Process{
					"test": {
						Enabled:      true,
						Type:         "longrun",
						InitialState: "running",
						Command:      []string{"sleep", "60"},
						Restart:      "always",
						Scale:        1,
					},
				},
			},
			expectError: true,
			errorField:  "global.resource_metrics_interval",
		},
		{
			name: "resource_metrics_max_samples exceeds max",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout:           30,
					LogLevel:                  "info",
					LogFormat:                 "json",
					MaxRestartAttempts:        3,
					RestartBackoff:            5,
					ResourceMetricsEnabled:    &boolTrue,
					ResourceMetricsInterval:   10,
					ResourceMetricsMaxSamples: MaxResourceMetricsSamples + 1, // Exceeds max
				},
				Processes: map[string]*Process{
					"test": {
						Enabled:      true,
						Type:         "longrun",
						InitialState: "running",
						Command:      []string{"sleep", "60"},
						Restart:      "always",
						Scale:        1,
					},
				},
			},
			expectError: true,
			errorField:  "global.resource_metrics_max_samples",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.config.ValidateComprehensive()

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for field %s, got none", tt.errorField)
				} else {
					found := false
					for _, e := range result.Errors {
						if e.Field == tt.errorField {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Expected error for field %s, but found errors: %v", tt.errorField, result.Errors)
					}
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
			}
		})
	}
}

// TestValidateComprehensive_ZombieReapIntervalBounds tests bounds for zombie_reap_interval
func TestValidateComprehensive_ZombieReapIntervalBounds(t *testing.T) {
	tests := []struct {
		name               string
		zombieReapInterval time.Duration
		expectError        bool
		errorField         string
	}{
		{
			name:               "below minimum (10ms)",
			zombieReapInterval: 10 * time.Millisecond, // Below 100ms minimum
			expectError:        true,
			errorField:         "global.zombie_reap_interval",
		},
		{
			name:               "below minimum (50ms)",
			zombieReapInterval: 50 * time.Millisecond, // Below 100ms minimum
			expectError:        true,
			errorField:         "global.zombie_reap_interval",
		},
		{
			name:               "at minimum (100ms)",
			zombieReapInterval: MinZombieReapInterval, // Exactly at minimum
			expectError:        false,
			errorField:         "",
		},
		{
			name:               "normal value (1s)",
			zombieReapInterval: 1 * time.Second, // Normal value
			expectError:        false,
			errorField:         "",
		},
		{
			name:               "at maximum (60s)",
			zombieReapInterval: MaxZombieReapInterval, // Exactly at maximum
			expectError:        false,
			errorField:         "",
		},
		{
			name:               "exceeds maximum (61s)",
			zombieReapInterval: 61 * time.Second, // Above 60s maximum
			expectError:        true,
			errorField:         "global.zombie_reap_interval",
		},
		{
			name:               "exceeds maximum (2m)",
			zombieReapInterval: 2 * time.Minute, // Way above 60s maximum
			expectError:        true,
			errorField:         "global.zombie_reap_interval",
		},
		{
			name:               "zero value (uses default)",
			zombieReapInterval: 0, // Zero means use default, should pass validation
			expectError:        false,
			errorField:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Global: GlobalConfig{
					ShutdownTimeout:    30,
					LogLevel:           "info",
					LogFormat:          "json",
					MaxRestartAttempts: 3,
					RestartBackoff:     5,
					ZombieReapInterval: tt.zombieReapInterval,
					APIPort:            9180, // Non-privileged port
					MetricsPort:        9181, // Non-privileged port
				},
				Processes: map[string]*Process{
					"test": {
						Enabled:      true,
						Type:         "longrun",
						InitialState: "running",
						Command:      []string{"sleep", "60"},
						Restart:      "always",
						Scale:        1,
					},
				},
			}

			result, err := cfg.ValidateComprehensive()

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for zombie_reap_interval=%v, got none", tt.zombieReapInterval)
				} else {
					found := false
					for _, e := range result.Errors {
						if e.Field == tt.errorField {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Expected error for field %s, but found errors: %v", tt.errorField, result.Errors)
					}
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for zombie_reap_interval=%v, got: %v", tt.zombieReapInterval, err)
				}
			}
		})
	}
}
