package config

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

// ValidationSeverity represents the severity level of a validation issue
type ValidationSeverity string

const (
	SeverityError      ValidationSeverity = "error"      // Blocking, must be fixed
	SeverityWarning    ValidationSeverity = "warning"    // Non-blocking, should review
	SeveritySuggestion ValidationSeverity = "suggestion" // Best practice recommendation
)

// ValidationIssue represents a single validation problem
type ValidationIssue struct {
	Severity    ValidationSeverity
	Field       string // Config field path (e.g., "global.log_level", "processes.php-fpm.command")
	Message     string
	Suggestion  string // How to fix it
	ProcessName string // Optional: which process this relates to
}

// ValidationResult contains all validation issues found
type ValidationResult struct {
	Errors      []ValidationIssue
	Warnings    []ValidationIssue
	Suggestions []ValidationIssue
}

// NewValidationResult creates an empty validation result
func NewValidationResult() *ValidationResult {
	return &ValidationResult{
		Errors:      []ValidationIssue{},
		Warnings:    []ValidationIssue{},
		Suggestions: []ValidationIssue{},
	}
}

// AddError adds a blocking error
func (vr *ValidationResult) AddError(field, message, suggestion string) {
	vr.Errors = append(vr.Errors, ValidationIssue{
		Severity:   SeverityError,
		Field:      field,
		Message:    message,
		Suggestion: suggestion,
	})
}

// AddWarning adds a non-blocking warning
func (vr *ValidationResult) AddWarning(field, message, suggestion string) {
	vr.Warnings = append(vr.Warnings, ValidationIssue{
		Severity:   SeverityWarning,
		Field:      field,
		Message:    message,
		Suggestion: suggestion,
	})
}

// AddSuggestion adds a best practice recommendation
func (vr *ValidationResult) AddSuggestion(field, message, suggestion string) {
	vr.Suggestions = append(vr.Suggestions, ValidationIssue{
		Severity:   SeveritySuggestion,
		Field:      field,
		Message:    message,
		Suggestion: suggestion,
	})
}

// AddProcessError adds a process-specific error
func (vr *ValidationResult) AddProcessError(processName, field, message, suggestion string) {
	vr.Errors = append(vr.Errors, ValidationIssue{
		Severity:    SeverityError,
		Field:       fmt.Sprintf("processes.%s.%s", processName, field),
		Message:     message,
		Suggestion:  suggestion,
		ProcessName: processName,
	})
}

// AddProcessWarning adds a process-specific warning
func (vr *ValidationResult) AddProcessWarning(processName, field, message, suggestion string) {
	vr.Warnings = append(vr.Warnings, ValidationIssue{
		Severity:    SeverityWarning,
		Field:       fmt.Sprintf("processes.%s.%s", processName, field),
		Message:     message,
		Suggestion:  suggestion,
		ProcessName: processName,
	})
}

// AddProcessSuggestion adds a process-specific suggestion
func (vr *ValidationResult) AddProcessSuggestion(processName, field, message, suggestion string) {
	vr.Suggestions = append(vr.Suggestions, ValidationIssue{
		Severity:    SeveritySuggestion,
		Field:       fmt.Sprintf("processes.%s.%s", processName, field),
		Message:     message,
		Suggestion:  suggestion,
		ProcessName: processName,
	})
}

// HasErrors returns true if there are blocking errors
func (vr *ValidationResult) HasErrors() bool {
	return len(vr.Errors) > 0
}

// HasWarnings returns true if there are warnings
func (vr *ValidationResult) HasWarnings() bool {
	return len(vr.Warnings) > 0
}

// HasSuggestions returns true if there are suggestions
func (vr *ValidationResult) HasSuggestions() bool {
	return len(vr.Suggestions) > 0
}

// TotalIssues returns the total count of all issues
func (vr *ValidationResult) TotalIssues() int {
	return len(vr.Errors) + len(vr.Warnings) + len(vr.Suggestions)
}

// ToError converts validation result to an error (only if errors exist)
func (vr *ValidationResult) ToError() error {
	if !vr.HasErrors() {
		return nil
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("configuration validation failed with %d error(s):", len(vr.Errors)))
	for _, err := range vr.Errors {
		lines = append(lines, fmt.Sprintf("  - [%s] %s", err.Field, err.Message))
		if err.Suggestion != "" {
			lines = append(lines, fmt.Sprintf("    → %s", err.Suggestion))
		}
	}

	return fmt.Errorf("%s", strings.Join(lines, "\n"))
}

// ValidateComprehensive performs comprehensive validation with errors, warnings, and suggestions
func (c *Config) ValidateComprehensive() (*ValidationResult, error) {
	result := NewValidationResult()

	// Run all validation steps
	c.validateGlobalSettings(result)
	c.validateProcesses(result)
	c.validateDependencies(result)
	c.lintConfiguration(result)
	c.validateSystem(result)
	c.validateSecurity(result)

	// Return error if there are blocking issues
	if result.HasErrors() {
		return result, result.ToError()
	}

	return result, nil
}

// Upper bound constants for configuration validation
const (
	MaxShutdownTimeout         = 3600    // 1 hour max
	MaxRestartAttemptsLimit    = 100     // Prevent infinite restart loops
	MaxResourceMetricsInterval = 3600    // 1 hour max
	MaxResourceMetricsSamples  = 100000  // Memory constraint
	MaxScheduleHistorySize     = 10000   // Per-job history limit
	MaxOneshotHistoryEntries   = 100000  // Oneshot history limit
	MaxProcessScaleLimit        = 1000                   // Per-process instance limit
	MaxAPIRequestBodySize       = 100 * 1024 * 1024      // 100MB max
	MinZombieReapInterval       = 100 * time.Millisecond // 100ms minimum (CPU efficiency)
	MaxZombieReapInterval       = 60 * time.Second       // 60 seconds max (timely cleanup)
)

// validateGlobalSettings validates global configuration fields
func (c *Config) validateGlobalSettings(result *ValidationResult) {
	c.validateGlobalBasicSettings(result)
	c.validateGlobalResourceSettings(result)
	c.validateGlobalLimits(result)
	c.validateGlobalAPISettings(result)
	c.validateGlobalMetricsSettings(result)
	c.validateGlobalReadinessSettings(result)
}

// validateGlobalBasicSettings validates shutdown timeout, logging, and restart settings
func (c *Config) validateGlobalBasicSettings(result *ValidationResult) {
	// Shutdown timeout
	if c.Global.ShutdownTimeout < 0 {
		result.AddError("global.shutdown_timeout", "Must be a positive number", "Set to at least 1 second (recommended: 30)")
	} else if c.Global.ShutdownTimeout > MaxShutdownTimeout {
		result.AddError("global.shutdown_timeout", fmt.Sprintf("Exceeds maximum (%ds > %ds)", c.Global.ShutdownTimeout, MaxShutdownTimeout), fmt.Sprintf("Set to %d seconds or less", MaxShutdownTimeout))
	} else if c.Global.ShutdownTimeout < 10 {
		result.AddWarning("global.shutdown_timeout", fmt.Sprintf("Very short timeout (%ds) may cause abrupt process termination", c.Global.ShutdownTimeout), "Consider increasing to 30+ seconds for graceful shutdown")
	} else if c.Global.ShutdownTimeout > 300 {
		result.AddSuggestion("global.shutdown_timeout", fmt.Sprintf("Long timeout (%ds) may delay container restarts", c.Global.ShutdownTimeout), "Consider reducing to 30-120 seconds unless you have long-running operations")
	}

	// Log level
	validLogLevels := []string{"debug", "info", "warn", "error"}
	if !contains(validLogLevels, c.Global.LogLevel) {
		result.AddError("global.log_level", fmt.Sprintf("Invalid log level: %s", c.Global.LogLevel), fmt.Sprintf("Must be one of: %s", strings.Join(validLogLevels, ", ")))
	} else if c.Global.LogLevel == "debug" {
		result.AddWarning("global.log_level", "Debug logging enabled in production may impact performance", "Use 'info' level for production deployments")
	}

	// Log format
	validLogFormats := []string{"json", "text"}
	if !contains(validLogFormats, c.Global.LogFormat) {
		result.AddError("global.log_format", fmt.Sprintf("Invalid log format: %s", c.Global.LogFormat), fmt.Sprintf("Must be one of: %s", strings.Join(validLogFormats, ", ")))
	} else if c.Global.LogFormat == "text" {
		result.AddSuggestion("global.log_format", "Text format is human-readable but not ideal for log aggregation", "Consider 'json' format for production with centralized logging (Elasticsearch, Loki, etc.)")
	}

	// Restart backoff
	if c.Global.RestartBackoff < 1 {
		result.AddWarning("global.restart_backoff", fmt.Sprintf("Very short backoff (%ds) may cause rapid restart loops", c.Global.RestartBackoff), "Recommended: 5-10 seconds for exponential backoff")
	}

	// Max restart attempts
	if c.Global.MaxRestartAttempts < 1 {
		result.AddWarning("global.max_restart_attempts", "Unlimited restarts may mask persistent failures", "Consider setting to 3-5 attempts to catch failing processes")
	} else if c.Global.MaxRestartAttempts > MaxRestartAttemptsLimit {
		result.AddError("global.max_restart_attempts", fmt.Sprintf("Exceeds maximum (%d > %d)", c.Global.MaxRestartAttempts, MaxRestartAttemptsLimit), fmt.Sprintf("Set to %d or less", MaxRestartAttemptsLimit))
	} else if c.Global.MaxRestartAttempts > 10 {
		result.AddSuggestion("global.max_restart_attempts", fmt.Sprintf("High restart attempts (%d) may delay failure detection", c.Global.MaxRestartAttempts), "Consider reducing to 3-5 for faster failure detection")
	}
}

// validateGlobalResourceSettings validates resource metrics settings
func (c *Config) validateGlobalResourceSettings(result *ValidationResult) {
	if !c.Global.ResourceMetricsEnabledValue() {
		return
	}

	if c.Global.ResourceMetricsInterval < 2 {
		result.AddWarning("global.resource_metrics_interval", fmt.Sprintf("Very frequent collection (%ds) increases CPU overhead", c.Global.ResourceMetricsInterval), "Recommended: 5-10 seconds for production")
	} else if c.Global.ResourceMetricsInterval > MaxResourceMetricsInterval {
		result.AddError("global.resource_metrics_interval", fmt.Sprintf("Exceeds maximum (%ds > %ds)", c.Global.ResourceMetricsInterval, MaxResourceMetricsInterval), fmt.Sprintf("Set to %d seconds or less", MaxResourceMetricsInterval))
	} else if c.Global.ResourceMetricsInterval > 60 {
		result.AddSuggestion("global.resource_metrics_interval", fmt.Sprintf("Infrequent collection (%ds) may miss short-lived issues", c.Global.ResourceMetricsInterval), "Consider 10-30 seconds for better monitoring resolution")
	}

	if c.Global.ResourceMetricsMaxSamples < 60 {
		result.AddWarning("global.resource_metrics_max_samples", fmt.Sprintf("Low sample count (%d) provides limited historical data", c.Global.ResourceMetricsMaxSamples), "Recommended: 720+ samples for 1 hour of history at 5s interval")
	} else if c.Global.ResourceMetricsMaxSamples > MaxResourceMetricsSamples {
		result.AddError("global.resource_metrics_max_samples", fmt.Sprintf("Exceeds maximum (%d > %d)", c.Global.ResourceMetricsMaxSamples, MaxResourceMetricsSamples), fmt.Sprintf("Set to %d or less to prevent excessive memory usage", MaxResourceMetricsSamples))
	}
}

// validateGlobalLimits validates upper bound limits and zombie reap interval
func (c *Config) validateGlobalLimits(result *ValidationResult) {
	// Schedule history size
	if c.Global.ScheduleHistorySize > MaxScheduleHistorySize {
		result.AddError("global.schedule_history_size", fmt.Sprintf("Exceeds maximum (%d > %d)", c.Global.ScheduleHistorySize, MaxScheduleHistorySize), fmt.Sprintf("Set to %d or less", MaxScheduleHistorySize))
	}

	// Oneshot history limits
	if c.Global.OneshotHistoryMaxEntries > MaxOneshotHistoryEntries {
		result.AddError("global.oneshot_history_max_entries", fmt.Sprintf("Exceeds maximum (%d > %d)", c.Global.OneshotHistoryMaxEntries, MaxOneshotHistoryEntries), fmt.Sprintf("Set to %d or less", MaxOneshotHistoryEntries))
	}

	// Max process scale
	if c.Global.MaxProcessScale > MaxProcessScaleLimit {
		result.AddError("global.max_process_scale", fmt.Sprintf("Exceeds maximum (%d > %d)", c.Global.MaxProcessScale, MaxProcessScaleLimit), fmt.Sprintf("Set to %d or less", MaxProcessScaleLimit))
	}

	// API max request body
	if c.Global.APIMaxRequestBody > MaxAPIRequestBodySize {
		result.AddError("global.api_max_request_body", fmt.Sprintf("Exceeds maximum (%d > %d bytes)", c.Global.APIMaxRequestBody, MaxAPIRequestBodySize), fmt.Sprintf("Set to %d bytes (100MB) or less", MaxAPIRequestBodySize))
	}

	// Zombie reap interval bounds (only validate if explicitly set, after defaults applied)
	if c.Global.ZombieReapInterval > 0 {
		if c.Global.ZombieReapInterval < MinZombieReapInterval {
			result.AddError("global.zombie_reap_interval", fmt.Sprintf("Below minimum (%v < %v)", c.Global.ZombieReapInterval, MinZombieReapInterval), fmt.Sprintf("Set to %v or more", MinZombieReapInterval))
		}
		if c.Global.ZombieReapInterval > MaxZombieReapInterval {
			result.AddError("global.zombie_reap_interval", fmt.Sprintf("Exceeds maximum (%v > %v)", c.Global.ZombieReapInterval, MaxZombieReapInterval), fmt.Sprintf("Set to %v or less", MaxZombieReapInterval))
		}
	}
}

// validateGlobalAPISettings validates API configuration
func (c *Config) validateGlobalAPISettings(result *ValidationResult) {
	if !c.Global.APIEnabledValue() {
		return
	}

	if c.Global.APIPort < 1024 && os.Getuid() != 0 {
		result.AddError("global.api_port", fmt.Sprintf("Privileged port %d requires root", c.Global.APIPort), "Use port >= 1024 or run as root")
	}
	if c.Global.APIAuth == "" && c.Global.APIACL == nil {
		result.AddWarning("global.api_auth", "API running without authentication or ACL", "Consider enabling API token auth or IP ACL for security")
	}
	if c.Global.APITLS == nil {
		result.AddSuggestion("global.api_tls", "API running without TLS/HTTPS", "Enable TLS for production to encrypt API traffic")
	}
}

// validateGlobalMetricsSettings validates metrics configuration
func (c *Config) validateGlobalMetricsSettings(result *ValidationResult) {
	if !c.Global.MetricsEnabledValue() {
		return
	}

	if c.Global.MetricsPort < 1024 && os.Getuid() != 0 {
		result.AddError("global.metrics_port", fmt.Sprintf("Privileged port %d requires root", c.Global.MetricsPort), "Use port >= 1024 or run as root")
	}
	if c.Global.MetricsACL == nil {
		result.AddSuggestion("global.metrics_acl", "Metrics endpoint without ACL exposes monitoring data", "Consider adding IP ACL to restrict access")
	}
}

// validateGlobalReadinessSettings validates readiness file configuration for Kubernetes
func (c *Config) validateGlobalReadinessSettings(result *ValidationResult) {
	if c.Global.Readiness == nil || !c.Global.Readiness.Enabled {
		return
	}

	// Validate mode
	validReadinessModes := []string{"all_healthy", "all_running"}
	if !contains(validReadinessModes, c.Global.Readiness.Mode) {
		result.AddError("global.readiness.mode", fmt.Sprintf("Invalid readiness mode: %s", c.Global.Readiness.Mode), fmt.Sprintf("Must be one of: %s", strings.Join(validReadinessModes, ", ")))
	}

	// Validate path
	if c.Global.Readiness.Path == "" {
		result.AddError("global.readiness.path", "Readiness file path is required when enabled", "Set path like /tmp/cbox-ready")
	}

	// Validate processes list if specified
	if len(c.Global.Readiness.Processes) > 0 {
		for _, procName := range c.Global.Readiness.Processes {
			if _, exists := c.Processes[procName]; !exists {
				result.AddWarning("global.readiness.processes", fmt.Sprintf("Process '%s' not found in process list", procName), "Ensure the process name matches a defined process")
			}
		}
	}

	// Suggestion for K8s deployment
	if !strings.HasPrefix(c.Global.Readiness.Path, "/tmp/") && !strings.HasPrefix(c.Global.Readiness.Path, "/var/run/") {
		result.AddSuggestion("global.readiness.path", "Readiness file path is not in standard temp directory", "Consider using /tmp/ or /var/run/ for K8s compatibility")
	}
}

// validateProcesses validates all process configurations
func (c *Config) validateProcesses(result *ValidationResult) {
	if len(c.Processes) == 0 {
		result.AddError("processes", "No processes defined", "Add at least one process to manage")
		return
	}

	for name, proc := range c.Processes {
		c.validateSingleProcess(name, proc, result)
	}
}

// validateSingleProcess validates a single process configuration
func (c *Config) validateSingleProcess(name string, proc *Process, result *ValidationResult) {
	// Required fields
	if len(proc.Command) == 0 {
		result.AddProcessError(name, "command", "No command specified", "Add command array (e.g., [\"php-fpm\", \"-F\"])")
		return // Skip further validation for this process
	}

	// Core field validation
	c.validateProcessCoreFields(name, proc, result)

	// Scale validation
	c.validateProcessScaleConfig(name, proc, result)

	// Oneshot-specific validation
	c.validateOneshotConstraints(name, proc, result)

	// Health check validation
	if proc.HealthCheck != nil {
		c.validateHealthCheck(name, proc.HealthCheck, result)
	}

	// Logging validation
	c.validateProcessLoggingConfig(name, proc, result)

	// Security linting
	c.lintProcessSecurity(name, proc, result)
}

// validateProcessCoreFields validates type, initial_state, and restart policy
func (c *Config) validateProcessCoreFields(name string, proc *Process, result *ValidationResult) {
	validTypes := []string{"oneshot", "longrun"}
	if !contains(validTypes, proc.Type) {
		result.AddProcessError(name, "type", fmt.Sprintf("Invalid type: %s", proc.Type), fmt.Sprintf("Must be one of: %s", strings.Join(validTypes, ", ")))
	}

	validStates := []string{"running", "stopped"}
	if !contains(validStates, proc.InitialState) {
		result.AddProcessError(name, "initial_state", fmt.Sprintf("Invalid initial state: %s", proc.InitialState), fmt.Sprintf("Must be one of: %s", strings.Join(validStates, ", ")))
	}

	validRestartPolicies := []string{"always", "on-failure", "never"}
	if !contains(validRestartPolicies, proc.Restart) {
		result.AddProcessError(name, "restart", fmt.Sprintf("Invalid restart policy: %s", proc.Restart), fmt.Sprintf("Must be one of: %s", strings.Join(validRestartPolicies, ", ")))
	}
}

// validateProcessScaleConfig validates scale and max_scale settings
func (c *Config) validateProcessScaleConfig(name string, proc *Process, result *ValidationResult) {
	if proc.Scale < 1 {
		result.AddProcessError(name, "scale", fmt.Sprintf("Invalid scale: %d", proc.Scale), "Must be at least 1")
	} else if proc.Scale > MaxProcessScaleLimit {
		result.AddProcessError(name, "scale", fmt.Sprintf("Exceeds maximum (%d > %d)", proc.Scale, MaxProcessScaleLimit), fmt.Sprintf("Set to %d or less", MaxProcessScaleLimit))
	} else if proc.Scale > 20 {
		result.AddProcessWarning(name, "scale", fmt.Sprintf("High scale (%d) may consume significant resources", proc.Scale), "Verify resource limits can accommodate all instances")
	}

	if proc.MaxScale > MaxProcessScaleLimit {
		result.AddProcessError(name, "max_scale", fmt.Sprintf("Exceeds maximum (%d > %d)", proc.MaxScale, MaxProcessScaleLimit), fmt.Sprintf("Set to %d or less", MaxProcessScaleLimit))
	} else if proc.MaxScale > 0 && proc.Scale > proc.MaxScale {
		result.AddProcessError(name, "max_scale", fmt.Sprintf("Scale (%d) exceeds max_scale (%d)", proc.Scale, proc.MaxScale), "Set scale <= max_scale")
	}
}

// validateOneshotConstraints validates oneshot-specific rules
func (c *Config) validateOneshotConstraints(name string, proc *Process, result *ValidationResult) {
	if proc.Type != "oneshot" {
		return
	}
	if proc.Restart == "always" {
		result.AddProcessError(name, "restart", "Oneshot cannot have restart: always", "Use 'on-failure' or 'never'")
	}
	if proc.Scale > 1 {
		result.AddProcessError(name, "scale", fmt.Sprintf("Oneshot cannot have scale > 1 (got %d)", proc.Scale), "Set scale: 1")
	}
}

// validateProcessLoggingConfig validates logging configuration
func (c *Config) validateProcessLoggingConfig(name string, proc *Process, result *ValidationResult) {
	if proc.Logging == nil {
		return
	}
	if !proc.Logging.Stdout && !proc.Logging.Stderr {
		result.AddProcessWarning(name, "logging", "Both stdout and stderr disabled", "Enable at least one stream for process output visibility")
	}
}

// validateHealthCheck validates health check configuration
func (c *Config) validateHealthCheck(processName string, hc *HealthCheck, result *ValidationResult) {
	validTypes := []string{"tcp", "http", "exec"}
	if !contains(validTypes, hc.Type) {
		result.AddProcessError(processName, "health_check.type", fmt.Sprintf("Invalid type: %s", hc.Type), fmt.Sprintf("Must be one of: %s", strings.Join(validTypes, ", ")))
	}

	switch hc.Type {
	case "tcp":
		if hc.Address == "" {
			result.AddProcessError(processName, "health_check.address", "TCP health check requires address", "Set address (e.g., 'localhost:9000')")
		}
	case "http":
		if hc.URL == "" {
			result.AddProcessError(processName, "health_check.url", "HTTP health check requires URL", "Set url (e.g., 'http://localhost:9180/health')")
		}
	case "exec":
		if len(hc.Command) == 0 {
			result.AddProcessError(processName, "health_check.command", "Exec health check requires command", "Set command array (e.g., ['php', 'artisan', 'health'])")
		}
	}

	if hc.Period < 1 {
		result.AddProcessWarning(processName, "health_check.period", fmt.Sprintf("Very frequent checks (%ds) increase overhead", hc.Period), "Recommended: 5-30 seconds")
	}

	if hc.Timeout >= hc.Period {
		result.AddProcessWarning(processName, "health_check.timeout", "Timeout >= period may cause overlapping checks", "Set timeout < period")
	}

	if hc.FailureThreshold < 1 {
		result.AddProcessSuggestion(processName, "health_check.failure_threshold", "Single health check failure triggers unhealthy", "Consider failure_threshold: 2-3 for transient failures")
	}
}

// validateDependencies validates process dependencies
func (c *Config) validateDependencies(result *ValidationResult) {
	// Check for circular dependencies
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	for name := range c.Processes {
		if !visited[name] {
			if c.detectCycle(name, visited, recStack, []string{}) {
				result.AddError("processes", fmt.Sprintf("Circular dependency detected involving: %s", name), "Remove circular dependencies to allow proper startup order")
				return
			}
		}
	}

	// Check for missing dependencies
	for name, proc := range c.Processes {
		for _, dep := range proc.DependsOn {
			if _, exists := c.Processes[dep]; !exists {
				result.AddProcessError(name, "depends_on", fmt.Sprintf("Dependency '%s' not defined", dep), fmt.Sprintf("Add process '%s' or remove from depends_on", dep))
			}
		}
	}
}

// detectCycle detects circular dependencies (DFS-based)
func (c *Config) detectCycle(name string, visited, recStack map[string]bool, path []string) bool {
	visited[name] = true
	recStack[name] = true

	proc, exists := c.Processes[name]
	if !exists {
		return false
	}

	for _, dep := range proc.DependsOn {
		if !visited[dep] {
			if c.detectCycle(dep, visited, recStack, append(path, name)) {
				return true
			}
		} else if recStack[dep] {
			return true // Cycle detected
		}
	}

	recStack[name] = false
	return false
}

// lintConfiguration performs best practice linting
func (c *Config) lintConfiguration(result *ValidationResult) {
	// Check for common misconfigurations
	if !c.Global.APIEnabledValue() && !c.Global.MetricsEnabledValue() {
		result.AddSuggestion("global", "No API or metrics enabled", "Enable API for runtime management or metrics for monitoring")
	}

	if c.Global.ResourceMetricsEnabledValue() && !c.Global.MetricsEnabledValue() {
		result.AddWarning("global.metrics_enabled", "Resource metrics enabled but Prometheus server disabled", "Enable metrics_enabled: true to expose metrics via /metrics endpoint")
	}

	// Check for enabled but unused processes
	for name, proc := range c.Processes {
		if !proc.Enabled {
			result.AddSuggestion(name, "Process defined but disabled", "Remove from config or enable to reduce clutter")
		}
	}
}

// lintProcessSecurity performs security-focused linting for a process
func (c *Config) lintProcessSecurity(name string, proc *Process, result *ValidationResult) {
	// Check for shell execution patterns
	if len(proc.Command) > 0 && (proc.Command[0] == "sh" || proc.Command[0] == "bash") {
		result.AddSuggestion(name, "Command uses shell wrapper", "Consider direct binary execution for better security and performance")
	}

	// Check for hardcoded secrets in environment
	for key, val := range proc.Env {
		lowerKey := strings.ToLower(key)
		if strings.Contains(lowerKey, "password") || strings.Contains(lowerKey, "secret") || strings.Contains(lowerKey, "token") {
			if !strings.Contains(val, "$") && !strings.Contains(val, "{") {
				result.AddProcessWarning(name, fmt.Sprintf("env.%s", key), "Possible hardcoded secret in environment variable", "Use environment variable interpolation (e.g., ${SECRET_FROM_RUNTIME})")
			}
		}
	}

	// Check user/group switching configuration
	if proc.User != "" || proc.Group != "" {
		// Warn if not running as root - user switching requires root privileges
		if os.Getuid() != 0 {
			result.AddProcessWarning(name, "user/group", fmt.Sprintf("User switching configured (user=%q, group=%q) but not running as root", proc.User, proc.Group), "Run cbox-init as root to enable per-process user switching")
		} else {
			// Running as root - good security practice to run processes as non-root
			result.AddProcessSuggestion(name, "user", fmt.Sprintf("Process configured to run as user=%q", proc.User), "Good security practice: running processes with reduced privileges")
		}
	} else if os.Getuid() == 0 {
		// Running as root but no user configured - security suggestion
		result.AddProcessSuggestion(name, "user", "No user specified for process running under root", "Consider setting user/group to run with least privilege (e.g., user: www-data)")
	}
}

// validateSystem validates system requirements and environment
func (c *Config) validateSystem(result *ValidationResult) {
	// Check OS compatibility
	if runtime.GOOS == "windows" {
		result.AddWarning("system.os", "PHPeek PM primarily designed for Linux containers", "Windows support is experimental")
	}

	// Check for PID 1 capability indicators
	if os.Getpid() == 1 {
		result.AddSuggestion("system.pid", "Running as PID 1 (signal handling and zombie reaping active)", "Ensure proper signal forwarding from container runtime")
	}
}

// validateSecurity performs security-focused validation
func (c *Config) validateSecurity(result *ValidationResult) {
	// Check for production security best practices
	if c.Global.APIEnabledValue() {
		if c.Global.APIAuth == "" && c.Global.APIACL == nil && c.Global.APITLS == nil {
			result.AddWarning("security.api", "API running without any security (auth/ACL/TLS)", "Enable at least one security measure for production")
		}
	}

	if c.Global.MetricsEnabledValue() {
		if c.Global.MetricsACL == nil && c.Global.MetricsTLS == nil {
			result.AddSuggestion("security.metrics", "Metrics endpoint without protection", "Consider IP ACL or TLS for production")
		}
	}

	// Check audit logging
	if !c.Global.AuditEnabled && c.Global.APIEnabledValue() {
		result.AddSuggestion("global.audit_enabled", "API enabled but audit logging disabled", "Enable audit logging for security event tracking")
	}
}

// contains checks if a string slice contains a value
func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}
