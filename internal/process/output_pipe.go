package process

import (
	"fmt"
	"io"
	"os"
	"time"
)

// outputPipe carries one output stream of a supervised instance into its log
// writer, through a pipe cbox-init owns instead of one os/exec makes.
//
// The difference is when the process counts as exited. Given an io.Writer,
// os/exec creates its own pipe and cmd.Wait() returns only once that pipe is at
// EOF — once EVERY process holding its write end has closed it. Anything the
// instance started inherits that end: a helper it backgrounds, or PostgreSQL's
// backends, each in a session of its own. While one of them lived, the
// supervisor never saw the instance exit: a crashed service was not restarted,
// and a stop whose SIGKILL had worked still ended in "did not exit after
// SIGKILL".
//
// Here the child gets the write end as an *os.File, which os/exec hands over
// as-is, so cmd.Wait() returns as soon as the process itself is gone. Copying
// is ours: whatever the process wrote before it exited is drained — bounded by
// drainGrace — before the exit is acted on, and a descendant that keeps
// writing afterwards is still logged, for as long as it holds the pipe.
type outputPipe struct {
	r, w *os.File
	done chan struct{}
}

// newOutputPipe creates the pipe and starts copying from it into dst. When
// every writer has closed the pipe, the copy ends, dst is flushed so a final
// unterminated line is not lost, and the read end is closed.
func newOutputPipe(dst interface {
	io.Writer
	Flush()
}) (*outputPipe, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("creating output pipe: %w", err)
	}

	p := &outputPipe{r: r, w: w, done: make(chan struct{})}
	go func() {
		defer close(p.done)
		// A read error is the pipe going away, which ends the stream either way.
		_, _ = io.Copy(dst, r)
		dst.Flush()
		_ = r.Close()
	}()

	return p, nil
}

// childEnd is the file to give to exec.Cmd as Stdout or Stderr.
func (p *outputPipe) childEnd() *os.File { return p.w }

// release closes cbox-init's own copy of the write end. Call it once the child
// has started (it holds a duplicate from then on) or failed to start: until it
// is closed, the pipe can never reach EOF.
func (p *outputPipe) release() { _ = p.w.Close() }

// drain waits up to timeout for the pipe to reach EOF and be copied out,
// reporting whether it was. A false return usually means a descendant still
// holds the pipe; copying continues regardless.
func (p *outputPipe) drain(timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-p.done:
		return true
	case <-timer.C:
		return false
	}
}
