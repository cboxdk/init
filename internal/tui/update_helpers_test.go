package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/table"
	"github.com/cboxdk/init/internal/process"
)

// TestGetSelectedInstanceID tests instance ID retrieval
func TestGetSelectedInstanceID(t *testing.T) {
	tests := []struct {
		name       string
		setupFunc  func(m *Model)
		expectedID string
	}{
		{
			name: "no columns returns empty",
			setupFunc: func(m *Model) {
				// No setup - instanceTable will be nil/empty
			},
			expectedID: "",
		},
		{
			name: "empty rows returns empty",
			setupFunc: func(m *Model) {
				m.setupInstanceTable()
				m.instanceTable.SetRows([]table.Row{})
			},
			expectedID: "",
		},
		{
			name: "with selected row returns ID",
			setupFunc: func(m *Model) {
				m.setupInstanceTable()
				rows := []table.Row{
					{"instance-1", "running", "12345", "1.2%", "128 MB", "10m", "0"},
					{"instance-2", "running", "12346", "2.3%", "256 MB", "20m", "1"},
				}
				m.instanceTable.SetRows(rows)
				// Table default selection is first row (index 0)
			},
			expectedID: "instance-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{
				width:  100,
				height: 30,
			}
			if tt.setupFunc != nil {
				tt.setupFunc(m)
			}

			result := m.getSelectedInstanceID()
			if result != tt.expectedID {
				t.Errorf("getSelectedInstanceID() = %q, expected %q", result, tt.expectedID)
			}
		})
	}
}

// TestOpenProcessDetail tests opening process detail view
func TestOpenProcessDetail(t *testing.T) {
	tests := []struct {
		name         string
		processName  string
		setupFunc    func(m *Model)
		expectedView viewMode
		expectedProc string
	}{
		{
			name:         "opens detail view with process name",
			processName:  "php-fpm",
			setupFunc:    func(m *Model) {},
			expectedView: viewProcessDetail,
			expectedProc: "php-fpm",
		},
		{
			name:        "initializes instance table if needed",
			processName: "nginx",
			setupFunc: func(m *Model) {
				// Instance table not initialized
			},
			expectedView: viewProcessDetail,
			expectedProc: "nginx",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{
				width:  100,
				height: 30,
			}
			if tt.setupFunc != nil {
				tt.setupFunc(m)
			}

			m.openProcessDetail(tt.processName)

			if m.currentView != tt.expectedView {
				t.Errorf("currentView = %v, expected %v", m.currentView, tt.expectedView)
			}
			if m.detailProc != tt.expectedProc {
				t.Errorf("detailProc = %q, expected %q", m.detailProc, tt.expectedProc)
			}
			if m.instanceTable.Columns() == nil {
				t.Error("Expected instanceTable to be initialized")
			}
		})
	}
}

// TestUpdateInstanceTableFromCache tests instance table update from cache
func TestUpdateInstanceTableFromCache(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(m *Model)
		checkFunc func(t *testing.T, m *Model)
	}{
		{
			name: "empty detailProc updates with nil",
			setupFunc: func(m *Model) {
				m.detailProc = ""
				m.setupInstanceTable()
			},
			checkFunc: func(t *testing.T, m *Model) {
				// Should update with nil - table should be empty
				if len(m.instanceTable.Rows()) != 0 {
					t.Error("Expected instance table to be empty")
				}
			},
		},
		{
			name: "process exists in cache - updates table",
			setupFunc: func(m *Model) {
				m.detailProc = "php-fpm"
				m.setupInstanceTable()
				m.processCache = map[string]process.ProcessInfo{
					"php-fpm": {
						Name:  "php-fpm",
						State: "running",
						Instances: []process.ProcessInstanceInfo{
							{ID: "php-fpm-0", State: "running", PID: 12345},
							{ID: "php-fpm-1", State: "running", PID: 12346},
						},
					},
				}
			},
			checkFunc: func(t *testing.T, m *Model) {
				// Should update with process info - table should have 2 rows
				if len(m.instanceTable.Rows()) != 2 {
					t.Errorf("Expected 2 instance table rows, got %d", len(m.instanceTable.Rows()))
				}
			},
		},
		{
			name: "process not in cache - updates with nil",
			setupFunc: func(m *Model) {
				m.detailProc = "missing"
				m.setupInstanceTable()
				m.processCache = map[string]process.ProcessInfo{
					"php-fpm": {Name: "php-fpm", State: "running"},
				}
			},
			checkFunc: func(t *testing.T, m *Model) {
				// Should update with nil - table should be empty
				if len(m.instanceTable.Rows()) != 0 {
					t.Error("Expected instance table to be empty for missing process")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{
				width:  100,
				height: 30,
			}
			if tt.setupFunc != nil {
				tt.setupFunc(m)
			}

			m.updateInstanceTableFromCache()

			if tt.checkFunc != nil {
				tt.checkFunc(t, m)
			}
		})
	}
}
