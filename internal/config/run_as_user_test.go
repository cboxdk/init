package config

import (
	"strings"
	"testing"
)

// user / group on exec health checks and on hooks.

func TestRunAsUser_ParsesFromYAML(t *testing.T) {
	cfg := loadForEnvTest(t, `
version: "1.0"
hooks:
  pre-start:
    - name: migrate
      command: ["psql", "-f", "/migrations/up.sql"]
      user: postgres
      group: postgres
processes:
  postgres:
    enabled: true
    command: ["postgres"]
    health_check:
      type: exec
      command: ["pg_isready", "-q"]
      user: postgres
      group: "999"
    shutdown:
      pre_stop_hook:
        name: checkpoint
        command: ["psql", "-c", "CHECKPOINT"]
        user: "999"
`)

	hook := cfg.Hooks.PreStart[0]
	if hook.User != "postgres" || hook.Group != "postgres" {
		t.Errorf("pre-start hook runs as %q:%q, want postgres:postgres", hook.User, hook.Group)
	}

	hc := cfg.Processes["postgres"].HealthCheck
	if hc.User != "postgres" || hc.Group != "999" {
		t.Errorf("health check runs as %q:%q, want postgres:999", hc.User, hc.Group)
	}

	if u := cfg.Processes["postgres"].Shutdown.PreStopHook.User; u != "999" {
		t.Errorf("pre_stop_hook user = %q, want 999", u)
	}
}

// user/group only mean something for a check that runs a command. On a tcp or
// http check they would be accepted and silently do nothing, so they are
// rejected — the operator clearly expected something to happen.
func TestRunAsUser_OnlyForExecHealthChecks(t *testing.T) {
	for _, hc := range []*HealthCheck{
		{Type: "tcp", Address: "127.0.0.1:5432", User: "postgres"},
		{Type: "http", URL: "http://127.0.0.1/health", Group: "www-data"},
	} {
		cfg := &Config{Processes: map[string]*Process{
			"db": {Enabled: true, Command: []string{"postgres"}, HealthCheck: hc},
		}}
		cfg.SetDefaults()

		err := cfg.Validate()
		if err == nil {
			t.Errorf("%s check with user/group: Validate() = nil, want an error", hc.Type)
		} else if !strings.Contains(err.Error(), "exec") {
			t.Errorf("%s check: error %q does not say user/group need an exec check", hc.Type, err)
		}

		result, _ := cfg.ValidateComprehensive()
		if !hasIssue(result.Errors, "health_check.user") {
			t.Errorf("%s check with user/group: no error in the check-config report; errors: %+v", hc.Type, result.Errors)
		}
	}

	cfg := &Config{Processes: map[string]*Process{
		"db": {Enabled: true, Command: []string{"postgres"}, HealthCheck: &HealthCheck{
			Type: "exec", Command: []string{"pg_isready"}, User: "postgres",
		}},
	}}
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		t.Errorf("exec check with user: Validate() = %v, want nil", err)
	}
}

// A changed user must count as a changed definition, or a reload keeps running
// the check or hook as the old one.
func TestRunAsUser_Equality(t *testing.T) {
	if healthCheckEqual(
		&HealthCheck{Type: "exec", Command: []string{"x"}, User: "a"},
		&HealthCheck{Type: "exec", Command: []string{"x"}, User: "b"},
	) {
		t.Error("health checks differing only in user compared equal")
	}
	if healthCheckEqual(
		&HealthCheck{Type: "exec", Command: []string{"x"}, Group: "a"},
		&HealthCheck{Type: "exec", Command: []string{"x"}, Group: "b"},
	) {
		t.Error("health checks differing only in group compared equal")
	}
	if hookEqual(&Hook{Command: []string{"x"}, User: "a"}, &Hook{Command: []string{"x"}, User: "b"}) {
		t.Error("hooks differing only in user compared equal")
	}
	if hookEqual(&Hook{Command: []string{"x"}, Group: "a"}, &Hook{Command: []string{"x"}, Group: "b"}) {
		t.Error("hooks differing only in group compared equal")
	}
}

// The env-var routes reach the new fields too: hooks defined entirely in the
// environment, and a process's health check overridden per field.
func TestRunAsUser_EnvOverrides(t *testing.T) {
	t.Setenv("CBOX_INIT_HOOK_PRE_START_0_COMMAND", "psql,-c,select 1")
	t.Setenv("CBOX_INIT_HOOK_PRE_START_0_USER", "postgres")
	t.Setenv("CBOX_INIT_HOOK_PRE_START_0_GROUP", "999")
	t.Setenv("CBOX_INIT_PROCESS_DB_HEALTH_CHECK_USER", "postgres")
	t.Setenv("CBOX_INIT_PROCESS_DB_SHUTDOWN_KILL_TIMEOUT", "12")

	cfg := loadForEnvTest(t, `
version: "1.0"
processes:
  db:
    enabled: true
    command: ["postgres"]
    health_check:
      type: exec
      command: ["pg_isready"]
    shutdown:
      kill_signal: SIGQUIT
`)

	if len(cfg.Hooks.PreStart) != 1 {
		t.Fatalf("pre-start hooks = %d, want 1", len(cfg.Hooks.PreStart))
	}
	if h := cfg.Hooks.PreStart[0]; h.User != "postgres" || h.Group != "999" {
		t.Errorf("env-defined hook runs as %q:%q, want postgres:999", h.User, h.Group)
	}
	if u := cfg.Processes["db"].HealthCheck.User; u != "postgres" {
		t.Errorf("health_check.user from env = %q, want postgres", u)
	}
	if kt := cfg.Processes["db"].Shutdown.KillTimeout; kt != 12 {
		t.Errorf("shutdown.kill_timeout from env = %d, want 12", kt)
	}
}

// A nested field whose last segment is also a top-level process field (user,
// group, command) must go to the nested field of the process that exists — not
// to a phantom process named after the rest of the variable. Reading
// DB_HEALTH_CHECK_COMMAND as process "db-health-check", field "command" created
// a second process with the health check's command as its own.
func TestProcessEnvOverride_PrefersExistingProcess(t *testing.T) {
	t.Setenv("CBOX_INIT_PROCESS_DB_HEALTH_CHECK_COMMAND", `["pg_isready","-q"]`)
	t.Setenv("CBOX_INIT_PROCESS_DB_SHUTDOWN_PRE_STOP_HOOK_USER", "postgres")

	cfg := loadForEnvTest(t, `
version: "1.0"
processes:
  db:
    enabled: true
    command: ["postgres"]
    health_check:
      type: exec
      command: ["true"]
    shutdown:
      pre_stop_hook:
        command: ["psql", "-c", "CHECKPOINT"]
`)

	if _, phantom := cfg.Processes["db-health-check"]; phantom {
		t.Error("a phantom process db-health-check was created from a health-check override")
	}
	if _, phantom := cfg.Processes["db-shutdown-pre-stop-hook"]; phantom {
		t.Error("a phantom process db-shutdown-pre-stop-hook was created from a hook override")
	}
	if got := cfg.Processes["db"].HealthCheck.Command; len(got) != 2 || got[0] != "pg_isready" {
		t.Errorf("health_check.command = %v, want [pg_isready -q]", got)
	}
	if got := cfg.Processes["db"].Shutdown.PreStopHook.User; got != "postgres" {
		t.Errorf("pre_stop_hook.user = %q, want postgres", got)
	}
}

// An env-only process (not in the YAML) still gets the longest name, as before.
func TestProcessEnvOverride_NewProcessKeepsLongestName(t *testing.T) {
	t.Setenv("CBOX_INIT_PROCESS_QUEUE_WORKER_COMMAND", "php,artisan,queue:work")

	cfg := loadForEnvTest(t, `
version: "1.0"
processes:
  web:
    enabled: true
    command: ["nginx"]
`)
	if _, ok := cfg.Processes["queue-worker"]; !ok {
		t.Errorf("env-defined process queue-worker not created; processes: %v", processNames(cfg))
	}
}

// A process defined only in the environment, with nested fields, becomes one
// process however the environment happens to be ordered: shorter keys (its
// top-level fields) are applied first, so the process exists by the time its
// nested fields are resolved.
func TestProcessEnvOverride_EnvOnlyProcessWithNestedFields(t *testing.T) {
	t.Setenv("CBOX_INIT_PROCESS_EXPORTER_HEALTH_CHECK_USER", "postgres")
	t.Setenv("CBOX_INIT_PROCESS_EXPORTER_HEALTH_CHECK_TYPE", "exec")
	t.Setenv("CBOX_INIT_PROCESS_EXPORTER_HEALTH_CHECK_COMMAND", `["true"]`)
	t.Setenv("CBOX_INIT_PROCESS_EXPORTER_COMMAND", "postgres_exporter")
	t.Setenv("CBOX_INIT_PROCESS_EXPORTER_ENABLED", "true")

	for i := 0; i < 20; i++ { // os.Environ order is not something to rely on
		cfg := loadForEnvTest(t, `
version: "1.0"
processes:
  web:
    enabled: true
    command: ["nginx"]
`)
		if _, phantom := cfg.Processes["exporter-health-check"]; phantom {
			t.Fatalf("phantom process exporter-health-check created; processes: %v", processNames(cfg))
		}
		if hc := cfg.Processes["exporter"].HealthCheck; hc == nil || hc.User != "postgres" {
			t.Fatalf("exporter health check = %+v, want user postgres", hc)
		}
	}
}
