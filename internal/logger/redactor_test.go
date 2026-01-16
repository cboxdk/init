package logger

import (
	"strings"
	"testing"

	"github.com/cboxdk/init/internal/config"
)

func TestNewRedactor_Disabled(t *testing.T) {
	tests := []struct {
		name   string
		config *config.RedactionConfig
	}{
		{
			name:   "nil config",
			config: nil,
		},
		{
			name: "disabled config",
			config: &config.RedactionConfig{
				Enabled: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewRedactor(tt.config)
			if err != nil {
				t.Fatalf("NewRedactor() error = %v", err)
			}
			if r.enabled {
				t.Error("expected disabled redactor")
			}
			if r.PatternCount() != 0 {
				t.Errorf("expected 0 patterns, got %d", r.PatternCount())
			}
		})
	}
}

func TestNewRedactor_InvalidPattern(t *testing.T) {
	cfg := &config.RedactionConfig{
		Enabled: true,
		Patterns: []config.RedactionPattern{
			{
				Name:        "invalid",
				Pattern:     "[invalid(regex", // Invalid regex
				Replacement: "***",
			},
		},
	}

	_, err := NewRedactor(cfg)
	if err == nil {
		t.Fatal("expected error for invalid regex pattern")
	}
	if !strings.Contains(err.Error(), "failed to compile") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNewRedactor_EmptyPattern(t *testing.T) {
	cfg := &config.RedactionConfig{
		Enabled: true,
		Patterns: []config.RedactionPattern{
			{
				Name:        "empty",
				Pattern:     "", // Empty pattern
				Replacement: "***",
			},
		},
	}

	_, err := NewRedactor(cfg)
	if err == nil {
		t.Fatal("expected error for empty pattern")
	}
	if !strings.Contains(err.Error(), "empty pattern") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRedactor_Disabled_FastPath(t *testing.T) {
	r, err := NewRedactor(nil)
	if err != nil {
		t.Fatalf("NewRedactor() error = %v", err)
	}

	input := "password=secret123 email=user@example.com"
	result := r.Redact(input)

	// Should return input unchanged (fast-path)
	if result != input {
		t.Errorf("disabled redactor should return input unchanged, got: %s", result)
	}
}

func TestRedactor_EmailRedaction(t *testing.T) {
	cfg := &config.RedactionConfig{
		Enabled: true,
		Patterns: []config.RedactionPattern{
			{
				Name:        "email",
				Pattern:     `\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`,
				Replacement: "***@***",
			},
		},
	}

	r, err := NewRedactor(cfg)
	if err != nil {
		t.Fatalf("NewRedactor() error = %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple email",
			input:    "User email: john.doe@example.com",
			expected: "User email: ***@***",
		},
		{
			name:     "multiple emails",
			input:    "Contact: admin@example.com or support@test.org",
			expected: "Contact: ***@*** or ***@***",
		},
		{
			name:     "email in JSON",
			input:    `{"user":"test@example.com","status":"active"}`,
			expected: `{"user":"***@***","status":"active"}`,
		},
		{
			name:     "no email",
			input:    "No sensitive data here",
			expected: "No sensitive data here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.Redact(tt.input)
			if result != tt.expected {
				t.Errorf("Redact() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestRedactor_PasswordRedaction(t *testing.T) {
	cfg := &config.RedactionConfig{
		Enabled: true,
		Patterns: []config.RedactionPattern{
			{
				Name:        "password",
				Pattern:     `(password|pwd|passwd)["\s:=]+([^\s&"]+)`,
				Replacement: "$1=***",
			},
		},
	}

	r, err := NewRedactor(cfg)
	if err != nil {
		t.Fatalf("NewRedactor() error = %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "password with equals",
			input:    "password=secret123",
			expected: "password=***",
		},
		{
			name:     "password with colon",
			input:    "password: mySecretPass",
			expected: "password=***",
		},
		{
			name:     "pwd abbreviation",
			input:    "pwd=tempPass456",
			expected: "pwd=***",
		},
		{
			name:     "password in query string",
			input:    "?user=admin&password=secret&session=abc",
			expected: "?user=admin&password=***&session=abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.Redact(tt.input)
			if result != tt.expected {
				t.Errorf("Redact() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestRedactor_CreditCardRedaction(t *testing.T) {
	cfg := &config.RedactionConfig{
		Enabled: true,
		Patterns: []config.RedactionPattern{
			{
				Name:        "credit_card",
				Pattern:     `\b\d{4}[\s-]?\d{4}[\s-]?\d{4}[\s-]?\d{4}\b`,
				Replacement: "****-****-****-****",
			},
		},
	}

	r, err := NewRedactor(cfg)
	if err != nil {
		t.Fatalf("NewRedactor() error = %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "card with spaces",
			input:    "Card: 4532 1234 5678 9010",
			expected: "Card: ****-****-****-****",
		},
		{
			name:     "card with dashes",
			input:    "Card: 4532-1234-5678-9010",
			expected: "Card: ****-****-****-****",
		},
		{
			name:     "card without separators",
			input:    "Card: 4532123456789010",
			expected: "Card: ****-****-****-****",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.Redact(tt.input)
			if result != tt.expected {
				t.Errorf("Redact() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestRedactor_MultiplePatterns(t *testing.T) {
	cfg := &config.RedactionConfig{
		Enabled: true,
		Patterns: []config.RedactionPattern{
			{
				Name:        "email",
				Pattern:     `\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`,
				Replacement: "***@***",
			},
			{
				Name:        "password",
				Pattern:     `(password|pwd)["\s:=]+([^\s&"]+)`,
				Replacement: "$1=***",
			},
			{
				Name:        "api_key",
				Pattern:     `(api[_-]?key)["\s:=]+([A-Za-z0-9_-]{20,})`,
				Replacement: "$1=***",
			},
		},
	}

	r, err := NewRedactor(cfg)
	if err != nil {
		t.Fatalf("NewRedactor() error = %v", err)
	}

	if r.PatternCount() != 3 {
		t.Errorf("expected 3 patterns, got %d", r.PatternCount())
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "all patterns",
			input:    "user@test.com password=secret api_key=abcdefghij1234567890",
			expected: "***@*** password=*** api_key=***",
		},
		{
			name:     "multiple emails and passwords",
			input:    "admin@example.com pwd=pass123 support@test.org",
			expected: "***@*** pwd=*** ***@***",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.Redact(tt.input)
			if result != tt.expected {
				t.Errorf("Redact() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestRedactor_DefaultReplacement(t *testing.T) {
	cfg := &config.RedactionConfig{
		Enabled: true,
		Patterns: []config.RedactionPattern{
			{
				Name:        "email",
				Pattern:     `\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`,
				Replacement: "", // Empty replacement should default to "***"
			},
		},
	}

	r, err := NewRedactor(cfg)
	if err != nil {
		t.Fatalf("NewRedactor() error = %v", err)
	}

	input := "Email: user@example.com"
	result := r.Redact(input)
	expected := "Email: ***"

	if result != expected {
		t.Errorf("Redact() = %q, want %q (default replacement)", result, expected)
	}
}

func TestRedactor_SSNRedaction(t *testing.T) {
	cfg := &config.RedactionConfig{
		Enabled: true,
		Patterns: []config.RedactionPattern{
			{
				Name:        "ssn",
				Pattern:     `\b\d{3}-\d{2}-\d{4}\b`,
				Replacement: "***-**-****",
			},
		},
	}

	r, err := NewRedactor(cfg)
	if err != nil {
		t.Fatalf("NewRedactor() error = %v", err)
	}

	input := "SSN: 123-45-6789"
	result := r.Redact(input)
	expected := "SSN: ***-**-****"

	if result != expected {
		t.Errorf("Redact() = %q, want %q", result, expected)
	}
}

func TestRedactor_IPAddressRedaction(t *testing.T) {
	cfg := &config.RedactionConfig{
		Enabled: true,
		Patterns: []config.RedactionPattern{
			{
				Name:        "ipv4",
				Pattern:     `\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`,
				Replacement: "x.x.x.x",
			},
		},
	}

	r, err := NewRedactor(cfg)
	if err != nil {
		t.Fatalf("NewRedactor() error = %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "single IP",
			input:    "Request from 192.168.1.100",
			expected: "Request from x.x.x.x",
		},
		{
			name:     "multiple IPs",
			input:    "Forwarded: 10.0.0.1, 172.16.0.1",
			expected: "Forwarded: x.x.x.x, x.x.x.x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.Redact(tt.input)
			if result != tt.expected {
				t.Errorf("Redact() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestRedactor_LaravelSessionToken(t *testing.T) {
	cfg := &config.RedactionConfig{
		Enabled: true,
		Patterns: []config.RedactionPattern{
			{
				Name:        "laravel_session",
				Pattern:     `(laravel_session|XSRF-TOKEN)[=:]\s*([A-Za-z0-9%]+)`,
				Replacement: "$1=***",
			},
		},
	}

	r, err := NewRedactor(cfg)
	if err != nil {
		t.Fatalf("NewRedactor() error = %v", err)
	}

	input := "Cookie: laravel_session=eyJ1c2VyX2lkIjoxMjM0fQ; XSRF-TOKEN=AbCdEf123456"
	result := r.Redact(input)
	expected := "Cookie: laravel_session=***; XSRF-TOKEN=***"

	if result != expected {
		t.Errorf("Redact() = %q, want %q", result, expected)
	}
}

func TestRedactor_IsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		config  *config.RedactionConfig
		enabled bool
	}{
		{
			name:    "nil config",
			config:  nil,
			enabled: false,
		},
		{
			name: "disabled config",
			config: &config.RedactionConfig{
				Enabled: false,
			},
			enabled: false,
		},
		{
			name: "enabled config",
			config: &config.RedactionConfig{
				Enabled: true,
				Patterns: []config.RedactionPattern{
					{
						Name:        "test",
						Pattern:     `test`,
						Replacement: "***",
					},
				},
			},
			enabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewRedactor(tt.config)
			if err != nil {
				t.Fatalf("NewRedactor() error = %v", err)
			}

			if r.IsEnabled() != tt.enabled {
				t.Errorf("IsEnabled() = %v, want %v", r.IsEnabled(), tt.enabled)
			}
		})
	}
}

func TestRedactor_NoPatterns(t *testing.T) {
	cfg := &config.RedactionConfig{
		Enabled:  true,
		Patterns: []config.RedactionPattern{}, // Empty patterns
	}

	r, err := NewRedactor(cfg)
	if err != nil {
		t.Fatalf("NewRedactor() error = %v", err)
	}

	input := "password=secret123"
	result := r.Redact(input)

	// Should return input unchanged when no patterns configured
	if result != input {
		t.Errorf("Redact() with no patterns = %q, want %q", result, input)
	}
}
