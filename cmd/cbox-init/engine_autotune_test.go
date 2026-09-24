package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CBOX_ENGINE=postgres used to make cbox-init exit 1 before any process
// started ("unknown engine"), so the PostgreSQL image had to leave it unset and
// explain why. It is now recognised, left untuned, and says so.
func TestRunEngineAutoTuning_PostgresContinuesWithANotice(t *testing.T) {
	fragment := filepath.Join(t.TempDir(), "conf.d", "zz-cbox-autotune.conf")
	t.Setenv("CBOX_ENGINE_CONFIG_PATH", fragment)

	var out bytes.Buffer
	if err := runEngineAutoTuning("postgres", "", &out); err != nil {
		t.Fatalf("runEngineAutoTuning(postgres) = %v; it must not stop the container", err)
	}

	if _, err := os.Stat(fragment); !os.IsNotExist(err) {
		t.Errorf("a config fragment was written for an engine that has no profile (stat: %v)", err)
	}
	if _, err := os.Stat(filepath.Dir(fragment)); !os.IsNotExist(err) {
		t.Errorf("the fragment's directory was created for an engine that has no profile (stat: %v)", err)
	}

	msg := out.String()
	for _, want := range []string{"CBOX_ENGINE=postgres", "no autotune profile", "unchanged"} {
		if !strings.Contains(msg, want) {
			t.Errorf("notice %q does not say %q", msg, want)
		}
	}
}

func TestRunEngineAutoTuning_UnknownEngineStillFails(t *testing.T) {
	t.Setenv("CBOX_ENGINE_CONFIG_PATH", filepath.Join(t.TempDir(), "x.conf"))

	var out bytes.Buffer
	if err := runEngineAutoTuning("percnoa", "", &out); err == nil {
		t.Fatal("a misspelled engine was accepted; the operator asked for tuning and would silently not get it")
	}
}

func TestRunEngineAutoTuning_PostgresStillValidatesWakeMode(t *testing.T) {
	var out bytes.Buffer
	if err := runEngineAutoTuning("postgres", "sometimes", &out); err == nil {
		t.Fatal("an invalid CBOX_WAKE_MODE was accepted")
	}
}
