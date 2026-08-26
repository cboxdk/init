package config

import "testing"

func TestIsLoopbackHost(t *testing.T) {
	loopback := []string{"", "127.0.0.1", "::1", "localhost", "127.0.0.5", " 127.0.0.1 "}
	for _, h := range loopback {
		if !isLoopbackHost(h) {
			t.Errorf("isLoopbackHost(%q) = false, want true", h)
		}
	}
	public := []string{"0.0.0.0", "10.0.0.5", "192.168.1.10", "::"}
	for _, h := range public {
		if isLoopbackHost(h) {
			t.Errorf("isLoopbackHost(%q) = true, want false", h)
		}
	}
}

func TestSetDefaults_APIHostLoopback(t *testing.T) {
	cfg := &Config{Processes: map[string]*Process{"web": {Command: []string{"nginx"}}}}
	cfg.SetDefaults()
	if cfg.Global.APIHost != "127.0.0.1" {
		t.Errorf("default APIHost = %q, want 127.0.0.1", cfg.Global.APIHost)
	}
}

func TestValidateAPIExposure(t *testing.T) {
	newCfg := func(mutate func(g *GlobalConfig)) *Config {
		cfg := &Config{
			Processes: map[string]*Process{"web": {Command: []string{"nginx"}}},
		}
		cfg.SetDefaults()
		mutate(&cfg.Global)
		return cfg
	}

	t.Run("API disabled: no error even on 0.0.0.0", func(t *testing.T) {
		cfg := newCfg(func(g *GlobalConfig) {
			g.SetAPIEnabled(false)
			g.APIHost = "0.0.0.0"
		})
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() = %v, want nil (API disabled)", err)
		}
	})

	t.Run("loopback default with no auth: allowed", func(t *testing.T) {
		cfg := newCfg(func(g *GlobalConfig) { g.SetAPIEnabled(true) })
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() = %v, want nil (loopback + no auth is fine)", err)
		}
	})

	t.Run("non-loopback with no auth and no ACL: rejected", func(t *testing.T) {
		cfg := newCfg(func(g *GlobalConfig) {
			g.SetAPIEnabled(true)
			g.APIHost = "0.0.0.0"
		})
		if err := cfg.Validate(); err == nil {
			t.Error("Validate() = nil, want error for unauthenticated non-loopback API")
		}
	})

	t.Run("non-loopback with auth: allowed", func(t *testing.T) {
		cfg := newCfg(func(g *GlobalConfig) {
			g.SetAPIEnabled(true)
			g.APIHost = "0.0.0.0"
			g.APIAuth = "secret-token"
		})
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() = %v, want nil (auth present)", err)
		}
	})

	t.Run("non-loopback with ACL: allowed", func(t *testing.T) {
		cfg := newCfg(func(g *GlobalConfig) {
			g.SetAPIEnabled(true)
			g.APIHost = "0.0.0.0"
			g.APIACL = &ACLConfig{Enabled: true, Mode: "allow", AllowList: []string{"10.0.0.0/8"}}
		})
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() = %v, want nil (ACL present)", err)
		}
	})
}
