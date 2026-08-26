package process

import "context"

// AllDeadChannel returns a channel that closes when all processes are dead.
func (m *Manager) AllDeadChannel() <-chan struct{} {
	return m.allDeadCh
}

// MonitorProcessHealth starts monitoring for all processes dying.
func (m *Manager) MonitorProcessHealth(ctx context.Context) {
	go func() {
		// CRITICAL: Panic recovery in monitoring goroutine
		defer func() {
			if r := recover(); r != nil {
				m.logger.Error("PANIC in MonitorProcessHealth recovered",
					"panic", r,
				)
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case processName := <-m.processDeathCh:
				m.logger.Debug("Process death notification received", "process", processName)
				m.checkAllProcessesDead()
			}
		}
	}()
}

// NotifyProcessDeath is called by supervisors when a process dies and won't restart.
func (m *Manager) NotifyProcessDeath(processName string) {
	select {
	case m.processDeathCh <- processName:
	default:
		// Channel full, check immediately
		m.checkAllProcessesDead()
	}
}

// TerminalExitCode returns the exit code PID 1 should use when all managed
// processes have died. It is 0 only when the container's death is "clean" —
// every process that ran was a oneshot that completed with exit 0. It is
// non-zero (1) when the death is abnormal, so an orchestrator restarts the
// container: any process exited non-zero or was killed by a signal, or a
// long-running process (which is meant to keep running) is now dead even if it
// exited 0. Processes that never ran are ignored.
func (m *Manager) TerminalExitCode() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for name, sup := range m.processes {
		code, exited := sup.LastExitCode()
		if !exited {
			continue
		}
		if code != 0 {
			return 1 // crashed or signalled
		}
		if procCfg, ok := m.config.Processes[name]; ok && procCfg.Type != "oneshot" {
			return 1 // a longrun workload is dead
		}
	}
	return 0
}

// checkAllProcessesDead checks if all processes are dead and signals if so.
func (m *Manager) checkAllProcessesDead() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	allDead := true
	for name, sup := range m.processes {
		instances := sup.GetInstances()
		hasRunningInstance := false

		for _, inst := range instances {
			// A checkpointed instance is alive-on-purpose (its process was
			// snapshotted to disk for the warm tier), not dead. Counting it as
			// dead would close the all-dead channel and shut the container down,
			// discarding the checkpoint.
			if inst.State == string(StateRunning) || inst.State == string(StateCheckpointed) {
				hasRunningInstance = true
				break
			}
		}

		if hasRunningInstance {
			allDead = false
			m.logger.Debug("Process still has running instances",
				"process", name,
				"instances", len(instances))
			break
		}
	}

	if allDead && len(m.processes) > 0 {
		m.logger.Warn("All managed processes have died - initiating shutdown")
		// CRITICAL: Use sync.Once to prevent double-close panic from concurrent calls
		m.allDeadOnce.Do(func() {
			close(m.allDeadCh)
		})
	}
}
