package process

import (
	"context"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cboxdk/init/internal/config"
)

// health_check.user / health_check.group run an exec check as a given user.
//
// Without them a check that must run as a particular uid — pg_isready as
// postgres, say — has to switch user itself (gosu, su-exec, runuser), which
// means one more binary in the image and one more place the uid is spelled.

func TestNewHealthChecker_ExecCarriesUserAndGroup(t *testing.T) {
	checker, err := NewHealthChecker(&config.HealthCheck{
		Type:    "exec",
		Command: []string{"pg_isready"},
		User:    "postgres",
		Group:   "postgres",
	})
	if err != nil {
		t.Fatalf("NewHealthChecker: %v", err)
	}
	exec, ok := checker.(*ExecHealthChecker)
	if !ok {
		t.Fatalf("checker is %T, want *ExecHealthChecker", checker)
	}
	if exec.user != "postgres" || exec.group != "postgres" {
		t.Errorf("checker runs as %q:%q, want postgres:postgres", exec.user, exec.group)
	}
}

func TestExecHealthChecker_CommandCarriesCredential(t *testing.T) {
	uid, gid := os.Geteuid()+4242, os.Getegid()+4242
	checker := &ExecHealthChecker{
		command: []string{"true"},
		user:    strconv.Itoa(uid),
		group:   strconv.Itoa(gid),
	}

	cmd, err := checker.buildCmd(context.Background())
	if err != nil {
		t.Fatalf("buildCmd: %v", err)
	}
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.Credential == nil {
		t.Fatal("the check command has no credential; it would run as cbox-init's own uid")
	}
	if got := cmd.SysProcAttr.Credential; int(got.Uid) != uid || int(got.Gid) != gid {
		t.Errorf("credential = %d:%d, want %d:%d", got.Uid, got.Gid, uid, gid)
	}
}

// A user that cannot be resolved fails the check — and the command is never
// run. Running it as root instead would be a silent privilege escalation, and
// passing it would report a database ready that was never probed.
func TestExecHealthChecker_UnresolvableUserFailsClosed(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	checker := &ExecHealthChecker{
		command: []string{"touch", marker},
		user:    "definitely-not-a-real-user-9999",
	}

	err := checker.Check(context.Background())
	if err == nil {
		t.Fatal("check passed although its user does not exist")
	}
	if !strings.Contains(err.Error(), "definitely-not-a-real-user-9999") {
		t.Errorf("error %q does not name the user", err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Error("the check command ran even though its user could not be resolved")
	}
}

func TestExecHealthChecker_RunsAsCurrentUser(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Skipf("cannot determine current user: %v", err)
	}
	checker := &ExecHealthChecker{
		command: []string{"sh", "-c", `test "$(id -u)" = "` + me.Uid + `"`},
		user:    me.Username,
	}
	if err := checker.Check(context.Background()); err != nil {
		t.Errorf("check as the current user failed: %v", err)
	}
}

func TestExecHealthChecker_RunsAsConfiguredUser(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("switching user needs root")
	}
	nobody, err := user.Lookup("nobody")
	if err != nil {
		t.Skipf("no nobody user: %v", err)
	}
	checker := &ExecHealthChecker{
		command: []string{"sh", "-c", `test "$(id -u)" = "` + nobody.Uid + `"`},
		user:    "nobody",
	}
	if err := checker.Check(context.Background()); err != nil {
		t.Errorf("check did not run as nobody: %v", err)
	}
}
