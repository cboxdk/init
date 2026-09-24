package process

import (
	"os/user"
	"strconv"
	"syscall"
	"testing"
)

func TestResolveCredentials_NoUserOrGroup(t *testing.T) {
	creds, err := ResolveCredentials("", "")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if creds != nil {
		t.Error("Expected nil credentials when no user/group specified")
	}
}

func TestResolveCredentials_NumericUID(t *testing.T) {
	// When specifying only UID, it tries to get primary group which may fail if user doesn't exist
	// Test with current user's UID which should always exist
	currentUser, err := user.Current()
	if err != nil {
		t.Skipf("Could not get current user: %v", err)
	}

	creds, err := ResolveCredentials(currentUser.Uid, "")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if creds == nil {
		t.Fatal("Expected credentials, got nil")
	}

	expectedUID, _ := strconv.ParseUint(currentUser.Uid, 10, 32)
	if creds.Uid != uint32(expectedUID) {
		t.Errorf("Expected UID %d, got: %d", expectedUID, creds.Uid)
	}
}

func TestResolveCredentials_NumericGID(t *testing.T) {
	creds, err := ResolveCredentials("", "1000")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if creds == nil {
		t.Fatal("Expected credentials, got nil")
	}
	if creds.Gid != 1000 {
		t.Errorf("Expected GID 1000, got: %d", creds.Gid)
	}
}

func TestResolveCredentials_BothNumeric(t *testing.T) {
	creds, err := ResolveCredentials("500", "501")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if creds == nil {
		t.Fatal("Expected credentials, got nil")
	}
	if creds.Uid != 500 {
		t.Errorf("Expected UID 500, got: %d", creds.Uid)
	}
	if creds.Gid != 501 {
		t.Errorf("Expected GID 501, got: %d", creds.Gid)
	}
}

func TestResolveCredentials_CurrentUser(t *testing.T) {
	// Get current user for testing with real user lookup
	currentUser, err := user.Current()
	if err != nil {
		t.Skipf("Could not get current user: %v", err)
	}

	creds, err := ResolveCredentials(currentUser.Username, "")
	if err != nil {
		t.Errorf("Expected no error resolving current user %q, got: %v", currentUser.Username, err)
	}
	if creds == nil {
		t.Fatal("Expected credentials, got nil")
	}

	expectedUID, _ := strconv.ParseUint(currentUser.Uid, 10, 32)
	if creds.Uid != uint32(expectedUID) {
		t.Errorf("Expected UID %d, got: %d", expectedUID, creds.Uid)
	}
}

func TestResolveCredentials_InvalidUser(t *testing.T) {
	_, err := ResolveCredentials("nonexistent_user_12345", "")
	if err == nil {
		t.Error("Expected error for nonexistent user, got nil")
	}
}

func TestResolveCredentials_InvalidGroup(t *testing.T) {
	_, err := ResolveCredentials("", "nonexistent_group_12345")
	if err == nil {
		t.Error("Expected error for nonexistent group, got nil")
	}
}

func TestApplySysProcAttr_NilCredentials(t *testing.T) {
	var creds *Credentials
	attr := &syscall.SysProcAttr{}

	creds.ApplySysProcAttr(attr)

	if attr.Credential != nil {
		t.Error("Expected nil credential in SysProcAttr for nil Credentials")
	}
}

func TestApplySysProcAttr_ValidCredentials(t *testing.T) {
	creds := &Credentials{
		Uid: 1000,
		Gid: 1001,
	}
	attr := &syscall.SysProcAttr{}

	creds.ApplySysProcAttr(attr)

	if attr.Credential == nil {
		t.Fatal("Expected credential to be set")
	}
	if attr.Credential.Uid != 1000 {
		t.Errorf("Expected UID 1000, got: %d", attr.Credential.Uid)
	}
	if attr.Credential.Gid != 1001 {
		t.Errorf("Expected GID 1001, got: %d", attr.Credential.Gid)
	}
}

func TestResolveUser_NumericUID(t *testing.T) {
	uid, err := resolveUser("65534")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if uid != 65534 {
		t.Errorf("Expected UID 65534, got: %d", uid)
	}
}

func TestResolveGroup_NumericGID(t *testing.T) {
	gid, err := resolveGroup("65534")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if gid != 65534 {
		t.Errorf("Expected GID 65534, got: %d", gid)
	}
}

func TestLookupUser_ByName(t *testing.T) {
	currentUser, err := user.Current()
	if err != nil {
		t.Skipf("Could not get current user: %v", err)
	}

	u, err := lookupUser(currentUser.Username)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if u.Username != currentUser.Username {
		t.Errorf("Expected username %q, got: %q", currentUser.Username, u.Username)
	}
}

func TestLookupUser_ByID(t *testing.T) {
	currentUser, err := user.Current()
	if err != nil {
		t.Skipf("Could not get current user: %v", err)
	}

	u, err := lookupUser(currentUser.Uid)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if u.Uid != currentUser.Uid {
		t.Errorf("Expected UID %q, got: %q", currentUser.Uid, u.Uid)
	}
}

func TestResolveGroup_ByName(t *testing.T) {
	// Get current user's primary group for testing
	currentUser, err := user.Current()
	if err != nil {
		t.Skipf("Could not get current user: %v", err)
	}

	// Lookup group by GID to get group name
	g, err := user.LookupGroupId(currentUser.Gid)
	if err != nil {
		t.Skipf("Could not lookup group: %v", err)
	}

	gid, err := resolveGroup(g.Name)
	if err != nil {
		t.Errorf("Expected no error resolving group %q, got: %v", g.Name, err)
	}

	expectedGID, _ := strconv.ParseUint(currentUser.Gid, 10, 32)
	if gid != uint32(expectedGID) {
		t.Errorf("Expected GID %d, got: %d", expectedGID, gid)
	}
}

func TestResolveGroup_Invalid(t *testing.T) {
	_, err := resolveGroup("nonexistent_group_xyz123")
	if err == nil {
		t.Error("Expected error for nonexistent group, got nil")
	}
}

func TestResolveUser_ByName(t *testing.T) {
	currentUser, err := user.Current()
	if err != nil {
		t.Skipf("Could not get current user: %v", err)
	}

	uid, err := resolveUser(currentUser.Username)
	if err != nil {
		t.Errorf("Expected no error resolving user %q, got: %v", currentUser.Username, err)
	}

	expectedUID, _ := strconv.ParseUint(currentUser.Uid, 10, 32)
	if uid != uint32(expectedUID) {
		t.Errorf("Expected UID %d, got: %d", expectedUID, uid)
	}
}

func TestResolveUser_Invalid(t *testing.T) {
	_, err := resolveUser("nonexistent_user_xyz123")
	if err == nil {
		t.Error("Expected error for nonexistent user, got nil")
	}
}

func TestResolveCredentials_UserAndGroup(t *testing.T) {
	// Get current user for testing with real lookups
	currentUser, err := user.Current()
	if err != nil {
		t.Skipf("Could not get current user: %v", err)
	}

	// Get group name
	g, err := user.LookupGroupId(currentUser.Gid)
	if err != nil {
		t.Skipf("Could not lookup group: %v", err)
	}

	// Test with both user name and group name
	creds, err := ResolveCredentials(currentUser.Username, g.Name)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if creds == nil {
		t.Fatal("Expected credentials, got nil")
	}

	expectedUID, _ := strconv.ParseUint(currentUser.Uid, 10, 32)
	expectedGID, _ := strconv.ParseUint(currentUser.Gid, 10, 32)

	if creds.Uid != uint32(expectedUID) {
		t.Errorf("Expected UID %d, got: %d", expectedUID, creds.Uid)
	}
	if creds.Gid != uint32(expectedGID) {
		t.Errorf("Expected GID %d, got: %d", expectedGID, creds.Gid)
	}
}

func TestResolveCredentials_UserWithDifferentGroup(t *testing.T) {
	// Get current user
	currentUser, err := user.Current()
	if err != nil {
		t.Skipf("Could not get current user: %v", err)
	}

	// Test with user and numeric group (different from primary)
	creds, err := ResolveCredentials(currentUser.Username, "1234")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if creds == nil {
		t.Fatal("Expected credentials, got nil")
	}

	expectedUID, _ := strconv.ParseUint(currentUser.Uid, 10, 32)
	if creds.Uid != uint32(expectedUID) {
		t.Errorf("Expected UID %d, got: %d", expectedUID, creds.Uid)
	}
	// Group should be overridden to 1234
	if creds.Gid != 1234 {
		t.Errorf("Expected GID 1234, got: %d", creds.Gid)
	}
}

func TestResolveCredentials_OnlyGroup(t *testing.T) {
	// Get current user's group for testing
	currentUser, err := user.Current()
	if err != nil {
		t.Skipf("Could not get current user: %v", err)
	}

	g, err := user.LookupGroupId(currentUser.Gid)
	if err != nil {
		t.Skipf("Could not lookup group: %v", err)
	}

	// Test with only group name
	creds, err := ResolveCredentials("", g.Name)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if creds == nil {
		t.Fatal("Expected credentials, got nil")
	}

	expectedGID, _ := strconv.ParseUint(currentUser.Gid, 10, 32)
	if creds.Gid != uint32(expectedGID) {
		t.Errorf("Expected GID %d, got: %d", expectedGID, creds.Gid)
	}
	// UID should be 0 (not set)
	if creds.Uid != 0 {
		t.Errorf("Expected UID 0, got: %d", creds.Uid)
	}
}
