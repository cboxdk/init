package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTracingSampleRateZeroSamplesNothing: SetDefaults treated 0 as "unset" and
// replaced it with 1.0, so tracing_sample_rate: 0.0 — the documented way to
// sample nothing — produced 100% sampling instead. The docs even advise
// "ensure not 0.0", which only makes sense if 0.0 means what it says.
func TestTracingSampleRateZeroSamplesNothing(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want float64
	}{
		{"explicit zero", "  tracing_sample_rate: 0.0\n", 0.0},
		{"explicit fraction", "  tracing_sample_rate: 0.25\n", 0.25},
		{"absent defaults to full", "", 1.0},
		{"explicit one", "  tracing_sample_rate: 1.0\n", 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := loadTracingConfig(t, "  tracing_enabled: true\n  tracing_exporter: stdout\n"+tt.yaml)

			if got := cfg.Global.TracingSampleRateValue(); got != tt.want {
				t.Errorf("sample rate = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestUnsupportedTracingExporterIsRejected: otlp-http, jaeger and zipkin were
// advertised in the field's comment and given default endpoints, but the
// provider implements only otlp-grpc and stdout. check-config passed, and then
// serve exited 1 at startup. A config the validator calls valid must boot.
func TestUnsupportedTracingExporterIsRejected(t *testing.T) {
	for _, exporter := range []string{"otlp-http", "jaeger", "zipkin", "otlp"} {
		t.Run(exporter, func(t *testing.T) {
			path := writeTracingConfig(t, "  tracing_enabled: true\n  tracing_exporter: "+exporter+"\n")

			if _, err := LoadWithEnvExpansion(path); err == nil {
				t.Errorf("tracing_exporter %q validated, but the provider cannot build it "+
					"— serve would exit 1 at startup", exporter)
			}
		})
	}

	for _, exporter := range []string{"otlp-grpc", "stdout"} {
		t.Run(exporter, func(t *testing.T) {
			path := writeTracingConfig(t, "  tracing_enabled: true\n  tracing_exporter: "+exporter+"\n")

			if _, err := LoadWithEnvExpansion(path); err != nil {
				t.Errorf("tracing_exporter %q rejected: %v", exporter, err)
			}
		})
	}
}

func writeTracingConfig(t *testing.T, globalExtra string) string {
	t.Helper()

	body := "version: \"1.0\"\nglobal:\n  log_level: info\n" + globalExtra +
		"processes:\n  app:\n    command: [\"/bin/true\"]\n"

	path := filepath.Join(t.TempDir(), "cbox-init.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func loadTracingConfig(t *testing.T, globalExtra string) *Config {
	t.Helper()

	cfg, err := LoadWithEnvExpansion(writeTracingConfig(t, globalExtra))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	return cfg
}
