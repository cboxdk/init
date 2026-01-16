package tui

import (
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/cboxdk/init/internal/process"
)

// TestDefaultTableHeight tests table height calculation
func TestDefaultTableHeight(t *testing.T) {
	tests := []struct {
		name           string
		modelHeight    int
		expectedHeight int
	}{
		{
			name:           "normal height",
			modelHeight:    30,
			expectedHeight: 25,
		},
		{
			name:           "small height",
			modelHeight:    10,
			expectedHeight: 5,
		},
		{
			name:           "very small height",
			modelHeight:    3,
			expectedHeight: 5,
		},
		{
			name:           "zero height",
			modelHeight:    0,
			expectedHeight: 5,
		},
		{
			name:           "negative height",
			modelHeight:    -5,
			expectedHeight: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{height: tt.modelHeight}
			result := m.defaultTableHeight()
			if result != tt.expectedHeight {
				t.Errorf("defaultTableHeight() = %d, expected %d", result, tt.expectedHeight)
			}
		})
	}
}

// TestDetailTableHeight tests detail table height calculation
func TestDetailTableHeight(t *testing.T) {
	tests := []struct {
		name           string
		modelHeight    int
		expectedHeight int
	}{
		{
			name:           "normal height",
			modelHeight:    30,
			expectedHeight: 23,
		},
		{
			name:           "small height",
			modelHeight:    10,
			expectedHeight: 6,
		},
		{
			name:           "very small height",
			modelHeight:    5,
			expectedHeight: 6,
		},
		{
			name:           "zero height",
			modelHeight:    0,
			expectedHeight: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{height: tt.modelHeight}
			result := m.detailTableHeight()
			if result != tt.expectedHeight {
				t.Errorf("detailTableHeight() = %d, expected %d", result, tt.expectedHeight)
			}
		})
	}
}

// TestDetailTableWidth tests detail table width calculation
func TestDetailTableWidth(t *testing.T) {
	tests := []struct {
		name          string
		modelWidth    int
		expectedWidth int
	}{
		{
			name:          "normal width",
			modelWidth:    100,
			expectedWidth: 100,
		},
		{
			name:          "zero width",
			modelWidth:    0,
			expectedWidth: 80,
		},
		{
			name:          "negative width",
			modelWidth:    -10,
			expectedWidth: 80,
		},
		{
			name:          "small width",
			modelWidth:    50,
			expectedWidth: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{width: tt.modelWidth}
			result := m.detailTableWidth()
			if result != tt.expectedWidth {
				t.Errorf("detailTableWidth() = %d, expected %d", result, tt.expectedWidth)
			}
		})
	}
}

// TestDefaultColumns tests default column definitions
func TestDefaultColumns(t *testing.T) {
	m := &Model{}
	columns := m.defaultColumns()

	expectedTitles := []string{"NAME", "TYPE", "STATE", "HEALTH", "SCALE", "CPU", "RAM", "UPTIME", "RESTARTS"}
	expectedWidths := []int{18, 10, 14, 12, 10, 8, 10, 12, 10}

	if len(columns) != len(expectedTitles) {
		t.Fatalf("Expected %d columns, got %d", len(expectedTitles), len(columns))
	}

	for i, col := range columns {
		if col.Title != expectedTitles[i] {
			t.Errorf("Column %d: expected title %q, got %q", i, expectedTitles[i], col.Title)
		}

		if col.Width != expectedWidths[i] {
			t.Errorf("Column %d: expected width %d, got %d", i, expectedWidths[i], col.Width)
		}
	}
}

// TestComputeInstanceColumns tests instance column computation
func TestComputeInstanceColumns(t *testing.T) {
	tests := []struct {
		name           string
		modelWidth     int
		expectedTitles []string
		minFirstWidth  int
	}{
		{
			name:           "normal width",
			modelWidth:     120,
			expectedTitles: []string{"ID", "STATE", "PID", "CPU", "RAM", "UPTIME", "RESTARTS"},
			minFirstWidth:  18,
		},
		{
			name:           "wide width",
			modelWidth:     200,
			expectedTitles: []string{"ID", "STATE", "PID", "CPU", "RAM", "UPTIME", "RESTARTS"},
			minFirstWidth:  50,
		},
		{
			name:           "narrow width",
			modelWidth:     80,
			expectedTitles: []string{"ID", "STATE", "PID", "CPU", "RAM", "UPTIME", "RESTARTS"},
			minFirstWidth:  18,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{width: tt.modelWidth}
			columns := m.computeInstanceColumns()

			if len(columns) != len(tt.expectedTitles) {
				t.Fatalf("Expected %d columns, got %d", len(tt.expectedTitles), len(columns))
			}

			for i, col := range columns {
				if col.Title != tt.expectedTitles[i] {
					t.Errorf("Column %d: expected title %q, got %q", i, tt.expectedTitles[i], col.Title)
				}
			}

			// First column should get extra space
			if columns[0].Width < tt.minFirstWidth {
				t.Errorf("First column width %d should be at least %d", columns[0].Width, tt.minFirstWidth)
			}
		})
	}
}

// TestEnsureCursorVisible tests cursor visibility logic
func TestEnsureCursorVisible(t *testing.T) {
	tests := []struct {
		name           string
		tableDataLen   int
		initialOffset  int
		initialCursor  int
		modelHeight    int
		expectedOffset int
	}{
		{
			name:           "cursor above viewport",
			tableDataLen:   20,
			initialOffset:  5,
			initialCursor:  3,
			modelHeight:    15,
			expectedOffset: 3,
		},
		{
			name:           "cursor below viewport",
			tableDataLen:   20,
			initialOffset:  0,
			initialCursor:  15,
			modelHeight:    15,
			expectedOffset: 6, // cursor(15) - height(10) + 1 = 6
		},
		{
			name:           "cursor in viewport",
			tableDataLen:   20,
			initialOffset:  5,
			initialCursor:  8,
			modelHeight:    15,
			expectedOffset: 5,
		},
		{
			name:           "offset exceeds max",
			tableDataLen:   10,
			initialOffset:  15,
			initialCursor:  5,
			modelHeight:    15,
			expectedOffset: 0,
		},
		{
			name:           "negative offset corrected",
			tableDataLen:   20,
			initialOffset:  -5,
			initialCursor:  5,
			modelHeight:    15,
			expectedOffset: 0,
		},
		{
			name:           "empty table",
			tableDataLen:   0,
			initialOffset:  5,
			initialCursor:  0,
			modelHeight:    15,
			expectedOffset: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{
				tableData:     make([]processDisplayRow, tt.tableDataLen),
				tableOffset:   tt.initialOffset,
				selectedIndex: tt.initialCursor,
				height:        tt.modelHeight,
			}

			m.ensureCursorVisible()

			if m.tableOffset != tt.expectedOffset {
				t.Errorf("tableOffset = %d, expected %d", m.tableOffset, tt.expectedOffset)
			}

			if m.tableOffset < 0 {
				t.Errorf("tableOffset should not be negative, got %d", m.tableOffset)
			}
		})
	}
}

// TestSetupProcessTable tests process table initialization
func TestSetupProcessTable(t *testing.T) {
	tests := []struct {
		name          string
		width         int
		height        int
		initialCursor int
		expectFocused bool
	}{
		{
			name:          "normal setup",
			width:         100,
			height:        30,
			initialCursor: -1,
			expectFocused: true,
		},
		{
			name:          "small dimensions",
			width:         50,
			height:        10,
			initialCursor: 0,
			expectFocused: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{
				width:         tt.width,
				height:        tt.height,
				selectedIndex: tt.initialCursor,
			}

			m.setupProcessTable()

			if m.processTable.Columns() == nil {
				t.Error("Expected columns to be set")
			}

			if m.selectedIndex < 0 {
				t.Errorf("selectedIndex should be >= 0, got %d", m.selectedIndex)
			}
		})
	}
}

// TestSetupInstanceTable tests instance table initialization
func TestSetupInstanceTable(t *testing.T) {
	m := &Model{
		width:  100,
		height: 30,
	}

	m.setupInstanceTable()

	if m.instanceTable.Columns() == nil {
		t.Error("Expected columns to be set")
	}

	if m.instanceColumns == nil {
		t.Error("Expected instance columns to be set")
	}

	if len(m.instanceColumns) == 0 {
		t.Error("Expected non-empty instance columns")
	}
}

// TestRenderProcessTable tests process table rendering
func TestRenderProcessTable(t *testing.T) {
	tests := []struct {
		name            string
		tableDataLen    int
		expectNoProcess bool
	}{
		{
			name:            "empty table",
			tableDataLen:    0,
			expectNoProcess: true,
		},
		{
			name:            "table with data",
			tableDataLen:    5,
			expectNoProcess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				tableData:         make([]processDisplayRow, tt.tableDataLen),
				tableColumnWidths: []int{18, 10, 14, 12, 10, 8, 10, 12, 10},
				width:             100,
				height:            30,
			}

			// Initialize with sample data if not empty
			if tt.tableDataLen > 0 {
				for i := 0; i < tt.tableDataLen; i++ {
					m.tableData[i] = processDisplayRow{
						name:        "test-proc",
						procType:    "service",
						state:       "running",
						health:      "healthy",
						scale:       "1/1",
						cpuUsage:    "2.5%",
						memoryUsage: "128 MB",
						uptime:      "1h 30m",
						restarts:    "0",
					}
				}
			}

			result := m.renderProcessTable()

			if tt.expectNoProcess {
				if result != "No processes found" {
					t.Errorf("Expected 'No processes found', got %q", result)
				}
			} else {
				if len(result) == 0 {
					t.Error("Expected non-empty result")
				}
				// Should contain header
				if !containsAny(result, []string{"NAME", "TYPE", "STATE"}) {
					t.Error("Expected result to contain table headers")
				}
			}
		})
	}
}

// Helper function for tests
func containsAny(s string, substrs []string) bool {
	for _, substr := range substrs {
		if len(s) >= len(substr) {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
		}
	}
	return false
}

// TestUpdateInstanceTable tests instance table updates
func TestUpdateInstanceTable(t *testing.T) {
	m := &Model{
		width:  100,
		height: 30,
	}

	// Test with nil process - should initialize table
	m.updateInstanceTable(nil)

	// Cursor should be at 0 or -1 for empty rows (bubbletea default behavior)
	cursor := m.instanceTable.Cursor()
	if cursor != 0 && cursor != -1 {
		t.Errorf("Expected cursor to be at 0 or -1 for nil process, got %d", cursor)
	}

	// Verify table was updated
	if m.instanceTable.Columns() == nil {
		t.Error("Expected columns to be set")
	}

	// Verify rows are empty for nil process
	rows := m.instanceTable.Rows()
	if len(rows) != 0 {
		t.Errorf("Expected 0 rows for nil process, got %d", len(rows))
	}
}

// TestProcessTableState tests process table state management
func TestProcessTableState(t *testing.T) {
	m := &Model{
		width:         100,
		height:        30,
		selectedIndex: -1,
	}

	// Initialize with previous rows
	m.setupProcessTable()
	prevRows := []table.Row{{"process1"}, {"process2"}}
	m.processTable.SetRows(prevRows)

	// Setup again - should preserve rows
	m.setupProcessTable()

	rows := m.processTable.Rows()
	if len(rows) != len(prevRows) {
		t.Errorf("Expected %d rows to be preserved, got %d", len(prevRows), len(rows))
	}
}

// TestScheduledColumns tests scheduled columns definition
func TestScheduledColumns(t *testing.T) {
	m := &Model{}
	columns := m.scheduledColumns()

	expectedTitles := []string{"NAME", "SCHEDULE", "STATE", "LAST RUN", "NEXT RUN", "EXIT", "RUNS", "SUCCESS"}
	expectedWidths := []int{18, 14, 12, 16, 16, 6, 6, 8}

	if len(columns) != len(expectedTitles) {
		t.Fatalf("Expected %d columns, got %d", len(expectedTitles), len(columns))
	}

	for i, col := range columns {
		if col.Title != expectedTitles[i] {
			t.Errorf("Column %d: expected title %q, got %q", i, expectedTitles[i], col.Title)
		}
		if col.Width != expectedWidths[i] {
			t.Errorf("Column %d: expected width %d, got %d", i, expectedWidths[i], col.Width)
		}
	}
}

// TestScheduleStateDisplay tests state display formatting
func TestScheduleStateDisplay(t *testing.T) {
	tests := []struct {
		state        string
		expectedText string
	}{
		{"idle", "⏰ Scheduled"},
		{"executing", "▶ Running"},
		{"paused", "⏸ Paused"},
		{"unknown", "unknown"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			text, _ := scheduleStateDisplay(tt.state)
			if text != tt.expectedText {
				t.Errorf("scheduleStateDisplay(%q) = %q, expected %q", tt.state, text, tt.expectedText)
			}
		})
	}
}

// TestFormatNextRun tests next run time formatting
func TestFormatNextRun(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		nextRun  int64
		expected string
	}{
		{"zero", 0, "-"},
		{"negative", -1, "-"},
		{"past", now.Add(-time.Hour).Unix(), "now"},
		{"seconds", now.Add(30 * time.Second).Unix(), "in 30s"},
		{"minutes", now.Add(5 * time.Minute).Unix(), "in 5m"},
		{"hours", now.Add(2*time.Hour + 30*time.Minute).Unix(), "in 2h30m"},
		{"days", now.Add(48 * time.Hour).Unix(), "in 2d0h"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatNextRun(tt.nextRun)
			// For time-based tests, we need some tolerance
			if tt.name == "zero" || tt.name == "negative" || tt.name == "past" {
				if result != tt.expected {
					t.Errorf("formatNextRun(%d) = %q, expected %q", tt.nextRun, result, tt.expected)
				}
			} else {
				// Just verify format pattern for dynamic values
				if len(result) == 0 {
					t.Errorf("formatNextRun(%d) returned empty string", tt.nextRun)
				}
				if result[0:2] != "in" {
					t.Errorf("formatNextRun(%d) = %q, expected to start with 'in'", tt.nextRun, result)
				}
			}
		})
	}
}

// TestFormatLastRun tests last run time formatting
func TestFormatLastRun(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		lastRun  int64
		expected string
	}{
		{"zero", 0, "never"},
		{"negative", -1, "never"},
		{"seconds", now.Add(-30 * time.Second).Unix(), "30s ago"},
		{"minutes", now.Add(-5 * time.Minute).Unix(), "5m ago"},
		{"hours", now.Add(-2*time.Hour - 30*time.Minute).Unix(), "2h30m ago"},
		{"days", now.Add(-48 * time.Hour).Unix(), "2d0h ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatLastRun(tt.lastRun)
			// For time-based tests, we need some tolerance
			if tt.name == "zero" || tt.name == "negative" {
				if result != tt.expected {
					t.Errorf("formatLastRun(%d) = %q, expected %q", tt.lastRun, result, tt.expected)
				}
			} else {
				// Just verify format pattern for dynamic values
				if len(result) == 0 {
					t.Errorf("formatLastRun(%d) returned empty string", tt.lastRun)
				}
				if !containsAny(result, []string{"ago"}) {
					t.Errorf("formatLastRun(%d) = %q, expected to contain 'ago'", tt.lastRun, result)
				}
			}
		})
	}
}

// TestRenderScheduledTable tests scheduled table rendering
func TestRenderScheduledTable(t *testing.T) {
	tests := []struct {
		name           string
		scheduledData  int
		expectNoJobs   bool
	}{
		{"empty table", 0, true},
		{"with data", 3, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				scheduledData: make([]scheduledDisplayRow, tt.scheduledData),
				width:         100,
				height:        30,
			}

			// Initialize with sample data if not empty
			if tt.scheduledData > 0 {
				for i := 0; i < tt.scheduledData; i++ {
					m.scheduledData[i] = scheduledDisplayRow{
						name:         "test-cron",
						schedule:     "* * * * *",
						state:        "⏰ Scheduled",
						lastRun:      "5m ago",
						nextRun:      "in 55s",
						lastExitCode: "0",
						runCount:     5,
						successRate:  "100%",
					}
				}
			}

			result := m.renderScheduledTable()

			if tt.expectNoJobs {
				if result != "No scheduled jobs found" {
					t.Errorf("Expected 'No scheduled jobs found', got %q", result)
				}
			} else {
				if len(result) == 0 {
					t.Error("Expected non-empty result")
				}
				// Should contain headers
				if !containsAny(result, []string{"NAME", "SCHEDULE", "STATE"}) {
					t.Error("Expected result to contain table headers")
				}
			}
		})
	}
}

// TestGetSelectedScheduledName tests getting selected scheduled job name
func TestGetSelectedScheduledName(t *testing.T) {
	tests := []struct {
		name           string
		scheduledData  []scheduledDisplayRow
		scheduledIndex int
		expectedName   string
	}{
		{
			name:           "valid selection",
			scheduledData:  []scheduledDisplayRow{{name: "cron-job-1"}, {name: "cron-job-2"}},
			scheduledIndex: 1,
			expectedName:   "cron-job-2",
		},
		{
			name:           "first item",
			scheduledData:  []scheduledDisplayRow{{name: "cron-job-1"}},
			scheduledIndex: 0,
			expectedName:   "cron-job-1",
		},
		{
			name:           "negative index",
			scheduledData:  []scheduledDisplayRow{{name: "cron-job-1"}},
			scheduledIndex: -1,
			expectedName:   "",
		},
		{
			name:           "index out of bounds",
			scheduledData:  []scheduledDisplayRow{{name: "cron-job-1"}},
			scheduledIndex: 5,
			expectedName:   "",
		},
		{
			name:           "empty data",
			scheduledData:  []scheduledDisplayRow{},
			scheduledIndex: 0,
			expectedName:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{
				scheduledData:  tt.scheduledData,
				scheduledIndex: tt.scheduledIndex,
			}

			result := m.getSelectedScheduledName()
			if result != tt.expectedName {
				t.Errorf("getSelectedScheduledName() = %q, expected %q", result, tt.expectedName)
			}
		})
	}
}

// TestRenderOneshotTable tests oneshot table rendering
func TestRenderOneshotTable(t *testing.T) {
	tests := []struct {
		name         string
		oneshotData  int
		expectNoData bool
	}{
		{"empty table", 0, true},
		{"with data", 3, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				oneshotData: make([]oneshotDisplayRow, tt.oneshotData),
				width:       100,
				height:      30,
			}

			// Initialize with sample data if not empty
			if tt.oneshotData > 0 {
				for i := 0; i < tt.oneshotData; i++ {
					m.oneshotData[i] = oneshotDisplayRow{
						id:          int64(i + 1),
						processName: "oneshot-task",
						instanceID:  "task-001",
						startedAt:   "2m ago",
						duration:    "1.5s",
						status:      "completed",
						exitCode:    "0",
						triggerType: "manual",
					}
				}
			}

			result := m.renderOneshotTable()

			if tt.expectNoData {
				if result != "No oneshot executions recorded" {
					t.Errorf("Expected 'No oneshot executions recorded', got %q", result)
				}
			} else {
				if len(result) == 0 {
					t.Error("Expected non-empty result")
				}
				// Should contain headers
				if !containsAny(result, []string{"PROCESS", "INSTANCE", "STATUS"}) {
					t.Error("Expected result to contain table headers")
				}
			}
		})
	}
}

// TestGetSelectedOneshotID tests getting selected oneshot execution ID
func TestGetSelectedOneshotID(t *testing.T) {
	tests := []struct {
		name         string
		oneshotData  []oneshotDisplayRow
		oneshotIndex int
		expectedID   int64
	}{
		{
			name:         "valid selection",
			oneshotData:  []oneshotDisplayRow{{id: 100}, {id: 200}},
			oneshotIndex: 1,
			expectedID:   200,
		},
		{
			name:         "first item",
			oneshotData:  []oneshotDisplayRow{{id: 100}},
			oneshotIndex: 0,
			expectedID:   100,
		},
		{
			name:         "negative index",
			oneshotData:  []oneshotDisplayRow{{id: 100}},
			oneshotIndex: -1,
			expectedID:   0,
		},
		{
			name:         "index out of bounds",
			oneshotData:  []oneshotDisplayRow{{id: 100}},
			oneshotIndex: 5,
			expectedID:   0,
		},
		{
			name:         "empty data",
			oneshotData:  []oneshotDisplayRow{},
			oneshotIndex: 0,
			expectedID:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{
				oneshotData:  tt.oneshotData,
				oneshotIndex: tt.oneshotIndex,
			}

			result := m.getSelectedOneshotID()
			if result != tt.expectedID {
				t.Errorf("getSelectedOneshotID() = %d, expected %d", result, tt.expectedID)
			}
		})
	}
}

// TestGetSelectedOneshotProcess tests getting selected oneshot process name
func TestGetSelectedOneshotProcess(t *testing.T) {
	tests := []struct {
		name         string
		oneshotData  []oneshotDisplayRow
		oneshotIndex int
		expectedName string
	}{
		{
			name:         "valid selection",
			oneshotData:  []oneshotDisplayRow{{processName: "task-1"}, {processName: "task-2"}},
			oneshotIndex: 1,
			expectedName: "task-2",
		},
		{
			name:         "first item",
			oneshotData:  []oneshotDisplayRow{{processName: "task-1"}},
			oneshotIndex: 0,
			expectedName: "task-1",
		},
		{
			name:         "negative index",
			oneshotData:  []oneshotDisplayRow{{processName: "task-1"}},
			oneshotIndex: -1,
			expectedName: "",
		},
		{
			name:         "index out of bounds",
			oneshotData:  []oneshotDisplayRow{{processName: "task-1"}},
			oneshotIndex: 5,
			expectedName: "",
		},
		{
			name:         "empty data",
			oneshotData:  []oneshotDisplayRow{},
			oneshotIndex: 0,
			expectedName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{
				oneshotData:  tt.oneshotData,
				oneshotIndex: tt.oneshotIndex,
			}

			result := m.getSelectedOneshotProcess()
			if result != tt.expectedName {
				t.Errorf("getSelectedOneshotProcess() = %q, expected %q", result, tt.expectedName)
			}
		})
	}
}

// TestScheduledTableWithPlainMode tests scheduled table rendering in plain mode (dialogs open)
func TestScheduledTableWithPlainMode(t *testing.T) {
	m := Model{
		scheduledData: []scheduledDisplayRow{
			{name: "cron-1", schedule: "*/5 * * * *", state: "idle"},
		},
		scheduledIndex:  0,
		width:           100,
		height:          30,
		showScaleDialog: true, // Plain mode triggered
	}

	result := m.renderScheduledTable()

	// Should still render without styles
	if len(result) == 0 {
		t.Error("Expected non-empty result in plain mode")
	}
	if !containsAny(result, []string{"NAME", "SCHEDULE"}) {
		t.Error("Expected headers in plain mode output")
	}
}

// TestOneshotTableWithPlainMode tests oneshot table rendering in plain mode
func TestOneshotTableWithPlainMode(t *testing.T) {
	m := Model{
		oneshotData: []oneshotDisplayRow{
			{processName: "task-1", status: "completed"},
		},
		oneshotIndex:     0,
		width:            100,
		height:           30,
		showConfirmation: true, // Plain mode triggered
	}

	result := m.renderOneshotTable()

	// Should still render without styles
	if len(result) == 0 {
		t.Error("Expected non-empty result in plain mode")
	}
	if !containsAny(result, []string{"PROCESS", "STATUS"}) {
		t.Error("Expected headers in plain mode output")
	}
}

// TestUpdateScheduledTable tests scheduled table population with various scenarios
func TestUpdateScheduledTable(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name           string
		processes      []process.ProcessInfo
		expectedCount  int
		expectedNames  []string
		initialIndex   int
		expectedIndex  int
	}{
		{
			name:          "empty processes",
			processes:     []process.ProcessInfo{},
			expectedCount: 0,
			expectedNames: []string{},
			initialIndex:  0,
			expectedIndex: 0,
		},
		{
			name: "filter only scheduled",
			processes: []process.ProcessInfo{
				{Name: "php-fpm", Type: "longrun"},
				{Name: "test-cron", Type: "scheduled", ScheduleState: "idle", Schedule: "* * * * *"},
				{Name: "nginx", Type: "longrun"},
			},
			expectedCount: 1,
			expectedNames: []string{"test-cron"},
			initialIndex:  0,
			expectedIndex: 0,
		},
		{
			name: "multiple scheduled sorted by name",
			processes: []process.ProcessInfo{
				{Name: "zebra-cron", Type: "scheduled", ScheduleState: "idle"},
				{Name: "alpha-cron", Type: "scheduled", ScheduleState: "idle"},
				{Name: "middle-cron", Type: "scheduled", ScheduleState: "idle"},
			},
			expectedCount: 3,
			expectedNames: []string{"alpha-cron", "middle-cron", "zebra-cron"},
			initialIndex:  0,
			expectedIndex: 0,
		},
		{
			name: "index bounds adjustment - exceeds data",
			processes: []process.ProcessInfo{
				{Name: "cron-1", Type: "scheduled"},
				{Name: "cron-2", Type: "scheduled"},
			},
			expectedCount: 2,
			expectedNames: []string{"cron-1", "cron-2"},
			initialIndex:  10,
			expectedIndex: 1, // Clamped to last valid index
		},
		{
			name: "index bounds adjustment - negative",
			processes: []process.ProcessInfo{
				{Name: "cron-1", Type: "scheduled"},
			},
			expectedCount: 1,
			expectedNames: []string{"cron-1"},
			initialIndex:  -5,
			expectedIndex: 0, // Clamped to 0
		},
		{
			name: "scheduled with various states",
			processes: []process.ProcessInfo{
				{Name: "executing-job", Type: "scheduled", ScheduleState: "executing"},
				{Name: "paused-job", Type: "scheduled", ScheduleState: "paused"},
				{Name: "idle-job", Type: "scheduled", ScheduleState: "idle"},
			},
			expectedCount: 3,
			expectedNames: []string{"executing-job", "idle-job", "paused-job"},
			initialIndex:  0,
			expectedIndex: 0,
		},
		{
			name: "scheduled with exit codes and run stats",
			processes: []process.ProcessInfo{
				{
					Name:          "job-with-history",
					Type:          "scheduled",
					ScheduleState: "idle",
					LastExitCode:  ptrInt(0),
					RunCount:      10,
					SuccessRate:   90.0,
					LastRun:       now.Add(-5 * time.Minute).Unix(),
					NextRun:       now.Add(5 * time.Minute).Unix(),
				},
			},
			expectedCount: 1,
			expectedNames: []string{"job-with-history"},
			initialIndex:  0,
			expectedIndex: 0,
		},
		{
			name: "scheduled with failed exit code",
			processes: []process.ProcessInfo{
				{
					Name:          "failed-job",
					Type:          "scheduled",
					ScheduleState: "idle",
					LastExitCode:  ptrInt(1),
					RunCount:      5,
					SuccessRate:   60.0,
				},
			},
			expectedCount: 1,
			expectedNames: []string{"failed-job"},
			initialIndex:  0,
			expectedIndex: 0,
		},
		{
			name: "scheduled with paused state overrides next run",
			processes: []process.ProcessInfo{
				{
					Name:          "paused-job",
					Type:          "scheduled",
					ScheduleState: "paused",
					NextRun:       now.Add(10 * time.Minute).Unix(),
				},
			},
			expectedCount: 1,
			expectedNames: []string{"paused-job"},
			initialIndex:  0,
			expectedIndex: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{
				scheduledIndex: tt.initialIndex,
			}

			m.updateScheduledTable(tt.processes)

			if len(m.scheduledData) != tt.expectedCount {
				t.Errorf("Expected %d scheduled items, got %d", tt.expectedCount, len(m.scheduledData))
			}

			for i, expectedName := range tt.expectedNames {
				if i >= len(m.scheduledData) {
					t.Errorf("Missing scheduled item at index %d", i)
					continue
				}
				if m.scheduledData[i].name != expectedName {
					t.Errorf("Expected name %q at index %d, got %q", expectedName, i, m.scheduledData[i].name)
				}
			}

			if m.scheduledIndex != tt.expectedIndex {
				t.Errorf("Expected scheduledIndex %d, got %d", tt.expectedIndex, m.scheduledIndex)
			}
		})
	}
}

// Helper function for pointer to int
func ptrInt(i int) *int {
	return &i
}

// TestUpdateScheduledTableStateStyles tests style assignment based on state
func TestUpdateScheduledTableStateStyles(t *testing.T) {
	tests := []struct {
		name          string
		state         string
		expectedState string
	}{
		{"executing state", "executing", "▶ Running"},
		{"paused state", "paused", "⏸ Paused"},
		{"idle state", "idle", "⏰ Scheduled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{}
			m.updateScheduledTable([]process.ProcessInfo{
				{Name: "test-job", Type: "scheduled", ScheduleState: tt.state},
			})

			if len(m.scheduledData) != 1 {
				t.Fatal("Expected 1 scheduled item")
			}

			if m.scheduledData[0].state != tt.expectedState {
				t.Errorf("Expected state %q, got %q", tt.expectedState, m.scheduledData[0].state)
			}
		})
	}
}

// TestUpdateScheduledTableLastRunFormatting tests last run time formatting
func TestUpdateScheduledTableLastRunFormatting(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name            string
		lastRun         int64
		expectedText    string
		expectedContain bool // Use contains instead of exact match for time-sensitive tests
	}{
		{"never run", 0, "never", false},
		{"negative timestamp", -1, "never", false},
		{"recent run", now.Add(-30 * time.Second).Unix(), "ago", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{}
			m.updateScheduledTable([]process.ProcessInfo{
				{Name: "test-job", Type: "scheduled", LastRun: tt.lastRun},
			})

			if len(m.scheduledData) != 1 {
				t.Fatal("Expected 1 scheduled item")
			}

			if tt.expectedContain {
				if !containsAny(m.scheduledData[0].lastRun, []string{tt.expectedText}) {
					t.Errorf("Expected lastRun to contain %q, got %q", tt.expectedText, m.scheduledData[0].lastRun)
				}
			} else {
				if m.scheduledData[0].lastRun != tt.expectedText {
					t.Errorf("Expected lastRun %q, got %q", tt.expectedText, m.scheduledData[0].lastRun)
				}
			}
		})
	}
}

// TestUpdateProcessTableWithScheduled tests that scheduled processes are filtered out of process table
// Scheduled processes go to a separate Scheduled tab, not the main Processes tab
func TestUpdateProcessTableWithScheduled(t *testing.T) {
	m := &Model{
		width:  100,
		height: 30,
	}
	m.setupProcessTable()

	// Update with mixed process types
	m.updateProcessTable([]process.ProcessInfo{
		{Name: "php-fpm", Type: "longrun", State: "running"},
		{Name: "test-cron", Type: "scheduled", State: "scheduled", ScheduleState: "idle"},
		{Name: "nginx", Type: "longrun", State: "running"},
	})

	// Process table should contain only non-scheduled processes (longrun, oneshot)
	// Scheduled processes go to the separate scheduled table
	if len(m.tableData) != 2 {
		t.Errorf("Expected 2 processes in tableData (scheduled should be filtered out), got %d", len(m.tableData))
	}

	// Verify the scheduled process is in the scheduled table instead
	if len(m.scheduledData) != 1 {
		t.Errorf("Expected 1 process in scheduledData, got %d", len(m.scheduledData))
	}
	if len(m.scheduledData) > 0 && m.scheduledData[0].name != "test-cron" {
		t.Errorf("Expected scheduled process name 'test-cron', got %q", m.scheduledData[0].name)
	}
}

// TestUpdateProcessTableStateDisplay tests state display formatting
func TestUpdateProcessTableStateDisplay(t *testing.T) {
	tests := []struct {
		name          string
		state         string
		expectedStyle bool // Just verify non-empty style
	}{
		{"running", "running", true},
		{"stopped", "stopped", true},
		{"starting", "starting", true},
		{"scheduled", "scheduled", true},
		{"unknown", "unknown", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{
				width:  100,
				height: 30,
			}
			m.setupProcessTable()

			m.updateProcessTable([]process.ProcessInfo{
				{Name: "test-proc", Type: "longrun", State: tt.state},
			})

			if len(m.tableData) != 1 {
				t.Fatal("Expected 1 process in tableData")
			}

			// Just verify state is populated
			if m.tableData[0].state == "" {
				t.Error("Expected non-empty state")
			}
		})
	}
}

