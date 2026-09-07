package config

import (
	"strings"
	"testing"
)

func federateConfig(sources ...FederateSourceConfig) *Config {
	cfg := &Config{
		Version: "1.0",
		Processes: map[string]*Process{
			"dummy": {Command: []string{"sleep", "1"}},
		},
	}
	cfg.Global.MetricsFederate = sources
	cfg.SetDefaults()
	return cfg
}

func validationErrors(cfg *Config) []string {
	res, _ := cfg.ValidateComprehensive()
	var msgs []string
	for _, iss := range res.Errors {
		msgs = append(msgs, iss.Field+": "+iss.Message)
	}
	return msgs
}

func TestFederateValidationAcceptsLoopback(t *testing.T) {
	for _, url := range []string{
		"http://127.0.0.1:9114/metrics",
		"http://localhost:9114/metrics",
		"http://[::1]:9114/metrics",
	} {
		cfg := federateConfig(FederateSourceConfig{Name: "ok", URL: url})
		if errs := validationErrors(cfg); len(errs) != 0 {
			t.Fatalf("loopback URL %q rejected: %v", url, errs)
		}
	}
}

func TestFederateValidationRejectsNonLoopback(t *testing.T) {
	cfg := federateConfig(FederateSourceConfig{Name: "bad", URL: "http://10.0.0.5:9114/metrics"})
	errs := validationErrors(cfg)
	if len(errs) == 0 {
		t.Fatal("non-loopback URL must be rejected - federation is not a proxy")
	}
	if !strings.Contains(strings.Join(errs, " "), "Non-loopback") {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestFederateValidationRejectsDuplicateNamesAndMissingFields(t *testing.T) {
	cfg := federateConfig(
		FederateSourceConfig{Name: "dup", URL: "http://127.0.0.1:1/metrics"},
		FederateSourceConfig{Name: "dup", URL: "http://127.0.0.1:2/metrics"},
		FederateSourceConfig{URL: "ftp://127.0.0.1/metrics"},
	)
	errs := strings.Join(validationErrors(cfg), " | ")
	for _, want := range []string{"Duplicate", "needs a name", "Unsupported scheme"} {
		if !strings.Contains(errs, want) {
			t.Fatalf("missing %q in: %s", want, errs)
		}
	}
}
