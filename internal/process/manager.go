package process

import (
	"log/slog"
	"sync"
	"time"

	"github.com/cboxdk/init/internal/audit"
	"github.com/cboxdk/init/internal/config"
	logpkg "github.com/cboxdk/init/internal/logger"
	"github.com/cboxdk/init/internal/metrics"
	"github.com/cboxdk/init/internal/readiness"
	"github.com/cboxdk/init/internal/schedule"
)

// Default timeouts and limits for process management operations.
// These can be overridden via configuration where applicable.
const (
	// DefaultDependencyReadinessTimeout is the maximum time to wait for
	// a process's dependencies to become ready before starting it.
	DefaultDependencyReadinessTimeout = 5 * time.Minute

	// DefaultProcessStartTimeout is the timeout for starting a single process.
	DefaultProcessStartTimeout = 30 * time.Second

	// DefaultProcessStopTimeout is the timeout for stopping a single process.
	DefaultProcessStopTimeout = 60 * time.Second

	// MaxProcessScale is the maximum number of instances a process can scale to.
	MaxProcessScale = 100
)

// Manager is the central coordinator for all process management operations.
// It handles process lifecycle (start, stop, restart, scale), dependency ordering,
// scheduled task execution, resource metrics collection, and health monitoring.
//
// Manager is safe for concurrent use from multiple goroutines.
//
// Example usage:
//
//	manager := process.NewManager(cfg, logger, auditLogger)
//	ctx := context.Background()
//	if err := manager.Start(ctx); err != nil {
//	    log.Fatalf("Failed to start manager: %v", err)
//	}
//	defer manager.Shutdown(ctx)
type Manager struct {
	config            *config.Config
	configPath        string // Path to config file for saving
	logger            *slog.Logger
	auditLogger       *audit.Logger
	processes         map[string]*Supervisor
	scheduler         *schedule.Scheduler        // Cron scheduler for scheduled processes
	scheduleExecutor  *schedule.ProcessExecutor  // Executor for scheduled processes
	resourceCollector *metrics.ResourceCollector // Shared resource metrics collector
	oneshotHistory    *OneshotHistory            // History for oneshot process executions
	readinessManager  *readiness.Manager         // Readiness file manager for K8s integration
	mu                sync.RWMutex
	shutdownCh        chan struct{}
	shutdownOnce      sync.Once // Ensures shutdownCh is closed only once
	allDeadCh         chan struct{}
	allDeadOnce       sync.Once // Ensures allDeadCh is closed only once
	processDeathCh    chan string
	startTime      time.Time
	logBroadcaster *logpkg.LogBroadcaster

	// Configurable timeouts and limits (initialized from global config or defaults)
	dependencyTimeout  time.Duration
	processStopTimeout time.Duration
	maxProcessScale    int
}

// NewManager creates a new Manager with the provided configuration.
//
// Parameters:
//   - cfg: The application configuration containing process definitions and global settings
//   - logger: Structured logger for operational logging
//   - auditLogger: Security audit logger for tracking process control actions
//
// The Manager is created but not started. Call Start() to initialize and start
// all configured processes. The Manager should be shut down via Shutdown()
// when no longer needed.
func NewManager(cfg *config.Config, logger *slog.Logger, auditLogger *audit.Logger) *Manager {
	startTime := time.Now()
	metrics.SetManagerStartTime(float64(startTime.Unix()))

	// Initialize resource collector if enabled
	var resourceCollector *metrics.ResourceCollector
	if cfg.Global.ResourceMetricsEnabledValue() {
		interval := time.Duration(cfg.Global.ResourceMetricsInterval) * time.Second
		maxSamples := cfg.Global.ResourceMetricsMaxSamples
		resourceCollector = metrics.NewResourceCollector(interval, maxSamples, logger)
		logger.Info("Resource metrics enabled",
			"interval", interval,
			"max_samples", maxSamples,
		)
	}

	// Initialize schedule executor and scheduler
	scheduleExecutor := schedule.NewProcessExecutor(logger)
	scheduler := schedule.NewScheduler(scheduleExecutor, cfg.Global.ScheduleHistorySize, logger)

	// Initialize oneshot history
	oneshotHistory := NewOneshotHistory(
		cfg.Global.OneshotHistoryMaxEntries,
		cfg.Global.OneshotHistoryMaxAge,
	)

	// Initialize readiness manager if configured
	var readinessMgr *readiness.Manager
	if cfg.Global.Readiness != nil && cfg.Global.Readiness.Enabled {
		readinessMgr = readiness.NewManager(cfg.Global.Readiness, logger)
		logger.Info("Readiness file manager enabled",
			"path", cfg.Global.Readiness.Path,
			"mode", cfg.Global.Readiness.Mode,
		)
	}

	// Determine configurable timeouts and limits (use config values or defaults)
	dependencyTimeout := DefaultDependencyReadinessTimeout
	if cfg.Global.DependencyTimeout > 0 {
		dependencyTimeout = cfg.Global.DependencyTimeout
	}

	processStopTimeout := DefaultProcessStopTimeout
	if cfg.Global.ProcessStopTimeout > 0 {
		processStopTimeout = cfg.Global.ProcessStopTimeout
	}

	maxProcessScale := MaxProcessScale
	if cfg.Global.MaxProcessScale > 0 {
		maxProcessScale = cfg.Global.MaxProcessScale
	}

	return &Manager{
		config:             cfg,
		logger:             logger,
		auditLogger:        auditLogger,
		processes:          make(map[string]*Supervisor),
		scheduler:          scheduler,
		scheduleExecutor:   scheduleExecutor,
		resourceCollector:  resourceCollector,
		oneshotHistory:     oneshotHistory,
		readinessManager:   readinessMgr,
		shutdownCh:         make(chan struct{}),
		allDeadCh:          make(chan struct{}),
		processDeathCh:     make(chan string, 10),
		startTime:          startTime,
		logBroadcaster:     logpkg.NewLogBroadcaster(),
		dependencyTimeout:  dependencyTimeout,
		processStopTimeout: processStopTimeout,
		maxProcessScale:    maxProcessScale,
	}
}

// ProcessInfo represents process status information returned by ListProcesses.
type ProcessInfo struct {
	Name           string                `json:"name"`
	Type           string                `json:"type"` // oneshot | longrun | scheduled
	State          string                `json:"state"`
	Scale          int                   `json:"scale"`
	DesiredScale   int                   `json:"desired_scale"`
	MaxScale       int                   `json:"max_scale"`
	CPUPercent     float64               `json:"cpu_percent"`
	MemoryRSSBytes uint64                `json:"memory_rss_bytes"`
	MemoryPercent  float64               `json:"memory_percent"`
	Instances      []ProcessInstanceInfo `json:"instances"`
	// Schedule fields (only for scheduled processes)
	Schedule      string `json:"schedule,omitempty"`       // Cron expression
	ScheduleState string `json:"schedule_state,omitempty"` // idle | executing | paused
	NextRun       int64  `json:"next_run,omitempty"`       // Unix timestamp of next scheduled run
	LastRun       int64  `json:"last_run,omitempty"`       // Unix timestamp of last run
	// Execution history fields (only for scheduled processes)
	RunCount     int     `json:"run_count,omitempty"`      // Total execution count
	SuccessRate  float64 `json:"success_rate,omitempty"`   // Success rate (0-100)
	LastExitCode *int    `json:"last_exit_code,omitempty"` // Exit code of last execution (nil if never run)
}

// ProcessInstanceInfo represents instance status within a process.
type ProcessInstanceInfo struct {
	ID             string  `json:"id"`
	State          string  `json:"state"`
	PID            int     `json:"pid"`
	StartedAt      int64   `json:"started_at"`
	RestartCount   int     `json:"restart_count"`
	CPUPercent     float64 `json:"cpu_percent"`
	MemoryRSSBytes uint64  `json:"memory_rss_bytes"`
	MemoryPercent  float64 `json:"memory_percent"`
}

// ListProcesses returns status of all processes including both regular
// and scheduled processes.
func (m *Manager) ListProcesses() []ProcessInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	processes := make([]ProcessInfo, 0, len(m.processes))
	rc := m.resourceCollector

	for name, sup := range m.processes {
		cfg := m.config.Processes[name]

		procType := "longrun" // default
		if cfg != nil && cfg.Type != "" {
			procType = cfg.Type
		}

		var desiredScale int
		var maxScale int
		if cfg != nil {
			desiredScale = cfg.Scale
			maxScale = cfg.MaxScale
		}

		info := ProcessInfo{
			Name:         name,
			Type:         procType,
			State:        string(sup.GetState()),
			Scale:        len(sup.GetInstances()),
			DesiredScale: desiredScale,
			MaxScale:     maxScale,
			Instances:    make([]ProcessInstanceInfo, 0),
		}

		var totalCPU float64
		var totalMem uint64
		var totalMemPct float64

		for _, inst := range sup.GetInstances() {
			instInfo := ProcessInstanceInfo{
				ID:           inst.ID,
				State:        string(inst.State),
				PID:          inst.PID,
				StartedAt:    inst.StartedAt,
				RestartCount: inst.RestartCount,
			}

			if rc != nil {
				if sample, ok := rc.GetLatest(name, inst.ID); ok {
					instInfo.CPUPercent = sample.CPUPercent
					instInfo.MemoryRSSBytes = sample.MemoryRSSBytes
					instInfo.MemoryPercent = float64(sample.MemoryPercent)

					totalCPU += sample.CPUPercent
					totalMem += sample.MemoryRSSBytes
					totalMemPct += float64(sample.MemoryPercent)
				}
			}

			info.Instances = append(info.Instances, instInfo)
		}

		info.CPUPercent = totalCPU
		info.MemoryRSSBytes = totalMem
		info.MemoryPercent = totalMemPct

		processes = append(processes, info)
	}

	// Add scheduled processes from scheduler
	if m.scheduler != nil {
		// Update NextRun times from cron before getting statuses
		m.scheduler.UpdateNextRunTimes()
		for name, job := range m.scheduler.GetAllJobs() {
			status := job.Status()

			// Map job state to process state
			var state string
			switch status.State {
			case "idle":
				state = "scheduled"
			case "executing":
				state = "running"
			case "paused":
				state = "paused"
			default:
				state = status.State
			}

			// Get last exit code from history
			var lastExitCode *int
			if lastEntry, ok := job.History.GetLast(); ok && !lastEntry.EndTime.IsZero() {
				exitCode := lastEntry.ExitCode
				lastExitCode = &exitCode
			}

			info := ProcessInfo{
				Name:          name,
				Type:          "scheduled",
				State:         state,
				Scale:         0,
				DesiredScale:  0,
				MaxScale:      0,
				Instances:     []ProcessInstanceInfo{},
				Schedule:      status.Schedule,
				ScheduleState: status.State,
				NextRun:       status.NextRun.Unix(),
				LastRun:       status.LastRun.Unix(),
				RunCount:      status.Stats.TotalExecutions,
				SuccessRate:   status.Stats.SuccessRate,
				LastExitCode:  lastExitCode,
			}
			processes = append(processes, info)
		}
	}

	return processes
}

// GetResourceCollector returns the resource collector (can be nil if disabled).
func (m *Manager) GetResourceCollector() *metrics.ResourceCollector {
	return m.resourceCollector
}

// LogBroadcaster returns the log broadcaster for real-time log subscriptions.
func (m *Manager) LogBroadcaster() *logpkg.LogBroadcaster {
	return m.logBroadcaster
}
