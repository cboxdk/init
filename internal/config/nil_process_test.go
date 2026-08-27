package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEmptyProcessBodyIsAnErrorNotAPanic covers a crash reachable from an
// ordinary config edit:
//
//	processes:
//	  app:        # <- decodes to a nil *Process
//
// which is what you get by commenting out a process while debugging, or by
// typing the name before filling in the body. SetDefaults dereferenced it and
// SIGSEGV'd — at boot, and in the watcher's reload goroutine on a container that
// was already running, where nothing recovers.
func TestEmptyProcessBodyIsAnErrorNotAPanic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cbox-init.yaml")

	const cfg = `version: "1.0"
global:
  shutdown_timeout: 10
  log_level: info
processes:
  app:
`
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadWithEnvExpansion(path)
	if err == nil {
		t.Fatal("a process with an empty body loaded successfully")
	}
	if !strings.Contains(err.Error(), "app") {
		t.Errorf("error does not name the offending process: %v", err)
	}
}

// TestNilProcessSurvivesEveryValidationPass: SetDefaults, Validate and the
// report-producing pass all iterate the process map, and any one of them
// dereferencing a nil entry is the same crash.
func TestNilProcessSurvivesEveryValidationPass(t *testing.T) {
	build := func() *Config {
		c := &Config{Version: "1.0", Processes: map[string]*Process{
			"app": nil,
			"web": {Command: []string{"nginx"}},
		}}
		return c
	}

	c := build()
	c.SetDefaults() // must not panic

	if err := c.Validate(); err == nil {
		t.Error("Validate accepted a nil process")
	}

	c2 := build()
	c2.SetDefaults()
	if _, err := c2.ValidateComprehensive(); err == nil {
		t.Error("ValidateComprehensive accepted a nil process")
	}
}
