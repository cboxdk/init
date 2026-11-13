package schedule

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A scheduled job's user is honoured. It used to be dropped on the way from the
// process config to the executor, so a job configured `user: postgres` ran as
// root — and pgBackRest, which refuses root, failed every night.

func TestProcessExecutor_Execute_UnresolvableUserDoesNotRun(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	e := NewProcessExecutor(testLogger())
	_ = e.RegisterProcess("job", ProcessConfig{
		Command: []string{"touch", marker},
		User:    "cbox-init-no-such-user",
	})

	exitCode, err := e.Execute(context.Background(), "job")

	if err == nil {
		t.Fatal("Execute() error = nil, want an error for an unresolvable user")
	}
	if exitCode != -1 {
		t.Errorf("exitCode = %d, want -1", exitCode)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Error("the job ran although its user could not be resolved")
	}
}

func TestProcessExecutor_Execute_RunsAsConfiguredUser(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "uid")

	if os.Geteuid() != 0 {
		// Not root: asking for root must make the start fail (EPERM). Before
		// the fix the user was ignored and the job ran as the test's own uid.
		e := NewProcessExecutor(testLogger())
		_ = e.RegisterProcess("job", ProcessConfig{
			Command: []string{"sh", "-c", "id -u > " + out},
			User:    "0",
			Group:   "0",
		})
		if _, err := e.Execute(context.Background(), "job"); err == nil {
			t.Fatal("Execute() error = nil, want a permission error switching to uid 0 as non-root")
		}
		if _, statErr := os.Stat(out); statErr == nil {
			t.Error("the job ran as the test's own uid instead of the configured user")
		}
		return
	}

	// Root: the job must actually run as the configured uid.
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	e := NewProcessExecutor(testLogger())
	_ = e.RegisterProcess("job", ProcessConfig{
		Command: []string{"sh", "-c", "id -u > " + out},
		User:    "65534",
		Group:   "65534",
	})
	if code, err := e.Execute(context.Background(), "job"); err != nil || code != 0 {
		t.Fatalf("Execute() = %d, %v; want 0, nil", code, err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "65534" {
		t.Errorf("job ran as uid %q, want 65534", strings.TrimSpace(string(got)))
	}
}
