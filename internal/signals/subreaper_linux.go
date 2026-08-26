//go:build linux

package signals

import "golang.org/x/sys/unix"

// SetSubreaper marks the current process as a "child subreaper" so that
// orphaned descendants (double-forked daemons whose immediate parent has
// exited) are re-parented onto this process instead of onto the real init.
//
// When cbox-init runs as PID 1 this is unnecessary — the kernel already
// re-parents every orphan in the PID namespace onto PID 1. But when cbox-init
// is wrapped (`docker run --init`, a shell entrypoint, or a Kubernetes pod
// where the pause container is PID 1 under shareProcessNamespace), it is NOT
// PID 1, and without this flag its zombie-reaping and restart guarantees
// silently stop applying to grandchildren. PR_SET_CHILD_SUBREAPER restores them.
func SetSubreaper() error {
	return unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0)
}
