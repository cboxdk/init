package api

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/cboxdk/init/internal/config"
)

// An ACL that is enabled but invalid must fail the server closed, not silently
// proceed with no ACL — that would expose the very endpoint the operator was
// locking down.
func TestNewServer_InvalidACLFailsClosed(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	aclCfg := &config.ACLConfig{
		Enabled:   true,
		Mode:      "allow",
		AllowList: []string{"not-a-cidr"}, // makes acl.NewChecker fail
	}

	srv := NewServer(19199, "", "", aclCfg, nil, false, 0, nil, log)

	if srv.aclChecker != nil {
		t.Error("aclChecker should be nil when the ACL config is invalid")
	}
	if srv.aclInitErr == nil {
		t.Fatal("expected aclInitErr to be set for an invalid enabled ACL")
	}

	err := srv.Start(context.Background())
	if err == nil {
		t.Fatal("Start() = nil, want error (server must refuse to start with an invalid ACL)")
	}
}

// A valid ACL still builds and starts (guarding against the fail-closed change
// over-rejecting).
func TestNewServer_ValidACLHasNoInitError(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	aclCfg := &config.ACLConfig{
		Enabled:   true,
		Mode:      "allow",
		AllowList: []string{"10.0.0.0/8", "127.0.0.1/32"},
	}

	srv := NewServer(19198, "", "", aclCfg, nil, false, 0, nil, log)
	if srv.aclInitErr != nil {
		t.Errorf("aclInitErr = %v, want nil for a valid ACL", srv.aclInitErr)
	}
	if srv.aclChecker == nil {
		t.Error("aclChecker should be set for a valid enabled ACL")
	}
}
