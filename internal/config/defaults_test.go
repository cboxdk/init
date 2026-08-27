package config

import (
	"os"
	"path/filepath"
	"testing"
)

// The documentation states enabled defaults to true, but the field is a plain
// bool: an omitted key decoded to false, so a process defined with a command
// and no 'enabled' was silently skipped and the container ran with no workload.
func TestLoad_EnabledDefaultsToTrue(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	// 'web' omits enabled (documented default true); 'off' sets it false.
	os.WriteFile(p, []byte("version: \"1.0\"\nprocesses:\n  web:\n    command: [\"sleep\",\"1\"]\n  off:\n    enabled: false\n    command: [\"sleep\",\"1\"]\n"), 0600)
	cfg, err := LoadWithEnvExpansion(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Processes["web"].Enabled {
		t.Error("a process that omits 'enabled' must default to enabled (documented default: true)")
	}
	if cfg.Processes["off"].Enabled {
		t.Error("an explicit 'enabled: false' must be respected")
	}
}

// logging.stdout/stderr have the same unset-vs-false ambiguity: an explicit
// false was indistinguishable from omitted and the defaulting turned the stream
// back on, so a process configured to suppress stdout still logged it.
func TestLoad_ExplicitLoggingStdoutFalseIsRespected(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	os.WriteFile(p, []byte("version: \"1.0\"\nprocesses:\n  quiet:\n    command: [\"sleep\",\"1\"]\n    logging:\n      stdout: false\n      stderr: true\n"), 0600)
	cfg, err := LoadWithEnvExpansion(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	lg := cfg.Processes["quiet"].Logging
	if lg == nil {
		t.Fatal("logging config missing")
	}
	if lg.Stdout {
		t.Error("an explicit logging.stdout: false was overwritten to true")
	}
	if !lg.Stderr {
		t.Error("logging.stderr: true was not respected")
	}
}
