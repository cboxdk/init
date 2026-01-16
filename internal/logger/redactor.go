package logger

import (
	"fmt"
	"regexp"

	"github.com/cboxdk/init/internal/config"
)

// Redactor handles sensitive data redaction for compliance
type Redactor struct {
	enabled  bool
	patterns []*compiledPattern
}

// compiledPattern represents a pre-compiled redaction pattern
type compiledPattern struct {
	name        string
	regex       *regexp.Regexp
	replacement string
}

// NewRedactor creates a new Redactor from configuration
// Returns an error if pattern compilation fails
func NewRedactor(cfg *config.RedactionConfig) (*Redactor, error) {
	if cfg == nil || !cfg.Enabled {
		return &Redactor{enabled: false}, nil
	}

	patterns := make([]*compiledPattern, 0, len(cfg.Patterns))
	for _, p := range cfg.Patterns {
		// Validate pattern fields
		if p.Pattern == "" {
			return nil, fmt.Errorf("redaction pattern '%s' has empty pattern", p.Name)
		}

		// Compile regex pattern
		regex, err := regexp.Compile(p.Pattern)
		if err != nil {
			return nil, fmt.Errorf("failed to compile redaction pattern '%s': %w", p.Name, err)
		}

		// Default replacement if not specified
		replacement := p.Replacement
		if replacement == "" {
			replacement = "***"
		}

		patterns = append(patterns, &compiledPattern{
			name:        p.Name,
			regex:       regex,
			replacement: replacement,
		})
	}

	return &Redactor{
		enabled:  true,
		patterns: patterns,
	}, nil
}

// Redact applies all redaction patterns to the input string
// Returns the redacted string with sensitive data masked
// Fast-path: if !enabled, returns input immediately without processing
func (r *Redactor) Redact(input string) string {
	// Fast-path: disabled redactor
	if !r.enabled {
		return input
	}

	// Fast-path: no patterns configured
	if len(r.patterns) == 0 {
		return input
	}

	// Apply each pattern sequentially
	result := input
	for _, p := range r.patterns {
		result = p.regex.ReplaceAllString(result, p.replacement)
	}

	return result
}

// IsEnabled returns whether redaction is enabled
func (r *Redactor) IsEnabled() bool {
	return r.enabled
}

// PatternCount returns the number of configured redaction patterns
func (r *Redactor) PatternCount() int {
	return len(r.patterns)
}
