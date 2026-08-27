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
