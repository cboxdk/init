package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// TestWizardOwnsItsKeyboard: the global q/? bindings ran before the view
// dispatch, so typing the wizard's own suggested command — `php artisan
// queue:work` — aborted at the "q" and threw the half-filled form away.
func TestWizardOwnsItsKeyboard(t *testing.T) {
	m := Model{currentView: viewWizard, wizardStep: 0}

	for _, r := range "queue" {
		next, _ := m.handleKeyPress(key(string(r)))
		m = next.(Model)
	}

	if m.currentView != viewWizard {
		t.Fatalf("wizard exited to view %v while the user was typing a name", m.currentView)
	}
	if m.wizardName != "queue" {
		t.Errorf("wizardName = %q, want %q", m.wizardName, "queue")
	}

	// "?" must not jump to help either.
	next, _ := m.handleKeyPress(key("?"))
	m = next.(Model)
	if m.currentView != viewWizard {
		t.Errorf("? left the wizard for view %v", m.currentView)
	}

	// esc still cancels, so the user is never trapped.
	next, _ = m.handleKeyPress(key("esc"))
	if next.(Model).currentView != viewProcessList {
		t.Error("esc no longer cancels the wizard")
	}
}

// TestCtrlCQuitsFromWizard: the wizard owns the keyboard, but ctrl+c must still
// quit from anywhere.
func TestCtrlCQuitsFromWizard(t *testing.T) {
	for _, m := range []Model{
		{currentView: viewWizard, wizardStep: 1},
		{currentView: viewProcessList, showConfirmation: true},
		{currentView: viewProcessList, showScaleDialog: true},
	} {
		if _, cmd := m.handleKeyPress(key("ctrl+c")); cmd == nil {
			t.Errorf("ctrl+c did not quit (view=%v confirm=%v scale=%v)",
				m.currentView, m.showConfirmation, m.showScaleDialog)
		}
	}
}

// TestViewOnlyTabsHaveNoActionTarget: navigation is tab-aware but selection was
// not, so selectedIndex stayed pointing at an unrendered Processes-tab row.
// Pressing "+" on the view-only Oneshot tab scaled a process the operator could
// not see.
func TestViewOnlyTabsHaveNoActionTarget(t *testing.T) {
	rows := []processDisplayRow{{name: "nginx"}, {name: "php-fpm"}}

	for _, tab := range []tabType{tabOneshot, tabSystem} {
		m := &Model{activeTab: tab, selectedIndex: 1, tableData: rows}

		if got := m.getSelectedProcess(); got != "" {
			t.Errorf("tab %d resolved an action target %q from an invisible row", tab, got)
		}
		if got := m.getSelectedProcessInfo(); got != nil {
			t.Errorf("tab %d resolved process info %q from an invisible row", tab, got.name)
		}
	}

	// The Processes tab still resolves normally.
	m := &Model{activeTab: tabProcesses, selectedIndex: 1, tableData: rows}
	if got := m.getSelectedProcess(); got != "php-fpm" {
		t.Errorf("Processes tab resolved %q, want php-fpm", got)
	}
}

// TestShrinkingListClampsScrollOffset: the refresh paths clamped the cursor but
// not the offset, so scrolling to the bottom of a long list and reloading into a
// short one left the viewport past the end — a header and zero rows, until the
// user happened to press a nav key.
func TestShrinkingListClampsScrollOffset(t *testing.T) {
	m := &Model{width: 120, height: 40}

	m.scheduledData = make([]scheduledDisplayRow, 50)
	m.scheduledIndex = 49
	m.scheduledOffset = 25
	m.scheduledData = m.scheduledData[:5]
	m.scheduledIndex = 4
	m.ensureScheduledCursorVisible()
	if m.scheduledOffset > len(m.scheduledData)-1 {
		t.Errorf("scheduledOffset = %d past the end of a %d-row list",
			m.scheduledOffset, len(m.scheduledData))
	}

	m.oneshotData = make([]oneshotDisplayRow, 50)
	m.oneshotIndex = 49
	m.oneshotOffset = 25
	m.oneshotData = m.oneshotData[:5]
	m.oneshotIndex = 4
	m.ensureOneshotCursorVisible()
	if m.oneshotOffset > len(m.oneshotData)-1 {
		t.Errorf("oneshotOffset = %d past the end of a %d-row list",
			m.oneshotOffset, len(m.oneshotData))
	}
}
