package config

import "testing"

// A publicly-bound management API must be refused unless something actually
// restricts who can reach it. Merely having an ACL "enabled" is not enough: a
// deny-mode ACL permits everyone except the listed addresses, and an allow-mode
// ACL with an empty list restricts nothing useful.
func TestValidateAPIExposure(t *testing.T) {
	mk := func(auth string, acl *ACLConfig) *Config {
		enabled := true
		return &Config{Global: GlobalConfig{APIEnabled: &enabled, APIHost: "0.0.0.0", APIAuth: auth, APIACL: acl}}
	}
	// must be REFUSED
	refuse := map[string]*Config{
		"no auth, no acl":       mk("", nil),
		"deny-mode acl":         mk("", &ACLConfig{Enabled: true, Mode: "deny"}),
		"deny-mode with denies": mk("", &ACLConfig{Enabled: true, Mode: "deny", DenyList: []string{"10.0.0.1"}}),
		"allow-mode empty list": mk("", &ACLConfig{Enabled: true, Mode: "allow"}),
		"acl disabled":          mk("", &ACLConfig{Enabled: false, Mode: "allow", AllowList: []string{"10.0.0.0/8"}}),
	}
	for name, c := range refuse {
		if err := c.validateAPIExposure(); err == nil {
			t.Errorf("EXPOSED: %s was accepted", name)
		}
	}
	// must be ACCEPTED
	accept := map[string]*Config{
		"bearer token":    mk("tok", nil),
		"real allow-list": mk("", &ACLConfig{Enabled: true, Mode: "allow", AllowList: []string{"10.0.0.0/8"}}),
	}
	for name, c := range accept {
		if err := c.validateAPIExposure(); err != nil {
			t.Errorf("REJECTED valid: %s: %v", name, err)
		}
	}
}

// A health check whose numeric fields are nonsense must be rejected at load: a
// non-positive period reaches time.NewTicker in a goroutine, and that panic
// takes PID 1 — and the container — down over a config typo.
func TestValidateProcessHealthCheck_RejectsBadNumerics(t *testing.T) {
	base := func() *Process {
		return &Process{
			Enabled: true, Command: []string{"app"},
			HealthCheck: &HealthCheck{Type: "exec", Command: []string{"true"}, Period: 10, Timeout: 1},
		}
	}
	c := &Config{}
	cases := map[string]func(*Process){
		"negative period":            func(p *Process) { p.HealthCheck.Period = -1 },
		"negative timeout":           func(p *Process) { p.HealthCheck.Timeout = -1 },
		"negative initial_delay":     func(p *Process) { p.HealthCheck.InitialDelay = -5 },
		"negative failure_threshold": func(p *Process) { p.HealthCheck.FailureThreshold = -2 },
		"unknown mode":               func(p *Process) { p.HealthCheck.Mode = "sometimes" },
	}
	for name, mutate := range cases {
		p := base()
		mutate(p)
		if err := c.validateProcessHealthCheck("app", p); err == nil {
			t.Errorf("%s was accepted; it must be rejected", name)
		}
	}
	if err := c.validateProcessHealthCheck("app", base()); err != nil {
		t.Errorf("a valid health check was rejected: %v", err)
	}
}

// TestLoopbackAPIWarningsAreSuggestions: an unauthenticated API bound to
// loopback was reported as a WARNING, so `check-config --strict` — the
// documented CI gate — failed on the default configuration and on every config
// the scaffold generates. Loopback is not the same exposure as a public bind,
// and Validate() already hard-refuses the unauthenticated non-loopback case, so
// it belongs in suggestions.
func TestLoopbackAPIWarningsAreSuggestions(t *testing.T) {
	apiEnabled := true

	loopback := &Config{Version: "1.0"}
	loopback.Global.APIEnabled = &apiEnabled
	loopback.Global.APIHost = "127.0.0.1"
	loopback.Global.APIPort = 9180
	loopback.SetDefaults()

	res, _ := loopback.ValidateComprehensive()
	for _, w := range res.Warnings {
		if w.Field == "global.api_auth" || w.Field == "security.api" {
			t.Errorf("loopback API produced warning %q (%s); --strict fails on the default config",
				w.Field, w.Message)
		}
	}
	if !hasEntry(res.Suggestions, "global.api_auth") {
		t.Error("the loopback case should still be surfaced as a suggestion")
	}

	// Bound beyond loopback it stays a warning — and Validate() refuses it
	// outright, which is the actual protection.
	public := &Config{Version: "1.0"}
	public.Global.APIEnabled = &apiEnabled
	public.Global.APIHost = "0.0.0.0"
	public.Global.APIPort = 9180
	public.SetDefaults()

	if err := public.Validate(); err == nil {
		t.Error("an unauthenticated API on 0.0.0.0 must not validate")
	}
	publicRes, _ := public.ValidateComprehensive()
	if !hasEntry(publicRes.Warnings, "global.api_auth") {
		t.Error("a non-loopback unauthenticated API should still warn")
	}
}

func hasEntry(entries []ValidationIssue, field string) bool {
	for _, e := range entries {
		if e.Field == field {
			return true
		}
	}
	return false
}
