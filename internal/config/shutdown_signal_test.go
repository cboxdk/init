package config

import "testing"

func TestIsValidSignalName(t *testing.T) {
	valid := []string{"SIGTERM", "TERM", "term", "sigkill", "SIGUSR2", "usr1", "SIGHUP"}
	for _, name := range valid {
		if !IsValidSignalName(name) {
			t.Errorf("IsValidSignalName(%q) = false, want true", name)
		}
	}
	invalid := []string{"", "SIGBOGUS", "kill9", "SIGGG", "terminate"}
	for _, name := range invalid {
		if IsValidSignalName(name) {
			t.Errorf("IsValidSignalName(%q) = true, want false", name)
		}
	}
}

func TestValidateProcessShutdown(t *testing.T) {
	base := func(sd *ShutdownConfig) *Config {
		cfg := &Config{
			Processes: map[string]*Process{
				"web": {Enabled: true, Command: []string{"nginx"}, Shutdown: sd},
			},
		}
		cfg.SetDefaults()
		return cfg
	}

	t.Run("valid signal and kill_signal", func(t *testing.T) {
		cfg := base(&ShutdownConfig{Signal: "SIGQUIT", KillSignal: "SIGKILL", Timeout: 30})
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() error = %v, want nil", err)
		}
	})

	t.Run("invalid signal rejected", func(t *testing.T) {
		cfg := base(&ShutdownConfig{Signal: "SIGWHAT"})
		if err := cfg.Validate(); err == nil {
			t.Error("Validate() = nil, want error for invalid shutdown.signal")
		}
	})

	t.Run("invalid kill_signal rejected", func(t *testing.T) {
		cfg := base(&ShutdownConfig{KillSignal: "BOOM"})
		if err := cfg.Validate(); err == nil {
			t.Error("Validate() = nil, want error for invalid shutdown.kill_signal")
		}
	})

	t.Run("defaults are valid", func(t *testing.T) {
		// SetDefaults fills Signal=SIGTERM, KillSignal=SIGKILL — must pass.
		cfg := base(&ShutdownConfig{})
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() with defaulted shutdown error = %v, want nil", err)
		}
	})
}
