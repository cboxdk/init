package config

import (
	"testing"
	"time"
)

const typedValuesBase = `version: "1.0"
global:
  log_level: info
processes:
  app:
    command: ["/bin/true"]
`

// TestEnvIntegerOneIsNotABoolean: env values were typed from their text, with
// strconv.ParseBool tried first, and ParseBool accepts "1" and "0". An integer
// field set to 1 therefore arrived as `true` and the config failed to decode
// ("cannot unmarshal !!bool into int") — a Postgres container with
// SCHEDULE_MAX_CONCURRENT=1 on its backup job crash-looped on start.
func TestEnvIntegerOneIsNotABoolean(t *testing.T) {
	t.Setenv("CBOX_INIT_PROCESS_BACKUP_COMMAND", `["/bin/true"]`)
	t.Setenv("CBOX_INIT_PROCESS_BACKUP_SCHEDULE", "0 2 * * *")
	t.Setenv("CBOX_INIT_PROCESS_BACKUP_SCHEDULE_MAX_CONCURRENT", "1")

	cfg := loadForEnvTest(t, typedValuesBase)

	if got := cfg.Processes["backup"].ScheduleMaxConcurrent; got != 1 {
		t.Errorf("schedule_max_concurrent = %d, want 1", got)
	}
}

func TestEnvGlobalIntegerOne(t *testing.T) {
	t.Setenv("CBOX_INIT_GLOBAL_MAX_RESTART_ATTEMPTS", "1")

	cfg := loadForEnvTest(t, typedValuesBase)

	if got := cfg.Global.MaxRestartAttempts; got != 1 {
		t.Errorf("max_restart_attempts = %d, want 1", got)
	}
}

// TestEnvBooleanStillAcceptsOneAndZero: what ParseBool accepts stays accepted
// where the field is a boolean.
func TestEnvBooleanStillAcceptsOneAndZero(t *testing.T) {
	t.Setenv("CBOX_INIT_PROCESS_ON_COMMAND", `["/bin/true"]`)
	t.Setenv("CBOX_INIT_PROCESS_ON_ENABLED", "1")
	t.Setenv("CBOX_INIT_PROCESS_APP_ENABLED", "0")

	cfg := loadForEnvTest(t, typedValuesBase)

	if !cfg.Processes["on"].Enabled {
		t.Error("ENABLED=1 did not enable the process")
	}
	if cfg.Processes["app"].Enabled {
		t.Error("ENABLED=0 did not disable the process")
	}
}

// TestEnvStringFieldKeepsItsText: a string field set to "1" became the boolean
// true and was decoded as the text "true" — USER=1 ran the process as a user
// named "true".
func TestEnvStringFieldKeepsItsText(t *testing.T) {
	t.Setenv("CBOX_INIT_PROCESS_APP_USER", "1")
	t.Setenv("CBOX_INIT_PROCESS_APP_GROUP", "0")

	cfg := loadForEnvTest(t, typedValuesBase)

	if got := cfg.Processes["app"].User; got != "1" {
		t.Errorf("user = %q, want %q", got, "1")
	}
	if got := cfg.Processes["app"].Group; got != "0" {
		t.Errorf("group = %q, want %q", got, "0")
	}
}

func TestEnvDurationTextStillDecodes(t *testing.T) {
	t.Setenv("CBOX_INIT_GLOBAL_RESTART_BACKOFF_MAX", "2m")

	cfg := loadForEnvTest(t, typedValuesBase)

	if got := cfg.Global.RestartBackoffMax; got != 2*time.Minute {
		t.Errorf("restart_backoff_max = %v, want 2m", got)
	}
}

// TestShutdownDeadlineFitsTheLongestStop: global.shutdown_timeout capped every
// process's own shutdown.timeout, so a worker allowed 1800 seconds to finish its
// job was force-killed after 30.
func TestShutdownDeadlineFitsTheLongestStop(t *testing.T) {
	cfg := loadForEnvTest(t, `version: "1.0"
global:
  log_level: info
  shutdown_timeout: 30
processes:
  web:
    command: ["/bin/true"]
  worker:
    command: ["/bin/true"]
    shutdown:
      timeout: 1800
  graceful:
    command: ["/bin/true"]
    shutdown:
      timeout: 100
      kill_signal: SIGINT
      kill_timeout: 20
  off:
    enabled: false
    command: ["/bin/true"]
    shutdown:
      timeout: 3000
`)

	if got := cfg.ShutdownDeadline(); got != 1800*time.Second {
		t.Errorf("deadline = %v, want 30m (the worker's own timeout)", got)
	}

	cfg.Processes["worker"].Shutdown.Timeout = 10
	if got := cfg.ShutdownDeadline(); got != 120*time.Second {
		t.Errorf("deadline = %v, want 120s (timeout plus kill_timeout after a non-SIGKILL kill signal)", got)
	}

	cfg.Processes["graceful"].Enabled = false
	if got := cfg.ShutdownDeadline(); got != 30*time.Second {
		t.Errorf("deadline = %v, want the global 30s when no enabled process needs longer", got)
	}

	cfg.Processes["worker"].Shutdown.Timeout = 7200
	if got := cfg.ShutdownDeadline(); got != MaxShutdownTimeout*time.Second {
		t.Errorf("deadline = %v, want the %ds maximum", got, MaxShutdownTimeout)
	}
}

func TestShutdownDeadlineGlobalCanBeRaised(t *testing.T) {
	t.Setenv("CBOX_INIT_GLOBAL_SHUTDOWN_TIMEOUT", "100")

	cfg := loadForEnvTest(t, typedValuesBase)

	if got := cfg.ShutdownDeadline(); got != 100*time.Second {
		t.Errorf("deadline = %v, want 100s from CBOX_INIT_GLOBAL_SHUTDOWN_TIMEOUT", got)
	}
}
