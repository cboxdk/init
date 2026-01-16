package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name        string
		setupEnv    func()
		createFile  bool
		fileContent string
		wantErr     bool
	}{
		{
			name: "load with custom env path",
			setupEnv: func() {
				tmpDir, _ := os.MkdirTemp("", "cbox-test-*")
				configPath := filepath.Join(tmpDir, "config.yaml")
				content := `version: "1.0"
global:
  shutdown_timeout: 30
  log_level: info
  log_format: json
processes:
  test:
    enabled: true
    command: ["sleep", "1"]
`
				_ = os.WriteFile(configPath, []byte(content), 0644)
				os.Setenv("CBOX_INIT_CONFIG", configPath)
			},
			wantErr: false,
		},
		{
			name: "load with missing file uses local fallback",
			setupEnv: func() {
				os.Unsetenv("CBOX_INIT_CONFIG")
				// Create local config
				content := `version: "1.0"
global:
  shutdown_timeout: 30
  log_level: info
  log_format: json
processes:
  test:
    enabled: true
    command: ["sleep", "1"]
`
				_ = os.WriteFile("cbox-init.yaml", []byte(content), 0644)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupEnv != nil {
				tt.setupEnv()
				defer func() {
					os.Unsetenv("CBOX_INIT_CONFIG")
					os.RemoveAll("cbox-init.yaml")
				}()
			}

			cfg, err := Load()
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && cfg == nil {
				t.Error("Load() returned nil config")
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "negative shutdown timeout",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout: -5,
					LogLevel:        "info",
					LogFormat:       "json",
				},
				Processes: map[string]*Process{
					"test": {Command: []string{"sleep", "1"}},
				},
			},
			wantErr: true,
			errMsg:  "shutdown_timeout must be positive",
		},
		{
			name: "invalid log level",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout: 30,
					LogLevel:        "invalid",
					LogFormat:       "json",
				},
				Processes: map[string]*Process{
					"test": {Command: []string{"sleep", "1"}},
				},
			},
			wantErr: true,
			errMsg:  "invalid log_level",
		},
		{
			name: "invalid log format",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout: 30,
					LogLevel:        "info",
					LogFormat:       "xml",
				},
				Processes: map[string]*Process{
					"test": {Command: []string{"sleep", "1"}},
				},
			},
			wantErr: true,
			errMsg:  "invalid log_format",
		},
		{
			name: "no processes",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout: 30,
					LogLevel:        "info",
					LogFormat:       "json",
				},
				Processes: map[string]*Process{},
			},
			wantErr: true,
			errMsg:  "no processes defined",
		},
		{
			name: "process with no command",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout: 30,
					LogLevel:        "info",
					LogFormat:       "json",
				},
				Processes: map[string]*Process{
					"test": {Command: []string{}},
				},
			},
			wantErr: true,
			errMsg:  "has no command",
		},
		{
			name: "invalid process type",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout: 30,
					LogLevel:        "info",
					LogFormat:       "json",
				},
				Processes: map[string]*Process{
					"test": {
						Type:    "invalid",
						Command: []string{"sleep", "1"},
					},
				},
			},
			wantErr: true,
			errMsg:  "invalid type",
		},
		{
			name: "invalid initial state",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout: 30,
					LogLevel:        "info",
					LogFormat:       "json",
				},
				Processes: map[string]*Process{
					"test": {
						Type:         "longrun",
						InitialState: "invalid",
						Command:      []string{"sleep", "1"},
					},
				},
			},
			wantErr: true,
			errMsg:  "invalid initial_state",
		},
		{
			name: "invalid restart policy",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout: 30,
					LogLevel:        "info",
					LogFormat:       "json",
				},
				Processes: map[string]*Process{
					"test": {
						Type:         "longrun",
						InitialState: "running",
						Restart:      "invalid",
						Command:      []string{"sleep", "1"},
					},
				},
			},
			wantErr: true,
			errMsg:  "invalid restart policy",
		},
		{
			name: "invalid scale",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout: 30,
					LogLevel:        "info",
					LogFormat:       "json",
				},
				Processes: map[string]*Process{
					"test": {
						Type:         "longrun",
						InitialState: "running",
						Restart:      "always",
						Scale:        0,
						Command:      []string{"sleep", "1"},
					},
				},
			},
			wantErr: true,
			errMsg:  "invalid scale",
		},
		{
			name: "scale locked with scale > 1",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout: 30,
					LogLevel:        "info",
					LogFormat:       "json",
				},
				Processes: map[string]*Process{
					"test": {
						Type:         "longrun",
						InitialState: "running",
						Restart:      "always",
						Scale:        3,
						MaxScale:     1,
						Command:      []string{"sleep", "1"},
					},
				},
			},
			wantErr: true,
			errMsg:  "scale (3) exceeding max_scale (1)",
		},
		{
			name: "oneshot with always restart",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout: 30,
					LogLevel:        "info",
					LogFormat:       "json",
				},
				Processes: map[string]*Process{
					"test": {
						Type:         "oneshot",
						InitialState: "running",
						Restart:      "always",
						Scale:        1,
						Command:      []string{"echo", "test"},
					},
				},
			},
			wantErr: true,
			errMsg:  "cannot have restart: always",
		},
		{
			name: "oneshot with scale > 1",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout: 30,
					LogLevel:        "info",
					LogFormat:       "json",
				},
				Processes: map[string]*Process{
					"test": {
						Type:         "oneshot",
						InitialState: "running",
						Restart:      "never",
						Scale:        2,
						Command:      []string{"echo", "test"},
					},
				},
			},
			wantErr: true,
			errMsg:  "cannot have scale > 1",
		},
		{
			name: "tcp health check without address",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout: 30,
					LogLevel:        "info",
					LogFormat:       "json",
				},
				Processes: map[string]*Process{
					"test": {
						Type:         "longrun",
						InitialState: "running",
						Restart:      "always",
						Scale:        1,
						Command:      []string{"sleep", "1"},
						HealthCheck: &HealthCheck{
							Type: "tcp",
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "tcp health check but no address",
		},
		{
			name: "http health check without url",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout: 30,
					LogLevel:        "info",
					LogFormat:       "json",
				},
				Processes: map[string]*Process{
					"test": {
						Type:         "longrun",
						InitialState: "running",
						Restart:      "always",
						Scale:        1,
						Command:      []string{"sleep", "1"},
						HealthCheck: &HealthCheck{
							Type: "http",
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "http health check but no url",
		},
		{
			name: "exec health check without command",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout: 30,
					LogLevel:        "info",
					LogFormat:       "json",
				},
				Processes: map[string]*Process{
					"test": {
						Type:         "longrun",
						InitialState: "running",
						Restart:      "always",
						Scale:        1,
						Command:      []string{"sleep", "1"},
						HealthCheck: &HealthCheck{
							Type:    "exec",
							Command: []string{},
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "exec health check but no command",
		},
		{
			name: "invalid health check type",
			config: &Config{
				Global: GlobalConfig{
					ShutdownTimeout: 30,
					LogLevel:        "info",
					LogFormat:       "json",
				},
				Processes: map[string]*Process{
					"test": {
						Type:         "longrun",
						InitialState: "running",
						Restart:      "always",
						Scale:        1,
						Command:      []string{"sleep", "1"},
						HealthCheck: &HealthCheck{
							Type: "invalid",
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "invalid health check type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("Validate() error = %v, want error containing %v", err, tt.errMsg)
			}
		})
	}
}

func TestCheckCircularDependencies(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "no circular dependencies",
			config: &Config{
				Processes: map[string]*Process{
					"a": {Command: []string{"sleep", "1"}, DependsOn: []string{}},
					"b": {Command: []string{"sleep", "1"}, DependsOn: []string{"a"}},
					"c": {Command: []string{"sleep", "1"}, DependsOn: []string{"b"}},
				},
			},
			wantErr: false,
		},
		{
			name: "self circular dependency",
			config: &Config{
				Processes: map[string]*Process{
					"a": {Command: []string{"sleep", "1"}, DependsOn: []string{"a"}},
				},
			},
			wantErr: true,
			errMsg:  "circular dependency",
		},
		{
			name: "two-node circular dependency",
			config: &Config{
				Processes: map[string]*Process{
					"a": {Command: []string{"sleep", "1"}, DependsOn: []string{"b"}},
					"b": {Command: []string{"sleep", "1"}, DependsOn: []string{"a"}},
				},
			},
			wantErr: true,
			errMsg:  "circular dependency",
		},
		{
			name: "three-node circular dependency",
			config: &Config{
				Processes: map[string]*Process{
					"a": {Command: []string{"sleep", "1"}, DependsOn: []string{"b"}},
					"b": {Command: []string{"sleep", "1"}, DependsOn: []string{"c"}},
					"c": {Command: []string{"sleep", "1"}, DependsOn: []string{"a"}},
				},
			},
			wantErr: true,
			errMsg:  "circular dependency",
		},
		{
			name: "non-existent dependency (no crash)",
			config: &Config{
				Processes: map[string]*Process{
					"a": {Command: []string{"sleep", "1"}, DependsOn: []string{"non-existent"}},
				},
			},
			wantErr: false, // hasCycle returns false for non-existent processes
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.checkCircularDependencies()
			if (err != nil) != tt.wantErr {
				t.Errorf("checkCircularDependencies() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("checkCircularDependencies() error = %v, want error containing %v", err, tt.errMsg)
			}
		})
	}
}

func TestSave(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name    string
		config  *Config
		path    string
		wantErr bool
	}{
		{
			name: "save valid config",
			config: &Config{
				Version: "1.0",
				Global: GlobalConfig{
					ShutdownTimeout: 30,
					LogLevel:        "info",
					LogFormat:       "json",
				},
				Processes: map[string]*Process{
					"test": {
						Enabled: true,
						Type:    "longrun",
						Command: []string{"sleep", "1"},
						Scale:   1,
					},
				},
			},
			path:    filepath.Join(tmpDir, "test-save.yaml"),
			wantErr: false,
		},
		{
			name: "save to invalid directory",
			config: &Config{
				Version: "1.0",
			},
			path:    "/nonexistent/directory/config.yaml",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Save(tt.path, tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("Save() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify file was created and is valid YAML
				data, err := os.ReadFile(tt.path)
				if err != nil {
					t.Errorf("Failed to read saved config: %v", err)
				}
				if len(data) == 0 {
					t.Error("Saved config is empty")
				}
			}
		})
	}
}

func TestValidate_ProcessSchedule(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid schedule expression",
			config: &Config{
				Version: "1.0",
				Processes: map[string]*Process{
					"cron-job": {
						Enabled:  true,
						Type:     "oneshot",
						Command:  []string{"echo", "hello"},
						Schedule: "*/5 * * * *",
						Restart:  "never",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid schedule expression",
			config: &Config{
				Version: "1.0",
				Processes: map[string]*Process{
					"bad-cron": {
						Enabled:  true,
						Type:     "oneshot",
						Command:  []string{"echo", "hello"},
						Schedule: "invalid cron",
						Restart:  "never",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "valid schedule with UTC timezone",
			config: &Config{
				Version: "1.0",
				Processes: map[string]*Process{
					"utc-job": {
						Enabled:          true,
						Type:             "oneshot",
						Command:          []string{"echo", "hello"},
						Schedule:         "0 * * * *",
						ScheduleTimezone: "UTC",
						Restart:          "never",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid schedule with Local timezone",
			config: &Config{
				Version: "1.0",
				Processes: map[string]*Process{
					"local-job": {
						Enabled:          true,
						Type:             "oneshot",
						Command:          []string{"echo", "hello"},
						Schedule:         "0 * * * *",
						ScheduleTimezone: "Local",
						Restart:          "never",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid schedule timezone",
			config: &Config{
				Version: "1.0",
				Processes: map[string]*Process{
					"bad-tz-job": {
						Enabled:          true,
						Type:             "oneshot",
						Command:          []string{"echo", "hello"},
						Schedule:         "0 * * * *",
						ScheduleTimezone: "America/New_York", // Not UTC or Local
						Restart:          "never",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "valid schedule timeout",
			config: &Config{
				Version: "1.0",
				Processes: map[string]*Process{
					"timeout-job": {
						Enabled:         true,
						Type:            "oneshot",
						Command:         []string{"echo", "hello"},
						Schedule:        "0 * * * *",
						ScheduleTimeout: "5m",
						Restart:         "never",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid schedule timeout",
			config: &Config{
				Version: "1.0",
				Processes: map[string]*Process{
					"bad-timeout-job": {
						Enabled:         true,
						Type:            "oneshot",
						Command:         []string{"echo", "hello"},
						Schedule:        "0 * * * *",
						ScheduleTimeout: "invalid",
						Restart:         "never",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid negative max_concurrent",
			config: &Config{
				Version: "1.0",
				Processes: map[string]*Process{
					"neg-concurrent-job": {
						Enabled:               true,
						Type:                  "oneshot",
						Command:               []string{"echo", "hello"},
						Schedule:              "0 * * * *",
						ScheduleMaxConcurrent: -1,
						Restart:               "never",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "valid max_concurrent zero means unlimited",
			config: &Config{
				Version: "1.0",
				Processes: map[string]*Process{
					"unlimited-job": {
						Enabled:               true,
						Type:                  "oneshot",
						Command:               []string{"echo", "hello"},
						Schedule:              "0 * * * *",
						ScheduleMaxConcurrent: 0,
						Restart:               "never",
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.SetDefaults()
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
