package credentials

import (
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestApplyToCmd_NoUserOrGroupLeavesCommandAlone(t *testing.T) {
	cmd := exec.Command("true")
	if err := ApplyToCmd(cmd, "", ""); err != nil {
		t.Fatalf("ApplyToCmd: %v", err)
	}
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Credential != nil {
		t.Errorf("credential set with no user or group configured: %+v", cmd.SysProcAttr.Credential)
	}
}

func TestApplyToCmd_SetsResolvedCredential(t *testing.T) {
	// Numeric ids that are not ours, so a switch is genuinely needed. Numeric
	// user AND group resolve without touching /etc/passwd.
	uid, gid := uint32(os.Geteuid()+4242), uint32(os.Getegid()+4242)

	cmd := exec.Command("true")
	if err := ApplyToCmd(cmd, strconv.Itoa(int(uid)), strconv.Itoa(int(gid))); err != nil {
		t.Fatalf("ApplyToCmd: %v", err)
	}
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.Credential == nil {
		t.Fatal("no credential set on the command")
	}
	if got := cmd.SysProcAttr.Credential; got.Uid != uid || got.Gid != gid {
		t.Errorf("credential = %d:%d, want %d:%d", got.Uid, got.Gid, uid, gid)
	}
}

// Existing SysProcAttr settings (a process group, say) must survive.
func TestApplyToCmd_KeepsExistingSysProcAttr(t *testing.T) {
	cmd := exec.Command("true")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := ApplyToCmd(cmd, strconv.Itoa(os.Geteuid()+4242), strconv.Itoa(os.Getegid()+4242)); err != nil {
		t.Fatalf("ApplyToCmd: %v", err)
	}
	if !cmd.SysProcAttr.Setpgid {
		t.Error("ApplyToCmd replaced the command's SysProcAttr instead of adding to it")
	}
}

func TestApplyToCmd_UnresolvableUserIsAnError(t *testing.T) {
	cmd := exec.Command("true")
	err := ApplyToCmd(cmd, "definitely-not-a-real-user-9999", "")
	if err == nil {
		t.Fatal("ApplyToCmd accepted a user that does not exist; the command would run as cbox-init's own uid")
	}
	if !strings.Contains(err.Error(), "definitely-not-a-real-user-9999") {
		t.Errorf("error %q does not name the user", err)
	}
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Credential != nil {
		t.Error("a credential was set despite the resolution error")
	}
}

// Asking to run as the user cbox-init already is must work without privileges.
// Setting a Credential makes the child call setgroups, which fails with EPERM
// for anyone but root — so `user: <self>` failed on a non-root cbox-init even
// though no switch was needed.
func TestApplySysProcAttr_CurrentIdentityIsANoOp(t *testing.T) {
	creds := &Credentials{Uid: uint32(os.Geteuid()), Gid: uint32(os.Getegid())}
	attr := &syscall.SysProcAttr{}
	creds.ApplySysProcAttr(attr)
	if attr.Credential != nil {
		t.Errorf("credential set for the identity cbox-init already runs as: %+v", attr.Credential)
	}
}

func TestApplyToCmd_CurrentUserRuns(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Skipf("cannot determine current user: %v", err)
	}

	cmd := exec.Command("id", "-u")
	if err := ApplyToCmd(cmd, me.Username, ""); err != nil {
		t.Fatalf("ApplyToCmd: %v", err)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("running as the current user failed: %v", err)
	}
	if strings.TrimSpace(string(out)) != me.Uid {
		t.Errorf("ran as uid %s, want %s", strings.TrimSpace(string(out)), me.Uid)
	}
}

// The real switch needs root. Where the tests run as root (containers, the
// integration images), prove the child actually runs as the requested user.
func TestApplyToCmd_SwitchesUserAsRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("switching user needs root")
	}
	nobody, err := user.Lookup("nobody")
	if err != nil {
		t.Skipf("no nobody user: %v", err)
	}

	cmd := exec.Command("id", "-u")
	if err := ApplyToCmd(cmd, "nobody", ""); err != nil {
		t.Fatalf("ApplyToCmd: %v", err)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run as nobody: %v", err)
	}
	if strings.TrimSpace(string(out)) != nobody.Uid {
		t.Errorf("ran as uid %s, want nobody (%s)", strings.TrimSpace(string(out)), nobody.Uid)
	}
}
