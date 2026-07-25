// Package snapshot lets cbox-init cooperate with the node's cortex-agent to
// checkpoint and restore its children — the warm tier of scale-to-zero.
//
// The division of labour is the point. cbox-init stays alive as PID 1 and only
// its children are checkpointed, so containerd and the kubelet see a container
// that is genuinely running and nobody has to be deceived. All privileged work
// (CRIU itself) happens in cortex-agent on the node; the container needs no
// capabilities at all.
//
// Three facts from live testing shape this package, and each was a failure
// before it was a design:
//
//   - CRIU can only hand a child its original stdout back if someone outside
//     the dumped tree still holds the pipe's *write* end. Recreating it is not
//     an option: CRIU silently makes a fresh pipe, the restored process writes
//     into it, finds no reader and dies of SIGPIPE. Hence Stream.
//   - The supervisor must reap the whole descendant tree after a dump, not just
//     the process it started. An unreaped grandchild keeps its PID and the
//     restore fails with "Can't fork: File exists".
//   - Idleness has to be decided from connections and in-flight requests. A
//     proxy that leaves the upstream open after the client has gone counts a
//     request as in flight forever and the workload simply never sleeps.
package snapshot

import (
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
)

// Stream is a pipe owned by cbox-init rather than by os/exec.
//
// This exists because of how os/exec behaves: given an io.Writer it allocates
// its own pipe and copies through a goroutine, and the write end is not
// reachable from here. Given an *os.File it dups the descriptor straight onto
// the child's fd 1 and runs no goroutine at all. Only the second form can
// survive a checkpoint, because only then do we still hold the write end and
// have something to hand back on restore.
//
// The write end is deliberately never closed while the process is supervised —
// not even while the child is checkpointed and gone. That is what keeps the
// pipe, and therefore the inode CRIU recorded, alive across the whole cycle.
type Stream struct {
	r     *os.File
	w     *os.File
	inode uint64

	mu     sync.Mutex
	closed bool
	done   chan struct{}
}

// NewStream creates a pipe and starts copying everything written to it into
// dst. dst is the ordinary log writer; from its point of view nothing about
// checkpointing is visible.
func NewStream(dst io.Writer) (*Stream, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("creating stream pipe: %w", err)
	}

	ino, err := inode(w)
	if err != nil {
		_ = r.Close()
		_ = w.Close()
		return nil, err
	}

	s := &Stream{r: r, w: w, inode: ino, done: make(chan struct{})}

	go func() {
		defer close(s.done)
		// Errors here are the pipe being closed on shutdown, which is not worth
		// reporting: the reader's job ends exactly when the stream does.
		_, _ = io.Copy(dst, r)
	}()

	return s, nil
}

// Writer is the end to give to exec.Cmd. It must be assigned as an *os.File so
// os/exec dups it rather than wrapping it in a copy goroutine.
func (s *Stream) Writer() *os.File { return s.w }

// Inode identifies the pipe to CRIU. It is the same number that
// `ls -l /proc/<pid>/fd` prints for the child's stdout, and it is the key that
// `--inherit-fd fd[N]:pipe:[<inode>]` matches on at restore.
//
// The name matters: CRIU has no external type for pipes, so declaring one with
// `--external pipe[<inode>]:<name>` at dump does nothing. It is not rejected
// either — CRIU accepts the argument, ignores it, and dumps an ordinary pipe,
// which is why an inherit keyed on that name silently restores into a fresh
// pipe with no reader.
func (s *Stream) Inode() uint64 { return s.inode }

// Close tears the stream down. Only call this when the process is finished for
// good — closing it while a child is checkpointed destroys the pipe the restore
// is going to ask for.
func (s *Stream) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	// Close the write end first so the copy goroutine sees EOF and drains what
	// is still buffered, rather than losing a process's last words.
	werr := s.w.Close()
	<-s.done
	rerr := s.r.Close()

	if werr != nil {
		return werr
	}
	return rerr
}

func inode(f *os.File) (uint64, error) {
	var st syscall.Stat_t
	if err := syscall.Fstat(int(f.Fd()), &st); err != nil {
		return 0, fmt.Errorf("stat pipe: %w", err)
	}
	return uint64(st.Ino), nil //nolint:unconvert // Ino is uint32 on some platforms
}
