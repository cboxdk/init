package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/cboxdk/init/internal/apiclient"
	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/process"
)

// viewMode represents the current TUI view
type viewMode int

const (
	viewProcessList viewMode = iota
	viewProcessDetail
	viewLogs
	viewHelp
	viewWizard
)

// tabType represents the k9s-style tabs
type tabType int

const (
	tabProcesses tabType = iota // Regular longrun/oneshot processes
	tabScheduled                // Cron/scheduled jobs
	tabOneshot                  // Oneshot execution history
	tabSystem                   // System controls (reload, save config)
)

// Tab definitions for UI rendering
var tabNames = []string{"Processes", "Scheduled", "Oneshot", "System"}
var tabShortcuts = []string{"1", "2", "3", "4"}

// logScope determines whether logs are shown for entire stack or specific process
type logScope int

const (
	logScopeStack logScope = iota
	logScopeProcess
)

type wizardMode int

const (
	wizardModeCreate wizardMode = iota
	wizardModeEdit
)

// actionType represents process actions that can be performed
type actionType int

const (
	actionNone actionType = iota
	actionRestart
	actionStop
	actionStart
	actionScale
	actionDelete
	actionSchedulePause
	actionScheduleResume
	actionScheduleTrigger
)

// Model is the main Bubbletea model for the TUI
type Model struct {
	manager      *process.Manager // For embedded mode
	client       *apiclient.Client // For remote mode
	isRemote     bool             // true if using API client
	currentView  viewMode
	activeTab    tabType // k9s-style tab selection
	processTable table.Model
	logViewport  viewport.Model
	selectedProc string
	detailProc   string
	processCache map[string]process.ProcessInfo
	logsPaused   bool
	logBuffer    []string
	logScope     logScope
	logReturn    viewMode
	width        int
	height       int
	err          error
	quitting     bool

	// Confirmation dialog
	showConfirmation bool
	pendingAction    actionType
	pendingTarget    string

	// Scale dialog
	showScaleDialog bool
	scaleInput      string

	// Toast notifications
	toast         string
	toastDuration time.Duration
	toastExpiry   time.Time

	// Wizard state
	wizardStep        int
	wizardName        string
	wizardCommandLine string
	wizardScale       int
	wizardScaleInput  string
	wizardRestart     string
	wizardEnabled     bool
	wizardError       string
	wizardMode        wizardMode
	wizardOriginal    string
	wizardNameLocked  bool
	wizardBaseConfig  *config.Process
	wizardCursor      int // cursor position in current input field

	// Table rendering state
	tableData         []processDisplayRow
	tableColumnWidths []int
	tableOffset       int
	selectedIndex     int
	instanceTable     table.Model
	instanceColumns   []table.Column
	logInstance       string

	// k9s-style tab data
	// Processes tab uses: tableData, selectedIndex, tableOffset (existing fields)
	// Scheduled tab uses: scheduledData, scheduledIndex, scheduledOffset
	scheduledData   []scheduledDisplayRow // Scheduled/cron jobs
	scheduledIndex  int
	scheduledOffset int

	// Oneshot tab data
	oneshotData   []oneshotDisplayRow // Oneshot execution history
	oneshotIndex  int
	oneshotOffset int

	// System tab data
	systemMenuIndex int // Currently selected system menu option
}

// NewModel creates a new TUI model for embedded mode
func NewModel(mgr *process.Manager) Model {
	return Model{
		manager:       mgr,
		client:        nil,
		isRemote:      false,
		currentView:   viewProcessList,
		processCache:  make(map[string]process.ProcessInfo),
		logBuffer:     make([]string, 0, 1000),
		logsPaused:    false,
		logScope:      logScopeStack,
		logReturn:     viewProcessList,
		selectedIndex: 0,
	}
}

// NewRemoteModel creates a new TUI model for remote mode
func NewRemoteModel(apiURL, auth string) Model {
	return Model{
		manager:       nil,
		client:        apiclient.New(apiURL, auth),
		isRemote:      true,
		currentView:   viewProcessList,
		processCache:  make(map[string]process.ProcessInfo),
		logBuffer:     make([]string, 0, 1000),
		logsPaused:    false,
		logScope:      logScopeStack,
		logReturn:     viewProcessList,
		selectedIndex: 0,
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		m.refreshProcessListCmd(),
		tea.EnterAltScreen,
	)
}

// Messages for Bubbletea
type tickMsg time.Time
type tickLogRefreshMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func tickLogRefreshCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tickLogRefreshMsg(t)
	})
}

// showToast displays a toast notification for the specified duration
func (m *Model) showToast(message string, duration time.Duration) {
	m.toast = message
	m.toastDuration = duration
	m.toastExpiry = time.Now().Add(duration)
}

// clearToastIfExpired clears toast if it has expired
func (m *Model) clearToastIfExpired() {
	if m.toast != "" && time.Now().After(m.toastExpiry) {
		m.toast = ""
	}
}

// confirmAction shows confirmation dialog for an action
func (m *Model) confirmAction(action actionType, target string) {
	m.showConfirmation = true
	m.pendingAction = action
	m.pendingTarget = target
}

func (m *Model) triggerAction(action actionType, target string) tea.Cmd {
	if target == "" {
		return nil
	}
	m.pendingAction = action
	m.pendingTarget = target
	return m.executeAction()
}

// cancelConfirmation cancels pending action
func (m *Model) cancelConfirmation() {
	m.showConfirmation = false
	m.pendingAction = actionNone
	m.pendingTarget = ""
}

// executeAction performs the confirmed action
func (m *Model) executeAction() tea.Cmd {
	if m.pendingAction == actionNone || m.pendingTarget == "" {
		m.cancelConfirmation()
		return nil
	}

	target := m.pendingTarget
	action := m.pendingAction
	m.cancelConfirmation()

	// Return async command to execute action
	return func() tea.Msg {
		successMsg, err := m.dispatchAction(action, target)
		if err != nil {
			return actionResultMsg{success: false, message: fmt.Sprintf("✗ Error: %v", err)}
		}
		if successMsg == "" {
			return nil
		}
		return actionResultMsg{success: true, message: successMsg}
	}
}

// dispatchAction routes actions to their handlers and returns success message or error
func (m *Model) dispatchAction(action actionType, target string) (string, error) {
	switch action {
	case actionRestart:
		return m.executeRestart(target)
	case actionStop:
		return m.executeStop(target)
	case actionStart:
		return m.executeStart(target)
	case actionScale:
		return "", nil // Scale is handled separately in executeScale
	case actionDelete:
		return m.executeDelete(target)
	case actionSchedulePause:
		return m.executeSchedulePause(target)
	case actionScheduleResume:
		return m.executeScheduleResume(target)
	case actionScheduleTrigger:
		return m.executeScheduleTrigger(target)
	default:
		return "", nil
	}
}

// executeRestart handles process restart action
func (m *Model) executeRestart(target string) (string, error) {
	var err error
	if m.isRemote {
		err = m.client.RestartProcess(target)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err = m.manager.RestartProcess(ctx, target)
	}
	return fmt.Sprintf("✓ Restarted %s", target), err
}

// executeStop handles process stop action
func (m *Model) executeStop(target string) (string, error) {
	var err error
	if m.isRemote {
		err = m.client.StopProcess(target)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err = m.manager.StopProcess(ctx, target)
	}
	return fmt.Sprintf("✓ Stopped %s", target), err
}

// executeStart handles process start action
func (m *Model) executeStart(target string) (string, error) {
	var err error
	if m.isRemote {
		err = m.client.StartProcess(target)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err = m.manager.StartProcess(ctx, target)
	}
	return fmt.Sprintf("✓ Started %s", target), err
}

// executeDelete handles process delete action
func (m *Model) executeDelete(target string) (string, error) {
	var err error
	if m.isRemote {
		err = m.client.DeleteProcess(target)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err = m.manager.RemoveProcess(ctx, target)
	}
	return fmt.Sprintf("✓ Removed %s", target), err
}

// executeSchedulePause handles schedule pause action
func (m *Model) executeSchedulePause(target string) (string, error) {
	var err error
	if m.isRemote {
		err = m.client.PauseSchedule(target)
	} else {
		err = m.manager.PauseSchedule(target)
	}
	return fmt.Sprintf("✓ Paused schedule %s", target), err
}

// executeScheduleResume handles schedule resume action
func (m *Model) executeScheduleResume(target string) (string, error) {
	var err error
	if m.isRemote {
		err = m.client.ResumeSchedule(target)
	} else {
		err = m.manager.ResumeSchedule(target)
	}
	return fmt.Sprintf("✓ Resumed schedule %s", target), err
}

// executeScheduleTrigger handles schedule trigger action
func (m *Model) executeScheduleTrigger(target string) (string, error) {
	var err error
	if m.isRemote {
		err = m.client.TriggerSchedule(target)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err = m.manager.TriggerSchedule(ctx, target)
	}
	return fmt.Sprintf("✓ Triggered %s", target), err
}

// actionResultMsg carries the result of an async action
type actionResultMsg struct {
	success bool
	message string
}

// openScaleDialog opens the scale input dialog
func (m *Model) openScaleDialog(target string) {
	m.showScaleDialog = true
	m.pendingTarget = target
	m.scaleInput = ""
}

// closeScaleDialog closes the scale dialog
func (m *Model) closeScaleDialog() {
	m.showScaleDialog = false
	m.pendingTarget = ""
	m.scaleInput = ""
}

// resetWizard resets the wizard to initial state
func (m *Model) resetWizard() {
	m.wizardStep = 0
	m.wizardName = ""
	m.wizardCommandLine = ""
	m.wizardScale = 1
	m.wizardScaleInput = "1"
	m.wizardRestart = "always"
	m.wizardEnabled = true
	m.wizardError = ""
	m.wizardMode = wizardModeCreate
	m.wizardOriginal = ""
	m.wizardNameLocked = false
	m.wizardBaseConfig = nil
	m.wizardCursor = 0
}

// startWizard initializes and opens the wizard
func (m *Model) startWizard() {
	m.resetWizard()
	m.currentView = viewWizard
}

// startEditWizard opens the wizard populated with an existing config
func (m *Model) startEditWizard(name string, procCfg *config.Process) {
	m.resetWizard()
	m.wizardMode = wizardModeEdit
	m.wizardOriginal = name
	m.wizardName = name
	m.wizardNameLocked = true
	m.wizardStep = 1
	m.wizardBaseConfig = cloneProcessConfig(procCfg)
	if procCfg != nil {
		m.wizardCommandLine = strings.Join(procCfg.Command, " ")
		m.wizardCursor = len(m.wizardCommandLine) // cursor at end
		if procCfg.Scale > 0 {
			m.wizardScale = procCfg.Scale
			m.wizardScaleInput = fmt.Sprintf("%d", procCfg.Scale)
		}
		if procCfg.Restart != "" {
			m.wizardRestart = procCfg.Restart
		}
		m.wizardEnabled = procCfg.Enabled
	}
	m.currentView = viewWizard
}

func cloneProcessConfig(proc *config.Process) *config.Process {
	if proc == nil {
		return nil
	}
	clone := *proc
	if proc.Command != nil {
		clone.Command = append([]string{}, proc.Command...)
	}
	if proc.DependsOn != nil {
		clone.DependsOn = append([]string{}, proc.DependsOn...)
	}
	if proc.Env != nil {
		clone.Env = make(map[string]string, len(proc.Env))
		for k, v := range proc.Env {
			clone.Env[k] = v
		}
	}
	return &clone
}

// validateWizardStep validates the current wizard step
func (m *Model) validateWizardStep() bool {
	m.wizardError = ""

	switch m.wizardStep {
	case 0: // Process name
		if m.wizardNameLocked {
			return true
		}
		if m.wizardName == "" {
			m.wizardError = "Process name cannot be empty"
			return false
		}
		if strings.ContainsAny(m.wizardName, " \t\n") {
			m.wizardError = "Process name cannot contain whitespace"
			return false
		}

	case 1: // Command
		if strings.TrimSpace(m.wizardCommandLine) == "" {
			m.wizardError = "Command cannot be empty."
			return false
		}

	case 2: // Scale
		if m.wizardScale < 1 {
			m.wizardError = "Scale must be at least 1"
			return false
		}

	case 3: // Restart policy - always valid (dropdown)

	case 4: // Preview - always valid (read-only)
	}

	return true
}

// advanceWizardStep moves to the next wizard step
func (m *Model) advanceWizardStep() {
	if !m.validateWizardStep() {
		return
	}

	if m.wizardStep < 4 {
		m.wizardStep++
		// Set cursor to end of new step's content
		m.setCursorForCurrentStep()
	}
}

// previousWizardStep moves to the previous wizard step
func (m *Model) previousWizardStep() {
	if m.wizardStep > 0 {
		m.wizardStep--
		m.wizardError = ""
		// Set cursor to end of new step's content
		m.setCursorForCurrentStep()
	}
}

// setCursorForCurrentStep sets cursor position based on current wizard step
func (m *Model) setCursorForCurrentStep() {
	switch m.wizardStep {
	case 0: // Name step
		m.wizardCursor = len(m.wizardName)
	case 1: // Command step
		m.wizardCursor = len(m.wizardCommandLine)
	default:
		// Steps 2-4 don't use text cursor
		m.wizardCursor = 0
	}
}
