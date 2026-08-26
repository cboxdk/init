package config

import (
	"strings"
	"testing"
)

func TestFormatValidationReport(t *testing.T) {
	tests := []struct {
		name     string
		result   *ValidationResult
		contains []string
	}{
		{
			name:   "no issues",
			result: NewValidationResult(),
			contains: []string{
				"✅ Configuration validation passed with no issues",
			},
		},
		{
			name: "with errors",
			result: func() *ValidationResult {
				r := NewValidationResult()
				r.AddError("test.field", "test error message", "fix suggestion")
				return r
			}(),
			contains: []string{
				"Configuration Validation Report",
				"Total Issues: 1",
				"❌ 1 Error(s)",
				"❌ ERRORS (must be fixed):",
				"[test.field]",
				"test error message",
				"→ Fix: fix suggestion",
				"❌ Validation failed: please fix errors before starting",
			},
		},
		{
			name: "with warnings only",
			result: func() *ValidationResult {
				r := NewValidationResult()
				r.AddWarning("warn.field", "warning message", "recommendation")
				return r
			}(),
			contains: []string{
				"Total Issues: 1",
				"⚠️  1 Warning(s)",
				"⚠️  WARNINGS (should be reviewed):",
				"[warn.field]",
				"warning message",
				"→ Recommendation: recommendation",
				"✅ Validation passed (with warnings)",
			},
		},
		{
			name: "with suggestions only",
			result: func() *ValidationResult {
				r := NewValidationResult()
				r.AddSuggestion("sugg.field", "suggestion message", "consider this")
				return r
			}(),
			contains: []string{
				"Total Issues: 1",
				"💡 1 Suggestion(s)",
				"💡 SUGGESTIONS (best practices):",
				"[sugg.field]",
				"suggestion message",
				"→ Consider: consider this",
				"✅ Validation passed (with suggestions)",
			},
		},
		{
			name: "with multiple errors",
			result: func() *ValidationResult {
				r := NewValidationResult()
				r.AddError("error1", "first error", "fix1")
				r.AddError("error2", "second error", "fix2")
				return r
			}(),
			contains: []string{
				"Total Issues: 2",
				"❌ 2 Error(s)",
				"[error1]",
				"first error",
				"[error2]",
				"second error",
			},
		},
		{
			name: "with all issue types",
			result: func() *ValidationResult {
				r := NewValidationResult()
				r.AddError("error.field", "error message", "error fix")
				r.AddWarning("warn.field", "warning message", "warning fix")
				r.AddSuggestion("sugg.field", "suggestion message", "suggestion fix")
				return r
			}(),
			contains: []string{
				"Total Issues: 3",
				"❌ 1 Error(s)",
				"⚠️  1 Warning(s)",
				"💡 1 Suggestion(s)",
				"❌ ERRORS (must be fixed):",
				"⚠️  WARNINGS (should be reviewed):",
				"💡 SUGGESTIONS (best practices):",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := FormatValidationReport(tt.result)

			for _, expected := range tt.contains {
				if !strings.Contains(report, expected) {
					t.Errorf("Report missing expected content: %q\nFull report:\n%s", expected, report)
				}
			}
		})
	}
}

func TestFormatValidationSummary(t *testing.T) {
	tests := []struct {
		name   string
		result *ValidationResult
		want   string
	}{
		{
			name:   "no issues",
			result: NewValidationResult(),
			want:   "✅ Validation passed",
		},
		{
			name: "errors only",
			result: func() *ValidationResult {
				r := NewValidationResult()
				r.AddError("field", "msg", "fix")
				r.AddError("field2", "msg2", "fix2")
				return r
			}(),
			want: "❌ 2 error(s)",
		},
		{
			name: "warnings only",
			result: func() *ValidationResult {
				r := NewValidationResult()
				r.AddWarning("field", "msg", "fix")
				return r
			}(),
			want: "⚠️  1 warning(s)",
		},
		{
			name: "suggestions only",
			result: func() *ValidationResult {
				r := NewValidationResult()
				r.AddSuggestion("field", "msg", "fix")
				r.AddSuggestion("field2", "msg2", "fix2")
				r.AddSuggestion("field3", "msg3", "fix3")
				return r
			}(),
			want: "💡 3 suggestion(s)",
		},
		{
			name: "errors and warnings",
			result: func() *ValidationResult {
				r := NewValidationResult()
				r.AddError("field", "msg", "fix")
				r.AddWarning("field2", "msg2", "fix2")
				return r
			}(),
			want: "❌ 1 error(s), ⚠️  1 warning(s)",
		},
		{
			name: "all types",
			result: func() *ValidationResult {
				r := NewValidationResult()
				r.AddError("e", "m", "f")
				r.AddWarning("w", "m", "f")
				r.AddSuggestion("s", "m", "f")
				return r
			}(),
			want: "❌ 1 error(s), ⚠️  1 warning(s), 💡 1 suggestion(s)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatValidationSummary(tt.result)
			if got != tt.want {
				t.Errorf("FormatValidationSummary() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatValidationJSON(t *testing.T) {
	tests := []struct {
		name     string
		result   *ValidationResult
		validate func(*testing.T, map[string]any)
	}{
		{
			name:   "no issues",
			result: NewValidationResult(),
			validate: func(t *testing.T, json map[string]any) {
				if passed, ok := json["passed"].(bool); !ok || !passed {
					t.Error("Expected passed=true for no issues")
				}
				summary := json["summary"].(map[string]int)
				if summary["errors"] != 0 {
					t.Errorf("Expected 0 errors, got %d", summary["errors"])
				}
				if summary["warnings"] != 0 {
					t.Errorf("Expected 0 warnings, got %d", summary["warnings"])
				}
				if summary["suggestions"] != 0 {
					t.Errorf("Expected 0 suggestions, got %d", summary["suggestions"])
				}
				if summary["total"] != 0 {
					t.Errorf("Expected 0 total, got %d", summary["total"])
				}
			},
		},
		{
			name: "with errors",
			result: func() *ValidationResult {
				r := NewValidationResult()
				r.AddError("test.field", "error msg", "fix it")
				return r
			}(),
			validate: func(t *testing.T, json map[string]any) {
				if passed, ok := json["passed"].(bool); !ok || passed {
					t.Error("Expected passed=false with errors")
				}
				summary := json["summary"].(map[string]int)
				if summary["errors"] != 1 {
					t.Errorf("Expected 1 error, got %d", summary["errors"])
				}
				if summary["total"] != 1 {
					t.Errorf("Expected 1 total, got %d", summary["total"])
				}

				errors := json["errors"].([]map[string]string)
				if len(errors) != 1 {
					t.Fatalf("Expected 1 error in array, got %d", len(errors))
				}
				if errors[0]["severity"] != "error" {
					t.Errorf("Expected severity 'error', got %s", errors[0]["severity"])
				}
				if errors[0]["field"] != "test.field" {
					t.Errorf("Expected field 'test.field', got %s", errors[0]["field"])
				}
				if errors[0]["message"] != "error msg" {
					t.Errorf("Expected message 'error msg', got %s", errors[0]["message"])
				}
				if errors[0]["suggestion"] != "fix it" {
					t.Errorf("Expected suggestion 'fix it', got %s", errors[0]["suggestion"])
				}
			},
		},
		{
			name: "with process error",
			result: func() *ValidationResult {
				r := NewValidationResult()
				r.AddProcessError("php-fpm", "command", "missing command", "add it")
				return r
			}(),
			validate: func(t *testing.T, json map[string]any) {
				errors := json["errors"].([]map[string]string)
				if len(errors) != 1 {
					t.Fatalf("Expected 1 error, got %d", len(errors))
				}
				if errors[0]["process"] != "php-fpm" {
					t.Errorf("Expected process 'php-fpm', got %s", errors[0]["process"])
				}
			},
		},
		{
			name: "all types",
			result: func() *ValidationResult {
				r := NewValidationResult()
				r.AddError("e.field", "error", "fix")
				r.AddWarning("w.field", "warning", "consider")
				r.AddSuggestion("s.field", "suggestion", "maybe")
				return r
			}(),
			validate: func(t *testing.T, json map[string]any) {
				summary := json["summary"].(map[string]int)
				if summary["errors"] != 1 {
					t.Errorf("Expected 1 error, got %d", summary["errors"])
				}
				if summary["warnings"] != 1 {
					t.Errorf("Expected 1 warning, got %d", summary["warnings"])
				}
				if summary["suggestions"] != 1 {
					t.Errorf("Expected 1 suggestion, got %d", summary["suggestions"])
				}
				if summary["total"] != 3 {
					t.Errorf("Expected 3 total, got %d", summary["total"])
				}

				errors := json["errors"].([]map[string]string)
				if len(errors) != 1 {
					t.Errorf("Expected 1 error, got %d", len(errors))
				}

				warnings := json["warnings"].([]map[string]string)
				if len(warnings) != 1 {
					t.Errorf("Expected 1 warning, got %d", len(warnings))
				}

				suggestions := json["suggestions"].([]map[string]string)
				if len(suggestions) != 1 {
					t.Errorf("Expected 1 suggestion, got %d", len(suggestions))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			json := FormatValidationJSON(tt.result)
			tt.validate(t, json)
		})
	}
}
