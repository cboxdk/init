package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/cboxdk/init/internal/process"
)

type processDisplayRow struct {
	name         string
	nameStyle    lipgloss.Style
	procType     string
	state        string
	rawState     string
	stateStyle   lipgloss.Style
	health       string
	healthStyle  lipgloss.Style
	scale        string
	currentScale int
	desiredScale int
	maxScale     int
	cpuUsage     string
	memoryUsage  string
	uptime       string
	restarts     string
	// Schedule fields (kept for backward compat)
	isScheduled   bool
	schedule      string
	scheduleState string
	nextRun       int64
	lastRun       int64
}

// scheduledDisplayRow represents a scheduled/cron job for the Scheduled tab
type scheduledDisplayRow struct {
	name          string
	nameStyle     lipgloss.Style
	schedule      string // Cron expression e.g. "*/5 * * * *"
	state         string // Formatted state for display (e.g. "⏸ Paused")
	stateStyle    lipgloss.Style
	rawState      string // Raw schedule state (idle, executing, paused)
	lastRun       string // Formatted last run time
	lastRunStyle  lipgloss.Style
	nextRun       string // Formatted next run time
	nextRunStyle  lipgloss.Style
	lastExitCode  string // Exit code of last run
	exitCodeStyle lipgloss.Style
	runCount      int    // Total executions
	successRate   string // Success rate percentage
	// Raw values for sorting/filtering
	rawLastRun int64
	rawNextRun int64
}

// ExecutionHistoryEntry represents a single execution in the history
type ExecutionHistoryEntry struct {
	ID        int64  `json:"id"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Duration  string `json:"duration"`
	ExitCode  int    `json:"exit_code"`
	Success   bool   `json:"success"`
	Error     string `json:"error"`
	Triggered string `json:"triggered"` // "schedule" or "manual"
}

// oneshotDisplayRow represents a oneshot execution for the Oneshot tab
type oneshotDisplayRow struct {
	id            int64
	processName   string
	nameStyle     lipgloss.Style
	instanceID    string
	startedAt     string
	startedStyle  lipgloss.Style
	finishedAt    string
	finishedStyle lipgloss.Style
	duration      string
	exitCode      string
	exitStyle     lipgloss.Style
	status        string // "✓ Success", "✗ Failed", "⏳ Running"
	statusStyle   lipgloss.Style
	triggerType   string // "startup", "manual", "api"
	error         string
	// Raw values for sorting
	rawStartedAt int64
}

// setupProcessTable initializes the process table
func (m *Model) setupProcessTable() {
	var prevRows []table.Row
	if m.processTable.Columns() != nil {
		prevRows = m.processTable.Rows()
	}

	t := table.New(
		table.WithColumns(m.defaultColumns()),
		table.WithFocused(true),
		table.WithHeight(m.defaultTableHeight()),
		table.WithWidth(m.width),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(primaryColor).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(primaryColor).
		Bold(false)

	t.SetStyles(s)

	if len(prevRows) > 0 {
		t.SetRows(prevRows)
	}
	if m.selectedIndex < 0 {
		m.selectedIndex = 0
	}
	t.SetCursor(m.selectedIndex)

	m.processTable = t
}

// setupInstanceTable initializes the instance table for process detail view
func (m *Model) setupInstanceTable() {
	m.instanceColumns = m.computeInstanceColumns()
	t := table.New(
		table.WithColumns(m.instanceColumns),
		table.WithFocused(true),
		table.WithHeight(m.detailTableHeight()),
		table.WithWidth(m.detailTableWidth()),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(primaryColor).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(primaryColor).
		Bold(false)

	t.SetStyles(s)
	m.instanceTable = t
}

// defaultColumns returns columns for the Processes tab (longrun/oneshot)
func (m *Model) defaultColumns() []table.Column {
	titles := []string{"NAME", "TYPE", "STATE", "HEALTH", "SCALE", "CPU", "RAM", "UPTIME", "RESTARTS"}
	widths := []int{18, 10, 14, 12, 10, 8, 10, 12, 10}
	columns := make([]table.Column, len(titles))
	for i, title := range titles {
		columns[i] = table.Column{Title: title, Width: widths[i]}
	}
	return columns
}

// scheduledColumns returns columns for the Scheduled tab (cron jobs)
func (m *Model) scheduledColumns() []table.Column {
	titles := []string{"NAME", "SCHEDULE", "STATE", "LAST RUN", "NEXT RUN", "EXIT", "RUNS", "SUCCESS"}
	widths := []int{18, 14, 12, 16, 16, 6, 6, 8}
	columns := make([]table.Column, len(titles))
	for i, title := range titles {
		columns[i] = table.Column{Title: title, Width: widths[i]}
	}
	return columns
}

func (m *Model) defaultTableHeight() int {
	height := m.height - 5
	if height < 5 {
		return 5
	}
	return height
}

func (m *Model) detailTableHeight() int {
	height := m.height - 7
	if height < 6 {
		height = 6
	}
	return height
}

func (m *Model) detailTableWidth() int {
	if m.width <= 0 {
		return 80
	}
	return m.width
}

func (m *Model) computeInstanceColumns() []table.Column {
	titles := []string{"ID", "STATE", "PID", "CPU", "RAM", "UPTIME", "RESTARTS"}
	widths := []int{18, 12, 8, 8, 12, 12, 10}
	total := 0
	for _, w := range widths {
		total += w
	}
	available := m.detailTableWidth() - 4
	if extra := available - total; extra > 0 {
		widths[0] += extra
	}

	cols := make([]table.Column, len(titles))
	for i, title := range titles {
		cols[i] = table.Column{Title: title, Width: widths[i]}
	}
	return cols
}

// updateProcessTable updates the cached table data and column widths
// This now only handles longrun/oneshot processes (Processes tab)
// Scheduled processes are handled by updateScheduledTable (Scheduled tab)
func (m *Model) updateProcessTable(processes []process.ProcessInfo) {
	// Also update scheduled table for the Scheduled tab
	m.updateScheduledTable(processes)

	// Filter out scheduled processes - they go to the Scheduled tab
	regularProcs := m.filterRegularProcesses(processes)

	headers := []string{"NAME", "TYPE", "STATE", "HEALTH", "SCALE", "CPU", "RAM", "UPTIME", "RESTARTS"}
	colWidths := m.initColumnWidths(headers)

	displayRows := make([]processDisplayRow, 0, len(regularProcs))
	cursorRows := make([]table.Row, 0, len(regularProcs))

	for _, proc := range regularProcs {
		row := m.buildProcessDisplayRow(proc)
		displayRows = append(displayRows, row)
		cursorRows = append(cursorRows, table.Row{proc.Name})
		m.updateColumnWidths(colWidths, row)
	}

	m.applyProcessTableLayout(displayRows, cursorRows, colWidths, headers)
}

// filterRegularProcesses filters out scheduled processes and sorts by name
func (m *Model) filterRegularProcesses(processes []process.ProcessInfo) []process.ProcessInfo {
	regularProcs := make([]process.ProcessInfo, 0)
	for _, proc := range processes {
		if proc.Type != "scheduled" {
			regularProcs = append(regularProcs, proc)
		}
	}
	sort.Slice(regularProcs, func(i, j int) bool {
		return regularProcs[i].Name < regularProcs[j].Name
	})
	return regularProcs
}

// initColumnWidths initializes column widths based on header lengths
func (m *Model) initColumnWidths(headers []string) []int {
	colWidths := make([]int, len(headers))
	for i, header := range headers {
		colWidths[i] = lipgloss.Width(header)
	}
	return colWidths
}

// buildProcessDisplayRow creates a display row for a regular process
func (m *Model) buildProcessDisplayRow(proc process.ProcessInfo) processDisplayRow {
	stats := m.calculateProcessStats(proc)
	stateText, stateStyle := stateDisplay(proc.State)
	healthText, healthStyle := m.determineHealthDisplay(proc, stats)
	nameStyle := m.determineNameStyle(proc.State)
	scaleText := m.formatScaleText(stats.currentInstances, proc.DesiredScale, proc.MaxScale)

	return processDisplayRow{
		name:         proc.Name,
		nameStyle:    nameStyle,
		procType:     proc.Type,
		state:        stateText,
		rawState:     proc.State,
		stateStyle:   stateStyle,
		health:       healthText,
		healthStyle:  healthStyle,
		scale:        scaleText,
		currentScale: stats.currentInstances,
		desiredScale: proc.DesiredScale,
		maxScale:     proc.MaxScale,
		cpuUsage:     formatCPUUsage(proc.CPUPercent),
		memoryUsage:  formatMemoryUsage(proc.MemoryRSSBytes),
		uptime:       formatUptime(stats.oldestStart),
		restarts:     fmt.Sprintf("%d", stats.totalRestarts),
	}
}

// processStats holds computed statistics for a process
type processStats struct {
	currentInstances int
	totalRestarts    int
	oldestStart      int64
	allHealthy       bool
	hasFailed        bool
}

// calculateProcessStats computes statistics from process instances
func (m *Model) calculateProcessStats(proc process.ProcessInfo) processStats {
	stats := processStats{
		currentInstances: len(proc.Instances),
		allHealthy:       true,
	}

	for _, inst := range proc.Instances {
		stats.totalRestarts += inst.RestartCount
		if stats.oldestStart == 0 || inst.StartedAt < stats.oldestStart {
			stats.oldestStart = inst.StartedAt
		}
		if inst.State != "running" {
			stats.allHealthy = false
			if inst.State == "failed" {
				stats.hasFailed = true
			}
		}
	}
	return stats
}

// determineHealthDisplay determines the health text and style for a process
func (m *Model) determineHealthDisplay(proc process.ProcessInfo, stats processStats) (string, lipgloss.Style) {
	switch {
	case proc.State != string(process.StateRunning) && proc.State != "running":
		return "–", dimStyle
	case stats.hasFailed:
		return healthDisplay(false)
	case stats.currentInstances != proc.DesiredScale:
		return "⟳ Scaling", highlightStyle
	case !stats.allHealthy:
		return "⟳ Scaling", highlightStyle
	default:
		healthy := stats.allHealthy && stats.currentInstances > 0
		return healthDisplay(healthy)
	}
}

// determineNameStyle returns the style for the process name based on state
func (m *Model) determineNameStyle(state string) lipgloss.Style {
	switch state {
	case "running":
		return successStyle
	case "failed":
		return errorStyle
	default:
		return warnStyle
	}
}

// formatScaleText formats the scale display string
func (m *Model) formatScaleText(current, desired, max int) string {
	if max > 0 {
		return fmt.Sprintf("%d/%d/%d", current, desired, max)
	}
	return fmt.Sprintf("%d/%d/-", current, desired)
}

// updateColumnWidths updates column widths based on row values
func (m *Model) updateColumnWidths(colWidths []int, row processDisplayRow) {
	values := []string{
		row.name, row.procType, row.state, row.health,
		row.scale, row.cpuUsage, row.memoryUsage, row.uptime, row.restarts,
	}
	for i, value := range values {
		if w := lipgloss.Width(value); w > colWidths[i] {
			colWidths[i] = w
		}
	}
}

// applyProcessTableLayout applies the final layout to the process table
func (m *Model) applyProcessTableLayout(displayRows []processDisplayRow, cursorRows []table.Row, colWidths []int, headers []string) {
	totalWidth := 0
	for _, w := range colWidths {
		totalWidth += w
	}

	separatorWidth := (len(headers) - 1) * len(columnSeparator)
	available := m.width - separatorWidth
	if available > totalWidth {
		colWidths[0] += available - totalWidth
		totalWidth = available
	}
	if available < totalWidth {
		available = totalWidth
	}

	m.tableData = displayRows
	m.tableColumnWidths = colWidths
	m.processTable.SetColumns(m.defaultColumns())
	m.processTable.SetWidth(available + separatorWidth)
	m.processTable.SetRows(cursorRows)

	m.clampSelectedIndex(len(m.tableData))
	m.processTable.SetCursor(m.selectedIndex)
	m.ensureCursorVisible()
}

// clampSelectedIndex ensures the selected index is within valid bounds
func (m *Model) clampSelectedIndex(dataLen int) {
	if dataLen == 0 {
		m.selectedIndex = 0
	} else {
		if m.selectedIndex >= dataLen {
			m.selectedIndex = dataLen - 1
		}
		if m.selectedIndex < 0 {
			m.selectedIndex = 0
		}
	}
}

// ensureCursorVisible keeps the cursor within the visible viewport
func (m *Model) ensureCursorVisible() {
	height := m.defaultTableHeight()
	if height <= 0 {
		height = 1
	}

	maxOffset := len(m.tableData) - height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.tableOffset > maxOffset {
		m.tableOffset = maxOffset
	}

	cursor := m.selectedIndex
	if cursor < m.tableOffset {
		m.tableOffset = cursor
	} else if cursor >= m.tableOffset+height {
		m.tableOffset = cursor - height + 1
		if m.tableOffset > maxOffset {
			m.tableOffset = maxOffset
		}
	}
	if m.tableOffset < 0 {
		m.tableOffset = 0
	}
}

// updateInstanceTable renders the instance table from process info
func (m *Model) updateInstanceTable(process *process.ProcessInfo) {
	if m.instanceTable.Columns() == nil {
		m.setupInstanceTable()
	}

	rows := []table.Row{}
	if process != nil {
		for _, inst := range process.Instances {
			stateText, _ := stateDisplay(inst.State)
			rows = append(rows, table.Row{
				inst.ID,
				stateText,
				fmt.Sprintf("%d", inst.PID),
				formatCPUUsage(inst.CPUPercent),
				formatMemoryUsage(inst.MemoryRSSBytes),
				formatUptime(inst.StartedAt),
				fmt.Sprintf("%d", inst.RestartCount),
			})
		}
	}

	m.instanceColumns = m.computeInstanceColumns()
	m.instanceTable.SetColumns(m.instanceColumns)
	m.instanceTable.SetRows(rows)
	m.instanceTable.SetWidth(m.detailTableWidth())
	m.instanceTable.SetHeight(m.detailTableHeight())
	if len(rows) == 0 {
		m.instanceTable.SetCursor(0)
	} else if m.instanceTable.Cursor() >= len(rows) {
		m.instanceTable.SetCursor(len(rows) - 1)
	}
}

// renderProcessTable renders the process list with custom alignment/colours
func (m Model) renderProcessTable() string {
	if len(m.tableData) == 0 {
		return "No processes found"
	}

	alignLeft := []bool{true, true, false, false, false, false, false, false, false}
	headers := []string{"NAME", "TYPE", "STATE", "HEALTH", "SCALE", "CPU", "RAM", "UPTIME", "RESTARTS"}

	usePlain := m.showScaleDialog || m.showConfirmation

	var b strings.Builder
	headerStyles := buildHeaderStyles(len(headers), usePlain)
	headerLine := formatRow(headers, headerStyles, m.tableColumnWidths, alignLeft)
	if usePlain {
		b.WriteString(headerLine)
	} else {
		b.WriteString(tableHeaderStyle.Render(headerLine))
	}
	b.WriteString("\n")

	height := m.defaultTableHeight()
	start := m.tableOffset
	end := start + height
	if end > len(m.tableData) {
		end = len(m.tableData)
	}

	for i := start; i < end; i++ {
		row := m.tableData[i]
		rowStyles := []lipgloss.Style{row.nameStyle, dimStyle, row.stateStyle, row.healthStyle, dimStyle, dimStyle, dimStyle, dimStyle, dimStyle}
		if usePlain {
			rowStyles = nil
		}
		line := formatRow(
			[]string{row.name, row.procType, row.state, row.health, row.scale, row.cpuUsage, row.memoryUsage, row.uptime, row.restarts},
			rowStyles,
			m.tableColumnWidths,
			alignLeft,
		)

		if i == m.selectedIndex {
			if usePlain {
				if len(line) >= 2 {
					line = "> " + line[2:]
				} else {
					line = "> " + line
				}
			} else {
				line = tableSelectedStyle.Render(line)
			}
		}

		b.WriteString(line)
		if i != end-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

func buildHeaderStyles(count int, usePlain bool) []lipgloss.Style {
	styles := make([]lipgloss.Style, count)
	for i := 0; i < count; i++ {
		if usePlain {
			styles[i] = lipgloss.NewStyle()
		} else {
			styles[i] = tableHeaderStyle
		}
	}
	return styles
}

const columnSeparator = "  "

func formatRow(values []string, styles []lipgloss.Style, widths []int, alignLeft []bool) string {
	parts := make([]string, len(values))
	for i, value := range values {
		width := len(value)
		if i < len(widths) {
			width = widths[i]
		}

		align := true
		if i < len(alignLeft) {
			align = alignLeft[i]
		}

		style := lipgloss.NewStyle()
		if styles != nil && i < len(styles) {
			style = styles[i]
		}

		colored := style.Render(value)

		padding := width - lipgloss.Width(value)
		if padding < 0 {
			padding = 0
		}
		spaces := strings.Repeat(" ", padding)
		if align {
			parts[i] = colored + spaces
		} else {
			parts[i] = spaces + colored
		}
	}

	return strings.Join(parts, columnSeparator)
}

// formatUptime formats a timestamp as uptime duration
func formatUptime(startedAt int64) string {
	if startedAt == 0 {
		return "-"
	}

	duration := time.Since(time.Unix(startedAt, 0))

	hours := int(duration.Hours())
	minutes := int(duration.Minutes()) % 60

	if hours > 24 {
		days := hours / 24
		hours = hours % 24
		return fmt.Sprintf("%dd %dh", days, hours)
	}

	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}

	if minutes > 0 {
		return fmt.Sprintf("%dm", minutes)
	}

	return fmt.Sprintf("%ds", int(duration.Seconds()))
}

func formatCPUUsage(cpu float64) string {
	if cpu <= 0 {
		return "-"
	}
	if cpu < 10 {
		return fmt.Sprintf("%.1f%%", cpu)
	}
	return fmt.Sprintf("%.0f%%", cpu)
}

func formatMemoryUsage(bytes uint64) string {
	if bytes == 0 {
		return "-"
	}

	const (
		kilobyte = 1024
		megabyte = kilobyte * 1024
		gigabyte = megabyte * 1024
	)

	switch {
	case bytes >= gigabyte:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gigabyte))
	case bytes >= megabyte:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(megabyte))
	default:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kilobyte))
	}
}

// scheduleStateDisplay returns formatted state text and style for scheduled jobs
func scheduleStateDisplay(state string) (string, lipgloss.Style) {
	switch state {
	case "idle":
		return "⏰ Scheduled", highlightStyle
	case "executing":
		return "▶ Running", successStyle
	case "paused":
		return "⏸ Paused", warnStyle
	default:
		return state, dimStyle
	}
}

// formatNextRun formats a Unix timestamp as relative time until next run
func formatNextRun(nextRun int64) string {
	if nextRun <= 0 {
		return "-"
	}

	nextTime := time.Unix(nextRun, 0)
	until := time.Until(nextTime)

	if until < 0 {
		return "now"
	}

	if until < time.Minute {
		return fmt.Sprintf("in %ds", int(until.Seconds()))
	}

	if until < time.Hour {
		return fmt.Sprintf("in %dm", int(until.Minutes()))
	}

	if until < 24*time.Hour {
		hours := int(until.Hours())
		mins := int(until.Minutes()) % 60
		return fmt.Sprintf("in %dh%dm", hours, mins)
	}

	days := int(until.Hours()) / 24
	hours := int(until.Hours()) % 24
	return fmt.Sprintf("in %dd%dh", days, hours)
}

// formatLastRun formats a Unix timestamp as relative time since last run
func formatLastRun(lastRun int64) string {
	if lastRun <= 0 {
		return "never"
	}

	lastTime := time.Unix(lastRun, 0)
	since := time.Since(lastTime)

	if since < time.Minute {
		return fmt.Sprintf("%ds ago", int(since.Seconds()))
	}

	if since < time.Hour {
		return fmt.Sprintf("%dm ago", int(since.Minutes()))
	}

	if since < 24*time.Hour {
		hours := int(since.Hours())
		mins := int(since.Minutes()) % 60
		return fmt.Sprintf("%dh%dm ago", hours, mins)
	}

	days := int(since.Hours()) / 24
	hours := int(since.Hours()) % 24
	return fmt.Sprintf("%dd%dh ago", days, hours)
}

// updateScheduledTable updates the scheduled jobs data for the Scheduled tab
func (m *Model) updateScheduledTable(processes []process.ProcessInfo) {
	// Filter only scheduled processes
	scheduled := make([]process.ProcessInfo, 0)
	for _, proc := range processes {
		if proc.Type == "scheduled" {
			scheduled = append(scheduled, proc)
		}
	}

	// Sort by name for stable order
	sort.Slice(scheduled, func(i, j int) bool {
		return scheduled[i].Name < scheduled[j].Name
	})

	displayRows := make([]scheduledDisplayRow, 0, len(scheduled))

	for _, proc := range scheduled {
		stateText, stateStyle := scheduleStateDisplay(proc.ScheduleState)

		var nameStyle lipgloss.Style
		switch proc.ScheduleState {
		case "executing":
			nameStyle = successStyle
		case "paused":
			nameStyle = warnStyle
		default:
			nameStyle = highlightStyle
		}

		// Format last run time
		lastRunText := formatLastRun(proc.LastRun)
		var lastRunStyle lipgloss.Style
		if proc.LastRun <= 0 {
			lastRunStyle = dimStyle
		} else {
			lastRunStyle = successStyle
		}

		// Format next run time
		nextRunText := formatNextRun(proc.NextRun)
		var nextRunStyle lipgloss.Style
		if proc.ScheduleState == "paused" {
			nextRunStyle = warnStyle
			nextRunText = "paused"
		} else if proc.NextRun <= 0 {
			nextRunStyle = dimStyle
		} else {
			nextRunStyle = highlightStyle
		}

		// Exit code from last run (from execution history)
		exitCodeText := "-"
		var exitCodeStyle lipgloss.Style = dimStyle
		if proc.LastExitCode != nil {
			exitCodeText = fmt.Sprintf("%d", *proc.LastExitCode)
			if *proc.LastExitCode == 0 {
				exitCodeStyle = successStyle
			} else {
				exitCodeStyle = errorStyle
			}
		}

		// Success rate formatting
		successRateText := "-"
		if proc.RunCount > 0 {
			successRateText = fmt.Sprintf("%.0f%%", proc.SuccessRate)
		}

		row := scheduledDisplayRow{
			name:          proc.Name,
			nameStyle:     nameStyle,
			schedule:      proc.Schedule,
			state:         stateText,
			stateStyle:    stateStyle,
			rawState:      proc.ScheduleState,
			lastRun:       lastRunText,
			lastRunStyle:  lastRunStyle,
			nextRun:       nextRunText,
			nextRunStyle:  nextRunStyle,
			lastExitCode:  exitCodeText,
			exitCodeStyle: exitCodeStyle,
			runCount:      proc.RunCount,
			successRate:   successRateText,
			rawLastRun:    proc.LastRun,
			rawNextRun:    proc.NextRun,
		}

		displayRows = append(displayRows, row)
	}

	m.scheduledData = displayRows

	// Update selection bounds
	if len(m.scheduledData) == 0 {
		m.scheduledIndex = 0
	} else if m.scheduledIndex >= len(m.scheduledData) {
		m.scheduledIndex = len(m.scheduledData) - 1
	}
	if m.scheduledIndex < 0 {
		m.scheduledIndex = 0
	}
}

// renderScheduledTable renders the scheduled jobs table for the Scheduled tab
func (m Model) renderScheduledTable() string {
	if len(m.scheduledData) == 0 {
		return "No scheduled jobs found"
	}

	headers := []string{"NAME", "SCHEDULE", "STATE", "LAST RUN", "NEXT RUN", "EXIT", "RUNS", "SUCCESS"}
	alignLeft := []bool{true, true, false, false, false, false, false, false}

	// Calculate column widths
	colWidths := make([]int, len(headers))
	for i, header := range headers {
		colWidths[i] = lipgloss.Width(header)
	}

	for _, row := range m.scheduledData {
		values := []string{row.name, row.schedule, row.state, row.lastRun, row.nextRun, row.lastExitCode, fmt.Sprintf("%d", row.runCount), row.successRate}
		for i, value := range values {
			if w := lipgloss.Width(value); w > colWidths[i] {
				colWidths[i] = w
			}
		}
	}

	usePlain := m.showScaleDialog || m.showConfirmation

	var b strings.Builder

	// Header
	headerStyles := buildHeaderStyles(len(headers), usePlain)
	headerLine := formatRow(headers, headerStyles, colWidths, alignLeft)
	if usePlain {
		b.WriteString(headerLine)
	} else {
		b.WriteString(tableHeaderStyle.Render(headerLine))
	}
	b.WriteString("\n")

	// Rows
	height := m.defaultTableHeight()
	start := m.scheduledOffset
	end := start + height
	if end > len(m.scheduledData) {
		end = len(m.scheduledData)
	}

	for i := start; i < end; i++ {
		row := m.scheduledData[i]
		rowStyles := []lipgloss.Style{row.nameStyle, dimStyle, row.stateStyle, row.lastRunStyle, row.nextRunStyle, row.exitCodeStyle, dimStyle, dimStyle}
		if usePlain {
			rowStyles = nil
		}
		line := formatRow(
			[]string{row.name, row.schedule, row.state, row.lastRun, row.nextRun, row.lastExitCode, fmt.Sprintf("%d", row.runCount), row.successRate},
			rowStyles,
			colWidths,
			alignLeft,
		)

		if i == m.scheduledIndex {
			if usePlain {
				if len(line) >= 2 {
					line = "> " + line[2:]
				} else {
					line = "> " + line
				}
			} else {
				line = tableSelectedStyle.Render(line)
			}
		}

		b.WriteString(line)
		if i != end-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

// getSelectedScheduledName returns the name of the currently selected scheduled job
func (m *Model) getSelectedScheduledName() string {
	if m.scheduledIndex >= 0 && m.scheduledIndex < len(m.scheduledData) {
		return m.scheduledData[m.scheduledIndex].name
	}
	return ""
}

// renderOneshotTable renders the oneshot execution history table
func (m Model) renderOneshotTable() string {
	if len(m.oneshotData) == 0 {
		return "No oneshot executions recorded"
	}

	headers := []string{"PROCESS", "INSTANCE", "STARTED", "DURATION", "STATUS", "EXIT", "TRIGGER"}
	alignLeft := []bool{true, true, false, false, false, false, true}

	// Calculate column widths
	colWidths := make([]int, len(headers))
	for i, header := range headers {
		colWidths[i] = lipgloss.Width(header)
	}

	for _, row := range m.oneshotData {
		values := []string{row.processName, row.instanceID, row.startedAt, row.duration, row.status, row.exitCode, row.triggerType}
		for i, value := range values {
			if w := lipgloss.Width(value); w > colWidths[i] {
				colWidths[i] = w
			}
		}
	}

	usePlain := m.showScaleDialog || m.showConfirmation

	var b strings.Builder

	// Header
	headerStyles := buildHeaderStyles(len(headers), usePlain)
	headerLine := formatRow(headers, headerStyles, colWidths, alignLeft)
	if usePlain {
		b.WriteString(headerLine)
	} else {
		b.WriteString(tableHeaderStyle.Render(headerLine))
	}
	b.WriteString("\n")

	// Rows
	height := m.defaultTableHeight()
	start := m.oneshotOffset
	end := start + height
	if end > len(m.oneshotData) {
		end = len(m.oneshotData)
	}

	for i := start; i < end; i++ {
		row := m.oneshotData[i]
		rowStyles := []lipgloss.Style{row.nameStyle, dimStyle, row.startedStyle, dimStyle, row.statusStyle, row.exitStyle, dimStyle}
		if usePlain {
			rowStyles = nil
		}
		line := formatRow(
			[]string{row.processName, row.instanceID, row.startedAt, row.duration, row.status, row.exitCode, row.triggerType},
			rowStyles,
			colWidths,
			alignLeft,
		)

		if i == m.oneshotIndex {
			if usePlain {
				if len(line) >= 2 {
					line = "> " + line[2:]
				} else {
					line = "> " + line
				}
			} else {
				line = tableSelectedStyle.Render(line)
			}
		}

		b.WriteString(line)
		if i != end-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

// getSelectedOneshotID returns the ID of the currently selected oneshot execution
func (m *Model) getSelectedOneshotID() int64 {
	if m.oneshotIndex >= 0 && m.oneshotIndex < len(m.oneshotData) {
		return m.oneshotData[m.oneshotIndex].id
	}
	return 0
}

// getSelectedOneshotProcess returns the process name of the currently selected oneshot execution
func (m *Model) getSelectedOneshotProcess() string {
	if m.oneshotIndex >= 0 && m.oneshotIndex < len(m.oneshotData) {
		return m.oneshotData[m.oneshotIndex].processName
	}
	return ""
}
