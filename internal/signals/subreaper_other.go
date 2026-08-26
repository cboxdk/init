//go:build !linux

package signals

// SetSubreaper is a no-op on non-Linux platforms, which have no
// PR_SET_CHILD_SUBREAPER equivalent. cbox-init ships for Linux containers; this
// stub exists so the daemon still builds and runs for local development on
// macOS, where subreaper semantics do not apply.
func SetSubreaper() error {
	return nil
}
