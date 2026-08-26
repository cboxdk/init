package config

import (
	"fmt"
	"strings"
)

// FormatValidationReport formats validation results as a human-readable report
func FormatValidationReport(result *ValidationResult) string {
	if result.TotalIssues() == 0 {
		return "✅ Configuration validation passed with no issues"
	}

	var lines []string

	// Header
	lines = append(lines, "")
	lines = append(lines, "═══════════════════════════════════════════════════════════════")
	lines = append(lines, "  Configuration Validation Report")
	lines = append(lines, "═══════════════════════════════════════════════════════════════")
	lines = append(lines, "")

	// Summary
	summary := fmt.Sprintf("  Total Issues: %d  ", result.TotalIssues())
	if len(result.Errors) > 0 {
		summary += fmt.Sprintf("❌ %d Error(s)  ", len(result.Errors))
	}
	if len(result.Warnings) > 0 {
		summary += fmt.Sprintf("⚠️  %d Warning(s)  ", len(result.Warnings))
	}
	if len(result.Suggestions) > 0 {
		summary += fmt.Sprintf("💡 %d Suggestion(s)", len(result.Suggestions))
	}
	lines = append(lines, summary)
	lines = append(lines, "")

	// Errors section (blocking)
	if len(result.Errors) > 0 {
		lines = append(lines, "❌ ERRORS (must be fixed):")
		lines = append(lines, strings.Repeat("─", 63))
		for i, err := range result.Errors {
			lines = append(lines, fmt.Sprintf("  %d. [%s]", i+1, err.Field))
			lines = append(lines, fmt.Sprintf("     %s", err.Message))
			if err.Suggestion != "" {
				lines = append(lines, fmt.Sprintf("     → Fix: %s", err.Suggestion))
			}
			if i < len(result.Errors)-1 {
				lines = append(lines, "")
			}
		}
		lines = append(lines, "")
	}

	// Warnings section (should review)
	if len(result.Warnings) > 0 {
		lines = append(lines, "⚠️  WARNINGS (should be reviewed):")
		lines = append(lines, strings.Repeat("─", 63))
		for i, warn := range result.Warnings {
			lines = append(lines, fmt.Sprintf("  %d. [%s]", i+1, warn.Field))
			lines = append(lines, fmt.Sprintf("     %s", warn.Message))
			if warn.Suggestion != "" {
				lines = append(lines, fmt.Sprintf("     → Recommendation: %s", warn.Suggestion))
			}
			if i < len(result.Warnings)-1 {
				lines = append(lines, "")
			}
		}
		lines = append(lines, "")
	}

	// Suggestions section (best practices)
	if len(result.Suggestions) > 0 {
		lines = append(lines, "💡 SUGGESTIONS (best practices):")
		lines = append(lines, strings.Repeat("─", 63))
		for i, sugg := range result.Suggestions {
			lines = append(lines, fmt.Sprintf("  %d. [%s]", i+1, sugg.Field))
			lines = append(lines, fmt.Sprintf("     %s", sugg.Message))
			if sugg.Suggestion != "" {
				// Suggestions are already phrased as advice ("Consider …",
				// "Use …"), so the arrow alone reads correctly — a "Consider:"
				// prefix here produced "→ Consider: Consider …".
				lines = append(lines, fmt.Sprintf("     → %s", sugg.Suggestion))
			}
			if i < len(result.Suggestions)-1 {
				lines = append(lines, "")
			}
		}
		lines = append(lines, "")
	}

	// Footer
	lines = append(lines, "═══════════════════════════════════════════════════════════════")

	if result.HasErrors() {
		lines = append(lines, "  ❌ Validation failed: please fix errors before starting")
	} else if result.HasWarnings() {
		lines = append(lines, "  ✅ Validation passed (with warnings)")
	} else {
		lines = append(lines, "  ✅ Validation passed (with suggestions)")
	}

	lines = append(lines, "═══════════════════════════════════════════════════════════════")
	lines = append(lines, "")

	return strings.Join(lines, "\n")
}

// FormatValidationSummary formats a brief validation summary (one line)
func FormatValidationSummary(result *ValidationResult) string {
	if result.TotalIssues() == 0 {
		return "✅ Validation passed"
	}

	parts := []string{}
	if len(result.Errors) > 0 {
		parts = append(parts, fmt.Sprintf("❌ %d error(s)", len(result.Errors)))
	}
	if len(result.Warnings) > 0 {
		parts = append(parts, fmt.Sprintf("⚠️  %d warning(s)", len(result.Warnings)))
	}
	if len(result.Suggestions) > 0 {
		parts = append(parts, fmt.Sprintf("💡 %d suggestion(s)", len(result.Suggestions)))
	}

	return strings.Join(parts, ", ")
}

// FormatValidationJSON formats validation results as JSON (for API/programmatic use)
func FormatValidationJSON(result *ValidationResult) map[string]any {
	return map[string]any{
		"passed": !result.HasErrors(),
		"summary": map[string]int{
			"errors":      len(result.Errors),
			"warnings":    len(result.Warnings),
			"suggestions": len(result.Suggestions),
			"total":       result.TotalIssues(),
		},
		"errors":      formatIssuesJSON(result.Errors),
		"warnings":    formatIssuesJSON(result.Warnings),
		"suggestions": formatIssuesJSON(result.Suggestions),
	}
}

func formatIssuesJSON(issues []ValidationIssue) []map[string]string {
	result := make([]map[string]string, len(issues))
	for i, issue := range issues {
		result[i] = map[string]string{
			"severity":   string(issue.Severity),
			"field":      issue.Field,
			"message":    issue.Message,
			"suggestion": issue.Suggestion,
		}
		if issue.ProcessName != "" {
			result[i]["process"] = issue.ProcessName
		}
	}
	return result
}
