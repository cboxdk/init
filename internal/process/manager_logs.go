package process

import (
	"fmt"
	"sort"

	"github.com/cboxdk/init/internal/logger"
)

// GetLogs returns log entries for a specific process.
// If limit > 0, returns only the most recent 'limit' entries.
// Returns error if process doesn't exist.
// Supports both regular processes (supervisors) and scheduled processes.
func (m *Manager) GetLogs(processName string, limit int) ([]logger.LogEntry, error) {
	m.mu.RLock()
	sup, exists := m.processes[processName]
	m.mu.RUnlock()

	if exists {
		return sup.GetLogs(limit), nil
	}

	// Check if it's a scheduled process
	if m.scheduleExecutor != nil && m.scheduleExecutor.HasProcess(processName) {
		return m.scheduleExecutor.GetLogs(processName, limit), nil
	}

	return nil, fmt.Errorf("process not found: %s", processName)
}

// SubscribeLogs registers a real-time log subscriber.
// filter is a process name — empty string receives all processes.
// Returns a read channel and an unsubscribe function.
func (m *Manager) SubscribeLogs(filter string) (<-chan logger.LogEntry, func()) {
	return m.logBroadcaster.Subscribe(filter)
}

// GetStackLogs aggregates logs from all processes in the manager.
// Returns the most recent entries across the stack capped by limit (if > 0).
func (m *Manager) GetStackLogs(limit int) []logger.LogEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	allLogs := make([]logger.LogEntry, 0, len(m.processes)*limit)

	for _, sup := range m.processes {
		// Reuse supervisor ordering logic and enforce a per-process cap
		allLogs = append(allLogs, sup.GetLogs(limit)...)
	}

	// Sort entire stack newest-first
	sort.Slice(allLogs, func(i, j int) bool {
		return allLogs[i].Timestamp.After(allLogs[j].Timestamp)
	})

	if limit > 0 && len(allLogs) > limit {
		allLogs = allLogs[:limit]
	}

	return allLogs
}
