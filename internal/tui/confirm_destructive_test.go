package tui

import "testing"

// TestListViewConfirmsDestructiveActions asserts the process-list view opens a
// confirmation dialog for the destructive actions (restart, stop) and runs the
// non-destructive one (start) immediately — the help text promises exactly this
// and it used to fire restart/stop with no prompt (DX-4).
func TestListViewConfirmsDestructiveActions(t *testing.T) {
	for _, tc := range []struct {
		key, state string
		want       bool
	}{
		{"r", "running", true},  // restart must confirm
		{"x", "running", true},  // stop must confirm (process is running)
		{"s", "stopped", false}, // start is immediate
	} {
		m := Model{
			currentView:   viewProcessList,
			selectedIndex: 0,
			tableData:     []processDisplayRow{{name: "web", rawState: tc.state}},
		}
		handled, newM, _ := m.handleProcessActionKeys(tc.key)
		if !handled {
			t.Fatalf("key %q not handled", tc.key)
		}
		if newM.showConfirmation != tc.want {
			t.Errorf("key %q (state %s): showConfirmation=%v, want %v", tc.key, tc.state, newM.showConfirmation, tc.want)
		}
		if tc.want && newM.pendingTarget != "web" {
			t.Errorf("key %q: pendingTarget=%q, want web", tc.key, newM.pendingTarget)
		}
	}
}
