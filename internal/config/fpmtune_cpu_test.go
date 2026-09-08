package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFPMTuneCPUCeilingFields(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(p, []byte(`version: "1.0"
global:
  fpm_tune:
    enabled: true
    cpu_ceiling: true
    cpu_headroom: 2.5
processes:
  app:
    command: ["sleep", "1"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithEnvExpansion(p)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	ft := cfg.Global.FPMTune
	if ft == nil || !ft.CPUCeiling || ft.CPUHeadroom != 2.5 {
		t.Fatalf("cpu fields not loaded: %+v", ft)
	}
}

func TestFPMTuneCPUHeadroomEnvOverride(t *testing.T) {
	t.Setenv("CBOX_INIT_GLOBAL_FPM_TUNE_CPU_CEILING", "true")
	t.Setenv("CBOX_INIT_GLOBAL_FPM_TUNE_CPU_HEADROOM", "3.0")
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(p, []byte(`version: "1.0"
global:
  fpm_tune:
    enabled: true
processes:
  app:
    command: ["sleep", "1"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithEnvExpansion(p)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	ft := cfg.Global.FPMTune
	if ft == nil || !ft.CPUCeiling || ft.CPUHeadroom != 3.0 {
		t.Fatalf("env override not applied: %+v", ft)
	}
}

func TestFPMTuneNegativeCPUHeadroomRejected(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(p, []byte(`version: "1.0"
global:
  fpm_tune:
    enabled: true
    cpu_headroom: -1
processes:
  app:
    command: ["sleep", "1"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadWithEnvExpansion(p); err == nil {
		t.Fatal("negative cpu_headroom accepted")
	}
}
