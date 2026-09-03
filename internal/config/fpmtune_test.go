package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// baseWithFPMTune returns a minimal valid config carrying the given autotuner
// block, defaults already applied, ready for Validate.
func baseWithFPMTune(ft *FPMTuneConfig) *Config {
	c := &Config{
		Version: "1.0",
		Global:  GlobalConfig{FPMTune: ft},
		Processes: map[string]*Process{
			"php-fpm": {Enabled: true, Command: []string{"php-fpm", "-F"}},
		},
	}
	c.SetDefaults()

	return c
}

// TestFPMTuneRoundTripsUnderStrictDecode: config load uses KnownFields(true), so
// every fpm_tune key must be declared on the struct or a real config fails to
// load. This is the guard that the yaml tags match the field names.
func TestFPMTuneRoundTripsUnderStrictDecode(t *testing.T) {
	body := `
version: "1.0"
global:
  fpm_tune:
    enabled: true
    mode: apply
    interval: 15s
    reserve_fraction: 0.2
    workload: web
    drop_in_dir: /etc/php/8.4/fpm/pool.d
    state_path: /var/lib/cbox-init/fpm-tune/state.json
    backup_dir: /var/lib/cbox-init/fpm-tune/backup
    metrics_addr: ":9110"
    recommend_path: /var/lib/cbox-init/fpm-tune/recommended.conf
processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F"]
`
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := LoadWithEnvExpansion(p)
	if err != nil {
		t.Fatalf("a valid fpm_tune block failed to load: %v", err)
	}
	ft := cfg.Global.FPMTune
	if ft == nil {
		t.Fatal("fpm_tune block was dropped on load")
	}
	if ft.Mode != "apply" || ft.Interval != 15*time.Second || ft.MetricsAddr != ":9110" {
		t.Errorf("fpm_tune round-tripped wrong: %+v", ft)
	}
}

func TestFPMTuneDefaults(t *testing.T) {
	// An enabled block with only the gate set gets the apply-mode defaults.
	c := baseWithFPMTune(&FPMTuneConfig{Enabled: true})
	ft := c.Global.FPMTune
	if ft.Mode != "apply" {
		t.Errorf("mode default = %q, want apply", ft.Mode)
	}
	if ft.Interval != 30*time.Second {
		t.Errorf("interval default = %v, want 30s", ft.Interval)
	}
	if ft.Workload != "web" {
		t.Errorf("workload default = %q, want web", ft.Workload)
	}
	// Paths are left empty on purpose, so fpm-tune's own Defaults() resolve them.
	if ft.StatePath != "" || ft.BackupDir != "" || ft.DropInDir != "" {
		t.Errorf("paths should be left empty for fpm-tune to resolve, got %+v", ft)
	}

	// No block stays no block.
	if got := baseWithFPMTune(nil).Global.FPMTune; got != nil {
		t.Errorf("a nil fpm_tune became %+v", got)
	}
}

func TestFPMTuneValidation(t *testing.T) {
	cases := []struct {
		name    string
		ft      *FPMTuneConfig
		wantErr bool
	}{
		{"valid apply", &FPMTuneConfig{Enabled: true, Mode: "apply"}, false},
		{"valid advisory", &FPMTuneConfig{Enabled: true, Mode: "advisory"}, false},
		{"bad mode", &FPMTuneConfig{Enabled: true, Mode: "act"}, true},
		{"negative interval", &FPMTuneConfig{Enabled: true, Mode: "apply", Interval: -1}, true},
		{"reserve too high", &FPMTuneConfig{Enabled: true, Mode: "apply", ReserveFraction: 1.0}, true},
		{"reserve negative", &FPMTuneConfig{Enabled: true, Mode: "apply", ReserveFraction: -0.1}, true},
		{
			"recommend inside drop-in",
			&FPMTuneConfig{Enabled: true, Mode: "advisory", DropInDir: "/etc/php/pool.d", RecommendPath: "/etc/php/pool.d/rec.conf"},
			true,
		},
		// A disabled block is never acted on, so even a nonsense one must not block a load.
		{"disabled with bad mode is a no-op", &FPMTuneConfig{Enabled: false, Mode: "act"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := baseWithFPMTune(tc.ft).Validate()
			if tc.wantErr && err == nil {
				t.Error("expected a validation error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected validation error: %v", err)
			}
		})
	}
}

// TestFPMTuneComprehensiveValidation: check-config uses ValidateComprehensive, a
// separate path from the fail-fast Validate, so the block must be caught there too
// — with a suggestion that apply mode writes and reloads.
func TestFPMTuneComprehensiveValidation(t *testing.T) {
	// Bad mode is a hard error on the comprehensive path.
	c := baseWithFPMTune(&FPMTuneConfig{Enabled: true, Mode: "act", MetricsAddr: ":9110"})
	res, err := c.ValidateComprehensive()
	if err == nil || !res.HasErrors() {
		t.Fatal("a bad fpm_tune.mode was not reported by ValidateComprehensive")
	}

	// A valid apply block passes but is flagged as writing/reloading.
	c = baseWithFPMTune(&FPMTuneConfig{Enabled: true, Mode: "apply", MetricsAddr: ":9110"})
	res, err = c.ValidateComprehensive()
	if err != nil {
		t.Fatalf("a valid apply block failed comprehensive validation: %v", err)
	}
	found := false
	for _, s := range res.Suggestions {
		if strings.Contains(s.Field, "fpm_tune") {
			found = true
			break
		}
	}
	if !found {
		t.Error("apply mode did not produce a suggestion about writing/reloading php-fpm")
	}
}
