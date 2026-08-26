package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DX-3: a misspelled key must be rejected at load, not silently dropped. A
// mistyped `health_check` used to disable health checking with no warning.
func TestLoadWithEnvExpansion_RejectsUnknownKeys(t *testing.T) {
	cases := map[string]string{
		"typo'd command": `
version: "1.0"
processes:
  web:
    enabled: true
    comand: ["nginx"]
`,
		"typo'd health_check": `
version: "1.0"
processes:
  web:
    enabled: true
    command: ["nginx"]
    health_chek:
      type: tcp
      address: "127.0.0.1:80"
`,
		"unknown global key": `
version: "1.0"
global:
  shutdown_timeoutt: 30
processes:
  web:
    enabled: true
    command: ["nginx"]
`,
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "c.yaml")
			if err := os.WriteFile(p, []byte(body), 0600); err != nil {
				t.Fatalf("write: %v", err)
			}
			_, err := LoadWithEnvExpansion(p)
			if err == nil {
				t.Fatal("expected an error for an unknown key, got nil")
			}
			if !strings.Contains(err.Error(), "not found") {
				t.Errorf("error should name the unknown field: %v", err)
			}
		})
	}
}

func TestLoadWithEnvExpansion_AcceptsValidConfig(t *testing.T) {
	body := `
version: "1.0"
global:
  shutdown_timeout: 30
  log_level: info
processes:
  web:
    enabled: true
    command: ["nginx"]
    restart: always
    scale: 1
    health_check:
      type: tcp
      address: "127.0.0.1:80"
`
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadWithEnvExpansion(p); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

// DX-3: the fail-fast validator must report the same error every run, not a
// different one depending on map iteration order.
func TestValidate_DeterministicErrorAcrossRuns(t *testing.T) {
	build := func() *Config {
		cfg := &Config{
			Processes: map[string]*Process{
				"aaa": {Enabled: true, Command: []string{"x"}, Restart: "bogus"}, // invalid restart
				"bbb": {Enabled: true, Command: []string{}},                      // no command
				"ccc": {Enabled: true, Command: []string{"x"}, Type: "weird"},    // invalid type
			},
		}
		cfg.SetDefaults()
		return cfg
	}

	first := build().Validate()
	if first == nil {
		t.Fatal("expected a validation error")
	}
	for i := 0; i < 20; i++ {
		if got := build().Validate(); got == nil || got.Error() != first.Error() {
			t.Fatalf("non-deterministic validation error: %q vs %q", got, first)
		}
	}
}
