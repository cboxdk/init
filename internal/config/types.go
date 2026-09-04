package config

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Config represents the complete cbox-init configuration
type Config struct {
	Version   string              `yaml:"version" json:"version"`
	Global    GlobalConfig        `yaml:"global" json:"global"`
	Hooks     HooksConfig         `yaml:"hooks" json:"hooks"`
	Processes map[string]*Process `yaml:"processes" json:"processes"`
}

// GlobalConfig contains global settings for the process manager
type GlobalConfig struct {
	ShutdownTimeout           int           `yaml:"shutdown_timeout" json:"shutdown_timeout"`                         // seconds
	HealthCheckInterval       int           `yaml:"health_check_interval" json:"health_check_interval"`               // seconds
	RestartPolicy             string        `yaml:"restart_policy" json:"restart_policy"`                             // always | on-failure | never
	MaxRestartAttempts        int           `yaml:"max_restart_attempts" json:"max_restart_attempts"`                 //
	RestartBackoff            int           `yaml:"restart_backoff" json:"restart_backoff"`                           // seconds (legacy, prefer restart_backoff_initial/max)
	RestartBackoffInitial     time.Duration `yaml:"restart_backoff_initial" json:"restart_backoff_initial"`           // initial duration (supports "5s" style)
	RestartBackoffMax         time.Duration `yaml:"restart_backoff_max" json:"restart_backoff_max"`                   // max duration
	RestartStabilityWindow    time.Duration `yaml:"restart_stability_window" json:"restart_stability_window"`         // uptime after which the restart budget resets (default 60s; negative disables)
	AutotuneMemoryThreshold   float64       `yaml:"autotune_memory_threshold" json:"autotune_memory_threshold"`       // 0.0-2.0, overrides profile MaxMemoryUsage
	AutotuneStrict            bool          `yaml:"autotune_strict" json:"autotune_strict"`                           // Fail boot (exit PID 1) when the profile does not fit; default false clamps and boots
	LogFormat                 string        `yaml:"log_format" json:"log_format"`                                     // json | text
	LogLevel                  string        `yaml:"log_level" json:"log_level"`                                       // debug | info | warn | error
	LogTimestamps             bool          `yaml:"log_timestamps" json:"log_timestamps"`                             //
	MetricsEnabled            *bool         `yaml:"metrics_enabled" json:"metrics_enabled"`                           //
	MetricsPort               int           `yaml:"metrics_port" json:"metrics_port"`                                 //
	MetricsPath               string        `yaml:"metrics_path" json:"metrics_path"`                                 //
	MetricsHost               string        `yaml:"metrics_host" json:"metrics_host"`                                 // Bind host for metrics (default: all interfaces)
	APIEnabled                *bool         `yaml:"api_enabled" json:"api_enabled"`                                   //
	APIPort                   int           `yaml:"api_port" json:"api_port"`                                         //
	APIHost                   string        `yaml:"api_host" json:"api_host"`                                         // Bind host for the management API (default: 127.0.0.1, loopback-only; set 0.0.0.0 to expose — requires api_auth or api_acl)
	APISocket                 string        `yaml:"api_socket" json:"api_socket"`                                     // Unix socket path (e.g. /var/run/cbox-init.sock)
	APIAuth                   string        `yaml:"api_auth" json:"api_auth"`                                         // Bearer token
	APITLS                    *TLSConfig    `yaml:"api_tls" json:"api_tls"`                                           // TLS configuration for API
	APIACL                    *ACLConfig    `yaml:"api_acl" json:"api_acl"`                                           // IP ACL for API
	MetricsTLS                *TLSConfig    `yaml:"metrics_tls" json:"metrics_tls"`                                   // TLS configuration for metrics
	MetricsACL                *ACLConfig    `yaml:"metrics_acl" json:"metrics_acl"`                                   // IP ACL for metrics
	ResourceMetricsEnabled    *bool         `yaml:"resource_metrics_enabled" json:"resource_metrics_enabled"`         // Enable CPU/RAM collection
	ResourceMetricsInterval   int           `yaml:"resource_metrics_interval" json:"resource_metrics_interval"`       // seconds (default: 5)
	ResourceMetricsMaxSamples int           `yaml:"resource_metrics_max_samples" json:"resource_metrics_max_samples"` // Per-instance buffer size (default: 720 = 1h at 5s)
	AuditEnabled              bool          `yaml:"audit_enabled" json:"audit_enabled"`                               // Enable audit logging
	TracingEnabled            bool          `yaml:"tracing_enabled" json:"tracing_enabled"`                           // Enable distributed tracing
	TracingExporter           string        `yaml:"tracing_exporter" json:"tracing_exporter"`                         // otlp-grpc | stdout
	TracingEndpoint           string        `yaml:"tracing_endpoint" json:"tracing_endpoint"`                         // Exporter endpoint (e.g., localhost:4317)
	// TracingSampleRate is a pointer so an explicit 0.0 — the documented way to
	// sample nothing — can be told from an absent key. Treating 0 as "unset"
	// turned it into 100% sampling, the exact opposite of what was asked for.
	// Read it through TracingSampleRateValue, which applies the 1.0 default.
	TracingSampleRate        *float64         `yaml:"tracing_sample_rate" json:"tracing_sample_rate"`                 // 0.0-1.0 (default: 1.0 = 100%)
	TracingServiceName       string           `yaml:"tracing_service_name" json:"tracing_service_name"`               // Service name for traces (default: cbox-init)
	TracingUseTLS            bool             `yaml:"tracing_use_tls" json:"tracing_use_tls"`                         // Enable TLS for production (default: false)
	ScheduleHistorySize      int              `yaml:"schedule_history_size" json:"schedule_history_size"`             // Max execution history entries per job (default: 100)
	OneshotHistoryMaxEntries int              `yaml:"oneshot_history_max_entries" json:"oneshot_history_max_entries"` // Max oneshot history entries per process (default: 5000)
	OneshotHistoryMaxAge     time.Duration    `yaml:"oneshot_history_max_age" json:"oneshot_history_max_age"`         // Max age of oneshot history entries (default: 24h)
	Readiness                *ReadinessConfig `yaml:"readiness" json:"readiness"`                                     // Container readiness file config for K8s
	FPMTune                  *FPMTuneConfig   `yaml:"fpm_tune" json:"fpm_tune"`                                       // Built-in runtime PHP-FPM autotuner (embeds cboxdk/fpm-tune)
	HealthCheckStrict        bool             `yaml:"health_check_strict" json:"health_check_strict"`                 // Fail process startup if health monitor creation fails (default: false)
	DependencyTimeout        time.Duration    `yaml:"dependency_timeout" json:"dependency_timeout"`                   // Max time to wait for dependencies to become ready (default: 5m)
	ProcessStartTimeout      time.Duration    `yaml:"process_start_timeout" json:"process_start_timeout"`             // Timeout for starting a single process (default: 30s)
	ProcessStopTimeout       time.Duration    `yaml:"process_stop_timeout" json:"process_stop_timeout"`               // Timeout for stopping a single process (default: 60s)
	MaxProcessScale          int              `yaml:"max_process_scale" json:"max_process_scale"`                     // Maximum instances per process (default: 100)
	APIMaxRequestBody        int64            `yaml:"api_max_request_body" json:"api_max_request_body"`               // Max request body size in bytes (default: 8MB)
	ZombieReapInterval       time.Duration    `yaml:"zombie_reap_interval" json:"zombie_reap_interval"`               // Interval for zombie process reaping (default: 1s)
}

// HooksConfig contains lifecycle hooks
type HooksConfig struct {
	PreStart  []Hook `yaml:"pre-start" json:"pre_start"`
	PostStart []Hook `yaml:"post-start" json:"post_start"`
	PreStop   []Hook `yaml:"pre-stop" json:"pre_stop"`
	PostStop  []Hook `yaml:"post-stop" json:"post_stop"`
}

// Hook represents a lifecycle hook command
type Hook struct {
	Name            string            `yaml:"name" json:"name"`
	Command         []string          `yaml:"command" json:"command"`
	Timeout         int               `yaml:"timeout" json:"timeout"` // seconds
	Retry           int               `yaml:"retry" json:"retry"`
	RetryDelay      int               `yaml:"retry_delay" json:"retry_delay"` // seconds
	ContinueOnError bool              `yaml:"continue_on_error" json:"continue_on_error"`
	Env             map[string]string `yaml:"env" json:"env"`
	WorkingDir      string            `yaml:"working_dir" json:"working_dir"`
}

// Process represents a managed process definition
type Process struct {
	Enabled               bool              `yaml:"enabled" json:"enabled"`
	Type                  string            `yaml:"type" json:"type"`                   // oneshot | longrun (default: longrun)
	InitialState          string            `yaml:"initial_state" json:"initial_state"` // running | stopped (default: running)
	Command               []string          `yaml:"command" json:"command"`
	WorkingDir            string            `yaml:"working_dir" json:"working_dir"` // Working directory override
	User                  string            `yaml:"user" json:"user"`               // Run as user (name or uid)
	Group                 string            `yaml:"group" json:"group"`             // Run as group (name or gid)
	Stdout                *bool             `yaml:"stdout" json:"stdout"`           // Legacy shorthand for logging.stdout
	Stderr                *bool             `yaml:"stderr" json:"stderr"`           // Legacy shorthand for logging.stderr
	Restart               string            `yaml:"restart" json:"restart"`         // always | on-failure | never
	Scale                 int               `yaml:"scale" json:"scale"`             // Number of instances
	MaxScale              int               `yaml:"max_scale" json:"max_scale"`     // Maximum instances (0 = no limit)
	DependsOn             []string          `yaml:"depends_on" json:"depends_on"`   // Process dependencies
	Env                   map[string]string `yaml:"env" json:"env"`
	HealthCheck           *HealthCheck      `yaml:"health_check" json:"health_check"`
	Shutdown              *ShutdownConfig   `yaml:"shutdown" json:"shutdown"`
	Logging               *LoggingConfig    `yaml:"logging" json:"logging"`
	Schedule              string            `yaml:"schedule" json:"schedule"`                               // Cron expression: "*/5 * * * *"
	ScheduleTimezone      string            `yaml:"schedule_timezone" json:"schedule_timezone"`             // Timezone: "UTC" (default) | "Local"
	ScheduleTimeout       string            `yaml:"schedule_timeout" json:"schedule_timeout"`               // Execution timeout: "30s", "5m", "1h" (default: no timeout)
	ScheduleMaxConcurrent int               `yaml:"schedule_max_concurrent" json:"schedule_max_concurrent"` // Accepted but not yet honored: overlapping runs are always skipped, so >1 does not enable parallel executions
	Heartbeat             *HeartbeatConfig  `yaml:"heartbeat" json:"heartbeat"`                             // Heartbeat monitoring config
	MaxMemoryMB           int               `yaml:"max_memory_mb" json:"max_memory_mb"`                     // Max memory in MB before restart (0 = disabled)
	PortBase              int               `yaml:"port_base" json:"port_base"`                             // Base port for scaled instances (PORT = port_base + index)
}

// HeartbeatConfig configures heartbeat monitoring for scheduled jobs
type HeartbeatConfig struct {
	Enabled  bool `yaml:"enabled" json:"enabled"`   // Enable heartbeat monitoring
	Interval int  `yaml:"interval" json:"interval"` // Expected interval in seconds
	Grace    int  `yaml:"grace" json:"grace"`       // Grace period before alert in seconds
}

// HealthCheck configuration
type HealthCheck struct {
	Type             string   `yaml:"type" json:"type"`                   // tcp | http | exec
	Address          string   `yaml:"address" json:"address"`             // For TCP
	URL              string   `yaml:"url" json:"url"`                     // For HTTP
	Command          []string `yaml:"command" json:"command"`             // For exec
	InitialDelay     int      `yaml:"initial_delay" json:"initial_delay"` // seconds
	Period           int      `yaml:"period" json:"period"`               // seconds
	Timeout          int      `yaml:"timeout" json:"timeout"`             // seconds
	FailureThreshold int      `yaml:"failure_threshold" json:"failure_threshold"`
	SuccessThreshold int      `yaml:"success_threshold" json:"success_threshold"`
	ExpectedStatus   int      `yaml:"expected_status" json:"expected_status"` // For HTTP
	Mode             string   `yaml:"mode" json:"mode"`                       // liveness | readiness | both (default: both)
}

// ShutdownConfig configures graceful shutdown behavior
type ShutdownConfig struct {
	Signal      string `yaml:"signal" json:"signal"`               // SIGTERM, SIGQUIT, etc.
	Timeout     int    `yaml:"timeout" json:"timeout"`             // seconds
	KillSignal  string `yaml:"kill_signal" json:"kill_signal"`     // SIGKILL
	Graceful    bool   `yaml:"graceful" json:"graceful"`           // Wait for connections
	PreStopHook *Hook  `yaml:"pre_stop_hook" json:"pre_stop_hook"` // Per-process pre-stop hook
}

// LoggingConfig configures per-process logging
type LoggingConfig struct {
	Stdout         bool                      `yaml:"stdout" json:"stdout"`
	Stderr         bool                      `yaml:"stderr" json:"stderr"`
	Labels         map[string]string         `yaml:"labels" json:"labels"`                   // Additional log labels
	MinLevel       string                    `yaml:"min_level" json:"min_level"`             // Minimum log level to output (debug|info|warn|error)
	Redaction      *RedactionConfig          `yaml:"redaction" json:"redaction"`             // Sensitive data redaction
	Multiline      *MultilineConfig          `yaml:"multiline" json:"multiline"`             // Multiline log handling
	JSON           *JSONConfig               `yaml:"json" json:"json"`                       // JSON log parsing
	LevelDetection *LevelDetectionConfig     `yaml:"level_detection" json:"level_detection"` // Log level detection from content
	Filters        *FilterConfig             `yaml:"filters" json:"filters"`                 // Include/exclude filtering
	Files          map[string]*LogFileConfig `yaml:"files" json:"files"`                     // Log file tailing
}

// RedactionConfig configures sensitive data redaction for compliance
type RedactionConfig struct {
	Enabled  bool               `yaml:"enabled" json:"enabled"`
	Patterns []RedactionPattern `yaml:"patterns" json:"patterns"`
}

// RedactionPattern defines a regex pattern for redacting sensitive data
type RedactionPattern struct {
	Name        string `yaml:"name" json:"name"`               // Pattern name for reference
	Pattern     string `yaml:"pattern" json:"pattern"`         // Regex pattern to match
	Replacement string `yaml:"replacement" json:"replacement"` // Replacement text (e.g., "***")
}

// MultilineConfig configures multiline log handling (e.g., stack traces)
type MultilineConfig struct {
	Enabled  bool   `yaml:"enabled" json:"enabled"`
	Pattern  string `yaml:"pattern" json:"pattern"`     // Regex pattern matching start of new log entry
	MaxLines int    `yaml:"max_lines" json:"max_lines"` // Max lines to buffer (default: 100)
	Timeout  int    `yaml:"timeout" json:"timeout"`     // Flush timeout in seconds (default: 1)
}

// JSONConfig configures JSON log parsing
type JSONConfig struct {
	Enabled        bool `yaml:"enabled" json:"enabled"`
	DetectAuto     bool `yaml:"detect_auto" json:"detect_auto"`         // Auto-detect JSON logs
	ExtractLevel   bool `yaml:"extract_level" json:"extract_level"`     // Extract 'level' field
	ExtractMessage bool `yaml:"extract_message" json:"extract_message"` // Extract 'message' field
	MergeFields    bool `yaml:"merge_fields" json:"merge_fields"`       // Merge other fields as attributes
}

// LevelDetectionConfig configures log level detection from log content
type LevelDetectionConfig struct {
	Enabled      bool              `yaml:"enabled" json:"enabled"`
	Patterns     map[string]string `yaml:"patterns" json:"patterns"`           // Map of level -> regex pattern
	DefaultLevel string            `yaml:"default_level" json:"default_level"` // Default level if no match (default: info)
}

// FilterConfig configures log filtering
type FilterConfig struct {
	Exclude []string `yaml:"exclude" json:"exclude"` // Exclude logs matching these patterns
	Include []string `yaml:"include" json:"include"` // Only include logs matching these patterns (if specified)
}

// LogFileConfig configures tailing of a local log file
type LogFileConfig struct {
	Path           string                `yaml:"path" json:"path"`
	Rotate         *RotateConfig         `yaml:"rotate" json:"rotate"`
	MinLevel       string                `yaml:"min_level" json:"min_level"`
	JSON           *JSONConfig           `yaml:"json" json:"json"`
	LevelDetection *LevelDetectionConfig `yaml:"level_detection" json:"level_detection"`
	Multiline      *MultilineConfig      `yaml:"multiline" json:"multiline"`
	Filters        *FilterConfig         `yaml:"filters" json:"filters"`
}

// RotateConfig configures size-based log file rotation
type RotateConfig struct {
	MaxSize  string `yaml:"max_size" json:"max_size"`   // Human-readable size: "50MB", "100KB", "1GB"
	MaxFiles int    `yaml:"max_files" json:"max_files"` // Number of rotated files to keep
}

// ParseSize parses a human-readable size string (e.g., "50MB", "100KB", "1GB") into bytes.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size string")
	}

	s = strings.ToUpper(s)

	multipliers := []struct {
		suffix string
		mult   int64
	}{
		{"GB", 1024 * 1024 * 1024},
		{"MB", 1024 * 1024},
		{"KB", 1024},
		{"B", 1},
	}

	for _, m := range multipliers {
		if strings.HasSuffix(s, m.suffix) {
			numStr := strings.TrimSuffix(s, m.suffix)
			numStr = strings.TrimSpace(numStr)
			val, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid size number %q: %w", numStr, err)
			}
			if val <= 0 {
				return 0, fmt.Errorf("size must be positive, got %s", s)
			}
			return int64(val * float64(m.mult)), nil
		}
	}

	return 0, fmt.Errorf("invalid size format %q (use KB, MB, or GB suffix)", s)
}

// TLSConfig configures TLS/HTTPS for API and metrics endpoints
type TLSConfig struct {
	Enabled            bool     `yaml:"enabled" json:"enabled"`                           // Enable TLS
	CertFile           string   `yaml:"cert_file" json:"cert_file"`                       // Path to certificate file
	KeyFile            string   `yaml:"key_file" json:"key_file"`                         // Path to private key file
	CAFile             string   `yaml:"ca_file" json:"ca_file"`                           // Path to CA certificate (for mTLS)
	ClientAuth         string   `yaml:"client_auth" json:"client_auth"`                   // none | request | require | verify (default: none)
	MinVersion         string   `yaml:"min_version" json:"min_version"`                   // TLS 1.2 | TLS 1.3 (default: TLS 1.2)
	CipherSuites       []string `yaml:"cipher_suites" json:"cipher_suites"`               // Allowed cipher suites (empty = defaults)
	AutoReload         bool     `yaml:"auto_reload" json:"auto_reload"`                   // Auto-reload certs on file change (default: false)
	AutoReloadInterval int      `yaml:"auto_reload_interval" json:"auto_reload_interval"` // Check interval in seconds (default: 300)
}

// ACLConfig configures IP-based Access Control Lists
type ACLConfig struct {
	Enabled    bool     `yaml:"enabled" json:"enabled"`         // Enable IP ACL
	Mode       string   `yaml:"mode" json:"mode"`               // allow | deny (default: allow)
	AllowList  []string `yaml:"allow_list" json:"allow_list"`   // Allowed IP addresses/CIDRs
	DenyList   []string `yaml:"deny_list" json:"deny_list"`     // Denied IP addresses/CIDRs
	TrustProxy bool     `yaml:"trust_proxy" json:"trust_proxy"` // Trust X-Forwarded-For header (default: false)
	// TrustedProxies lists the IPs/CIDRs of reverse proxies whose
	// X-Forwarded-For header is honored. X-Forwarded-For is trusted ONLY when
	// trust_proxy is true AND the direct connection comes from one of these —
	// otherwise any client could spoof the header to forge an allowed source IP.
	TrustedProxies []string `yaml:"trusted_proxies" json:"trusted_proxies"`
}

// ReadinessConfig configures container readiness for Kubernetes integration.
//
// Two complementary signals are supported:
//   - A readiness FILE (Path) for exec probes (test -f). Passive.
//   - An HTTP endpoint (HTTPPort) exposing /readyz + /livez for httpGet probes.
//     ACTIVE: because cbox-init itself serves it, a wedged/hung supervisor stops
//     answering and the probe fails — a stale file cannot express that.
type ReadinessConfig struct {
	Enabled   bool     `yaml:"enabled" json:"enabled"`     // Enable readiness tracking
	Path      string   `yaml:"path" json:"path"`           // Path to readiness file (default: /tmp/cbox-ready); empty disables the file
	Mode      string   `yaml:"mode" json:"mode"`           // Readiness mode: "all_healthy" | "all_running" (default: all_healthy)
	Content   string   `yaml:"content" json:"content"`     // Optional content to write to the file
	Processes []string `yaml:"processes" json:"processes"` // Specific processes to check (empty = all enabled longrun)
	HTTPPort  int      `yaml:"http_port" json:"http_port"` // Serve /readyz + /livez on this port (0 = disabled)
	HTTPHost  string   `yaml:"http_host" json:"http_host"` // Bind host for the HTTP endpoint (default: 0.0.0.0 so kubelet can reach it)
}

// FPMTuneConfig configures the built-in runtime PHP-FPM autotuner.
//
// The boot-time calculator (the php-fpm autotune profile) still sizes
// pm.max_children once, before php-fpm starts, because a loop cannot size a
// master that is not running yet. This loop takes over at runtime: it measures
// live per-worker memory (PSS), and when the right size has moved it rewrites a
// pool drop-in and reloads php-fpm with SIGUSR2, never a restart. Change is
// validated against a throwaway copy, written atomically, and rolled back if the
// master does not come back.
//
// It embeds github.com/cboxdk/fpm-tune, which is also a standalone tool. Do not
// run both against the same pools: the daemon takes a lock on its state file, so
// a second copy pointed at the same state refuses to start.
type FPMTuneConfig struct {
	Enabled         bool          `yaml:"enabled" json:"enabled"`                   // Enable the runtime autotuner
	Mode            string        `yaml:"mode" json:"mode"`                         // "apply" (write + reload) | "advisory" (observe + recommend only); default apply
	Interval        time.Duration `yaml:"interval" json:"interval"`                 // How often pools are sampled (default 30s)
	ReserveFraction float64       `yaml:"reserve_fraction" json:"reserve_fraction"` // Fraction of the budget held back from the pools (0 = fpm-tune's default)
	Workload        string        `yaml:"workload" json:"workload"`                 // Default workload class for pools that declare none (default "web")
	DropInDir       string        `yaml:"drop_in_dir" json:"drop_in_dir"`           // Where pool drop-ins are written; empty = the directory the master includes
	StatePath       string        `yaml:"state_path" json:"state_path"`             // Where learned baselines persist (empty = fpm-tune's default)
	BackupDir       string        `yaml:"backup_dir" json:"backup_dir"`             // Rollback / self-repair directory (empty = fpm-tune's default)
	MetricsAddr     string        `yaml:"metrics_addr" json:"metrics_addr"`         // Address for fpm-tune's own /metrics, e.g. ":9110" (empty disables it)
	RecommendPath   string        `yaml:"recommend_path" json:"recommend_path"`     // Advisory mode: write the plan here for copying by hand (empty disables it)
}

// setGlobalDefaults sets default values for global configuration
func (c *Config) setGlobalDefaults() {
	c.setGlobalBasicDefaults()
	c.setGlobalAPIMetricsDefaults()
	c.setGlobalTLSDefaults()
	c.setGlobalACLDefaults()
	c.setGlobalTracingDefaults()
	c.setGlobalHistoryDefaults()
	c.setGlobalFPMTuneDefaults()
}

// setGlobalFPMTuneDefaults fills the runtime autotuner's defaults, leaving the
// paths (state, backup, drop-in) empty so fpm-tune's own Defaults() resolve them
// the same way the standalone tool does.
func (c *Config) setGlobalFPMTuneDefaults() {
	ft := c.Global.FPMTune
	if ft == nil {
		return
	}
	if ft.Mode == "" {
		ft.Mode = "apply"
	}
	if ft.Interval == 0 {
		ft.Interval = 30 * time.Second
	}
	if ft.Workload == "" {
		ft.Workload = "web"
	}
}

// setGlobalBasicDefaults sets basic global defaults
func (c *Config) setGlobalBasicDefaults() {
	if c.Global.ShutdownTimeout == 0 {
		c.Global.ShutdownTimeout = 30
	}
	if c.Global.HealthCheckInterval == 0 {
		c.Global.HealthCheckInterval = 10
	}
	if c.Global.RestartPolicy == "" {
		c.Global.RestartPolicy = "always"
	}
	if c.Global.MaxRestartAttempts == 0 {
		c.Global.MaxRestartAttempts = 3
	}
	if c.Global.RestartBackoff == 0 {
		c.Global.RestartBackoff = 5
	}
	legacyBackoff := time.Duration(c.Global.RestartBackoff) * time.Second
	if c.Global.RestartBackoffInitial == 0 {
		if legacyBackoff > 0 {
			c.Global.RestartBackoffInitial = legacyBackoff
		} else {
			c.Global.RestartBackoffInitial = 5 * time.Second
		}
	}
	if c.Global.RestartBackoffMax == 0 {
		if legacyBackoff > 0 {
			c.Global.RestartBackoffMax = legacyBackoff * 12
		}
		if c.Global.RestartBackoffMax == 0 {
			c.Global.RestartBackoffMax = 1 * time.Minute
		}
	}
	if c.Global.LogFormat == "" {
		c.Global.LogFormat = "json"
	}
	if c.Global.LogLevel == "" {
		c.Global.LogLevel = "info"
	}
	c.Global.LogTimestamps = true
	if c.Global.ZombieReapInterval == 0 {
		c.Global.ZombieReapInterval = 1 * time.Second
	}
}

// setGlobalAPIMetricsDefaults sets API and metrics defaults
func (c *Config) setGlobalAPIMetricsDefaults() {
	if c.Global.MetricsEnabled == nil {
		c.Global.SetMetricsEnabled(false)
	}
	if c.Global.MetricsPort == 0 {
		c.Global.MetricsPort = 9090
	}
	if c.Global.MetricsPath == "" {
		c.Global.MetricsPath = "/metrics"
	}
	if c.Global.APIEnabled == nil {
		c.Global.SetAPIEnabled(false)
	}
	if c.Global.APIPort == 0 {
		c.Global.APIPort = 9180
	}
	// Safe by default: the management API is a control plane (add/stop
	// processes, save/reload config) and must not be exposed on all interfaces
	// unless the operator opts in. Empty means loopback-only; set an explicit
	// api_host (e.g. 0.0.0.0) to open it, which validation then requires be
	// paired with authentication or an ACL.
	if c.Global.APIHost == "" {
		c.Global.APIHost = "127.0.0.1"
	}
	if c.Global.ResourceMetricsEnabled == nil {
		c.Global.SetResourceMetricsEnabled(false)
	}
	if c.Global.ResourceMetricsInterval == 0 {
		c.Global.ResourceMetricsInterval = 5
	}
	if c.Global.ResourceMetricsMaxSamples == 0 {
		c.Global.ResourceMetricsMaxSamples = 720
	}
}

// setGlobalTLSDefaults sets TLS defaults for API and Metrics
func (c *Config) setGlobalTLSDefaults() {
	if c.Global.APITLS != nil {
		setTLSConfigDefaults(c.Global.APITLS)
	}
	if c.Global.MetricsTLS != nil {
		setTLSConfigDefaults(c.Global.MetricsTLS)
	}
	if c.Global.Readiness != nil {
		if c.Global.Readiness.Path == "" {
			c.Global.Readiness.Path = "/tmp/cbox-ready"
		}
		if c.Global.Readiness.Mode == "" {
			c.Global.Readiness.Mode = "all_healthy"
		}
	}
}

// setTLSConfigDefaults sets defaults for a TLS configuration
func setTLSConfigDefaults(tls *TLSConfig) {
	if tls.MinVersion == "" {
		tls.MinVersion = "TLS 1.2"
	}
	if tls.ClientAuth == "" {
		tls.ClientAuth = "none"
	}
	if tls.AutoReloadInterval == 0 {
		tls.AutoReloadInterval = 300
	}
}

// setGlobalACLDefaults sets ACL defaults
func (c *Config) setGlobalACLDefaults() {
	if c.Global.APIACL != nil && c.Global.APIACL.Mode == "" {
		c.Global.APIACL.Mode = "allow"
	}
	if c.Global.MetricsACL != nil && c.Global.MetricsACL.Mode == "" {
		c.Global.MetricsACL.Mode = "allow"
	}
}

// setGlobalTracingDefaults sets distributed tracing defaults
func (c *Config) setGlobalTracingDefaults() {
	if c.Global.TracingExporter == "" {
		c.Global.TracingExporter = "stdout"
	}

	if c.Global.TracingServiceName == "" {
		c.Global.TracingServiceName = "cbox-init"
	}
	// Only the exporters the tracing provider actually implements get a default
	// endpoint. Handing otlp-http, jaeger and zipkin a plausible one made them
	// look supported: check-config passed, and then serve exited 1 on
	// "unsupported trace exporter". Validate rejects them by name instead.
	if c.Global.TracingEndpoint == "" && c.Global.TracingExporter == "otlp-grpc" {
		c.Global.TracingEndpoint = "localhost:4317"
	}
}

// setGlobalHistoryDefaults sets history-related defaults
func (c *Config) setGlobalHistoryDefaults() {
	if c.Global.ScheduleHistorySize == 0 {
		c.Global.ScheduleHistorySize = 100
	}
	if c.Global.OneshotHistoryMaxEntries == 0 {
		c.Global.OneshotHistoryMaxEntries = 5000
	}
	if c.Global.OneshotHistoryMaxAge == 0 {
		c.Global.OneshotHistoryMaxAge = 24 * time.Hour
	}
}

// setProcessDefaults sets defaults for a single process
func (c *Config) setProcessDefaults(name string, proc *Process) {
	if proc.Type == "" {
		proc.Type = "longrun"
	}
	if proc.InitialState == "" {
		proc.InitialState = "running"
	}
	if proc.Restart == "" {
		// Oneshot processes default to "never" restart, longrun uses global default
		if proc.Type == "oneshot" {
			proc.Restart = "never"
		} else {
			proc.Restart = c.Global.RestartPolicy
		}
	}
	if proc.Scale == 0 {
		proc.Scale = 1
	}
	if proc.Schedule != "" && proc.ScheduleTimezone == "" {
		proc.ScheduleTimezone = "UTC"
	}

	c.setProcessHealthCheckDefaults(proc)
	c.setProcessShutdownDefaults(proc)
	c.setProcessLoggingDefaults(name, proc)
}

// setProcessHealthCheckDefaults sets health check defaults for a process
func (c *Config) setProcessHealthCheckDefaults(proc *Process) {
	if proc.HealthCheck == nil {
		return
	}
	hc := proc.HealthCheck
	if hc.InitialDelay == 0 {
		hc.InitialDelay = 5
	}
	if hc.Period == 0 {
		hc.Period = 10
	}
	if hc.Timeout == 0 {
		hc.Timeout = 3
	}
	if hc.FailureThreshold == 0 {
		hc.FailureThreshold = 3
	}
	if hc.SuccessThreshold == 0 {
		hc.SuccessThreshold = 1
	}
	if hc.ExpectedStatus == 0 {
		hc.ExpectedStatus = 200
	}
	if hc.Mode == "" {
		hc.Mode = "both"
	}
}

// setProcessShutdownDefaults sets shutdown defaults for a process
func (c *Config) setProcessShutdownDefaults(proc *Process) {
	if proc.Shutdown == nil {
		return
	}
	sd := proc.Shutdown
	if sd.Signal == "" {
		sd.Signal = "SIGTERM"
	}
	if sd.Timeout == 0 {
		sd.Timeout = 30
	}
	if sd.KillSignal == "" {
		sd.KillSignal = "SIGKILL"
	}
}

// setProcessLoggingDefaults sets logging defaults for a process
func (c *Config) setProcessLoggingDefaults(name string, proc *Process) {
	stdoutEnabled := true
	if proc.Stdout != nil {
		stdoutEnabled = *proc.Stdout
	}
	stderrEnabled := true
	if proc.Stderr != nil {
		stderrEnabled = *proc.Stderr
	}

	if proc.Logging == nil {
		proc.Logging = &LoggingConfig{
			Stdout: stdoutEnabled,
			Stderr: stderrEnabled,
			Labels: map[string]string{"process": name},
		}
	} else {
		if proc.Stdout != nil {
			proc.Logging.Stdout = *proc.Stdout
		}
		if proc.Stderr != nil {
			proc.Logging.Stderr = *proc.Stderr
		}
		if proc.Stdout == nil && !proc.Logging.Stdout {
			proc.Logging.Stdout = true
		}
		if proc.Stderr == nil && !proc.Logging.Stderr {
			proc.Logging.Stderr = true
		}
	}

	c.setProcessLoggingAdvancedDefaults(proc)
}

// setProcessLoggingAdvancedDefaults sets advanced logging feature defaults
func (c *Config) setProcessLoggingAdvancedDefaults(proc *Process) {
	if proc.Logging == nil {
		return
	}
	if proc.Logging.Multiline != nil {
		if proc.Logging.Multiline.MaxLines == 0 {
			proc.Logging.Multiline.MaxLines = 100
		}
		if proc.Logging.Multiline.Timeout == 0 {
			proc.Logging.Multiline.Timeout = 1
		}
	}
	if proc.Logging.LevelDetection != nil && proc.Logging.LevelDetection.DefaultLevel == "" {
		proc.Logging.LevelDetection.DefaultLevel = "info"
	}
	if proc.Logging.MinLevel == "" {
		proc.Logging.MinLevel = "info"
	}

	// Set defaults for log file tailing configs
	for _, fileCfg := range proc.Logging.Files {
		if fileCfg.Multiline != nil {
			if fileCfg.Multiline.MaxLines == 0 {
				fileCfg.Multiline.MaxLines = 100
			}
			if fileCfg.Multiline.Timeout == 0 {
				fileCfg.Multiline.Timeout = 1
			}
		}
		if fileCfg.LevelDetection != nil && fileCfg.LevelDetection.DefaultLevel == "" {
			fileCfg.LevelDetection.DefaultLevel = "info"
		}
		if fileCfg.Rotate != nil && fileCfg.Rotate.MaxFiles == 0 {
			fileCfg.Rotate.MaxFiles = 5
		}
	}
}

// SetDefaults sets sensible default values for the configuration
func (c *Config) SetDefaults() {
	if c.Version == "" {
		c.Version = "1.0"
	}

	// Set global defaults
	c.setGlobalDefaults()

	// Hook defaults
	c.setHookDefaults()

	// Process defaults.
	//
	// A YAML key with no body decodes to a nil *Process:
	//
	//   processes:
	//     app:          # <- nil
	//
	// which is what you get by commenting out a process while debugging, or by
	// adding a name before filling it in. Dereferencing it here SIGSEGV'd — at
	// boot, and worse, in the watcher's reload goroutine, where nothing
	// recovers: a running container died the moment its config was edited.
	// Validate() reports it properly; defaults just has to not crash first.
	for name, proc := range c.Processes {
		if proc == nil {
			continue
		}
		c.setProcessDefaults(name, proc)
	}
}

// hookLists returns the four hook lists with their YAML keys, in lifecycle order.
func (c *Config) hookLists() []struct {
	Key   string
	Hooks []Hook
} {
	return []struct {
		Key   string
		Hooks []Hook
	}{
		{"pre-start", c.Hooks.PreStart},
		{"post-start", c.Hooks.PostStart},
		{"pre-stop", c.Hooks.PreStop},
		{"post-stop", c.Hooks.PostStop},
	}
}

// setHookDefaults assigns a stable default name (<hook-list>-<index>) to hooks
// defined without one, so logs and metrics always carry a hook identity.
func (c *Config) setHookDefaults() {
	for _, list := range c.hookLists() {
		for i := range list.Hooks {
			if list.Hooks[i].Name == "" {
				list.Hooks[i].Name = fmt.Sprintf("%s-%d", list.Key, i)
			}
		}
	}
}

// MetricsEnabledValue returns true if metrics are enabled (default false)
func (g *GlobalConfig) MetricsEnabledValue() bool {
	if g == nil || g.MetricsEnabled == nil {
		return false
	}
	return *g.MetricsEnabled
}

// SetMetricsEnabled sets the metrics_enabled flag
func (g *GlobalConfig) SetMetricsEnabled(v bool) {
	g.MetricsEnabled = boolPtr(v)
}

// ResourceMetricsEnabledValue returns true if resource metrics enabled (default false)
func (g *GlobalConfig) ResourceMetricsEnabledValue() bool {
	if g == nil || g.ResourceMetricsEnabled == nil {
		return false
	}
	return *g.ResourceMetricsEnabled
}

// SetResourceMetricsEnabled sets resource metrics flag
func (g *GlobalConfig) SetResourceMetricsEnabled(v bool) {
	g.ResourceMetricsEnabled = boolPtr(v)
}

// APIEnabledValue returns true if API enabled (default false)
// TracingSampleRateValue returns the effective sample rate: the configured
// value if the key is present (including an explicit 0.0, which samples
// nothing), or 1.0 when it was never set.
func (g *GlobalConfig) TracingSampleRateValue() float64 {
	if g == nil || g.TracingSampleRate == nil {
		return 1.0
	}

	return *g.TracingSampleRate
}

func (g *GlobalConfig) APIEnabledValue() bool {
	if g == nil || g.APIEnabled == nil {
		return false
	}
	return *g.APIEnabled
}

// SetAPIEnabled sets the api_enabled flag
func (g *GlobalConfig) SetAPIEnabled(v bool) {
	g.APIEnabled = boolPtr(v)
}

func boolPtr(v bool) *bool {
	return &v
}

// Equal compares two Process configurations for equality
func (p *Process) Equal(other *Process) bool {
	if p == nil || other == nil {
		return p == other
	}

	// Compare basic fields. Every scalar/string field is listed here: a field
	// omitted from this comparison makes a reload that changes only that field a
	// silent no-op (the process keeps running under the old value). That is how
	// a change to `user`/`group` — dropping a service's privileges — was being
	// missed. Keep this list exhaustive against the Process struct.
	if p.Enabled != other.Enabled ||
		p.Type != other.Type ||
		p.InitialState != other.InitialState ||
		p.Scale != other.Scale ||
		p.MaxScale != other.MaxScale ||
		p.Restart != other.Restart ||
		p.WorkingDir != other.WorkingDir ||
		p.User != other.User ||
		p.Group != other.Group ||
		p.MaxMemoryMB != other.MaxMemoryMB ||
		p.PortBase != other.PortBase ||
		p.Schedule != other.Schedule ||
		p.ScheduleTimezone != other.ScheduleTimezone ||
		p.ScheduleTimeout != other.ScheduleTimeout ||
		p.ScheduleMaxConcurrent != other.ScheduleMaxConcurrent {
		return false
	}

	// Legacy stdout/stderr shorthands (*bool).
	if !boolPtrEqual(p.Stdout, other.Stdout) || !boolPtrEqual(p.Stderr, other.Stderr) {
		return false
	}

	// Compare command slices
	if !stringSliceEqual(p.Command, other.Command) {
		return false
	}

	// Compare depends_on slices
	if !stringSliceEqual(p.DependsOn, other.DependsOn) {
		return false
	}

	// Compare environment variables
	if !stringMapEqual(p.Env, other.Env) {
		return false
	}

	// Compare health check config
	if !healthCheckEqual(p.HealthCheck, other.HealthCheck) {
		return false
	}

	// Compare shutdown config
	if !shutdownConfigEqual(p.Shutdown, other.Shutdown) {
		return false
	}

	// Compare the deep opt-in config trees structurally. Both sides come from
	// the same YAML-load pipeline in every caller, so DeepEqual's nil-vs-empty
	// strictness is not a concern, and it can never drift out of date the way a
	// hand-written comparator for these large nested structs would.
	if !reflect.DeepEqual(p.Logging, other.Logging) {
		return false
	}
	if !reflect.DeepEqual(p.Heartbeat, other.Heartbeat) {
		return false
	}

	return true
}

// boolPtrEqual compares two *bool values, treating nil (unset) distinctly from
// an explicit true/false.
func boolPtrEqual(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// stringSliceEqual compares two string slices for equality
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// stringMapEqual compares two string maps for equality
func stringMapEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// healthCheckEqual compares two HealthCheck configs
func healthCheckEqual(a, b *HealthCheck) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Type == b.Type &&
		a.Address == b.Address &&
		a.URL == b.URL &&
		a.InitialDelay == b.InitialDelay &&
		a.Period == b.Period &&
		a.Timeout == b.Timeout &&
		a.FailureThreshold == b.FailureThreshold &&
		a.SuccessThreshold == b.SuccessThreshold &&
		a.ExpectedStatus == b.ExpectedStatus &&
		a.Mode == b.Mode &&
		stringSliceEqual(a.Command, b.Command)
}

// shutdownConfigEqual compares two ShutdownConfig configs
func shutdownConfigEqual(a, b *ShutdownConfig) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Signal != b.Signal ||
		a.Timeout != b.Timeout ||
		a.KillSignal != b.KillSignal ||
		a.Graceful != b.Graceful {
		return false
	}
	// Compare pre-stop hook
	return hookEqual(a.PreStopHook, b.PreStopHook)
}

// hookEqual compares two Hook configs
func hookEqual(a, b *Hook) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Name == b.Name &&
		a.Timeout == b.Timeout &&
		a.Retry == b.Retry &&
		a.RetryDelay == b.RetryDelay &&
		a.ContinueOnError == b.ContinueOnError &&
		a.WorkingDir == b.WorkingDir &&
		stringSliceEqual(a.Command, b.Command) &&
		stringMapEqual(a.Env, b.Env)
}
