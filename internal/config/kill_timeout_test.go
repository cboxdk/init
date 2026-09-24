package config

import (
	"strings"
	"testing"
)

// shutdown.kill_timeout is how long cbox-init waits after delivering
// kill_signal before it escalates to SIGKILL (or, when kill_signal already is
// SIGKILL, before it gives up on the process being reaped).
//
// It exists because the wait was a fixed 5s, and PostgreSQL's immediate
// shutdown (SIGQUIT) waits exactly those same 5s before SIGKILLing backends
// that have not exited. cbox-init won that race and SIGKILLed the postmaster
// out from under the child cleanup it had just asked for.

func TestKillTimeout_ParsesFromYAML(t *testing.T) {
	cfg := loadForEnvTest(t, `
version: "1.0"
processes:
  postgres:
    enabled: true
    command: ["postgres"]
    shutdown:
      signal: SIGINT
      timeout: 100
      kill_signal: SIGQUIT
      kill_timeout: 15
`)
	if got := cfg.Processes["postgres"].Shutdown.KillTimeout; got != 15 {
		t.Fatalf("shutdown.kill_timeout = %d, want 15", got)
	}
}

func TestKillTimeout_DefaultsToFiveSeconds(t *testing.T) {
	cfg := &Config{Processes: map[string]*Process{
		"web": {Enabled: true, Command: []string{"nginx"}, Shutdown: &ShutdownConfig{}},
	}}
	cfg.SetDefaults()

	if got := cfg.Processes["web"].Shutdown.KillTimeout; got != DefaultKillTimeout {
		t.Errorf("defaulted kill_timeout = %d, want %d (unchanged from the old fixed wait)", got, DefaultKillTimeout)
	}
	if DefaultKillTimeout != 5 {
		t.Errorf("DefaultKillTimeout = %d; the default must stay 5s so existing configs behave exactly as before", DefaultKillTimeout)
	}
}

func TestKillTimeout_Validate(t *testing.T) {
	build := func(kt int) *Config {
		cfg := &Config{Processes: map[string]*Process{
			"web": {Enabled: true, Command: []string{"nginx"}, Shutdown: &ShutdownConfig{KillTimeout: kt}},
		}}
		cfg.SetDefaults()
		// SetDefaults turns 0 into the default; put the value under test back.
		cfg.Processes["web"].Shutdown.KillTimeout = kt
		return cfg
	}

	for _, kt := range []int{0, 1, 5, 30, MaxKillTimeout} {
		if err := build(kt).Validate(); err != nil {
			t.Errorf("kill_timeout=%d: Validate() = %v, want nil", kt, err)
		}
	}

	for _, kt := range []int{-1, MaxKillTimeout + 1} {
		err := build(kt).Validate()
		if err == nil {
			t.Errorf("kill_timeout=%d: Validate() = nil, want an error", kt)
			continue
		}
		if !strings.Contains(err.Error(), "kill_timeout") {
			t.Errorf("kill_timeout=%d: error %q does not name the field", kt, err)
		}
	}
}

// check-config's report must reject the same values Validate does.
func TestKillTimeout_ValidateComprehensive(t *testing.T) {
	build := func(killTimeout int) *Config {
		cfg := &Config{
			Global: GlobalConfig{ShutdownTimeout: 120},
			Processes: map[string]*Process{
				"db": {Enabled: true, Command: []string{"postgres"}, Shutdown: &ShutdownConfig{
					Signal: "SIGINT", Timeout: 100, KillSignal: "SIGQUIT", KillTimeout: killTimeout,
				}},
			},
		}
		cfg.SetDefaults()
		return cfg
	}

	for _, kt := range []int{-1, MaxKillTimeout + 1} {
		result, _ := build(kt).ValidateComprehensive()
		if !hasIssue(result.Errors, "shutdown.kill_timeout") {
			t.Errorf("kill_timeout=%d: no error in the report; errors: %+v", kt, result.Errors)
		}
	}

	result, _ := build(10).ValidateComprehensive()
	if hasIssue(result.Errors, "shutdown.kill_timeout") || hasIssue(result.Warnings, "shutdown.kill_timeout") {
		t.Errorf("kill_timeout=10 reported an issue; errors: %+v warnings: %+v", result.Errors, result.Warnings)
	}
}

func TestShutdownConfigEqual_KillTimeout(t *testing.T) {
	a := &ShutdownConfig{Signal: "SIGTERM", Timeout: 30, KillSignal: "SIGKILL", KillTimeout: 5}
	b := &ShutdownConfig{Signal: "SIGTERM", Timeout: 30, KillSignal: "SIGKILL", KillTimeout: 10}
	if shutdownConfigEqual(a, b) {
		t.Error("configs differing only in kill_timeout compared equal; a reload would not apply the change")
	}
}

func hasIssue(issues []ValidationIssue, fieldSuffix string) bool {
	for _, issue := range issues {
		if strings.HasSuffix(issue.Field, fieldSuffix) {
			return true
		}
	}
	return false
}
