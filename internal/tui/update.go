package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/logger"
	"github.com/cboxdk/init/internal/process"
)

// processListResultMsg carries process listing data fetched asynchronously
type processListResultMsg struct {
	processes []process.ProcessInfo
	err       error
}

type processConfigResultMsg struct {
	name string
	cfg  *config.Process
	err  error
}

// Update handles all events (Elm architecture)
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateComponentSizes()
		return m, nil

	case tickMsg:
		// Clear expired toast
		m.clearToastIfExpired()
		// Refresh process state every second without blocking UI
		return m, tea.Batch(
			tickCmd(),
			m.refreshProcessListCmd(),
		)

	case tickLogRefreshMsg:
		// Refresh logs if in log view
		if m.currentView == viewLogs {
			m.refreshLogs()
			return m, tickLogRefreshCmd()
		}
		return m, nil

	case actionResultMsg:
		// Show toast notification for action result
		if msg.success {
			m.showToast(msg.message, 3*time.Second)
		} else {
			m.showToast(msg.message, 5*time.Second)
		}
		// Trigger async refresh to show updated state
		return m, m.refreshProcessListCmd()

	case processListResultMsg:
		m.applyProcessListResult(msg)
		return m, nil

	case processConfigResultMsg:
		if msg.err != nil {
			m.showToast(fmt.Sprintf("✗ Failed to load process: %v", msg.err), 5*time.Second)
			return m, nil
		}
		m.startEditWizard(msg.name, msg.cfg)
		return m, nil

	case tea.QuitMsg:
		m.quitting = true
		return m, tea.Quit

	default:
		return m, nil
	}
}

// handleKeyPress processes keyboard input
func (m Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle confirmation dialog
	if m.showConfirmation {
		return m.handleConfirmationKeys(msg)
	}

	// Handle scale dialog
	if m.showScaleDialog {
		return m.handleScaleDialogKeys(msg)
	}

	// Global keys
	switch msg.String() {
	case "ctrl+c", "q":
		if m.currentView == viewProcessList {
			return m, tea.Quit
		}
		// In other views, q returns to process list
		if msg.String() == "q" && m.currentView != viewProcessList {
			m.currentView = viewProcessList
			return m, nil
		}
		return m, tea.Quit

	case "?":
		m.currentView = viewHelp
		return m, nil

	case "esc":
		if m.currentView == viewLogs {
			m.currentView = m.logReturn
		} else if m.currentView != viewProcessList {
			m.currentView = viewProcessList
		}
		return m, nil
	}

	// View-specific keys
	switch m.currentView {
	case viewProcessList:
		return m.handleProcessListKeys(msg)
	case viewProcessDetail:
		return m.handleProcessDetailKeys(msg)
	case viewLogs:
		return m.handleLogsKeys(msg)
	case viewHelp:
		return m.handleHelpKeys(msg)
	case viewWizard:
		return m.handleWizardKeys(msg)
	}

	return m, nil
}

// handleProcessListKeys handles keys in process list view
func (m Model) handleProcessListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Tab navigation keys (1-4)
	if handled, newM := m.handleTabNavigation(key); handled {
		return newM, nil
	}

	// Selection navigation (k/up, j/down, g, G)
	if handled, newM := m.handleSelectionNavigation(key); handled {
		return newM, nil
	}

	// Process action keys
	if handled, newM, cmd := m.handleProcessActionKeys(key); handled {
		return newM, cmd
	}

	// Scale operation keys (+, -, c)
	if handled, newM, cmd := m.handleScaleKeys(key); handled {
		return newM, cmd
	}

	// Schedule operation keys (p, t)
	if handled, newM, cmd := m.handleScheduleKeys(key); handled {
		return newM, cmd
	}

	// Utility keys (l, a)
	if handled, newM, cmd := m.handleUtilityKeys(key); handled {
		return newM, cmd
	}

	// System tab shortcuts (R, S)
	if handled, newM, cmd := m.handleSystemTabKeys(key); handled {
		return newM, cmd
	}

	return m, nil
}

// handleTabNavigation handles tab switching keys (1-4)
func (m Model) handleTabNavigation(key string) (bool, Model) {
	switch key {
	case "1":
		m.activeTab = tabProcesses
		return true, m
	case "2":
		m.activeTab = tabScheduled
		return true, m
	case "3":
		m.activeTab = tabOneshot
		return true, m
	case "4":
		m.activeTab = tabSystem
		return true, m
	}
	return false, m
}

// handleSelectionNavigation handles cursor movement keys
func (m Model) handleSelectionNavigation(key string) (bool, Model) {
	switch key {
	case "k", "up":
		m.moveSelection(-1)
		return true, m
	case "j", "down":
		m.moveSelection(1)
		return true, m
	case "g":
		m.setSelection(0)
		return true, m
	case "G":
		count := m.getCurrentTabCount()
		if count > 0 {
			m.setSelection(count - 1)
		}
		return true, m
	}
	return false, m
}

// handleProcessActionKeys handles process action keys (enter, r, s, x, d, e)
func (m Model) handleProcessActionKeys(key string) (bool, Model, tea.Cmd) {
	switch key {
	case "enter":
		if m.activeTab == tabSystem {
			return true, m, m.executeSystemAction()
		}
		procName := m.getSelectedProcess()
		if procName == "" {
			m.showToast("✗ No process selected", 3*time.Second)
			return true, m, nil
		}
		m.openProcessDetail(procName)
		return true, m, nil

	case "r":
		procName := m.getSelectedProcess()
		if procName != "" {
			return true, m, m.triggerAction(actionRestart, procName)
		}
		return true, m, nil

	case "s":
		info := m.getSelectedProcessInfo()
		if info == nil {
			m.showToast("✗ No process selected", 3*time.Second)
			return true, m, nil
		}
		if strings.Contains(strings.ToLower(info.rawState), "run") {
			m.showToast("Process already running", 3*time.Second)
			return true, m, nil
		}
		return true, m, m.triggerAction(actionStart, info.name)

	case "x":
		info := m.getSelectedProcessInfo()
		if info == nil {
			m.showToast("✗ No process selected", 3*time.Second)
			return true, m, nil
		}
		if strings.Contains(strings.ToLower(info.rawState), "stop") {
			m.showToast("Process already stopped", 3*time.Second)
			return true, m, nil
		}
		return true, m, m.triggerAction(actionStop, info.name)

	case "d":
		procName := m.getSelectedProcess()
		if procName == "" {
			m.showToast("✗ No process selected", 3*time.Second)
			return true, m, nil
		}
		m.confirmAction(actionDelete, procName)
		return true, m, nil

	case "e":
		procName := m.getSelectedProcess()
		if procName == "" {
			m.showToast("✗ No process selected", 3*time.Second)
			return true, m, nil
		}
		return true, m, m.fetchProcessConfigCmd(procName)
	}
	return false, m, nil
}

// handleScaleKeys handles scale operation keys (+, -, c)
func (m Model) handleScaleKeys(key string) (bool, Model, tea.Cmd) {
	switch key {
	case "+", "=":
		newM, cmd := m.handleQuickScale(1)
		return true, newM.(Model), cmd
	case "-", "_":
		newM, cmd := m.handleQuickScale(-1)
		return true, newM.(Model), cmd
	case "c":
		info := m.getSelectedProcessInfo()
		if info == nil {
			m.showToast("✗ No process selected", 3*time.Second)
			return true, m, nil
		}
		m.openScaleDialog(info.name)
		return true, m, nil
	}
	return false, m, nil
}

// handleScheduleKeys handles schedule operation keys (p, t)
func (m Model) handleScheduleKeys(key string) (bool, Model, tea.Cmd) {
	switch key {
	case "p":
		info := m.getSelectedProcessInfo()
		if info == nil {
			m.showToast("✗ No process selected", 3*time.Second)
			return true, m, nil
		}
		if !info.isScheduled {
			m.showToast("✗ Not a scheduled process", 3*time.Second)
			return true, m, nil
		}
		if info.scheduleState == "paused" {
			return true, m, m.triggerAction(actionScheduleResume, info.name)
		}
		return true, m, m.triggerAction(actionSchedulePause, info.name)

	case "t":
		info := m.getSelectedProcessInfo()
		if info == nil {
			m.showToast("✗ No process selected", 3*time.Second)
			return true, m, nil
		}
		if !info.isScheduled {
			m.showToast("✗ Not a scheduled process", 3*time.Second)
			return true, m, nil
		}
		if info.scheduleState == "executing" {
			m.showToast("✗ Already executing", 3*time.Second)
			return true, m, nil
		}
		return true, m, m.triggerAction(actionScheduleTrigger, info.name)
	}
	return false, m, nil
}

// handleUtilityKeys handles utility keys (l, a)
func (m Model) handleUtilityKeys(key string) (bool, Model, tea.Cmd) {
	switch key {
	case "l":
		procName := m.getSelectedProcess()
		if procName == "" {
			m.showToast("✗ No process selected", 3*time.Second)
			return true, m, nil
		}
		return true, m, m.openLogView(logScopeProcess, procName, "")
	case "a":
		m.startWizard()
		return true, m, nil
	}
	return false, m, nil
}

// handleSystemTabKeys handles system tab shortcuts (R, S)
func (m Model) handleSystemTabKeys(key string) (bool, Model, tea.Cmd) {
	switch key {
	case "R":
		if m.activeTab == tabSystem {
			return true, m, m.reloadConfigCmd()
		}
		return true, m, nil
	case "S":
		if m.activeTab == tabSystem {
			return true, m, m.saveConfigCmd()
		}
		return true, m, nil
	}
	return false, m, nil
}

func (m *Model) moveSelection(delta int) {
	// Tab-aware navigation
	switch m.activeTab {
	case tabScheduled:
		m.moveScheduledSelection(delta)
		return
	case tabOneshot:
		m.moveOneshotSelection(delta)
		return
	case tabSystem:
		m.moveSystemSelection(delta)
		return
	}

	// Processes tab
	if len(m.tableData) == 0 {
		m.selectedIndex = 0
		m.processTable.SetCursor(0)
		return
	}
	newIdx := m.selectedIndex + delta
	if newIdx < 0 {
		newIdx = 0
	}
	if newIdx >= len(m.tableData) {
		newIdx = len(m.tableData) - 1
	}
	m.setSelection(newIdx)
}

func (m *Model) moveScheduledSelection(delta int) {
	if len(m.scheduledData) == 0 {
		m.scheduledIndex = 0
		return
	}
	newIdx := m.scheduledIndex + delta
	if newIdx < 0 {
		newIdx = 0
	}
	if newIdx >= len(m.scheduledData) {
		newIdx = len(m.scheduledData) - 1
	}
	m.setScheduledSelection(newIdx)
}

func (m *Model) setSelection(index int) {
	// Tab-aware selection
	switch m.activeTab {
	case tabScheduled:
		m.setScheduledSelection(index)
		return
	case tabOneshot:
		m.setOneshotSelection(index)
		return
	case tabSystem:
		m.setSystemSelection(index)
		return
	}

	// Processes tab
	if len(m.tableData) == 0 {
		m.selectedIndex = 0
		m.processTable.SetCursor(0)
		return
	}
	if index < 0 {
		index = 0
	}
	if index >= len(m.tableData) {
		index = len(m.tableData) - 1
	}
	m.selectedIndex = index
	m.processTable.SetCursor(index)
	m.ensureCursorVisible()
}

func (m *Model) setScheduledSelection(index int) {
	if len(m.scheduledData) == 0 {
		m.scheduledIndex = 0
		return
	}
	if index < 0 {
		index = 0
	}
	if index >= len(m.scheduledData) {
		index = len(m.scheduledData) - 1
	}
	m.scheduledIndex = index
	m.ensureScheduledCursorVisible()
}

func (m *Model) ensureScheduledCursorVisible() {
	height := m.defaultTableHeight()
	if height <= 0 {
		height = 1
	}

	maxOffset := len(m.scheduledData) - height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.scheduledOffset > maxOffset {
		m.scheduledOffset = maxOffset
	}

	cursor := m.scheduledIndex
	if cursor < m.scheduledOffset {
		m.scheduledOffset = cursor
	} else if cursor >= m.scheduledOffset+height {
		m.scheduledOffset = cursor - height + 1
		if m.scheduledOffset > maxOffset {
			m.scheduledOffset = maxOffset
		}
	}
	if m.scheduledOffset < 0 {
		m.scheduledOffset = 0
	}
}

// Oneshot tab selection functions
func (m *Model) moveOneshotSelection(delta int) {
	if len(m.oneshotData) == 0 {
		m.oneshotIndex = 0
		return
	}
	newIdx := m.oneshotIndex + delta
	if newIdx < 0 {
		newIdx = 0
	}
	if newIdx >= len(m.oneshotData) {
		newIdx = len(m.oneshotData) - 1
	}
	m.setOneshotSelection(newIdx)
}

func (m *Model) setOneshotSelection(index int) {
	if len(m.oneshotData) == 0 {
		m.oneshotIndex = 0
		return
	}
	if index < 0 {
		index = 0
	}
	if index >= len(m.oneshotData) {
		index = len(m.oneshotData) - 1
	}
	m.oneshotIndex = index
	m.ensureOneshotCursorVisible()
}

func (m *Model) ensureOneshotCursorVisible() {
	height := m.defaultTableHeight()
	if height <= 0 {
		height = 1
	}

	maxOffset := len(m.oneshotData) - height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.oneshotOffset > maxOffset {
		m.oneshotOffset = maxOffset
	}

	cursor := m.oneshotIndex
	if cursor < m.oneshotOffset {
		m.oneshotOffset = cursor
	} else if cursor >= m.oneshotOffset+height {
		m.oneshotOffset = cursor - height + 1
		if m.oneshotOffset > maxOffset {
			m.oneshotOffset = maxOffset
		}
	}
	if m.oneshotOffset < 0 {
		m.oneshotOffset = 0
	}
}

// System tab selection functions
func (m *Model) moveSystemSelection(delta int) {
	newIdx := m.systemMenuIndex + delta
	if newIdx < 0 {
		newIdx = 0
	}
	if newIdx >= systemMenuItemCount {
		newIdx = systemMenuItemCount - 1
	}
	m.systemMenuIndex = newIdx
}

func (m *Model) setSystemSelection(index int) {
	if index < 0 {
		index = 0
	}
	if index >= systemMenuItemCount {
		index = systemMenuItemCount - 1
	}
	m.systemMenuIndex = index
}

// getCurrentTabCount returns the number of items in the current tab
func (m *Model) getCurrentTabCount() int {
	switch m.activeTab {
	case tabScheduled:
		return len(m.scheduledData)
	case tabOneshot:
		return len(m.oneshotData)
	case tabSystem:
		return systemMenuItemCount
	}
	return len(m.tableData)
}

// systemMenuItemCount is the number of items in the system menu
const systemMenuItemCount = 2 // Reload Config, Save Config

// handleProcessDetailKeys handles keys when viewing a single process
func (m Model) handleProcessDetailKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "l":
		if m.detailProc == "" {
			m.showToast("✗ No process selected", 3*time.Second)
			return m, nil
		}
		instance := m.getSelectedInstanceID()
		return m, m.openLogView(logScopeProcess, m.detailProc, instance)

	case "r":
		if m.detailProc == "" {
			m.showToast("✗ No process selected", 3*time.Second)
			return m, nil
		}
		return m, m.triggerAction(actionRestart, m.detailProc)

	case "s":
		if m.detailProc == "" {
			m.showToast("✗ No process selected", 3*time.Second)
			return m, nil
		}
		if info, ok := m.processCache[m.detailProc]; ok {
			if strings.Contains(strings.ToLower(info.State), "stop") {
				m.showToast("Process already stopped", 3*time.Second)
				return m, nil
			}
		}
		return m, m.triggerAction(actionStop, m.detailProc)
	}

	var cmd tea.Cmd
	if m.instanceTable.Columns() != nil {
		m.instanceTable, cmd = m.instanceTable.Update(msg)
		return m, cmd
	}
	return m, nil
}

// handleLogsKeys handles keys in log viewer
func (m Model) handleLogsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case " ":
		// Toggle pause
		m.logsPaused = !m.logsPaused

	case "up", "k":
		m.logViewport.ScrollUp(1)

	case "down", "j":
		m.logViewport.ScrollDown(1)

	case "g":
		m.logViewport.GotoTop()

	case "G":
		m.logViewport.GotoBottom()

	case "ctrl+u":
		m.logViewport.HalfPageUp()

	case "ctrl+d":
		m.logViewport.HalfPageDown()
	}

	return m, nil
}

// handleHelpKeys handles keys in help view
func (m Model) handleHelpKeys(_ tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Any key returns to previous view
	m.currentView = viewProcessList
	return m, nil
}

// executeSystemAction executes the currently selected system menu action
func (m *Model) executeSystemAction() tea.Cmd {
	switch m.systemMenuIndex {
	case 0: // Reload Configuration
		return m.reloadConfigCmd()
	case 1: // Save Configuration
		return m.saveConfigCmd()
	}
	return nil
}

// reloadConfigCmd returns a command to reload configuration from disk
func (m *Model) reloadConfigCmd() tea.Cmd {
	return func() tea.Msg {
		var err error

		if m.isRemote {
			err = m.client.ReloadConfig()
		} else if m.manager != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			err = m.manager.ReloadConfig(ctx)
		} else {
			return actionResultMsg{success: false, message: "✗ No manager available"}
		}

		if err != nil {
			return actionResultMsg{success: false, message: fmt.Sprintf("✗ Reload failed: %v", err)}
		}

		return actionResultMsg{success: true, message: "✓ Configuration reloaded"}
	}
}

// saveConfigCmd returns a command to save running configuration to file
func (m *Model) saveConfigCmd() tea.Cmd {
	return func() tea.Msg {
		var err error

		if m.isRemote {
			err = m.client.SaveConfig()
		} else if m.manager != nil {
			err = m.manager.SaveConfig()
		} else {
			return actionResultMsg{success: false, message: "✗ No manager available"}
		}

		if err != nil {
			return actionResultMsg{success: false, message: fmt.Sprintf("✗ Save failed: %v", err)}
		}

		return actionResultMsg{success: true, message: "✓ Configuration saved"}
	}
}

// updateComponentSizes updates component dimensions on terminal resize
func (m *Model) updateComponentSizes() {
	// Reserve space for header (3 lines) and footer (2 lines)
	availableHeight := m.height - 5

	// Recreate table with new height
	m.setupProcessTable()
	m.ensureCursorVisible()

	// Log viewport
	m.logViewport.Width = m.width - 2
	m.logViewport.Height = availableHeight

	if m.instanceTable.Columns() != nil {
		m.instanceTable.SetWidth(m.detailTableWidth())
		m.instanceTable.SetHeight(m.detailTableHeight())
	}
}

// getSelectedProcess returns the name of the currently selected process (tab-aware)
func (m *Model) getSelectedProcess() string {
	if m.activeTab == tabScheduled {
		return m.getSelectedScheduledName()
	}
	if m.selectedIndex >= 0 && m.selectedIndex < len(m.tableData) {
		return m.tableData[m.selectedIndex].name
	}
	return ""
}

func (m *Model) getSelectedProcessInfo() *processDisplayRow {
	// For Scheduled tab, we need to convert scheduled row to process row for compatibility
	if m.activeTab == tabScheduled {
		if m.scheduledIndex >= 0 && m.scheduledIndex < len(m.scheduledData) {
			sched := m.scheduledData[m.scheduledIndex]
			// Create a process display row with scheduled process info
			return &processDisplayRow{
				name:          sched.name,
				nameStyle:     sched.nameStyle,
				procType:      "scheduled",
				state:         sched.state,
				rawState:      sched.rawState,
				stateStyle:    sched.stateStyle,
				isScheduled:   true,
				schedule:      sched.schedule,
				scheduleState: sched.rawState,
				nextRun:       sched.rawNextRun,
				lastRun:       sched.rawLastRun,
			}
		}
		return nil
	}

	// Processes tab
	idx := m.selectedIndex
	if idx >= 0 && idx < len(m.tableData) {
		return &m.tableData[idx]
	}
	return nil
}

func (m *Model) getSelectedInstanceID() string {
	if m.instanceTable.Columns() == nil || len(m.instanceTable.Rows()) == 0 {
		return ""
	}
	row := m.instanceTable.SelectedRow()
	if len(row) > 0 {
		return row[0]
	}
	return ""
}

func (m Model) handleQuickScale(delta int) (tea.Model, tea.Cmd) {
	info := m.getSelectedProcessInfo()
	if info == nil {
		m.showToast("✗ No process selected", 3*time.Second)
		return m, nil
	}

	target := info.name
	return m, func() tea.Msg {
		if m.isRemote {
			if err := m.client.ScaleProcessDelta(target, delta); err != nil {
				return actionResultMsg{success: false, message: fmt.Sprintf("✗ Scale failed: %v", err)}
			}
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			if err := m.manager.AdjustScale(ctx, target, delta); err != nil {
				return actionResultMsg{success: false, message: fmt.Sprintf("✗ Scale failed: %v", err)}
			}
		}

		return actionResultMsg{success: true, message: fmt.Sprintf("✓ Scaled %s by %+d", target, delta)}
	}
}

func (m *Model) openProcessDetail(name string) {
	m.detailProc = name
	if m.instanceTable.Columns() == nil {
		m.setupInstanceTable()
	}
	m.updateInstanceTableFromCache()
	m.currentView = viewProcessDetail
}

func (m *Model) updateInstanceTableFromCache() {
	if m.detailProc == "" {
		m.updateInstanceTable(nil)
		return
	}
	if info, ok := m.processCache[m.detailProc]; ok {
		procCopy := info
		m.updateInstanceTable(&procCopy)
	} else {
		m.updateInstanceTable(nil)
	}
}

// openLogView switches to the log view with the provided scope
func (m *Model) openLogView(scope logScope, processName string, instance string) tea.Cmd {
	m.logScope = scope
	m.logsPaused = false
	m.logBuffer = []string{}
	if scope == logScopeProcess {
		m.selectedProc = processName
		m.logInstance = instance
	} else {
		m.selectedProc = ""
		m.logInstance = ""
	}
	m.logReturn = m.currentView
	m.currentView = viewLogs
	return m.startLogTailing()
}

// startLogTailing begins tailing logs for the selected process
func (m *Model) startLogTailing() tea.Cmd {
	// Fetch initial logs
	m.refreshLogs()

	// Start periodic log refresh
	return tickLogRefreshCmd()
}

// refreshLogs fetches and displays logs for the selected process
func (m *Model) refreshLogs() {
	const logLimit = 100

	logs, err := m.fetchLogs(logLimit)
	if err != nil {
		m.setLogError(err.Error())
		return
	}

	m.formatAndDisplayLogs(logs)
}

// fetchLogs retrieves logs based on current scope and mode
func (m *Model) fetchLogs(limit int) ([]logger.LogEntry, error) {
	switch m.logScope {
	case logScopeStack:
		return m.fetchStackLogs(limit)
	case logScopeProcess:
		return m.fetchProcessLogs(limit)
	default:
		return nil, nil
	}
}

// fetchStackLogs retrieves stack-level logs
func (m *Model) fetchStackLogs(limit int) ([]logger.LogEntry, error) {
	if m.isRemote {
		if m.client == nil {
			return nil, fmt.Errorf("api client not initialized")
		}
		return m.client.GetStackLogs(limit)
	}
	if m.manager == nil {
		return nil, fmt.Errorf("manager not initialized")
	}
	return m.manager.GetStackLogs(limit), nil
}

// fetchProcessLogs retrieves process-level logs with optional instance filtering
func (m *Model) fetchProcessLogs(limit int) ([]logger.LogEntry, error) {
	if m.selectedProc == "" {
		return nil, fmt.Errorf("no process selected")
	}

	var logs []logger.LogEntry
	var err error

	if m.isRemote {
		if m.client == nil {
			return nil, fmt.Errorf("api client not initialized")
		}
		logs, err = m.client.GetLogs(m.selectedProc, limit)
	} else {
		if m.manager == nil {
			return nil, fmt.Errorf("manager not initialized")
		}
		logs, err = m.manager.GetLogs(m.selectedProc, limit)
	}

	if err != nil {
		return nil, err
	}

	// Apply instance filter if set
	if m.logInstance != "" {
		logs = m.filterLogsByInstance(logs)
	}

	return logs, nil
}

// filterLogsByInstance filters logs to only include entries for a specific instance
func (m *Model) filterLogsByInstance(logs []logger.LogEntry) []logger.LogEntry {
	filtered := make([]logger.LogEntry, 0, len(logs))
	for _, entry := range logs {
		if entry.InstanceID == m.logInstance {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

// setLogError sets an error message in the log viewport
func (m *Model) setLogError(msg string) {
	m.logBuffer = []string{msg}
	m.logViewport.SetContent(msg)
}

// formatAndDisplayLogs formats log entries and updates the viewport
func (m *Model) formatAndDisplayLogs(logs []logger.LogEntry) {
	m.logBuffer = make([]string, 0, len(logs))

	// Format logs for display (oldest first)
	for i := len(logs) - 1; i >= 0; i-- {
		entry := logs[i]
		m.logBuffer = append(m.logBuffer, m.formatLogEntry(entry))
	}

	if len(m.logBuffer) == 0 {
		m.logBuffer = []string{"No logs available yet. Logs will appear as the process runs."}
	}

	m.logViewport.SetContent(strings.Join(m.logBuffer, "\n"))

	// Auto-scroll to bottom if not paused
	if !m.logsPaused {
		m.logViewport.GotoBottom()
	}
}

// formatLogEntry formats a single log entry for display
func (m *Model) formatLogEntry(entry logger.LogEntry) string {
	// Handle event entries as dividers
	if entry.Level == "event" {
		timestamp := entry.Timestamp.Format("15:04:05")
		return eventDividerStyle.Render(fmt.Sprintf("──── %s %s ────", timestamp, entry.Message))
	}

	// Format: [timestamp] [level] [stream] [instance] message
	timestamp := entry.Timestamp.Format("15:04:05.000")
	levelStr := m.formatLogLevel(entry.Level)
	instance := entry.InstanceID
	if entry.ProcessName != "" {
		instance = fmt.Sprintf("%s/%s", entry.ProcessName, entry.InstanceID)
	}

	return fmt.Sprintf("[%s] %s [%s] [%s] %s",
		timestamp,
		levelStr,
		entry.Stream,
		instance,
		entry.Message,
	)
}

// formatLogLevel adds color styling to log levels
func (m *Model) formatLogLevel(level string) string {
	switch level {
	case "error":
		return errorStyle.Render("ERROR")
	case "warn":
		return warnStyle.Render("WARN ")
	case "info":
		return successStyle.Render("INFO ")
	case "debug":
		return dimStyle.Render("DEBUG")
	default:
		return level
	}
}

// handleConfirmationKeys handles keys in confirmation dialog
func (m Model) handleConfirmationKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		// Confirm action
		return m, m.executeAction()

	case "n", "N", "esc":
		// Cancel action
		m.cancelConfirmation()
		return m, nil
	}

	return m, nil
}

// handleScaleDialogKeys handles keys in scale dialog
func (m Model) handleScaleDialogKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		// Execute scale with entered value
		return m, m.executeScale()

	case "esc":
		// Cancel
		m.closeScaleDialog()
		return m, nil

	case "backspace":
		// Remove last character
		if len(m.scaleInput) > 0 {
			m.scaleInput = m.scaleInput[:len(m.scaleInput)-1]
		}

	default:
		// Add digit
		if len(msg.String()) == 1 && msg.String()[0] >= '0' && msg.String()[0] <= '9' {
			m.scaleInput += msg.String()
		}
	}

	return m, nil
}

// executeScale executes scale action with user input
func (m *Model) executeScale() tea.Cmd {
	if m.scaleInput == "" || m.pendingTarget == "" {
		m.closeScaleDialog()
		return nil
	}

	// Parse scale value
	var desired int
	if _, err := fmt.Sscanf(m.scaleInput, "%d", &desired); err != nil || desired < 1 {
		m.showToast("✗ Invalid scale value (must be >= 1)", 3*time.Second)
		m.closeScaleDialog()
		return nil
	}

	target := m.pendingTarget
	m.closeScaleDialog()

	return m.scaleProcess(target, desired)
}

// handleWizardKeys handles keys in the wizard view
func (m Model) handleWizardKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.wizardStep == 0 && m.wizardNameLocked {
		m.wizardStep = 1
	}

	key := msg.String()

	// Navigation keys
	switch key {
	case "esc":
		// Cancel wizard
		m.currentView = viewProcessList
		m.resetWizard()
		return m, nil

	case "tab", "enter":
		// Advance to next step (with validation)
		if m.wizardStep == 4 {
			// Final step (Preview) - create/update process
			return m, m.executeWizardSubmit()
		}
		m.advanceWizardStep()
		return m, nil

	case "shift+tab":
		// Go back to previous step
		m.previousWizardStep()
		return m, nil
	}

	// Step-specific input handling
	switch m.wizardStep {
	case 0: // Process name
		return m.handleWizardNameInput(msg)
	case 1: // Command
		return m.handleWizardCommandInput(msg)
	case 2: // Scale
		return m.handleWizardScaleInput(msg)
	case 3: // Restart policy
		return m.handleWizardRestartInput(msg)
	case 4: // Preview/confirm - handled by enter key above
		return m, nil
	}

	return m, nil
}

// handleWizardNameInput handles input for process name step
func (m Model) handleWizardNameInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.wizardNameLocked {
		return m, nil
	}

	key := msg.String()
	text := m.wizardName
	cursor := m.wizardCursor

	// Clamp cursor
	if cursor > len(text) {
		cursor = len(text)
	}

	switch key {
	case "left":
		if cursor > 0 {
			m.wizardCursor--
		}
	case "right":
		if cursor < len(text) {
			m.wizardCursor++
		}
	case "home":
		m.wizardCursor = 0
	case "end":
		m.wizardCursor = len(text)
	case "backspace":
		if cursor > 0 {
			m.wizardName = text[:cursor-1] + text[cursor:]
			m.wizardCursor--
		}
	case "delete":
		if cursor < len(text) {
			m.wizardName = text[:cursor] + text[cursor+1:]
		}
	default:
		// Add character if printable (no spaces for name)
		if len(key) == 1 && key[0] >= 32 && key[0] <= 126 && key[0] != ' ' {
			m.wizardName = text[:cursor] + key + text[cursor:]
			m.wizardCursor++
		}
	}

	return m, nil
}

// handleWizardCommandInput handles input for command step
func (m Model) handleWizardCommandInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	text := m.wizardCommandLine
	cursor := m.wizardCursor

	// Clamp cursor
	if cursor > len(text) {
		cursor = len(text)
	}

	switch key {
	case "left":
		if cursor > 0 {
			m.wizardCursor--
		}
	case "right":
		if cursor < len(text) {
			m.wizardCursor++
		}
	case "home":
		m.wizardCursor = 0
	case "end":
		m.wizardCursor = len(text)
	case "backspace":
		if cursor > 0 {
			m.wizardCommandLine = text[:cursor-1] + text[cursor:]
			m.wizardCursor--
		}
	case "delete":
		if cursor < len(text) {
			m.wizardCommandLine = text[:cursor] + text[cursor+1:]
		}
	default:
		// Add character if printable (spaces allowed for command)
		if len(key) == 1 && key[0] >= 32 && key[0] <= 126 {
			m.wizardCommandLine = text[:cursor] + key + text[cursor:]
			m.wizardCursor++
		} else if key == " " {
			m.wizardCommandLine = text[:cursor] + " " + text[cursor:]
			m.wizardCursor++
		}
	}

	return m, nil
}

// handleWizardScaleInput handles input for scale step
func (m Model) handleWizardScaleInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "backspace":
		if len(m.wizardScaleInput) > 0 {
			m.wizardScaleInput = m.wizardScaleInput[:len(m.wizardScaleInput)-1]
			if m.wizardScaleInput == "" {
				m.wizardScale = 1
			} else {
				_, _ = fmt.Sscanf(m.wizardScaleInput, "%d", &m.wizardScale)
			}
		}

	default:
		// Add digit
		if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
			m.wizardScaleInput += key
			_, _ = fmt.Sscanf(m.wizardScaleInput, "%d", &m.wizardScale)
		}
	}

	return m, nil
}

// handleWizardRestartInput handles input for restart policy step
func (m Model) handleWizardRestartInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "up", "k":
		// Cycle through options: always -> on-failure -> never
		switch m.wizardRestart {
		case "always":
			m.wizardRestart = "never"
		case "on-failure":
			m.wizardRestart = "always"
		case "never":
			m.wizardRestart = "on-failure"
		}

	case "down", "j":
		// Cycle through options: always -> on-failure -> never
		switch m.wizardRestart {
		case "always":
			m.wizardRestart = "on-failure"
		case "on-failure":
			m.wizardRestart = "never"
		case "never":
			m.wizardRestart = "always"
		}
	}

	return m, nil
}

// executeWizardSubmit handles create or edit submissions
func (m *Model) executeWizardSubmit() tea.Cmd {
	// Validate final step
	if !m.validateWizardStep() {
		return nil
	}

	// Capture all values BEFORE resetting wizard (closure would see reset values otherwise)
	cfg := m.wizardProcessConfig()
	targetName := m.wizardName
	if m.wizardMode == wizardModeEdit && m.wizardOriginal != "" {
		targetName = m.wizardOriginal
	}
	isEditMode := m.wizardMode == wizardModeEdit
	commandParts := splitCommandLine(m.wizardCommandLine)
	scale := m.wizardScale
	restart := m.wizardRestart
	enabled := m.wizardEnabled
	isRemote := m.isRemote
	client := m.client
	manager := m.manager

	// Return to process list
	m.currentView = viewProcessList
	m.resetWizard()

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		var err error
		var successMsg string

		if isEditMode {
			successMsg = fmt.Sprintf("✓ Updated %s", targetName)
			if isRemote {
				err = client.UpdateProcess(targetName, cfg)
			} else {
				err = manager.UpdateProcess(ctx, targetName, cfg)
			}
		} else {
			successMsg = fmt.Sprintf("✓ Process %s created successfully", targetName)
			if isRemote {
				err = client.AddProcess(ctx, targetName, commandParts, scale, restart, enabled)
			} else {
				// Embedded mode - create config and add process
				return actionResultMsg{success: false, message: "✗ Wizard not supported in embedded mode yet"}
			}
		}

		if err != nil {
			return actionResultMsg{success: false, message: fmt.Sprintf("✗ Failed to apply process: %v", err)}
		}

		return actionResultMsg{success: true, message: successMsg}
	}
}

func (m *Model) wizardProcessConfig() *config.Process {
	var cfg config.Process
	if m.wizardMode == wizardModeEdit && m.wizardBaseConfig != nil {
		cfg = *m.wizardBaseConfig
	}

	cfg.Enabled = m.wizardEnabled
	cfg.Command = splitCommandLine(m.wizardCommandLine)
	cfg.Scale = m.wizardScale
	cfg.Restart = m.wizardRestart

	return &cfg
}

func splitCommandLine(line string) []string {
	if strings.TrimSpace(line) == "" {
		return []string{}
	}
	return strings.Fields(line)
}

// refreshProcessListCmd fetches process data asynchronously
func (m Model) refreshProcessListCmd() tea.Cmd {
	return func() tea.Msg {
		return m.fetchProcessList()
	}
}

// fetchProcessList retrieves process data for either embedded or remote mode
func (m Model) fetchProcessList() processListResultMsg {
	var (
		processes []process.ProcessInfo
		err       error
	)

	if m.isRemote {
		if m.client == nil {
			err = fmt.Errorf("API client not initialized")
		} else {
			processes, err = m.client.ListProcesses()
		}
	} else {
		if m.manager == nil {
			err = fmt.Errorf("manager not initialized")
		} else {
			processes = m.manager.ListProcesses()
		}
	}

	return processListResultMsg{
		processes: processes,
		err:       err,
	}
}

func (m Model) fetchProcessConfigCmd(name string) tea.Cmd {
	return func() tea.Msg {
		cfg, err := m.getProcessConfig(name)
		return processConfigResultMsg{name: name, cfg: cfg, err: err}
	}
}

func (m *Model) getProcessConfig(name string) (*config.Process, error) {
	if name == "" {
		return nil, fmt.Errorf("process name is required")
	}

	if m.isRemote {
		if m.client == nil {
			return nil, fmt.Errorf("API client not initialized")
		}
		return m.client.GetProcessConfig(name)
	}

	if m.manager == nil {
		return nil, fmt.Errorf("manager not initialized")
	}

	return m.manager.GetProcessConfig(name)
}

// applyProcessListResult updates state after a process refresh completes
func (m *Model) applyProcessListResult(msg processListResultMsg) {
	if msg.err != nil {
		m.err = fmt.Errorf("failed to fetch processes: %w", msg.err)
		return
	}

	m.err = nil
	m.updateProcessTable(msg.processes)

	// Refresh process cache for detail/log views
	m.processCache = make(map[string]process.ProcessInfo, len(msg.processes))
	for _, proc := range msg.processes {
		m.processCache[proc.Name] = proc
	}

	// Refresh oneshot history data
	m.refreshOneshotData()

	if m.detailProc != "" {
		if info, exists := m.processCache[m.detailProc]; exists {
			procCopy := info
			m.updateInstanceTable(&procCopy)
		} else {
			m.detailProc = ""
			if m.currentView == viewProcessDetail {
				m.showToast("Selected process no longer exists", 3*time.Second)
				m.currentView = viewProcessList
			}
		}
	}

	if m.logScope == logScopeProcess && m.selectedProc != "" {
		if _, exists := m.processCache[m.selectedProc]; !exists && m.currentView == viewLogs {
			m.showToast("Process logs unavailable (process removed)", 3*time.Second)
		}
	}
}

// refreshOneshotData fetches and populates oneshot execution history
func (m *Model) refreshOneshotData() {
	var executions []process.OneshotExecution

	if m.isRemote {
		// Remote mode: fetch via API
		if m.client == nil {
			return
		}
		var err error
		executions, err = m.client.GetOneshotHistory(100)
		if err != nil {
			// Silently fail - oneshot history is optional
			return
		}
	} else {
		// Embedded mode: fetch from manager
		if m.manager == nil {
			return
		}
		executions = m.manager.GetAllOneshotExecutions(100)
	}

	// Convert to display rows
	m.oneshotData = make([]oneshotDisplayRow, 0, len(executions))
	for _, exec := range executions {
		row := m.convertOneshotExecution(exec)
		m.oneshotData = append(m.oneshotData, row)
	}

	// Ensure selection is valid
	if m.oneshotIndex >= len(m.oneshotData) {
		if len(m.oneshotData) > 0 {
			m.oneshotIndex = len(m.oneshotData) - 1
		} else {
			m.oneshotIndex = 0
		}
	}
}

// convertOneshotExecution converts a process.OneshotExecution to an oneshotDisplayRow
func (m *Model) convertOneshotExecution(exec process.OneshotExecution) oneshotDisplayRow {
	row := oneshotDisplayRow{
		id:           exec.ID,
		processName:  exec.ProcessName,
		instanceID:   exec.InstanceID,
		triggerType:  exec.TriggerType,
		rawStartedAt: exec.StartedAt.Unix(),
	}

	// Format started time
	row.startedAt = exec.StartedAt.Format("15:04:05")
	row.startedStyle = dimStyle

	// Calculate duration and status
	if exec.FinishedAt.IsZero() {
		// Still running
		row.status = "Running"
		row.statusStyle = highlightStyle
		row.duration = time.Since(exec.StartedAt).Truncate(time.Second).String()
		row.exitCode = "-"
		row.finishedAt = "-"
	} else {
		row.finishedAt = exec.FinishedAt.Format("15:04:05")
		row.finishedStyle = dimStyle
		row.duration = exec.Duration // Already a formatted string

		if exec.ExitCode == 0 {
			row.status = "Success"
			row.statusStyle = successStyle
			row.exitCode = "0"
			row.exitStyle = successStyle
		} else {
			row.status = "Failed"
			row.statusStyle = errorStyle
			row.exitCode = fmt.Sprintf("%d", exec.ExitCode)
			row.exitStyle = errorStyle
		}

		if exec.Error != "" {
			row.error = exec.Error
		}
	}

	return row
}

func (m *Model) scaleProcess(target string, desired int) tea.Cmd {
	return func() tea.Msg {
		var err error

		if m.isRemote {
			err = m.client.ScaleProcess(target, desired)
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			err = m.manager.ScaleProcess(ctx, target, desired)
		}

		if err != nil {
			return actionResultMsg{success: false, message: fmt.Sprintf("✗ Scale failed: %v", err)}
		}

		return actionResultMsg{success: true, message: fmt.Sprintf("✓ Scaled %s to %d", target, desired)}
	}
}
