package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEnvDefinedProcessIsEnabledByDefault: the `enabled: true` default was
// applied to the raw config BEFORE the environment overrides ran, so a process
// defined entirely through CBOX_INIT_PROCESS_* variables was added afterwards
// and never got it. Its plain-bool Enabled field stayed false and the process
// was silently skipped — which is exactly the failure the defaulting was written
// to prevent, still live for anyone configuring a container from the
// environment.
func TestEnvDefinedProcessIsEnabledByDefault(t *testing.T) {
	t.Setenv("CBOX_INIT_PROCESS_WORKER_COMMAND", "/bin/true")

	cfg := loadForEnvTest(t, `version: "1.0"
global:
  log_level: info
processes:
  app:
    command: ["/bin/true"]
`)

	worker, ok := cfg.Processes["worker"]
	if !ok {
		t.Fatalf("the env-defined process was not created; processes: %v", processNames(cfg))
	}
	if !worker.Enabled {
		t.Error("an env-defined process defaulted to enabled=false and would be " +
			"silently skipped, leaving the container with no workload")
	}

	// The file-defined process is unaffected.
	if !cfg.Processes["app"].Enabled {
		t.Error("the file-defined process lost its enabled default")
	}
}

// TestExplicitDisableSurvivesTheDefault: defaulting only fills in ABSENT keys.
func TestExplicitDisableSurvivesTheDefault(t *testing.T) {
	cfg := loadForEnvTest(t, `version: "1.0"
global:
  log_level: info
processes:
  app:
    command: ["/bin/true"]
  off:
    enabled: false
    command: ["/bin/true"]
`)

	if cfg.Processes["off"].Enabled {
		t.Error("an explicit enabled: false was overwritten by the default")
	}
}

func loadForEnvTest(t *testing.T, body string) *Config {
	t.Helper()

	path := filepath.Join(t.TempDir(), "cbox-init.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWithEnvExpansion(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	return cfg
}

func processNames(c *Config) []string {
	names := make([]string, 0, len(c.Processes))
	for n := range c.Processes {
		names = append(names, n)
	}
	return names
}
