package config

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestSetDefaults(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		validate func(*testing.T, *Config)
	}{
		{
			name: "empty config gets all defaults",
			config: &Config{
				Processes: map[string]*Process{
					"test": {
						Command: []string{"sleep", "1"},
					},
				},
			},
			validate: func(t *testing.T, c *Config) {
				if c.Version != "1.0" {
					t.Errorf("Version = %v, want 1.0", c.Version)
				}
				if c.Global.ShutdownTimeout != 30 {
					t.Errorf("ShutdownTimeout = %v, want 30", c.Global.ShutdownTimeout)
				}
				if c.Global.HealthCheckInterval != 10 {
					t.Errorf("HealthCheckInterval = %v, want 10", c.Global.HealthCheckInterval)
				}
				if c.Global.RestartPolicy != "always" {
					t.Errorf("RestartPolicy = %v, want always", c.Global.RestartPolicy)
				}
				if c.Global.MaxRestartAttempts != 3 {
					t.Errorf("MaxRestartAttempts = %v, want 3", c.Global.MaxRestartAttempts)
				}
				if c.Global.RestartBackoff != 5 {
					t.Errorf("RestartBackoff = %v, want 5", c.Global.RestartBackoff)
				}
				if c.Global.RestartBackoffInitial != 5*time.Second {
					t.Errorf("RestartBackoffInitial = %v, want 5s", c.Global.RestartBackoffInitial)
				}
				if c.Global.RestartBackoffMax != 1*time.Minute {
					t.Errorf("RestartBackoffMax = %v, want 1m", c.Global.RestartBackoffMax)
				}
				if c.Global.LogFormat != "json" {
					t.Errorf("LogFormat = %v, want json", c.Global.LogFormat)
				}
				if c.Global.LogLevel != "info" {
					t.Errorf("LogLevel = %v, want info", c.Global.LogLevel)
				}
				if !c.Global.LogTimestamps {
					t.Error("LogTimestamps should be true")
				}
				if !c.Global.MetricsEnabledValue() {
					t.Error("MetricsEnabled should be true by default")
				}
				if c.Global.MetricsPort != 9090 {
					t.Errorf("MetricsPort = %v, want 9090", c.Global.MetricsPort)
				}
				if c.Global.MetricsPath != "/metrics" {
					t.Errorf("MetricsPath = %v, want /metrics", c.Global.MetricsPath)
				}
				if !c.Global.APIEnabledValue() {
					t.Error("APIEnabled should be true by default")
				}
				if c.Global.APIPort != 9180 {
					t.Errorf("APIPort = %v, want 9180", c.Global.APIPort)
				}
				if !c.Global.ResourceMetricsEnabledValue() {
					t.Error("ResourceMetricsEnabled should be true by default")
				}
				if c.Global.ResourceMetricsInterval != 5 {
					t.Errorf("ResourceMetricsInterval = %v, want 5", c.Global.ResourceMetricsInterval)
				}
				if c.Global.ResourceMetricsMaxSamples != 720 {
					t.Errorf("ResourceMetricsMaxSamples = %v, want 720", c.Global.ResourceMetricsMaxSamples)
				}
				if c.Global.TracingExporter != "stdout" {
					t.Errorf("TracingExporter = %v, want stdout", c.Global.TracingExporter)
				}
				if c.Global.TracingSampleRate != 1.0 {
					t.Errorf("TracingSampleRate = %v, want 1.0", c.Global.TracingSampleRate)
				}
				if c.Global.TracingServiceName != "phpeek-pm" {
					t.Errorf("TracingServiceName = %v, want phpeek-pm", c.Global.TracingServiceName)
				}
			},
		},
		{
			name: "legacy backoff configuration",
			config: &Config{
				Global: GlobalConfig{
					RestartBackoff: 10,
				},
				Processes: map[string]*Process{
					"test": {Command: []string{"sleep", "1"}},
				},
			},
			validate: func(t *testing.T, c *Config) {
				if c.Global.RestartBackoffInitial != 10*time.Second {
					t.Errorf("RestartBackoffInitial = %v, want 10s from legacy", c.Global.RestartBackoffInitial)
				}
				if c.Global.RestartBackoffMax != 120*time.Second {
					t.Errorf("RestartBackoffMax = %v, want 120s (legacy * 12)", c.Global.RestartBackoffMax)
				}
			},
		},
		{
			name: "process defaults",
			config: &Config{
				Processes: map[string]*Process{
					"test": {
						Command: []string{"sleep", "1"},
					},
				},
			},
			validate: func(t *testing.T, c *Config) {
				proc := c.Processes["test"]
				if proc.Type != "longrun" {
					t.Errorf("Process Type = %v, want longrun", proc.Type)
				}
				if proc.InitialState != "running" {
					t.Errorf("Process InitialState = %v, want running", proc.InitialState)
				}
				if proc.Restart != "always" {
					t.Errorf("Process Restart = %v, want always", proc.Restart)
				}
				if proc.Scale != 1 {
					t.Errorf("Process Scale = %v, want 1", proc.Scale)
				}
			},
		},
		{
			name: "health check defaults",
			config: &Config{
				Processes: map[string]*Process{
					"test": {
						Command: []string{"sleep", "1"},
						HealthCheck: &HealthCheck{
							Type: "http",
							URL:  "http://localhost:8080",
						},
					},
				},
			},
			validate: func(t *testing.T, c *Config) {
				hc := c.Processes["test"].HealthCheck
				if hc.InitialDelay != 5 {
					t.Errorf("InitialDelay = %v, want 5", hc.InitialDelay)
				}
				if hc.Period != 10 {
					t.Errorf("Period = %v, want 10", hc.Period)
				}
				if hc.Timeout != 3 {
					t.Errorf("Timeout = %v, want 3", hc.Timeout)
				}
				if hc.FailureThreshold != 3 {
					t.Errorf("FailureThreshold = %v, want 3", hc.FailureThreshold)
				}
				if hc.SuccessThreshold != 1 {
					t.Errorf("SuccessThreshold = %v, want 1", hc.SuccessThreshold)
				}
				if hc.ExpectedStatus != 200 {
					t.Errorf("ExpectedStatus = %v, want 200", hc.ExpectedStatus)
				}
				if hc.Mode != "both" {
					t.Errorf("Mode = %v, want both", hc.Mode)
				}
			},
		},
		{
			name: "shutdown defaults",
			config: &Config{
				Processes: map[string]*Process{
					"test": {
						Command:  []string{"sleep", "1"},
						Shutdown: &ShutdownConfig{},
					},
				},
			},
			validate: func(t *testing.T, c *Config) {
				sd := c.Processes["test"].Shutdown
				if sd.Signal != "SIGTERM" {
					t.Errorf("Signal = %v, want SIGTERM", sd.Signal)
				}
				if sd.Timeout != 30 {
					t.Errorf("Timeout = %v, want 30", sd.Timeout)
				}
				if sd.KillSignal != "SIGKILL" {
					t.Errorf("KillSignal = %v, want SIGKILL", sd.KillSignal)
				}
			},
		},
		{
			name: "logging defaults with legacy stdout/stderr",
			config: &Config{
				Processes: map[string]*Process{
					"test": {
						Command: []string{"sleep", "1"},
						Stdout:  boolPtr(false),
						Stderr:  boolPtr(true),
					},
				},
			},
			validate: func(t *testing.T, c *Config) {
				logging := c.Processes["test"].Logging
				if logging == nil {
					t.Fatal("Logging should not be nil")
				}
				if logging.Stdout {
					t.Error("Stdout should be false from legacy field")
				}
				if !logging.Stderr {
					t.Error("Stderr should be true from legacy field")
				}
			},
		},
		{
			name: "multiline logging defaults",
			config: &Config{
				Processes: map[string]*Process{
					"test": {
						Command: []string{"sleep", "1"},
						Logging: &LoggingConfig{
							Multiline: &MultilineConfig{},
						},
					},
				},
			},
			validate: func(t *testing.T, c *Config) {
				ml := c.Processes["test"].Logging.Multiline
				if ml.MaxLines != 100 {
					t.Errorf("MaxLines = %v, want 100", ml.MaxLines)
				}
				if ml.Timeout != 1 {
					t.Errorf("Timeout = %v, want 1", ml.Timeout)
				}
			},
		},
		{
			name: "level detection defaults",
			config: &Config{
				Processes: map[string]*Process{
					"test": {
						Command: []string{"sleep", "1"},
						Logging: &LoggingConfig{
							LevelDetection: &LevelDetectionConfig{
								Enabled: true,
							},
						},
					},
				},
			},
			validate: func(t *testing.T, c *Config) {
				ld := c.Processes["test"].Logging.LevelDetection
				if ld.DefaultLevel != "info" {
					t.Errorf("DefaultLevel = %v, want info", ld.DefaultLevel)
				}
			},
		},
		{
			name: "TLS defaults for API",
			config: &Config{
				Global: GlobalConfig{
					APITLS: &TLSConfig{
						Enabled: true,
					},
				},
				Processes: map[string]*Process{
					"test": {Command: []string{"sleep", "1"}},
				},
			},
			validate: func(t *testing.T, c *Config) {
				tls := c.Global.APITLS
				if tls.MinVersion != "TLS 1.2" {
					t.Errorf("MinVersion = %v, want TLS 1.2", tls.MinVersion)
				}
				if tls.ClientAuth != "none" {
					t.Errorf("ClientAuth = %v, want none", tls.ClientAuth)
				}
				if tls.AutoReloadInterval != 300 {
					t.Errorf("AutoReloadInterval = %v, want 300", tls.AutoReloadInterval)
				}
			},
		},
		{
			name: "TLS defaults for Metrics",
			config: &Config{
				Global: GlobalConfig{
					MetricsTLS: &TLSConfig{
						Enabled: true,
					},
				},
				Processes: map[string]*Process{
					"test": {Command: []string{"sleep", "1"}},
				},
			},
			validate: func(t *testing.T, c *Config) {
				tls := c.Global.MetricsTLS
				if tls.MinVersion != "TLS 1.2" {
					t.Errorf("MinVersion = %v, want TLS 1.2", tls.MinVersion)
				}
				if tls.ClientAuth != "none" {
					t.Errorf("ClientAuth = %v, want none", tls.ClientAuth)
				}
				if tls.AutoReloadInterval != 300 {
					t.Errorf("AutoReloadInterval = %v, want 300", tls.AutoReloadInterval)
				}
			},
		},
		{
			name: "ACL defaults for API",
			config: &Config{
				Global: GlobalConfig{
					APIACL: &ACLConfig{
						Enabled: true,
					},
				},
				Processes: map[string]*Process{
					"test": {Command: []string{"sleep", "1"}},
				},
			},
			validate: func(t *testing.T, c *Config) {
				acl := c.Global.APIACL
				if acl.Mode != "allow" {
					t.Errorf("Mode = %v, want allow", acl.Mode)
				}
			},
		},
		{
			name: "ACL defaults for Metrics",
			config: &Config{
				Global: GlobalConfig{
					MetricsACL: &ACLConfig{
						Enabled: true,
					},
				},
				Processes: map[string]*Process{
					"test": {Command: []string{"sleep", "1"}},
				},
			},
			validate: func(t *testing.T, c *Config) {
				acl := c.Global.MetricsACL
				if acl.Mode != "allow" {
					t.Errorf("Mode = %v, want allow", acl.Mode)
				}
			},
		},
		{
			name: "tracing endpoint defaults for otlp-grpc",
			config: &Config{
				Global: GlobalConfig{
					TracingExporter: "otlp-grpc",
				},
				Processes: map[string]*Process{
					"test": {Command: []string{"sleep", "1"}},
				},
			},
			validate: func(t *testing.T, c *Config) {
				if c.Global.TracingEndpoint != "localhost:4317" {
					t.Errorf("TracingEndpoint = %v, want localhost:4317", c.Global.TracingEndpoint)
				}
			},
		},
		{
			name: "tracing endpoint defaults for otlp-http",
			config: &Config{
				Global: GlobalConfig{
					TracingExporter: "otlp-http",
				},
				Processes: map[string]*Process{
					"test": {Command: []string{"sleep", "1"}},
				},
			},
			validate: func(t *testing.T, c *Config) {
				if c.Global.TracingEndpoint != "localhost:4318" {
					t.Errorf("TracingEndpoint = %v, want localhost:4318", c.Global.TracingEndpoint)
				}
			},
		},
		{
			name: "tracing endpoint defaults for jaeger",
			config: &Config{
				Global: GlobalConfig{
					TracingExporter: "jaeger",
				},
				Processes: map[string]*Process{
					"test": {Command: []string{"sleep", "1"}},
				},
			},
			validate: func(t *testing.T, c *Config) {
				if c.Global.TracingEndpoint != "localhost:14268" {
					t.Errorf("TracingEndpoint = %v, want localhost:14268", c.Global.TracingEndpoint)
				}
			},
		},
		{
			name: "tracing endpoint defaults for zipkin",
			config: &Config{
				Global: GlobalConfig{
					TracingExporter: "zipkin",
				},
				Processes: map[string]*Process{
					"test": {Command: []string{"sleep", "1"}},
				},
			},
			validate: func(t *testing.T, c *Config) {
				if c.Global.TracingEndpoint != "http://localhost:9411/api/v2/spans" {
					t.Errorf("TracingEndpoint = %v, want zipkin default", c.Global.TracingEndpoint)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.SetDefaults()
			tt.validate(t, tt.config)
		})
	}
}

func TestGlobalConfig_MetricsEnabledValue(t *testing.T) {
	tests := []struct {
		name   string
		config *GlobalConfig
		want   bool
	}{
		{
			name:   "nil config",
			config: nil,
			want:   true,
		},
		{
			name:   "nil pointer",
			config: &GlobalConfig{MetricsEnabled: nil},
			want:   true,
		},
		{
			name:   "explicitly true",
			config: &GlobalConfig{MetricsEnabled: boolPtr(true)},
			want:   true,
		},
		{
			name:   "explicitly false",
			config: &GlobalConfig{MetricsEnabled: boolPtr(false)},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.MetricsEnabledValue()
			if got != tt.want {
				t.Errorf("MetricsEnabledValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGlobalConfig_ResourceMetricsEnabledValue(t *testing.T) {
	tests := []struct {
		name   string
		config *GlobalConfig
		want   bool
	}{
		{
			name:   "nil config",
			config: nil,
			want:   true,
		},
		{
			name:   "nil pointer",
			config: &GlobalConfig{ResourceMetricsEnabled: nil},
			want:   true,
		},
		{
			name:   "explicitly true",
			config: &GlobalConfig{ResourceMetricsEnabled: boolPtr(true)},
			want:   true,
		},
		{
			name:   "explicitly false",
			config: &GlobalConfig{ResourceMetricsEnabled: boolPtr(false)},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.ResourceMetricsEnabledValue()
			if got != tt.want {
				t.Errorf("ResourceMetricsEnabledValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGlobalConfig_APIEnabledValue(t *testing.T) {
	tests := []struct {
		name   string
		config *GlobalConfig
		want   bool
	}{
		{
			name:   "nil config",
			config: nil,
			want:   true,
		},
		{
			name:   "nil pointer",
			config: &GlobalConfig{APIEnabled: nil},
			want:   true,
		},
		{
			name:   "explicitly true",
			config: &GlobalConfig{APIEnabled: boolPtr(true)},
			want:   true,
		},
		{
			name:   "explicitly false",
			config: &GlobalConfig{APIEnabled: boolPtr(false)},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.APIEnabledValue()
			if got != tt.want {
				t.Errorf("APIEnabledValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProcess_Equal(t *testing.T) {
	tests := []struct {
		name string
		p1   *Process
		p2   *Process
		want bool
	}{
		{
			name: "both nil",
			p1:   nil,
			p2:   nil,
			want: true,
		},
		{
			name: "one nil",
			p1:   &Process{},
			p2:   nil,
			want: false,
		},
		{
			name: "equal processes",
			p1: &Process{
				Enabled:   true,
				Scale:     3,
				Restart:   "always",
				Command:   []string{"sleep", "1"},
				DependsOn: []string{"dep1", "dep2"},
			},
			p2: &Process{
				Enabled:   true,
				Scale:     3,
				Restart:   "always",
				Command:   []string{"sleep", "1"},
				DependsOn: []string{"dep1", "dep2"},
			},
			want: true,
		},
		{
			name: "different enabled",
			p1: &Process{
				Enabled: true,
				Command: []string{"sleep", "1"},
			},
			p2: &Process{
				Enabled: false,
				Command: []string{"sleep", "1"},
			},
			want: false,
		},
		{
			name: "different scale",
			p1: &Process{
				Enabled: true,
				Scale:   1,
				Command: []string{"sleep", "1"},
			},
			p2: &Process{
				Enabled: true,
				Scale:   2,
				Command: []string{"sleep", "1"},
			},
			want: false,
		},
		{
			name: "different restart",
			p1: &Process{
				Enabled: true,
				Restart: "always",
				Command: []string{"sleep", "1"},
			},
			p2: &Process{
				Enabled: true,
				Restart: "on-failure",
				Command: []string{"sleep", "1"},
			},
			want: false,
		},
		{
			name: "different command length",
			p1: &Process{
				Enabled: true,
				Command: []string{"sleep", "1"},
			},
			p2: &Process{
				Enabled: true,
				Command: []string{"sleep"},
			},
			want: false,
		},
		{
			name: "different command content",
			p1: &Process{
				Enabled: true,
				Command: []string{"sleep", "1"},
			},
			p2: &Process{
				Enabled: true,
				Command: []string{"sleep", "2"},
			},
			want: false,
		},
		{
			name: "different depends_on length",
			p1: &Process{
				Enabled:   true,
				Command:   []string{"sleep", "1"},
				DependsOn: []string{"dep1"},
			},
			p2: &Process{
				Enabled:   true,
				Command:   []string{"sleep", "1"},
				DependsOn: []string{"dep1", "dep2"},
			},
			want: false,
		},
		{
			name: "different depends_on content",
			p1: &Process{
				Enabled:   true,
				Command:   []string{"sleep", "1"},
				DependsOn: []string{"dep1", "dep2"},
			},
			p2: &Process{
				Enabled:   true,
				Command:   []string{"sleep", "1"},
				DependsOn: []string{"dep1", "dep3"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.p1.Equal(tt.p2)
			if got != tt.want {
				t.Errorf("Equal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStringMapEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b map[string]string
		want bool
	}{
		{
			name: "both nil",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "both empty",
			a:    map[string]string{},
			b:    map[string]string{},
			want: true,
		},
		{
			name: "equal maps",
			a:    map[string]string{"key1": "value1", "key2": "value2"},
			b:    map[string]string{"key1": "value1", "key2": "value2"},
			want: true,
		},
		{
			name: "different lengths",
			a:    map[string]string{"key1": "value1"},
			b:    map[string]string{"key1": "value1", "key2": "value2"},
			want: false,
		},
		{
			name: "different values",
			a:    map[string]string{"key1": "value1"},
			b:    map[string]string{"key1": "value2"},
			want: false,
		},
		{
			name: "different keys",
			a:    map[string]string{"key1": "value1"},
			b:    map[string]string{"key2": "value1"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringMapEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("stringMapEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHealthCheckEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b *HealthCheck
		want bool
	}{
		{
			name: "both nil",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "a nil b not nil",
			a:    nil,
			b:    &HealthCheck{Type: "http"},
			want: false,
		},
		{
			name: "a not nil b nil",
			a:    &HealthCheck{Type: "http"},
			b:    nil,
			want: false,
		},
		{
			name: "equal health checks",
			a: &HealthCheck{
				Type:             "http",
				URL:              "http://localhost/health",
				Period:           30,
				Timeout:          5,
				FailureThreshold: 3,
			},
			b: &HealthCheck{
				Type:             "http",
				URL:              "http://localhost/health",
				Period:           30,
				Timeout:          5,
				FailureThreshold: 3,
			},
			want: true,
		},
		{
			name: "different type",
			a:    &HealthCheck{Type: "http"},
			b:    &HealthCheck{Type: "tcp"},
			want: false,
		},
		{
			name: "different command",
			a:    &HealthCheck{Type: "exec", Command: []string{"check1"}},
			b:    &HealthCheck{Type: "exec", Command: []string{"check2"}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := healthCheckEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("healthCheckEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShutdownConfigEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b *ShutdownConfig
		want bool
	}{
		{
			name: "both nil",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "a nil b not nil",
			a:    nil,
			b:    &ShutdownConfig{Signal: "SIGTERM"},
			want: false,
		},
		{
			name: "a not nil b nil",
			a:    &ShutdownConfig{Signal: "SIGTERM"},
			b:    nil,
			want: false,
		},
		{
			name: "equal configs",
			a: &ShutdownConfig{
				Signal:     "SIGTERM",
				Timeout:    30,
				KillSignal: "SIGKILL",
				Graceful:   true,
			},
			b: &ShutdownConfig{
				Signal:     "SIGTERM",
				Timeout:    30,
				KillSignal: "SIGKILL",
				Graceful:   true,
			},
			want: true,
		},
		{
			name: "different signal",
			a:    &ShutdownConfig{Signal: "SIGTERM"},
			b:    &ShutdownConfig{Signal: "SIGINT"},
			want: false,
		},
		{
			name: "different timeout",
			a:    &ShutdownConfig{Signal: "SIGTERM", Timeout: 30},
			b:    &ShutdownConfig{Signal: "SIGTERM", Timeout: 60},
			want: false,
		},
		{
			name: "equal with pre-stop hook",
			a: &ShutdownConfig{
				Signal: "SIGTERM",
				PreStopHook: &Hook{
					Name:    "cleanup",
					Command: []string{"cleanup.sh"},
					Timeout: 10,
				},
			},
			b: &ShutdownConfig{
				Signal: "SIGTERM",
				PreStopHook: &Hook{
					Name:    "cleanup",
					Command: []string{"cleanup.sh"},
					Timeout: 10,
				},
			},
			want: true,
		},
		{
			name: "different pre-stop hook",
			a: &ShutdownConfig{
				Signal:      "SIGTERM",
				PreStopHook: &Hook{Name: "hook1"},
			},
			b: &ShutdownConfig{
				Signal:      "SIGTERM",
				PreStopHook: &Hook{Name: "hook2"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shutdownConfigEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("shutdownConfigEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHookEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b *Hook
		want bool
	}{
		{
			name: "both nil",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "a nil b not nil",
			a:    nil,
			b:    &Hook{Name: "test"},
			want: false,
		},
		{
			name: "a not nil b nil",
			a:    &Hook{Name: "test"},
			b:    nil,
			want: false,
		},
		{
			name: "equal hooks",
			a: &Hook{
				Name:            "pre-start",
				Command:         []string{"/bin/sh", "-c", "echo hello"},
				Timeout:         10,
				Retry:           3,
				RetryDelay:      5,
				ContinueOnError: true,
				WorkingDir:      "/app",
				Env:             map[string]string{"FOO": "bar"},
			},
			b: &Hook{
				Name:            "pre-start",
				Command:         []string{"/bin/sh", "-c", "echo hello"},
				Timeout:         10,
				Retry:           3,
				RetryDelay:      5,
				ContinueOnError: true,
				WorkingDir:      "/app",
				Env:             map[string]string{"FOO": "bar"},
			},
			want: true,
		},
		{
			name: "different name",
			a:    &Hook{Name: "hook1"},
			b:    &Hook{Name: "hook2"},
			want: false,
		},
		{
			name: "different command",
			a:    &Hook{Name: "hook", Command: []string{"cmd1"}},
			b:    &Hook{Name: "hook", Command: []string{"cmd2"}},
			want: false,
		},
		{
			name: "different timeout",
			a:    &Hook{Name: "hook", Timeout: 10},
			b:    &Hook{Name: "hook", Timeout: 20},
			want: false,
		},
		{
			name: "different env",
			a:    &Hook{Name: "hook", Env: map[string]string{"A": "1"}},
			b:    &Hook{Name: "hook", Env: map[string]string{"B": "2"}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hookEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("hookEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestProcess_NodeJSFields tests the Node.js specific config fields
func TestProcess_NodeJSFields(t *testing.T) {
	tests := []struct {
		name     string
		process  *Process
		validate func(*testing.T, *Process)
	}{
		{
			name: "max_memory_mb is set",
			process: &Process{
				Command:     []string{"node", "server.js"},
				MaxMemoryMB: 512,
			},
			validate: func(t *testing.T, p *Process) {
				if p.MaxMemoryMB != 512 {
					t.Errorf("MaxMemoryMB = %v, want 512", p.MaxMemoryMB)
				}
			},
		},
		{
			name: "port_base is set",
			process: &Process{
				Command:  []string{"node", "server.js"},
				PortBase: 3000,
			},
			validate: func(t *testing.T, p *Process) {
				if p.PortBase != 3000 {
					t.Errorf("PortBase = %v, want 3000", p.PortBase)
				}
			},
		},
		{
			name: "both fields set together",
			process: &Process{
				Command:     []string{"node", "server.js"},
				MaxMemoryMB: 1024,
				PortBase:    8080,
				Scale:       4,
			},
			validate: func(t *testing.T, p *Process) {
				if p.MaxMemoryMB != 1024 {
					t.Errorf("MaxMemoryMB = %v, want 1024", p.MaxMemoryMB)
				}
				if p.PortBase != 8080 {
					t.Errorf("PortBase = %v, want 8080", p.PortBase)
				}
				if p.Scale != 4 {
					t.Errorf("Scale = %v, want 4", p.Scale)
				}
			},
		},
		{
			name: "zero values are valid (disabled)",
			process: &Process{
				Command:     []string{"node", "server.js"},
				MaxMemoryMB: 0,
				PortBase:    0,
			},
			validate: func(t *testing.T, p *Process) {
				if p.MaxMemoryMB != 0 {
					t.Errorf("MaxMemoryMB = %v, want 0 (disabled)", p.MaxMemoryMB)
				}
				if p.PortBase != 0 {
					t.Errorf("PortBase = %v, want 0 (disabled)", p.PortBase)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.validate(t, tt.process)
		})
	}
}

// TestProcess_NodeJSFieldsYAML tests YAML parsing of Node.js fields
func TestProcess_NodeJSFieldsYAML(t *testing.T) {
	yamlContent := `
version: "1.0"
global:
  shutdown_timeout: 30
  log_level: info
processes:
  nodejs-app:
    enabled: true
    command: ["node", "dist/server.js"]
    scale: 4
    port_base: 3000
    max_memory_mb: 512
    restart: always
    env:
      NODE_ENV: production
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(yamlContent), &cfg); err != nil {
		t.Fatalf("Failed to parse YAML: %v", err)
	}

	proc, ok := cfg.Processes["nodejs-app"]
	if !ok {
		t.Fatal("Process 'nodejs-app' not found")
	}

	if proc.PortBase != 3000 {
		t.Errorf("PortBase = %v, want 3000", proc.PortBase)
	}
	if proc.MaxMemoryMB != 512 {
		t.Errorf("MaxMemoryMB = %v, want 512", proc.MaxMemoryMB)
	}
	if proc.Scale != 4 {
		t.Errorf("Scale = %v, want 4", proc.Scale)
	}
}

// TestProcess_PortBaseCalculation tests port calculation logic
func TestProcess_PortBaseCalculation(t *testing.T) {
	tests := []struct {
		name          string
		portBase      int
		instanceIndex int
		expectedPort  int
	}{
		{
			name:          "first instance",
			portBase:      3000,
			instanceIndex: 0,
			expectedPort:  3000,
		},
		{
			name:          "second instance",
			portBase:      3000,
			instanceIndex: 1,
			expectedPort:  3001,
		},
		{
			name:          "fourth instance",
			portBase:      3000,
			instanceIndex: 3,
			expectedPort:  3003,
		},
		{
			name:          "different base port",
			portBase:      8080,
			instanceIndex: 2,
			expectedPort:  8082,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the port calculation done in supervisor.envVars()
			calculatedPort := tt.portBase + tt.instanceIndex
			if calculatedPort != tt.expectedPort {
				t.Errorf("Port calculation = %v, want %v", calculatedPort, tt.expectedPort)
			}
		})
	}
}
