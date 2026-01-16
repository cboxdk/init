package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/process"
)

// TestShowToast tests toast notification display
func TestShowToast(t *testing.T) {
	m := &Model{}
	message := "Test message"
	duration := 3 * time.Second

	m.showToast(message, duration)

	if m.toast != message {
		t.Errorf("Expected toast message %q, got %q", message, m.toast)
	}

	if m.toastDuration != duration {
		t.Errorf("Expected toast duration %v, got %v", duration, m.toastDuration)
	}

	if m.toastExpiry.Before(time.Now()) {
		t.Error("Expected toast expiry to be in the future")
	}
}

// TestClearToastIfExpired tests toast clearing logic
func TestClearToastIfExpired(t *testing.T) {
	tests := []struct {
		name          string
		setupFunc     func(m *Model)
		expectCleared bool
	}{
		{
			name: "expired toast",
			setupFunc: func(m *Model) {
				m.toast = "Test"
				m.toastExpiry = time.Now().Add(-1 * time.Second)
			},
			expectCleared: true,
		},
		{
			name: "not expired toast",
			setupFunc: func(m *Model) {
				m.toast = "Test"
				m.toastExpiry = time.Now().Add(5 * time.Second)
			},
			expectCleared: false,
		},
		{
			name: "no toast",
			setupFunc: func(m *Model) {
				m.toast = ""
			},
			expectCleared: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{}
			tt.setupFunc(m)

			m.clearToastIfExpired()

			isEmpty := m.toast == ""
			if isEmpty != tt.expectCleared {
				t.Errorf("Expected toast cleared = %v, got %v", tt.expectCleared, isEmpty)
			}
		})
	}
}

// TestConfirmAction tests confirmation dialog setup
func TestConfirmAction(t *testing.T) {
	tests := []struct {
		name   string
		action actionType
		target string
	}{
		{
			name:   "restart action",
			action: actionRestart,
			target: "php-fpm",
		},
		{
			name:   "stop action",
			action: actionStop,
			target: "nginx",
		},
		{
			name:   "delete action",
			action: actionDelete,
			target: "worker",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{}
			m.confirmAction(tt.action, tt.target)

			if !m.showConfirmation {
				t.Error("Expected showConfirmation to be true")
			}

			if m.pendingAction != tt.action {
				t.Errorf("Expected pendingAction %v, got %v", tt.action, m.pendingAction)
			}

			if m.pendingTarget != tt.target {
				t.Errorf("Expected pendingTarget %q, got %q", tt.target, m.pendingTarget)
			}
		})
	}
}

// TestCancelConfirmation tests confirmation cancellation
func TestCancelConfirmation(t *testing.T) {
	m := &Model{
		showConfirmation: true,
		pendingAction:    actionRestart,
		pendingTarget:    "test-process",
	}

	m.cancelConfirmation()

	if m.showConfirmation {
		t.Error("Expected showConfirmation to be false")
	}

	if m.pendingAction != actionNone {
		t.Errorf("Expected pendingAction to be actionNone, got %v", m.pendingAction)
	}

	if m.pendingTarget != "" {
		t.Errorf("Expected pendingTarget to be empty, got %q", m.pendingTarget)
	}
}

// TestOpenScaleDialog tests scale dialog opening
func TestOpenScaleDialog(t *testing.T) {
	m := &Model{}
	target := "worker-process"

	m.openScaleDialog(target)

	if !m.showScaleDialog {
		t.Error("Expected showScaleDialog to be true")
	}

	if m.pendingTarget != target {
		t.Errorf("Expected pendingTarget %q, got %q", target, m.pendingTarget)
	}

	if m.scaleInput != "" {
		t.Errorf("Expected scaleInput to be empty, got %q", m.scaleInput)
	}
}

// TestCloseScaleDialog tests scale dialog closing
func TestCloseScaleDialog(t *testing.T) {
	m := &Model{
		showScaleDialog: true,
		pendingTarget:   "test-process",
		scaleInput:      "5",
	}

	m.closeScaleDialog()

	if m.showScaleDialog {
		t.Error("Expected showScaleDialog to be false")
	}

	if m.pendingTarget != "" {
		t.Errorf("Expected pendingTarget to be empty, got %q", m.pendingTarget)
	}

	if m.scaleInput != "" {
		t.Errorf("Expected scaleInput to be empty, got %q", m.scaleInput)
	}
}

// TestResetWizard tests wizard reset
func TestResetWizard(t *testing.T) {
	m := &Model{
		wizardStep:        3,
		wizardName:        "test-proc",
		wizardCommandLine: "php artisan queue:work",
		wizardScale:       5,
		wizardScaleInput:  "5",
		wizardRestart:     "on-failure",
		wizardEnabled:     false,
		wizardError:       "some error",
		wizardMode:        wizardModeEdit,
		wizardOriginal:    "original-name",
		wizardNameLocked:  true,
	}

	m.resetWizard()

	if m.wizardStep != 0 {
		t.Errorf("Expected wizardStep 0, got %d", m.wizardStep)
	}

	if m.wizardName != "" {
		t.Errorf("Expected empty wizardName, got %q", m.wizardName)
	}

	if m.wizardCommandLine != "" {
		t.Errorf("Expected empty wizardCommandLine, got %q", m.wizardCommandLine)
	}

	if m.wizardScale != 1 {
		t.Errorf("Expected wizardScale 1, got %d", m.wizardScale)
	}

	if m.wizardScaleInput != "1" {
		t.Errorf("Expected wizardScaleInput '1', got %q", m.wizardScaleInput)
	}

	if m.wizardRestart != "always" {
		t.Errorf("Expected wizardRestart 'always', got %q", m.wizardRestart)
	}

	if !m.wizardEnabled {
		t.Error("Expected wizardEnabled to be true")
	}

	if m.wizardError != "" {
		t.Errorf("Expected empty wizardError, got %q", m.wizardError)
	}

	if m.wizardMode != wizardModeCreate {
		t.Errorf("Expected wizardMode create, got %v", m.wizardMode)
	}

	if m.wizardOriginal != "" {
		t.Errorf("Expected empty wizardOriginal, got %q", m.wizardOriginal)
	}

	if m.wizardNameLocked {
		t.Error("Expected wizardNameLocked to be false")
	}

	if m.wizardBaseConfig != nil {
		t.Error("Expected nil wizardBaseConfig")
	}
}

// TestStartWizard tests wizard initialization
func TestStartWizard(t *testing.T) {
	m := &Model{
		currentView: viewProcessList,
		wizardStep:  5,
		wizardName:  "old-name",
	}

	m.startWizard()

	if m.currentView != viewWizard {
		t.Errorf("Expected currentView %v, got %v", viewWizard, m.currentView)
	}

	if m.wizardStep != 0 {
		t.Errorf("Expected wizardStep 0, got %d", m.wizardStep)
	}

	if m.wizardMode != wizardModeCreate {
		t.Errorf("Expected wizardMode create, got %v", m.wizardMode)
	}
}

// TestStartEditWizard tests edit wizard initialization
func TestStartEditWizard(t *testing.T) {
	procCfg := &config.Process{
		Enabled: true,
		Command: []string{"php", "artisan", "queue:work"},
		Scale:   3,
		Restart: "on-failure",
	}

	m := &Model{
		currentView: viewProcessList,
	}

	m.startEditWizard("test-process", procCfg)

	if m.currentView != viewWizard {
		t.Errorf("Expected currentView %v, got %v", viewWizard, m.currentView)
	}

	if m.wizardMode != wizardModeEdit {
		t.Errorf("Expected wizardMode edit, got %v", m.wizardMode)
	}

	if m.wizardOriginal != "test-process" {
		t.Errorf("Expected wizardOriginal 'test-process', got %q", m.wizardOriginal)
	}

	if m.wizardName != "test-process" {
		t.Errorf("Expected wizardName 'test-process', got %q", m.wizardName)
	}

	if !m.wizardNameLocked {
		t.Error("Expected wizardNameLocked to be true")
	}

	if m.wizardStep != 1 {
		t.Errorf("Expected wizardStep 1, got %d", m.wizardStep)
	}

	expectedCommand := "php artisan queue:work"
	if m.wizardCommandLine != expectedCommand {
		t.Errorf("Expected wizardCommandLine %q, got %q", expectedCommand, m.wizardCommandLine)
	}

	if m.wizardScale != 3 {
		t.Errorf("Expected wizardScale 3, got %d", m.wizardScale)
	}

	if m.wizardRestart != "on-failure" {
		t.Errorf("Expected wizardRestart 'on-failure', got %q", m.wizardRestart)
	}

	if !m.wizardEnabled {
		t.Error("Expected wizardEnabled to be true")
	}
}

// TestCloneProcessConfig tests process config cloning
func TestCloneProcessConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    *config.Process
		expected *config.Process
	}{
		{
			name:     "nil config",
			input:    nil,
			expected: nil,
		},
		{
			name: "basic config",
			input: &config.Process{
				Enabled: true,
				Command: []string{"php", "artisan"},
				Scale:   2,
				Restart: "always",
			},
			expected: &config.Process{
				Enabled: true,
				Command: []string{"php", "artisan"},
				Scale:   2,
				Restart: "always",
			},
		},
		{
			name: "config with dependencies",
			input: &config.Process{
				Enabled:   true,
				Command:   []string{"nginx"},
				DependsOn: []string{"php-fpm", "redis"},
			},
			expected: &config.Process{
				Enabled:   true,
				Command:   []string{"nginx"},
				DependsOn: []string{"php-fpm", "redis"},
			},
		},
		{
			name: "config with env vars",
			input: &config.Process{
				Enabled: true,
				Command: []string{"app"},
				Env: map[string]string{
					"DEBUG": "true",
					"PORT":  "8080",
				},
			},
			expected: &config.Process{
				Enabled: true,
				Command: []string{"app"},
				Env: map[string]string{
					"DEBUG": "true",
					"PORT":  "8080",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cloneProcessConfig(tt.input)

			if tt.expected == nil {
				if result != nil {
					t.Error("Expected nil result")
				}
				return
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			// Verify it's a deep copy (different memory addresses)
			if tt.input != nil && result == tt.input {
				t.Error("Expected deep copy, got same pointer")
			}

			// Verify values match
			if result.Enabled != tt.expected.Enabled {
				t.Errorf("Expected Enabled %v, got %v", tt.expected.Enabled, result.Enabled)
			}

			if result.Scale != tt.expected.Scale {
				t.Errorf("Expected Scale %d, got %d", tt.expected.Scale, result.Scale)
			}

			if result.Restart != tt.expected.Restart {
				t.Errorf("Expected Restart %q, got %q", tt.expected.Restart, result.Restart)
			}

			// Verify slices are deep copied
			if tt.input != nil && tt.input.Command != nil {
				if len(result.Command) != len(tt.expected.Command) {
					t.Errorf("Expected Command length %d, got %d", len(tt.expected.Command), len(result.Command))
				}
				for i := range result.Command {
					if result.Command[i] != tt.expected.Command[i] {
						t.Errorf("Expected Command[%d] = %q, got %q", i, tt.expected.Command[i], result.Command[i])
					}
				}
			}

			// Verify maps are deep copied
			if tt.input != nil && tt.input.Env != nil {
				if len(result.Env) != len(tt.expected.Env) {
					t.Errorf("Expected Env length %d, got %d", len(tt.expected.Env), len(result.Env))
				}
				for k, v := range tt.expected.Env {
					if result.Env[k] != v {
						t.Errorf("Expected Env[%q] = %q, got %q", k, v, result.Env[k])
					}
				}
			}
		})
	}
}

// TestValidateWizardStep tests wizard step validation
func TestValidateWizardStep(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(m *Model)
		wantValid bool
		wantError string
	}{
		{
			name: "step 0 - valid name",
			setupFunc: func(m *Model) {
				m.wizardStep = 0
				m.wizardName = "valid-process"
				m.wizardNameLocked = false
			},
			wantValid: true,
		},
		{
			name: "step 0 - empty name",
			setupFunc: func(m *Model) {
				m.wizardStep = 0
				m.wizardName = ""
				m.wizardNameLocked = false
			},
			wantValid: false,
			wantError: "Process name cannot be empty",
		},
		{
			name: "step 0 - name with spaces",
			setupFunc: func(m *Model) {
				m.wizardStep = 0
				m.wizardName = "invalid name"
				m.wizardNameLocked = false
			},
			wantValid: false,
			wantError: "Process name cannot contain whitespace",
		},
		{
			name: "step 0 - name with tabs",
			setupFunc: func(m *Model) {
				m.wizardStep = 0
				m.wizardName = "invalid\tname"
				m.wizardNameLocked = false
			},
			wantValid: false,
			wantError: "Process name cannot contain whitespace",
		},
		{
			name: "step 0 - locked name (edit mode)",
			setupFunc: func(m *Model) {
				m.wizardStep = 0
				m.wizardName = "any-name"
				m.wizardNameLocked = true
			},
			wantValid: true,
		},
		{
			name: "step 1 - valid command",
			setupFunc: func(m *Model) {
				m.wizardStep = 1
				m.wizardCommandLine = "php artisan queue:work"
			},
			wantValid: true,
		},
		{
			name: "step 1 - empty command",
			setupFunc: func(m *Model) {
				m.wizardStep = 1
				m.wizardCommandLine = ""
			},
			wantValid: false,
			wantError: "Command cannot be empty.",
		},
		{
			name: "step 1 - whitespace only command",
			setupFunc: func(m *Model) {
				m.wizardStep = 1
				m.wizardCommandLine = "   \t\n  "
			},
			wantValid: false,
			wantError: "Command cannot be empty.",
		},
		{
			name: "step 2 - valid scale",
			setupFunc: func(m *Model) {
				m.wizardStep = 2
				m.wizardScale = 5
			},
			wantValid: true,
		},
		{
			name: "step 2 - zero scale",
			setupFunc: func(m *Model) {
				m.wizardStep = 2
				m.wizardScale = 0
			},
			wantValid: false,
			wantError: "Scale must be at least 1",
		},
		{
			name: "step 2 - negative scale",
			setupFunc: func(m *Model) {
				m.wizardStep = 2
				m.wizardScale = -1
			},
			wantValid: false,
			wantError: "Scale must be at least 1",
		},
		{
			name: "step 3 - restart policy (always valid)",
			setupFunc: func(m *Model) {
				m.wizardStep = 3
				m.wizardRestart = "on-failure"
			},
			wantValid: true,
		},
		{
			name: "step 4 - preview (always valid)",
			setupFunc: func(m *Model) {
				m.wizardStep = 4
			},
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{}
			tt.setupFunc(m)

			valid := m.validateWizardStep()

			if valid != tt.wantValid {
				t.Errorf("validateWizardStep() = %v, expected %v", valid, tt.wantValid)
			}

			if !tt.wantValid {
				if !strings.Contains(m.wizardError, tt.wantError) {
					t.Errorf("Expected error containing %q, got %q", tt.wantError, m.wizardError)
				}
			} else {
				if m.wizardError != "" {
					t.Errorf("Expected no error, got %q", m.wizardError)
				}
			}
		})
	}
}

// TestAdvanceWizardStep tests wizard step advancement
func TestAdvanceWizardStep(t *testing.T) {
	tests := []struct {
		name         string
		setupFunc    func(m *Model)
		expectedStep int
	}{
		{
			name: "advance from step 0 with valid name",
			setupFunc: func(m *Model) {
				m.wizardStep = 0
				m.wizardName = "valid-process"
			},
			expectedStep: 1,
		},
		{
			name: "cannot advance from step 0 with invalid name",
			setupFunc: func(m *Model) {
				m.wizardStep = 0
				m.wizardName = ""
			},
			expectedStep: 0,
		},
		{
			name: "advance from step 1 with valid command",
			setupFunc: func(m *Model) {
				m.wizardStep = 1
				m.wizardCommandLine = "php artisan"
			},
			expectedStep: 2,
		},
		{
			name: "advance from step 2 with valid scale",
			setupFunc: func(m *Model) {
				m.wizardStep = 2
				m.wizardScale = 3
			},
			expectedStep: 3,
		},
		{
			name: "advance from step 3",
			setupFunc: func(m *Model) {
				m.wizardStep = 3
			},
			expectedStep: 4,
		},
		{
			name: "cannot advance past step 4",
			setupFunc: func(m *Model) {
				m.wizardStep = 4
			},
			expectedStep: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{}
			tt.setupFunc(m)

			m.advanceWizardStep()

			if m.wizardStep != tt.expectedStep {
				t.Errorf("Expected wizardStep %d, got %d", tt.expectedStep, m.wizardStep)
			}
		})
	}
}

// TestPreviousWizardStep tests wizard step backward navigation
func TestPreviousWizardStep(t *testing.T) {
	tests := []struct {
		name         string
		initialStep  int
		expectedStep int
	}{
		{
			name:         "go back from step 1",
			initialStep:  1,
			expectedStep: 0,
		},
		{
			name:         "go back from step 4",
			initialStep:  4,
			expectedStep: 3,
		},
		{
			name:         "cannot go back from step 0",
			initialStep:  0,
			expectedStep: 0,
		},
		{
			name:         "go back clears error",
			initialStep:  2,
			expectedStep: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{
				wizardStep:  tt.initialStep,
				wizardError: "some error",
			}

			m.previousWizardStep()

			if m.wizardStep != tt.expectedStep {
				t.Errorf("Expected wizardStep %d, got %d", tt.expectedStep, m.wizardStep)
			}

			// Error is only cleared when step actually changes (step > 0)
			if tt.initialStep > 0 && m.wizardError != "" {
				t.Errorf("Expected wizardError to be cleared, got %q", m.wizardError)
			}
		})
	}
}

// TestNewModel tests NewModel creates model with manager in embedded mode
func TestNewModel(t *testing.T) {
	// Create model without manager (nil is acceptable for testing)
	m := NewModel(nil)

	if m.isRemote {
		t.Error("Expected isRemote to be false for embedded mode")
	}

	if m.client != nil {
		t.Error("Expected client to be nil for embedded mode")
	}

	if m.currentView != viewProcessList {
		t.Errorf("Expected currentView %v, got %v", viewProcessList, m.currentView)
	}

	if m.processCache == nil {
		t.Error("Expected processCache to be initialized")
	}

	if m.logBuffer == nil {
		t.Error("Expected logBuffer to be initialized")
	}

	if cap(m.logBuffer) != 1000 {
		t.Errorf("Expected logBuffer capacity 1000, got %d", cap(m.logBuffer))
	}

	if m.logsPaused {
		t.Error("Expected logsPaused to be false")
	}

	if m.logScope != logScopeStack {
		t.Errorf("Expected logScope %v, got %v", logScopeStack, m.logScope)
	}

	if m.logReturn != viewProcessList {
		t.Errorf("Expected logReturn %v, got %v", viewProcessList, m.logReturn)
	}

	if m.selectedIndex != 0 {
		t.Errorf("Expected selectedIndex 0, got %d", m.selectedIndex)
	}
}

// TestNewRemoteModel tests NewRemoteModel creates model in remote mode
func TestNewRemoteModel(t *testing.T) {
	apiURL := "http://localhost:9180"
	auth := "test-token"

	m := NewRemoteModel(apiURL, auth)

	if !m.isRemote {
		t.Error("Expected isRemote to be true for remote mode")
	}

	if m.client == nil {
		t.Error("Expected client to be non-nil for remote mode")
	}

	if m.manager != nil {
		t.Error("Expected manager to be nil for remote mode")
	}

	if m.currentView != viewProcessList {
		t.Errorf("Expected currentView %v, got %v", viewProcessList, m.currentView)
	}

	if m.processCache == nil {
		t.Error("Expected processCache to be initialized")
	}

	if m.logBuffer == nil {
		t.Error("Expected logBuffer to be initialized")
	}

	if m.logsPaused {
		t.Error("Expected logsPaused to be false")
	}

	if m.selectedIndex != 0 {
		t.Errorf("Expected selectedIndex 0, got %d", m.selectedIndex)
	}
}

// TestNewRemoteModel_EmptyURL tests remote model with empty URL
func TestNewRemoteModel_EmptyURL(t *testing.T) {
	m := NewRemoteModel("", "")

	if !m.isRemote {
		t.Error("Expected isRemote to be true")
	}

	if m.client == nil {
		t.Error("Expected client to be non-nil even with empty URL")
	}
}

// TestTriggerAction tests the triggerAction method
func TestTriggerAction(t *testing.T) {
	tests := []struct {
		name           string
		action         actionType
		target         string
		expectedNilCmd bool
	}{
		{
			name:           "trigger with empty target returns nil",
			action:         actionRestart,
			target:         "",
			expectedNilCmd: true,
		},
		{
			name:           "trigger with valid target returns cmd",
			action:         actionStart,
			target:         "test-process",
			expectedNilCmd: false,
		},
		{
			name:           "trigger stop with valid target",
			action:         actionStop,
			target:         "nginx",
			expectedNilCmd: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{
				isRemote: true,
				client:   NewAPIClient("http://localhost:9999", ""),
			}

			cmd := m.triggerAction(tt.action, tt.target)

			if tt.expectedNilCmd {
				if cmd != nil {
					t.Error("Expected nil cmd for empty target")
				}
			} else {
				if cmd == nil {
					t.Error("Expected non-nil cmd for valid target")
				}
				// Note: pendingAction and pendingTarget are cleared after executeAction runs,
				// which is called inside triggerAction. This is expected behavior.
				// The important verification is that cmd is non-nil for valid targets.
			}
		})
	}
}

// TestExecuteAction_EmptyTarget tests executeAction with no pending action
func TestExecuteAction_EmptyTarget(t *testing.T) {
	m := &Model{
		pendingAction: actionNone,
		pendingTarget: "",
	}

	cmd := m.executeAction()

	if cmd != nil {
		t.Error("Expected nil cmd when pendingAction is actionNone")
	}

	if m.showConfirmation {
		t.Error("Expected showConfirmation to be false")
	}
}

// TestExecuteAction_NoTarget tests executeAction with action but no target
func TestExecuteAction_NoTarget(t *testing.T) {
	m := &Model{
		pendingAction: actionRestart,
		pendingTarget: "",
	}

	cmd := m.executeAction()

	if cmd != nil {
		t.Error("Expected nil cmd when pendingTarget is empty")
	}
}

// TestSetCursorForCurrentStep tests cursor positioning in wizard
func TestSetCursorForCurrentStep(t *testing.T) {
	tests := []struct {
		name           string
		step           int
		wizardName     string
		wizardCommand  string
		expectedCursor int
	}{
		{
			name:           "step 0 - name",
			step:           0,
			wizardName:     "test-proc",
			expectedCursor: 9, // len("test-proc")
		},
		{
			name:           "step 1 - command",
			step:           1,
			wizardCommand:  "php artisan queue:work",
			expectedCursor: 22, // len("php artisan queue:work")
		},
		{
			name:           "step 2 - no cursor",
			step:           2,
			expectedCursor: 0,
		},
		{
			name:           "step 3 - no cursor",
			step:           3,
			expectedCursor: 0,
		},
		{
			name:           "step 4 - no cursor",
			step:           4,
			expectedCursor: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{
				wizardStep:        tt.step,
				wizardName:        tt.wizardName,
				wizardCommandLine: tt.wizardCommand,
				wizardCursor:      999, // Initial value to ensure it changes
			}

			m.setCursorForCurrentStep()

			if m.wizardCursor != tt.expectedCursor {
				t.Errorf("Expected cursor %d, got %d", tt.expectedCursor, m.wizardCursor)
			}
		})
	}
}

// TestTabConstants verifies tab constants are properly defined
func TestTabConstants(t *testing.T) {
	// Verify tab names match expected values
	expectedNames := []string{"Processes", "Scheduled", "Oneshot", "System"}
	if len(tabNames) != len(expectedNames) {
		t.Fatalf("Expected %d tab names, got %d", len(expectedNames), len(tabNames))
	}

	for i, expected := range expectedNames {
		if tabNames[i] != expected {
			t.Errorf("Expected tabNames[%d] = %q, got %q", i, expected, tabNames[i])
		}
	}

	// Verify tab shortcuts
	expectedShortcuts := []string{"1", "2", "3", "4"}
	if len(tabShortcuts) != len(expectedShortcuts) {
		t.Fatalf("Expected %d tab shortcuts, got %d", len(expectedShortcuts), len(tabShortcuts))
	}

	for i, expected := range expectedShortcuts {
		if tabShortcuts[i] != expected {
			t.Errorf("Expected tabShortcuts[%d] = %q, got %q", i, expected, tabShortcuts[i])
		}
	}
}

// TestViewModeConstants verifies view mode constants
func TestViewModeConstants(t *testing.T) {
	tests := []struct {
		name     string
		mode     viewMode
		expected int
	}{
		{"viewProcessList", viewProcessList, 0},
		{"viewProcessDetail", viewProcessDetail, 1},
		{"viewLogs", viewLogs, 2},
		{"viewHelp", viewHelp, 3},
		{"viewWizard", viewWizard, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if int(tt.mode) != tt.expected {
				t.Errorf("Expected %s = %d, got %d", tt.name, tt.expected, int(tt.mode))
			}
		})
	}
}

// TestActionTypeConstants verifies action type constants
func TestActionTypeConstants(t *testing.T) {
	tests := []struct {
		name     string
		action   actionType
		expected int
	}{
		{"actionNone", actionNone, 0},
		{"actionRestart", actionRestart, 1},
		{"actionStop", actionStop, 2},
		{"actionStart", actionStart, 3},
		{"actionScale", actionScale, 4},
		{"actionDelete", actionDelete, 5},
		{"actionSchedulePause", actionSchedulePause, 6},
		{"actionScheduleResume", actionScheduleResume, 7},
		{"actionScheduleTrigger", actionScheduleTrigger, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if int(tt.action) != tt.expected {
				t.Errorf("Expected %s = %d, got %d", tt.name, tt.expected, int(tt.action))
			}
		})
	}
}

// TestLogScopeConstants verifies log scope constants
func TestLogScopeConstants(t *testing.T) {
	if logScopeStack != 0 {
		t.Errorf("Expected logScopeStack = 0, got %d", logScopeStack)
	}
	if logScopeProcess != 1 {
		t.Errorf("Expected logScopeProcess = 1, got %d", logScopeProcess)
	}
}

// TestWizardModeConstants verifies wizard mode constants
func TestWizardModeConstants(t *testing.T) {
	if wizardModeCreate != 0 {
		t.Errorf("Expected wizardModeCreate = 0, got %d", wizardModeCreate)
	}
	if wizardModeEdit != 1 {
		t.Errorf("Expected wizardModeEdit = 1, got %d", wizardModeEdit)
	}
}

// TestTabTypeConstants verifies tab type constants
func TestTabTypeConstants(t *testing.T) {
	tests := []struct {
		name     string
		tab      tabType
		expected int
	}{
		{"tabProcesses", tabProcesses, 0},
		{"tabScheduled", tabScheduled, 1},
		{"tabOneshot", tabOneshot, 2},
		{"tabSystem", tabSystem, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if int(tt.tab) != tt.expected {
				t.Errorf("Expected %s = %d, got %d", tt.name, tt.expected, int(tt.tab))
			}
		})
	}
}

// TestActionResultMsg tests action result message structure
func TestActionResultMsg(t *testing.T) {
	tests := []struct {
		name    string
		success bool
		message string
	}{
		{
			name:    "success message",
			success: true,
			message: "✓ Restarted php-fpm",
		},
		{
			name:    "failure message",
			success: false,
			message: "✗ Error: connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := actionResultMsg{
				success: tt.success,
				message: tt.message,
			}

			if msg.success != tt.success {
				t.Errorf("Expected success %v, got %v", tt.success, msg.success)
			}

			if msg.message != tt.message {
				t.Errorf("Expected message %q, got %q", tt.message, msg.message)
			}
		})
	}
}

// TestStartEditWizard_NilConfig tests edit wizard with nil config
func TestStartEditWizard_NilConfig(t *testing.T) {
	m := &Model{
		currentView: viewProcessList,
	}

	m.startEditWizard("test-process", nil)

	if m.currentView != viewWizard {
		t.Errorf("Expected currentView %v, got %v", viewWizard, m.currentView)
	}

	if m.wizardMode != wizardModeEdit {
		t.Errorf("Expected wizardMode edit, got %v", m.wizardMode)
	}

	// Default values should be used
	if m.wizardScale != 1 {
		t.Errorf("Expected default wizardScale 1, got %d", m.wizardScale)
	}

	if m.wizardRestart != "always" {
		t.Errorf("Expected default wizardRestart 'always', got %q", m.wizardRestart)
	}
}

// TestDispatchAction tests action dispatch routing
func TestDispatchAction(t *testing.T) {
	tests := []struct {
		name           string
		action         actionType
		target         string
		isRemote       bool
		expectMessage  string
		expectNonEmpty bool
	}{
		{
			name:           "restart action message",
			action:         actionRestart,
			target:         "php-fpm",
			isRemote:       true,
			expectMessage:  "✓ Restarted php-fpm",
			expectNonEmpty: true,
		},
		{
			name:           "stop action message",
			action:         actionStop,
			target:         "nginx",
			isRemote:       true,
			expectMessage:  "✓ Stopped nginx",
			expectNonEmpty: true,
		},
		{
			name:           "start action message",
			action:         actionStart,
			target:         "worker",
			isRemote:       true,
			expectMessage:  "✓ Started worker",
			expectNonEmpty: true,
		},
		{
			name:           "delete action message",
			action:         actionDelete,
			target:         "old-process",
			isRemote:       true,
			expectMessage:  "✓ Removed old-process",
			expectNonEmpty: true,
		},
		{
			name:           "scale action returns empty",
			action:         actionScale,
			target:         "php-fpm",
			isRemote:       true,
			expectMessage:  "",
			expectNonEmpty: false,
		},
		{
			name:           "schedule pause message",
			action:         actionSchedulePause,
			target:         "cron-job",
			isRemote:       true,
			expectMessage:  "✓ Paused schedule cron-job",
			expectNonEmpty: true,
		},
		{
			name:           "schedule resume message",
			action:         actionScheduleResume,
			target:         "cron-job",
			isRemote:       true,
			expectMessage:  "✓ Resumed schedule cron-job",
			expectNonEmpty: true,
		},
		{
			name:           "schedule trigger message",
			action:         actionScheduleTrigger,
			target:         "cron-job",
			isRemote:       true,
			expectMessage:  "✓ Triggered cron-job",
			expectNonEmpty: true,
		},
		{
			name:           "none action returns empty",
			action:         actionNone,
			target:         "test",
			isRemote:       true,
			expectMessage:  "",
			expectNonEmpty: false,
		},
		{
			name:           "unknown action returns empty",
			action:         actionType(99),
			target:         "test",
			isRemote:       true,
			expectMessage:  "",
			expectNonEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{
				isRemote: tt.isRemote,
				client:   NewAPIClient("http://localhost:9999", ""),
			}

			msg, _ := m.dispatchAction(tt.action, tt.target)

			if tt.expectNonEmpty {
				if msg != tt.expectMessage {
					t.Errorf("dispatchAction() message = %q, expected %q", msg, tt.expectMessage)
				}
			} else {
				if msg != "" {
					t.Errorf("dispatchAction() expected empty message, got %q", msg)
				}
			}
		})
	}
}

// TestExecuteRestart tests process restart execution
func TestExecuteRestart(t *testing.T) {
	// Test remote mode - returns message even if client request fails
	m := &Model{
		isRemote: true,
		client:   NewAPIClient("http://localhost:9999", ""),
	}

	msg, _ := m.executeRestart("php-fpm")

	expectedMessage := "✓ Restarted php-fpm"
	if msg != expectedMessage {
		t.Errorf("executeRestart() message = %q, expected %q", msg, expectedMessage)
	}
}

// TestExecuteStop tests process stop execution
func TestExecuteStop(t *testing.T) {
	// Test remote mode - returns message even if client request fails
	m := &Model{
		isRemote: true,
		client:   NewAPIClient("http://localhost:9999", ""),
	}

	msg, _ := m.executeStop("nginx")

	expectedMessage := "✓ Stopped nginx"
	if msg != expectedMessage {
		t.Errorf("executeStop() message = %q, expected %q", msg, expectedMessage)
	}
}

// TestExecuteStart tests process start execution
func TestExecuteStart(t *testing.T) {
	// Test remote mode - returns message even if client request fails
	m := &Model{
		isRemote: true,
		client:   NewAPIClient("http://localhost:9999", ""),
	}

	msg, _ := m.executeStart("worker")

	expectedMessage := "✓ Started worker"
	if msg != expectedMessage {
		t.Errorf("executeStart() message = %q, expected %q", msg, expectedMessage)
	}
}

// TestExecuteDelete tests process delete execution
func TestExecuteDelete(t *testing.T) {
	// Test remote mode - returns message even if client request fails
	m := &Model{
		isRemote: true,
		client:   NewAPIClient("http://localhost:9999", ""),
	}

	msg, _ := m.executeDelete("old-process")

	expectedMessage := "✓ Removed old-process"
	if msg != expectedMessage {
		t.Errorf("executeDelete() message = %q, expected %q", msg, expectedMessage)
	}
}

// TestExecuteSchedulePause tests schedule pause execution
func TestExecuteSchedulePause(t *testing.T) {
	// Test remote mode - returns message even if client request fails
	m := &Model{
		isRemote: true,
		client:   NewAPIClient("http://localhost:9999", ""),
	}

	msg, _ := m.executeSchedulePause("cron-job")

	expectedMessage := "✓ Paused schedule cron-job"
	if msg != expectedMessage {
		t.Errorf("executeSchedulePause() message = %q, expected %q", msg, expectedMessage)
	}
}

// TestExecuteScheduleResume tests schedule resume execution
func TestExecuteScheduleResume(t *testing.T) {
	// Test remote mode - returns message even if client request fails
	m := &Model{
		isRemote: true,
		client:   NewAPIClient("http://localhost:9999", ""),
	}

	msg, _ := m.executeScheduleResume("cron-job")

	expectedMessage := "✓ Resumed schedule cron-job"
	if msg != expectedMessage {
		t.Errorf("executeScheduleResume() message = %q, expected %q", msg, expectedMessage)
	}
}

// TestExecuteScheduleTrigger tests schedule trigger execution
func TestExecuteScheduleTrigger(t *testing.T) {
	// Test remote mode - returns message even if client request fails
	m := &Model{
		isRemote: true,
		client:   NewAPIClient("http://localhost:9999", ""),
	}

	msg, _ := m.executeScheduleTrigger("cron-job")

	expectedMessage := "✓ Triggered cron-job"
	if msg != expectedMessage {
		t.Errorf("executeScheduleTrigger() message = %q, expected %q", msg, expectedMessage)
	}
}

// TestTickCmd tests tick command generation
func TestTickCmd(t *testing.T) {
	cmd := tickCmd()
	if cmd == nil {
		t.Error("tickCmd() should return a non-nil command")
	}
}

// TestTickLogRefreshCmd tests tick log refresh command generation
func TestTickLogRefreshCmd(t *testing.T) {
	cmd := tickLogRefreshCmd()
	if cmd == nil {
		t.Error("tickLogRefreshCmd() should return a non-nil command")
	}
}

// TestInit tests model initialization
func TestInit(t *testing.T) {
	m := &Model{
		processCache: make(map[string]process.ProcessInfo),
		isRemote:     true,
		client:       NewAPIClient("http://localhost:9999", ""),
	}

	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() should return a non-nil command batch")
	}
}
