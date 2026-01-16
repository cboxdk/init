package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Process metrics
	ProcessUp = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cbox_init_process_up",
			Help: "Process status (1=running, 0=stopped)",
		},
		[]string{"name", "instance"},
	)

	ProcessRestarts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cbox_init_process_restarts_total",
			Help: "Total number of process restarts",
		},
		[]string{"name", "reason"}, // reason: health_check, crash, manual
	)

	ProcessStartTime = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cbox_init_process_start_time_seconds",
			Help: "Unix timestamp when process started",
		},
		[]string{"name", "instance"},
	)

	ProcessExitCode = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cbox_init_process_last_exit_code",
			Help: "Last exit code of process",
		},
		[]string{"name", "instance"},
	)

	// Health check metrics
	HealthCheckStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cbox_init_health_check_status",
			Help: "Health check status (1=healthy, 0=unhealthy)",
		},
		[]string{"name", "type"},
	)

	HealthCheckDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cbox_init_health_check_duration_seconds",
			Help:    "Health check duration in seconds",
			Buckets: []float64{0.001, 0.01, 0.05, 0.1, 0.5, 1.0, 5.0},
		},
		[]string{"name", "type"},
	)

	HealthCheckTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cbox_init_health_check_total",
			Help: "Total number of health checks performed",
		},
		[]string{"name", "type", "status"}, // status: success, failure
	)

	HealthCheckConsecutiveFails = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cbox_init_health_check_consecutive_fails",
			Help: "Current consecutive health check failures",
		},
		[]string{"name"},
	)

	// Scaling metrics
	ProcessDesiredScale = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cbox_init_process_desired_scale",
			Help: "Desired number of process instances",
		},
		[]string{"name"},
	)

	ProcessCurrentScale = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cbox_init_process_current_scale",
			Help: "Current number of running instances",
		},
		[]string{"name"},
	)

	// Supervisor metrics
	SupervisorUptime = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cbox_init_supervisor_uptime_seconds",
			Help: "Supervisor uptime in seconds",
		},
		[]string{"name"},
	)

	// Lifecycle hook metrics
	HookExecutions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cbox_init_hook_executions_total",
			Help: "Total number of hook executions",
		},
		[]string{"name", "type", "status"}, // type: pre_start, post_start, pre_stop, post_stop; status: success, failure
	)

	HookDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cbox_init_hook_duration_seconds",
			Help:    "Hook execution duration in seconds",
			Buckets: []float64{0.1, 0.5, 1.0, 5.0, 10.0, 30.0, 60.0, 120.0},
		},
		[]string{"name", "type"},
	)

	// Manager metrics
	ManagerProcessCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "cbox_init_manager_process_count",
			Help: "Total number of managed processes",
		},
	)

	ManagerStartTime = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "cbox_init_manager_start_time_seconds",
			Help: "Unix timestamp when manager started",
		},
	)

	ShutdownDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "cbox_init_shutdown_duration_seconds",
			Help:    "Duration of graceful shutdown in seconds",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 180, 300},
		},
	)

	// Resource metrics (CPU, memory, etc.)
	ProcessCPUPercent = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cbox_init_process_cpu_percent",
			Help: "Process CPU usage percentage (per-core, can exceed 100)",
		},
		[]string{"process", "instance"},
	)

	ProcessMemoryBytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cbox_init_process_memory_bytes",
			Help: "Process memory usage in bytes",
		},
		[]string{"process", "instance", "type"}, // type: rss, vms
	)

	ProcessMemoryPercent = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cbox_init_process_memory_percent",
			Help: "Process memory usage as percentage of total system memory",
		},
		[]string{"process", "instance"},
	)

	ProcessThreads = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cbox_init_process_threads",
			Help: "Number of threads in process",
		},
		[]string{"process", "instance"},
	)

	ProcessFileDescriptors = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cbox_init_process_file_descriptors",
			Help: "Number of open file descriptors (Linux only)",
		},
		[]string{"process", "instance"},
	)

	ResourceCollectionDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "cbox_init_resource_collection_duration_seconds",
			Help:    "Time taken to collect resource metrics",
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05},
		},
	)

	ResourceCollectionErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cbox_init_resource_collection_errors_total",
			Help: "Total resource collection errors",
		},
		[]string{"process", "instance"},
	)

	// Build info
	BuildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cbox_init_build_info",
			Help: "PHPeek PM build information",
		},
		[]string{"version", "go_version"},
	)
)

// RecordProcessStart records a process start event
func RecordProcessStart(processName, instanceID string, startTime float64) {
	ProcessUp.WithLabelValues(processName, instanceID).Set(1)
	ProcessStartTime.WithLabelValues(processName, instanceID).Set(startTime)
	ProcessCurrentScale.WithLabelValues(processName).Inc()
}

// RecordProcessStop records a process stop event
func RecordProcessStop(processName, instanceID string, exitCode int) {
	ProcessUp.WithLabelValues(processName, instanceID).Set(0)
	ProcessExitCode.WithLabelValues(processName, instanceID).Set(float64(exitCode))
	ProcessCurrentScale.WithLabelValues(processName).Dec()
}

// RecordProcessRestart records a process restart
func RecordProcessRestart(processName, reason string) {
	ProcessRestarts.WithLabelValues(processName, reason).Inc()
}

// RecordHealthCheck records a health check result
func RecordHealthCheck(processName, checkType string, duration float64, healthy bool) {
	status := "success"
	statusValue := 1.0
	if !healthy {
		status = "failure"
		statusValue = 0.0
	}

	HealthCheckStatus.WithLabelValues(processName, checkType).Set(statusValue)
	HealthCheckDuration.WithLabelValues(processName, checkType).Observe(duration)
	HealthCheckTotal.WithLabelValues(processName, checkType, status).Inc()
}

// RecordHealthCheckFailures records consecutive health check failures
func RecordHealthCheckFailures(processName string, consecutiveFails int) {
	HealthCheckConsecutiveFails.WithLabelValues(processName).Set(float64(consecutiveFails))
}

// RecordHookExecution records a hook execution
func RecordHookExecution(hookName, hookType string, duration float64, success bool) {
	status := "success"
	if !success {
		status = "failure"
	}

	HookExecutions.WithLabelValues(hookName, hookType, status).Inc()
	HookDuration.WithLabelValues(hookName, hookType).Observe(duration)
}

// SetDesiredScale sets the desired process scale
func SetDesiredScale(processName string, scale int) {
	ProcessDesiredScale.WithLabelValues(processName).Set(float64(scale))
}

// SetManagerProcessCount sets the total number of managed processes
func SetManagerProcessCount(count int) {
	ManagerProcessCount.Set(float64(count))
}

// SetManagerStartTime sets the manager start time
func SetManagerStartTime(startTime float64) {
	ManagerStartTime.Set(startTime)
}

// SetBuildInfo sets build information
func SetBuildInfo(version, goVersion string) {
	BuildInfo.WithLabelValues(version, goVersion).Set(1)
}

// RecordShutdownDuration records the duration of graceful shutdown
func RecordShutdownDuration(duration float64) {
	ShutdownDuration.Observe(duration)
}
