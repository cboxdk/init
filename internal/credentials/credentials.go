package process

import (
	"fmt"
	"os/user"
	"strconv"
	"syscall"
)

// Credentials holds resolved user and group IDs for process execution.
// Used to run child processes under a different user/group than the parent
// process manager, typically for security isolation in container environments.
//
// Note: Switching users requires root privileges (or appropriate capabilities).
type Credentials struct {
	Uid uint32 // User ID for the process
	Gid uint32 // Group ID for the process
}

// ResolveCredentials resolves user and group names/IDs to numeric credentials.
// Returns nil if no user/group is specified.
// Supports both numeric IDs ("82") and names ("www-data").
func ResolveCredentials(userName, groupName string) (*Credentials, error) {
	if userName == "" && groupName == "" {
		return nil, nil
	}

	creds := &Credentials{}

	// Resolve user
	if userName != "" {
		uid, err := resolveUser(userName)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve user %q: %w", userName, err)
		}
		creds.Uid = uid

		// If no group specified, use user's primary group
		if groupName == "" {
			u, err := lookupUser(userName)
			if err != nil {
				return nil, fmt.Errorf("failed to lookup user %q for primary group: %w", userName, err)
			}
			gid, err := strconv.ParseUint(u.Gid, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("failed to parse primary group ID %q: %w", u.Gid, err)
			}
			creds.Gid = uint32(gid)
		}
	}

	// Resolve group (overrides primary group if specified)
	if groupName != "" {
		gid, err := resolveGroup(groupName)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve group %q: %w", groupName, err)
		}
		creds.Gid = gid
	}

	return creds, nil
}

// resolveUser resolves a username or numeric UID to a uint32 UID
func resolveUser(nameOrID string) (uint32, error) {
	// Try parsing as numeric UID first
	if uid, err := strconv.ParseUint(nameOrID, 10, 32); err == nil {
		return uint32(uid), nil
	}

	// Lookup by name
	u, err := user.Lookup(nameOrID)
	if err != nil {
		return 0, err
	}

	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("failed to parse UID %q: %w", u.Uid, err)
	}

	return uint32(uid), nil
}

// resolveGroup resolves a group name or numeric GID to a uint32 GID
func resolveGroup(nameOrID string) (uint32, error) {
	// Try parsing as numeric GID first
	if gid, err := strconv.ParseUint(nameOrID, 10, 32); err == nil {
		return uint32(gid), nil
	}

	// Lookup by name
	g, err := user.LookupGroup(nameOrID)
	if err != nil {
		return 0, err
	}

	gid, err := strconv.ParseUint(g.Gid, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("failed to parse GID %q: %w", g.Gid, err)
	}

	return uint32(gid), nil
}

// lookupUser is a helper that looks up user by name or ID
func lookupUser(nameOrID string) (*user.User, error) {
	// Try parsing as numeric UID first
	if _, err := strconv.ParseUint(nameOrID, 10, 32); err == nil {
		return user.LookupId(nameOrID)
	}
	return user.Lookup(nameOrID)
}

// ApplySysProcAttr applies credentials to syscall.SysProcAttr.
// This should be called when setting up exec.Cmd.SysProcAttr.
// The credentials will be applied when the child process starts.
// Note: Requires root privileges to switch to a different user.
func (c *Credentials) ApplySysProcAttr(attr *syscall.SysProcAttr) {
	if c == nil {
		return
	}
	attr.Credential = &syscall.Credential{
		Uid: c.Uid,
		Gid: c.Gid,
	}
}
