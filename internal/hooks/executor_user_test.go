package hooks

import (
	"context"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cboxdk/init/internal/config"
)

// hook.user / hook.group run a lifecycle hook as a given user, so a hook that
// talks to a database over a peer-authenticated socket, or writes files the
// workload must own, does not have to switch user itself.

func TestExecutor_BuildCmdCarriesCredential(t *testing.T) {
	uid, gid := os.Geteuid()+4242, os.Getegid()+4242
	e := NewExecutor(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	cmd, err := e.buildCmd(context.Background(), &config.Hook{
		Name:    "migrate",
		Command: []string{"true"},
		User:    strconv.Itoa(uid),
		Group:   strconv.Itoa(gid),
	})
	if err != nil {
		t.Fatalf("buildCmd: %v", err)
	}
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.Credential == nil {
		t.Fatal("the hook command has no credential; it would run as cbox-init's own uid")
	}
	if got := cmd.SysProcAttr.Credential; int(got.Uid) != uid || int(got.Gid) != gid {
		t.Errorf("credential = %d:%d, want %d:%d", got.Uid, got.Gid, uid, gid)
	}
}

// An unresolvable user fails the hook without running it — as root or at all.
// Retries apply as for any other failure, and continue_on_error still decides
// whether the lifecycle carries on.
func TestExecutor_UnresolvableUserFailsClosed(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	e := NewExecutor(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	err := e.Execute(context.Background(), &config.Hook{
		Name:    "as-nobody-real",
		Command: []string{"touch", marker},
		User:    "definitely-not-a-real-user-9999",
		Timeout: 5,
	})
	if err == nil {
		t.Fatal("hook succeeded although its user does not exist")
	}
	if !strings.Contains(err.Error(), "definitely-not-a-real-user-9999") {
		t.Errorf("error %q does not name the user", err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Error("the hook ran even though its user could not be resolved")
	}
}

func TestExecutor_RunsAsCurrentUser(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Skipf("cannot determine current user: %v", err)
	}
	e := NewExecutor(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	err = e.Execute(context.Background(), &config.Hook{
		Name:    "as-me",
		Command: []string{"sh", "-c", `test "$(id -u)" = "` + me.Uid + `"`},
		User:    me.Username,
		Timeout: 5,
	})
	if err != nil {
		t.Errorf("hook as the current user failed: %v", err)
	}
}

func TestExecutor_RunsAsConfiguredUser(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("switching user needs root")
	}
	nobody, err := user.Lookup("nobody")
	if err != nil {
		t.Skipf("no nobody user: %v", err)
	}
	e := NewExecutor(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	err = e.Execute(context.Background(), &config.Hook{
		Name:    "as-nobody",
		Command: []string{"sh", "-c", `test "$(id -u)" = "` + nobody.Uid + `"`},
		User:    "nobody",
		Timeout: 5,
	})
	if err != nil {
		t.Errorf("hook did not run as nobody: %v", err)
	}
}
