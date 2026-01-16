package tui

import (
	"testing"

	"github.com/cboxdk/init/internal/process"
)

// TestUpdateProcessTable tests process table update logic
func TestUpdateProcessTable(t *testing.T) {
	tests := []struct {
		name      string
		processes []process.ProcessInfo
		expectLen int
	}{
		{
			name:      "empty processes",
			processes: []process.ProcessInfo{},
			expectLen: 0,
		},
		{
			name: "single process",
			processes: []process.ProcessInfo{
				{Name: "php-fpm", State: "running", DesiredScale: 1, Instances: []process.ProcessInstanceInfo{{ID: "php-fpm-0", State: "running"}}},
			},
			expectLen: 1,
		},
		{
			name: "multiple processes sorted alphabetically",
			processes: []process.ProcessInfo{
				{Name: "nginx", State: "running", DesiredScale: 1},
				{Name: "php-fpm", State: "running", DesiredScale: 2},
				{Name: "horizon", State: "running", DesiredScale: 1},
			},
			expectLen: 3,
		},
		{
			name: "processes with different states",
			processes: []process.ProcessInfo{
				{Name: "running-proc", State: "running", DesiredScale: 1},
				{Name: "failed-proc", State: "failed", DesiredScale: 1},
				{Name: "stopped-proc", State: "stopped", DesiredScale: 1},
			},
			expectLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{
				width:        100,
				height:       30,
				processCache: make(map[string]process.ProcessInfo),
			}
			m.setupProcessTable()

			m.updateProcessTable(tt.processes)

			if len(m.tableData) != tt.expectLen {
				t.Errorf("Expected %d table rows, got %d", tt.expectLen, len(m.tableData))
			}

			// Note: processCache is not updated by updateProcessTable directly,
			// it's updated by applyProcessListResult in the real application
			// So we just verify tableData is correct

			// Verify processes are sorted alphabetically
			if len(m.tableData) > 1 {
				for i := 1; i < len(m.tableData); i++ {
					if m.tableData[i-1].name > m.tableData[i].name {
						t.Errorf("Processes not sorted: %s > %s", m.tableData[i-1].name, m.tableData[i].name)
					}
				}
			}
		})
	}
}

// TestGetSelectedProcess tests selected process retrieval
func TestGetSelectedProcess(t *testing.T) {
	tests := []struct {
		name           string
		tableData      []processDisplayRow
		selectedIndex  int
		expectedResult string
	}{
		{
			name: "valid selection",
			tableData: []processDisplayRow{
				{name: "proc1"},
				{name: "proc2"},
				{name: "proc3"},
			},
			selectedIndex:  1,
			expectedResult: "proc2",
		},
		{
			name: "first process",
			tableData: []processDisplayRow{
				{name: "proc1"},
			},
			selectedIndex:  0,
			expectedResult: "proc1",
		},
		{
			name:           "empty table",
			tableData:      []processDisplayRow{},
			selectedIndex:  0,
			expectedResult: "",
		},
		{
			name: "index out of range",
			tableData: []processDisplayRow{
				{name: "proc1"},
			},
			selectedIndex:  5,
			expectedResult: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				tableData:     tt.tableData,
				selectedIndex: tt.selectedIndex,
			}

			result := m.getSelectedProcess()
			if result != tt.expectedResult {
				t.Errorf("getSelectedProcess() = %q, expected %q", result, tt.expectedResult)
			}
		})
	}
}

// TestGetSelectedProcessInfo tests selected process display row retrieval
func TestGetSelectedProcessInfo(t *testing.T) {
	tests := []struct {
		name        string
		tableData   []processDisplayRow
		selectedIdx int
		expectNil   bool
	}{
		{
			name: "valid selection",
			tableData: []processDisplayRow{
				{name: "test-process", state: "running"},
			},
			selectedIdx: 0,
			expectNil:   false,
		},
		{
			name: "index out of range",
			tableData: []processDisplayRow{
				{name: "test-process"},
			},
			selectedIdx: 5,
			expectNil:   true,
		},
		{
			name:        "empty table",
			tableData:   []processDisplayRow{},
			selectedIdx: 0,
			expectNil:   true,
		},
		{
			name: "negative index",
			tableData: []processDisplayRow{
				{name: "test"},
			},
			selectedIdx: -1,
			expectNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				tableData:     tt.tableData,
				selectedIndex: tt.selectedIdx,
			}

			result := m.getSelectedProcessInfo()
			if tt.expectNil && result != nil {
				t.Error("Expected nil, got process info")
			}
			if !tt.expectNil && result == nil {
				t.Error("Expected process info, got nil")
			}
		})
	}
}

// TestMoveSelection tests selection movement
func TestMoveSelection(t *testing.T) {
	tests := []struct {
		name          string
		tableLen      int
		initialCursor int
		delta         int
		expectedIdx   int
	}{
		{
			name:          "move down in middle",
			tableLen:      5,
			initialCursor: 2,
			delta:         1,
			expectedIdx:   3,
		},
		{
			name:          "move up in middle",
			tableLen:      5,
			initialCursor: 2,
			delta:         -1,
			expectedIdx:   1,
		},
		{
			name:          "clamp at top",
			tableLen:      5,
			initialCursor: 0,
			delta:         -1,
			expectedIdx:   0,
		},
		{
			name:          "clamp at bottom",
			tableLen:      5,
			initialCursor: 4,
			delta:         1,
			expectedIdx:   4,
		},
		{
			name:          "empty table",
			tableLen:      0,
			initialCursor: 0,
			delta:         1,
			expectedIdx:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{
				tableData:     make([]processDisplayRow, tt.tableLen),
				selectedIndex: tt.initialCursor,
				height:        30,
			}

			m.moveSelection(tt.delta)

			if m.selectedIndex != tt.expectedIdx {
				t.Errorf("Expected selectedIndex %d, got %d", tt.expectedIdx, m.selectedIndex)
			}
		})
	}
}

// TestSetSelection tests direct selection setting
func TestSetSelection(t *testing.T) {
	tests := []struct {
		name        string
		tableLen    int
		setIndex    int
		expectedIdx int
	}{
		{
			name:        "valid index",
			tableLen:    5,
			setIndex:    2,
			expectedIdx: 2,
		},
		{
			name:        "index too high clamped",
			tableLen:    5,
			setIndex:    10,
			expectedIdx: 4,
		},
		{
			name:        "negative index clamped to 0",
			tableLen:    5,
			setIndex:    -1,
			expectedIdx: 0,
		},
		{
			name:        "empty table",
			tableLen:    0,
			setIndex:    0,
			expectedIdx: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{
				tableData: make([]processDisplayRow, tt.tableLen),
				height:    30,
			}

			m.setSelection(tt.setIndex)

			if m.selectedIndex != tt.expectedIdx {
				t.Errorf("Expected selectedIndex %d, got %d", tt.expectedIdx, m.selectedIndex)
			}
		})
	}
}

// TestUpdateComponentSizes tests component size updates
func TestUpdateComponentSizes(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
	}{
		{
			name:   "normal dimensions",
			width:  100,
			height: 30,
		},
		{
			name:   "small dimensions",
			width:  50,
			height: 10,
		},
		{
			name:   "zero dimensions",
			width:  0,
			height: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{
				width:  tt.width,
				height: tt.height,
			}
			m.setupProcessTable()

			// Should not panic
			m.updateComponentSizes()

			// Verify table dimensions updated
			if tt.width > 0 && tt.height > 0 {
				if m.processTable.Columns() == nil {
					t.Error("Expected process table columns to be set")
				}
			}
		})
	}
}
