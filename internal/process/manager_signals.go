package process

import "syscall"

// ForwardSignal relays an operational signal to every managed process group.
//
// As PID 1 (or a subreaper), cbox-init receives signals like SIGHUP, SIGUSR1
// and SIGUSR2 itself — the workload never sees them. Operators expect
// `docker kill -s HUP <container>` to reach the application (nginx config
// reload; php-fpm SIGUSR1 log reopen / SIGUSR2 graceful reload), so cbox-init
// forwards these to each supervised process group. Shutdown signals
// (SIGTERM/SIGINT/SIGQUIT) and SIGHUP-as-reload are handled by the daemon and
// are not forwarded through here.
func (m *Manager) ForwardSignal(sig syscall.Signal) {
	m.mu.RLock()
	sups := make([]*Supervisor, 0, len(m.processes))
	for _, sup := range m.processes {
		sups = append(sups, sup)
	}
	m.mu.RUnlock()

	m.logger.Info("Forwarding signal to managed processes",
		"signal", sig,
		"process_count", len(sups),
	)

	for _, sup := range sups {
		sup.ForwardSignal(sig)
	}
}
