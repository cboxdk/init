package tui

import (
	"strings"
	"testing"

	"github.com/cboxdk/init/internal/process"
)

// Helper to create a minimal model for view testing
func createTestModel() Model {
	m := Model{
		width:        80,
		height:       24,
		currentView:  viewProcessList,
		activeTab:    tabProcesses,
		processCache: make(map[string]process.ProcessInfo),
	}
	return m
}

func TestView_Quitting(t *testing.T) {
	m := createTestModel()
	m.quitting = true

	result := m.View()

	if result != "" {
		t.Errorf("expected empty string when quitting, got: %q", result)
	}
}

func TestView_ProcessList(t *testing.T) {
	m := createTestModel()
	m.currentView = viewProcessList

	result := m.View()

	// Should contain header
	if !strings.Contains(result, "PHPeek PM") {
		t.Error("expected process list to contain PHPeek PM header")
	}

	// Should contain tab bar
	if !strings.Contains(result, "Processes") {
		t.Error("expected process list to contain Processes tab")
	}
}

func TestView_ProcessDetail_NoProcess(t *testing.T) {
	m := createTestModel()
	m.currentView = viewProcessDetail
	m.detailProc = ""

	result := m.View()

	if !strings.Contains(result, "No process selected") {
		t.Errorf("expected 'No process selected' message, got: %s", result)
	}
}

func TestView_ProcessDetail_ProcessNotFound(t *testing.T) {
	m := createTestModel()
	m.currentView = viewProcessDetail
	m.detailProc = "nonexistent-proc"

	result := m.View()

	if !strings.Contains(result, "not found") {
		t.Errorf("expected 'not found' message, got: %s", result)
	}
}

func TestView_ProcessDetail_WithProcess(t *testing.T) {
	m := createTestModel()
	m.currentView = viewProcessDetail
	m.detailProc = "test-proc"
	m.processCache["test-proc"] = process.ProcessInfo{
		Name:         "test-proc",
		Type:         "longrun",
		State:        "running",
		Scale:        2,
		DesiredScale: 2,
		Instances:    []process.ProcessInstanceInfo{},
	}

	result := m.View()

	if !strings.Contains(result, "test-proc") {
		t.Error("expected detail view to contain process name")
	}
	if !strings.Contains(result, "Type: longrun") {
		t.Error("expected detail view to contain process type")
	}
}

func TestView_Logs(t *testing.T) {
	m := createTestModel()
	m.currentView = viewLogs
	m.logScope = logScopeStack

	result := m.View()

	if !strings.Contains(result, "Logs:") {
		t.Error("expected logs view to contain 'Logs:' header")
	}
	if !strings.Contains(result, "Stack") {
		t.Error("expected logs view to contain 'Stack' scope")
	}
}

func TestView_Logs_ProcessScope(t *testing.T) {
	m := createTestModel()
	m.currentView = viewLogs
	m.logScope = logScopeProcess
	m.selectedProc = "my-worker"

	result := m.View()

	if !strings.Contains(result, "my-worker") {
		t.Errorf("expected logs view to contain process name 'my-worker', got: %s", result)
	}
}

func TestView_Logs_WithInstance(t *testing.T) {
	m := createTestModel()
	m.currentView = viewLogs
	m.logScope = logScopeProcess
	m.selectedProc = "worker"
	m.logInstance = "worker-1"

	result := m.View()

	if !strings.Contains(result, "worker-1") {
		t.Errorf("expected logs view to contain instance 'worker-1', got: %s", result)
	}
}

func TestView_Logs_Paused(t *testing.T) {
	m := createTestModel()
	m.currentView = viewLogs
	m.logsPaused = true

	result := m.View()

	if !strings.Contains(result, "PAUSED") {
		t.Error("expected paused logs to show PAUSED indicator")
	}
}

func TestView_Logs_Live(t *testing.T) {
	m := createTestModel()
	m.currentView = viewLogs
	m.logsPaused = false

	result := m.View()

	if !strings.Contains(result, "LIVE") {
		t.Error("expected live logs to show LIVE indicator")
	}
}

func TestView_Help(t *testing.T) {
	m := createTestModel()
	m.currentView = viewHelp

	result := m.View()

	// Check for essential help content
	if !strings.Contains(result, "Keyboard Shortcuts") {
		t.Error("expected help view to contain 'Keyboard Shortcuts'")
	}
	if !strings.Contains(result, "Tab Navigation") {
		t.Error("expected help view to contain 'Tab Navigation'")
	}
	if !strings.Contains(result, "Press any key to return") {
		t.Error("expected help view to contain return instruction")
	}
}

func TestView_Wizard(t *testing.T) {
	m := createTestModel()
	m.currentView = viewWizard
	m.wizardMode = wizardModeCreate
	m.wizardStep = 0

	result := m.View()

	if !strings.Contains(result, "Add New Process") {
		t.Error("expected wizard to show 'Add New Process' title")
	}
	if !strings.Contains(result, "Step 1/5") {
		t.Error("expected wizard to show step indicator")
	}
}

func TestView_Wizard_EditMode(t *testing.T) {
	m := createTestModel()
	m.currentView = viewWizard
	m.wizardMode = wizardModeEdit
	m.wizardOriginal = "existing-proc"
	m.wizardStep = 0

	result := m.View()

	if !strings.Contains(result, "Edit Process") {
		t.Error("expected edit wizard to show 'Edit Process' title")
	}
	if !strings.Contains(result, "existing-proc") {
		t.Error("expected edit wizard to show original process name")
	}
}

func TestView_UnknownView(t *testing.T) {
	m := createTestModel()
	m.currentView = viewMode(999) // Unknown view mode

	result := m.View()

	if !strings.Contains(result, "Unknown view") {
		t.Errorf("expected 'Unknown view' for invalid view mode, got: %s", result)
	}
}

func TestRenderTabBar(t *testing.T) {
	tests := []struct {
		name      string
		activeTab tabType
		wantTabs  []string
	}{
		{
			name:      "processes tab active",
			activeTab: tabProcesses,
			wantTabs:  []string{"[1] Processes", "[2] Scheduled", "[3] Oneshot", "[4] System"},
		},
		{
			name:      "scheduled tab active",
			activeTab: tabScheduled,
			wantTabs:  []string{"[1] Processes", "[2] Scheduled", "[3] Oneshot", "[4] System"},
		},
		{
			name:      "system tab active",
			activeTab: tabSystem,
			wantTabs:  []string{"[1] Processes", "[2] Scheduled", "[3] Oneshot", "[4] System"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := createTestModel()
			m.activeTab = tt.activeTab

			result := m.renderTabBar()

			for _, tab := range tt.wantTabs {
				if !strings.Contains(result, tab) {
					t.Errorf("expected tab bar to contain %q, got: %s", tab, result)
				}
			}
		})
	}
}

func TestRenderProcessesTab_Empty(t *testing.T) {
	m := createTestModel()
	m.tableData = nil

	result := m.renderProcessesTab()

	if !strings.Contains(result, "No processes found") {
		t.Errorf("expected 'No processes found' for empty table, got: %s", result)
	}
}

func TestRenderScheduledTab_Empty(t *testing.T) {
	m := createTestModel()
	m.scheduledData = nil

	result := m.renderScheduledTab()

	if !strings.Contains(result, "No scheduled jobs found") {
		t.Errorf("expected 'No scheduled jobs found' for empty scheduled data, got: %s", result)
	}
}

func TestRenderOneshotTab_Empty(t *testing.T) {
	m := createTestModel()
	m.oneshotData = nil

	result := m.renderOneshotTab()

	if !strings.Contains(result, "No oneshot executions found") {
		t.Errorf("expected 'No oneshot executions found' for empty oneshot data, got: %s", result)
	}
	if !strings.Contains(result, "Execution history will appear here") {
		t.Error("expected helpful explanation for empty oneshot tab")
	}
}

func TestRenderSystemTab(t *testing.T) {
	m := createTestModel()
	m.systemMenuIndex = 0

	result := m.renderSystemTab()

	if !strings.Contains(result, "System Controls") {
		t.Error("expected system tab to contain 'System Controls' header")
	}
	if !strings.Contains(result, "Reload Configuration") {
		t.Error("expected system tab to contain 'Reload Configuration' option")
	}
	if !strings.Contains(result, "Save Configuration") {
		t.Error("expected system tab to contain 'Save Configuration' option")
	}
}

func TestRenderConfirmationOverlay(t *testing.T) {
	tests := []struct {
		name          string
		action        actionType
		target        string
		wantActionTxt string
	}{
		{"restart", actionRestart, "php-fpm", "Restart"},
		{"stop", actionStop, "nginx", "Stop"},
		{"start", actionStart, "worker", "Start"},
		{"schedule pause", actionSchedulePause, "cron-job", "Pause Schedule"},
		{"schedule resume", actionScheduleResume, "cron-job", "Resume Schedule"},
		{"schedule trigger", actionScheduleTrigger, "cron-job", "Trigger Schedule"},
		{"default execute", actionNone, "unknown", "Execute"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := createTestModel()
			m.pendingAction = tt.action
			m.pendingTarget = tt.target

			result := m.renderConfirmationOverlay()

			if !strings.Contains(result, tt.wantActionTxt) {
				t.Errorf("expected confirmation to contain action %q, got: %s", tt.wantActionTxt, result)
			}
			if !strings.Contains(result, tt.target) {
				t.Errorf("expected confirmation to contain target %q, got: %s", tt.target, result)
			}
			if !strings.Contains(result, "Are you sure") {
				t.Error("expected confirmation to contain 'Are you sure' prompt")
			}
		})
	}
}

func TestRenderScaleDialogOverlay(t *testing.T) {
	tests := []struct {
		name       string
		scaleInput string
		target     string
		wantScale  string
	}{
		{"empty input shows underscore", "", "worker", "_"},
		{"input shows value", "5", "worker", "5"},
		{"multi-digit input", "12", "queue", "12"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := createTestModel()
			m.scaleInput = tt.scaleInput
			m.pendingTarget = tt.target

			result := m.renderScaleDialogOverlay()

			if !strings.Contains(result, "Scale Process") {
				t.Error("expected scale dialog to contain 'Scale Process' header")
			}
			if !strings.Contains(result, tt.target) {
				t.Errorf("expected scale dialog to contain target %q, got: %s", tt.target, result)
			}
			if !strings.Contains(result, tt.wantScale) {
				t.Errorf("expected scale dialog to contain scale input %q, got: %s", tt.wantScale, result)
			}
		})
	}
}

func TestLogScopeDescription_View(t *testing.T) {
	tests := []struct {
		name         string
		scope        logScope
		selectedProc string
		want         string
	}{
		{"stack scope", logScopeStack, "", "Stack (all processes)"},
		{"process scope with name", logScopeProcess, "my-worker", "Process (my-worker)"},
		{"process scope without name", logScopeProcess, "", "Stack (all processes)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := createTestModel()
			m.logScope = tt.scope
			m.selectedProc = tt.selectedProc

			result := m.logScopeDescription()

			if result != tt.want {
				t.Errorf("logScopeDescription() = %q, want %q", result, tt.want)
			}
		})
	}
}

func TestWithOverlay_ZeroWidth(t *testing.T) {
	m := createTestModel()
	m.width = 0

	base := "base view"
	overlay := "overlay content"

	result := m.withOverlay(base, overlay)

	// Should just concatenate when width is 0
	if !strings.Contains(result, base) {
		t.Error("expected result to contain base view")
	}
	if !strings.Contains(result, overlay) {
		t.Error("expected result to contain overlay")
	}
}

func TestWithOverlay_WithWidth(t *testing.T) {
	m := createTestModel()
	m.width = 80

	base := "base view content"
	overlay := "overlay content"

	result := m.withOverlay(base, overlay)

	if !strings.Contains(result, base) {
		t.Error("expected result to contain base view")
	}
	if !strings.Contains(result, overlay) {
		t.Error("expected result to contain overlay")
	}
}

func TestPadViewHeight_ZeroDimensions(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
	}{
		{"zero width", 0, 24},
		{"zero height", 80, 0},
		{"both zero", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := createTestModel()
			m.width = tt.width
			m.height = tt.height

			view := "test view"
			result := m.padViewHeight(view)

			// Should return view unchanged
			if result != view {
				t.Errorf("expected unchanged view with zero dimensions, got different result")
			}
		})
	}
}

func TestPadViewHeight_ViewTallerThanScreen(t *testing.T) {
	m := createTestModel()
	m.width = 80
	m.height = 5

	// Create a view taller than the screen height
	view := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8"
	result := m.padViewHeight(view)

	// Should return view unchanged (no padding needed)
	if result != view {
		t.Error("expected unchanged view when view is taller than screen")
	}
}

func TestRenderProgressBar(t *testing.T) {
	tests := []struct {
		name       string
		step       int
		wantActive string
	}{
		{"step 0", 0, "Name"},
		{"step 1", 1, "Command"},
		{"step 2", 2, "Scale"},
		{"step 3", 3, "Restart"},
		{"step 4", 4, "Preview"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := createTestModel()
			m.wizardStep = tt.step

			result := m.renderProgressBar()

			// Should contain all step names
			for _, step := range []string{"Name", "Command", "Scale", "Restart", "Preview"} {
				if !strings.Contains(result, step) {
					t.Errorf("expected progress bar to contain step %q", step)
				}
			}
		})
	}
}

func TestRenderWizardStepName(t *testing.T) {
	t.Run("unlocked name", func(t *testing.T) {
		m := createTestModel()
		m.wizardNameLocked = false
		m.wizardName = "test-proc"

		result := m.renderWizardStepName()

		if !strings.Contains(result, "Process Name") {
			t.Error("expected step name header")
		}
		if !strings.Contains(result, "unique name") {
			t.Error("expected instruction for entering name")
		}
		if !strings.Contains(result, "test-proc") {
			t.Error("expected current name value")
		}
	})

	t.Run("locked name", func(t *testing.T) {
		m := createTestModel()
		m.wizardNameLocked = true
		m.wizardName = "existing-proc"

		result := m.renderWizardStepName()

		if !strings.Contains(result, "cannot be changed") {
			t.Error("expected locked name message")
		}
		if !strings.Contains(result, "existing-proc") {
			t.Error("expected existing name value")
		}
	})
}

func TestRenderWizardStepCommand(t *testing.T) {
	m := createTestModel()
	m.wizardCommandLine = "php artisan queue:work"

	result := m.renderWizardStepCommand()

	if !strings.Contains(result, "Process Command") {
		t.Error("expected step command header")
	}
	if !strings.Contains(result, "php artisan queue:work") {
		t.Error("expected command value")
	}
	if !strings.Contains(result, "Tip:") {
		t.Error("expected helpful tip")
	}
}

func TestRenderWizardStepScale(t *testing.T) {
	tests := []struct {
		name       string
		scaleInput string
		wantScale  string
	}{
		{"empty defaults to 1", "", "1"},
		{"shows input value", "3", "3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := createTestModel()
			m.wizardScaleInput = tt.scaleInput

			result := m.renderWizardStepScale()

			if !strings.Contains(result, "Process Scale") {
				t.Error("expected step scale header")
			}
			if !strings.Contains(result, tt.wantScale) {
				t.Errorf("expected scale value %q in result: %s", tt.wantScale, result)
			}
		})
	}
}

func TestRenderWizardStepRestart(t *testing.T) {
	tests := []struct {
		name    string
		restart string
	}{
		{"always", "always"},
		{"on-failure", "on-failure"},
		{"never", "never"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := createTestModel()
			m.wizardRestart = tt.restart

			result := m.renderWizardStepRestart()

			if !strings.Contains(result, "Restart Policy") {
				t.Error("expected step restart header")
			}
			if !strings.Contains(result, tt.restart) {
				t.Errorf("expected restart policy %q in result", tt.restart)
			}
			// Should contain all options
			for _, opt := range []string{"always", "on-failure", "never"} {
				if !strings.Contains(result, opt) {
					t.Errorf("expected option %q in result", opt)
				}
			}
		})
	}
}

func TestRenderWizardStepPreview(t *testing.T) {
	t.Run("create mode", func(t *testing.T) {
		m := createTestModel()
		m.wizardMode = wizardModeCreate
		m.wizardName = "new-worker"
		m.wizardCommandLine = "php worker.php"
		m.wizardScale = 2
		m.wizardRestart = "always"
		m.wizardEnabled = true

		result := m.renderWizardStepPreview()

		if !strings.Contains(result, "Preview Configuration") {
			t.Error("expected preview header")
		}
		if !strings.Contains(result, "new-worker") {
			t.Error("expected wizard name")
		}
		if !strings.Contains(result, "php worker.php") {
			t.Error("expected command")
		}
		if !strings.Contains(result, "Ready to create process") {
			t.Error("expected create mode message")
		}
	})

	t.Run("edit mode", func(t *testing.T) {
		m := createTestModel()
		m.wizardMode = wizardModeEdit
		m.wizardName = "existing-worker"
		m.wizardCommandLine = "php worker.php --modified"
		m.wizardScale = 3
		m.wizardRestart = "on-failure"
		m.wizardEnabled = true

		result := m.renderWizardStepPreview()

		if !strings.Contains(result, "Ready to update process") {
			t.Error("expected update mode message")
		}
	})

	t.Run("empty command", func(t *testing.T) {
		m := createTestModel()
		m.wizardCommandLine = ""

		result := m.renderWizardStepPreview()

		if !strings.Contains(result, "(not set)") {
			t.Error("expected '(not set)' for empty command")
		}
	})
}

func TestRenderInputWithCursor(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		cursor int
	}{
		{"cursor at start", "hello", 0},
		{"cursor in middle", "hello", 2},
		{"cursor at end", "hello", 5},
		{"cursor beyond end", "hello", 10}, // Should be clamped
		{"cursor negative", "hello", -5},   // Should be clamped
		{"empty text cursor at end", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := renderInputWithCursor(tt.text, tt.cursor)

			// Result should not be empty (even for empty text, cursor is shown)
			if result == "" && tt.text != "" {
				t.Error("expected non-empty result for non-empty text")
			}
		})
	}
}

func TestRenderProcessList_WithError(t *testing.T) {
	m := createTestModel()
	m.currentView = viewProcessList
	m.err = &testError{msg: "test error message"}

	result := m.View()

	if !strings.Contains(result, "Error:") {
		t.Error("expected error indicator in output")
	}
	if !strings.Contains(result, "test error message") {
		t.Error("expected error message in output")
	}
}

func TestRenderProcessList_WithToast(t *testing.T) {
	m := createTestModel()
	m.currentView = viewProcessList
	m.toast = "Process started successfully"

	result := m.View()

	if !strings.Contains(result, "Process started successfully") {
		t.Error("expected toast message in output")
	}
}

func TestRenderProcessList_ShowConfirmation(t *testing.T) {
	m := createTestModel()
	m.currentView = viewProcessList
	m.showConfirmation = true
	m.pendingAction = actionRestart
	m.pendingTarget = "php-fpm"

	result := m.View()

	if !strings.Contains(result, "Confirm") {
		t.Error("expected confirmation overlay")
	}
	if !strings.Contains(result, "php-fpm") {
		t.Error("expected target in confirmation")
	}
}

func TestRenderProcessList_ShowScaleDialog(t *testing.T) {
	m := createTestModel()
	m.currentView = viewProcessList
	m.showScaleDialog = true
	m.pendingTarget = "worker"
	m.scaleInput = "5"

	result := m.View()

	if !strings.Contains(result, "Scale Process") {
		t.Error("expected scale dialog overlay")
	}
	if !strings.Contains(result, "worker") {
		t.Error("expected target in scale dialog")
	}
}

func TestRenderProcessList_ScheduledTabFooter(t *testing.T) {
	m := createTestModel()
	m.currentView = viewProcessList
	m.activeTab = tabScheduled
	m.scheduledData = []scheduledDisplayRow{
		{rawState: "running"},
	}
	m.scheduledIndex = 0

	result := m.View()

	// Should show Pause (not Resume) since schedule is running
	if !strings.Contains(result, "Pause") {
		t.Error("expected 'Pause' in footer for running schedule")
	}
}

func TestRenderProcessList_ScheduledTabFooter_Paused(t *testing.T) {
	m := createTestModel()
	m.currentView = viewProcessList
	m.activeTab = tabScheduled
	m.scheduledData = []scheduledDisplayRow{
		{rawState: "paused"},
	}
	m.scheduledIndex = 0

	result := m.View()

	// Should show Resume (not Pause) since schedule is paused
	if !strings.Contains(result, "Resume") {
		t.Error("expected 'Resume' in footer for paused schedule")
	}
}

// testError is a simple error implementation for testing
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
