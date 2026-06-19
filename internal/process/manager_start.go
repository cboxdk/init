package process

import (
	"context"
	"fmt"
	"time"
)

// startSupervisor starts a supervisor with a bounded startup wait while keeping
// the child process lifetime independent from the timeout context.
func (m *Manager) startSupervisor(ctx context.Context, sup *Supervisor) error {
	if ctx == nil {
		ctx = context.Background()
	}

	timeout := m.processStartTimeout
	if timeout <= 0 {
		timeout = DefaultProcessStartTimeout
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Start(context.Background())
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		m.stopAfterFailedStart(sup)
		return fmt.Errorf("process start cancelled: %w", ctx.Err())
	case <-timer.C:
		m.stopAfterFailedStart(sup)
		return fmt.Errorf("process start timed out after %v", timeout)
	}
}

func (m *Manager) stopAfterFailedStart(sup *Supervisor) {
	stopCtx, cancel := context.WithTimeout(context.Background(), m.processStopTimeout)
	defer cancel()
	if err := sup.Stop(stopCtx); err != nil {
		m.logger.Warn("Failed to stop supervisor after unsuccessful start", "error", err)
	}
}
