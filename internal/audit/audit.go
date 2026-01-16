package audit

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// EventType represents the category of audit event
type EventType string

const (
	// API Events
	EventAPIRequest  EventType = "api.request"
	EventAPIResponse EventType = "api.response"
	EventAuthSuccess EventType = "auth.success"
	EventAuthFailure EventType = "auth.failure"

	// Process Events
	EventProcessStart   EventType = "process.start"
	EventProcessStop    EventType = "process.stop"
	EventProcessRestart EventType = "process.restart"
	EventProcessCrash   EventType = "process.crash"
	EventProcessScale   EventType = "process.scale"

	// Configuration Events
	EventConfigLoad   EventType = "config.load"
	EventConfigChange EventType = "config.change"
	EventConfigReload EventType = "config.reload"

	// Security Events
	EventACLDeny      EventType = "acl.deny"
	EventRateLimit    EventType = "rate_limit.exceed"
	EventTLSHandshake EventType = "tls.handshake"

	// System Events
	EventSystemStart    EventType = "system.start"
	EventSystemShutdown EventType = "system.shutdown"
	EventSystemError    EventType = "system.error"
)

// Status represents the outcome of an audited action
type Status string

const (
	StatusSuccess Status = "success"
	StatusFailure Status = "failure"
	StatusError   Status = "error"
)

// Actor represents who/what performed the action
type Actor struct {
	Type string `json:"type"` // "user", "system", "api"
	ID   string `json:"id"`   // User ID, system component name
	IP   string `json:"ip"`   // Source IP address
}

// Resource represents what was affected by the action
type Resource struct {
	Type string `json:"type"` // "process", "config", "api"
	ID   string `json:"id"`   // Process name, config key, endpoint
	Name string `json:"name"` // Human-readable name
}

// Event represents a single audit log entry
type Event struct {
	Timestamp time.Time              `json:"timestamp"`
	EventType EventType              `json:"event_type"`
	Actor     Actor                  `json:"actor"`
	Action    string                 `json:"action"`
	Resource  Resource               `json:"resource"`
	Status    Status                 `json:"status"`
	Message   string                 `json:"message"`
	Context   map[string]interface{} `json:"context,omitempty"`
}

// Logger provides structured audit logging
type Logger struct {
	logger  *slog.Logger
	enabled bool
}

// NewLogger creates a new audit logger
func NewLogger(log *slog.Logger, enabled bool) *Logger {
	return &Logger{
		logger:  log.With("subsystem", "audit"),
		enabled: enabled,
	}
}

// Log logs an audit event
func (l *Logger) Log(event Event) {
	if !l.enabled {
		return
	}

	// Set timestamp if not provided
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Convert to JSON for structured logging
	eventJSON, _ := json.Marshal(event)

	// Log at appropriate level based on status
	switch event.Status {
	case StatusFailure, StatusError:
		l.logger.Error("audit_event",
			"event_type", event.EventType,
			"actor", event.Actor.ID,
			"action", event.Action,
			"resource", event.Resource.ID,
			"status", event.Status,
			"message", event.Message,
			"event_json", string(eventJSON),
		)
	default:
		l.logger.Info("audit_event",
			"event_type", event.EventType,
			"actor", event.Actor.ID,
			"action", event.Action,
			"resource", event.Resource.ID,
			"status", event.Status,
			"message", event.Message,
			"event_json", string(eventJSON),
		)
	}
}

// LogAPIRequest logs an API request
func (l *Logger) LogAPIRequest(ip, method, path, auth string) {
	l.Log(Event{
		EventType: EventAPIRequest,
		Actor: Actor{
			Type: "api",
			ID:   auth,
			IP:   ip,
		},
		Action: method,
		Resource: Resource{
			Type: "api",
			ID:   path,
		},
		Status:  StatusSuccess,
		Message: "API request received",
		Context: map[string]interface{}{
			"method": method,
			"path":   path,
		},
	})
}

// LogAPIResponse logs an API response
func (l *Logger) LogAPIResponse(ip, method, path string, statusCode int, duration time.Duration) {
	status := StatusSuccess
	if statusCode >= 400 {
		status = StatusFailure
	}

	l.Log(Event{
		EventType: EventAPIResponse,
		Actor: Actor{
			Type: "api",
			IP:   ip,
		},
		Action: method,
		Resource: Resource{
			Type: "api",
			ID:   path,
		},
		Status:  status,
		Message: "API response sent",
		Context: map[string]interface{}{
			"status_code":    statusCode,
			"duration_ms":    duration.Milliseconds(),
			"duration_human": duration.String(),
		},
	})
}

// LogAuthFailure logs authentication failure
func (l *Logger) LogAuthFailure(ip, path, reason string) {
	l.Log(Event{
		EventType: EventAuthFailure,
		Actor: Actor{
			Type: "api",
			IP:   ip,
		},
		Action: "authenticate",
		Resource: Resource{
			Type: "api",
			ID:   path,
		},
		Status:  StatusFailure,
		Message: "Authentication failed",
		Context: map[string]interface{}{
			"reason": reason,
		},
	})
}

// LogACLDeny logs ACL denial
func (l *Logger) LogACLDeny(ip, path, reason string) {
	l.Log(Event{
		EventType: EventACLDeny,
		Actor: Actor{
			Type: "api",
			IP:   ip,
		},
		Action: "access",
		Resource: Resource{
			Type: "api",
			ID:   path,
		},
		Status:  StatusFailure,
		Message: "Access denied by ACL",
		Context: map[string]interface{}{
			"reason": reason,
		},
	})
}

// LogRateLimit logs rate limit exceeded
func (l *Logger) LogRateLimit(ip, path string) {
	l.Log(Event{
		EventType: EventRateLimit,
		Actor: Actor{
			Type: "api",
			IP:   ip,
		},
		Action: "access",
		Resource: Resource{
			Type: "api",
			ID:   path,
		},
		Status:  StatusFailure,
		Message: "Rate limit exceeded",
	})
}

// LogProcessStart logs process start
func (l *Logger) LogProcessStart(processName string, pid int, scale int) {
	l.Log(Event{
		EventType: EventProcessStart,
		Actor: Actor{
			Type: "system",
			ID:   "process_manager",
		},
		Action: "start",
		Resource: Resource{
			Type: "process",
			ID:   processName,
			Name: processName,
		},
		Status:  StatusSuccess,
		Message: "Process started",
		Context: map[string]interface{}{
			"pid":   pid,
			"scale": scale,
		},
	})
}

// LogProcessStop logs process stop
func (l *Logger) LogProcessStop(processName string, pid int, reason string) {
	l.Log(Event{
		EventType: EventProcessStop,
		Actor: Actor{
			Type: "system",
			ID:   "process_manager",
		},
		Action: "stop",
		Resource: Resource{
			Type: "process",
			ID:   processName,
			Name: processName,
		},
		Status:  StatusSuccess,
		Message: "Process stopped",
		Context: map[string]interface{}{
			"pid":    pid,
			"reason": reason,
		},
	})
}

// LogProcessCrash logs process crash
func (l *Logger) LogProcessCrash(processName string, pid int, exitCode int, signal string) {
	l.Log(Event{
		EventType: EventProcessCrash,
		Actor: Actor{
			Type: "system",
			ID:   "process_manager",
		},
		Action: "crash",
		Resource: Resource{
			Type: "process",
			ID:   processName,
			Name: processName,
		},
		Status:  StatusError,
		Message: "Process crashed",
		Context: map[string]interface{}{
			"pid":       pid,
			"exit_code": exitCode,
			"signal":    signal,
		},
	})
}

// LogProcessRestart logs process restart
func (l *Logger) LogProcessRestart(processName string, oldPID int, newPID int, reason string) {
	l.Log(Event{
		EventType: EventProcessRestart,
		Actor: Actor{
			Type: "system",
			ID:   "process_manager",
		},
		Action: "restart",
		Resource: Resource{
			Type: "process",
			ID:   processName,
			Name: processName,
		},
		Status:  StatusSuccess,
		Message: "Process restarted",
		Context: map[string]interface{}{
			"old_pid": oldPID,
			"new_pid": newPID,
			"reason":  reason,
		},
	})
}

// LogProcessScale logs process scaling
func (l *Logger) LogProcessScale(processName string, oldScale int, newScale int, actor string) {
	l.Log(Event{
		EventType: EventProcessScale,
		Actor: Actor{
			Type: "system",
			ID:   actor,
		},
		Action: "scale",
		Resource: Resource{
			Type: "process",
			ID:   processName,
			Name: processName,
		},
		Status:  StatusSuccess,
		Message: "Process scaled",
		Context: map[string]interface{}{
			"old_scale": oldScale,
			"new_scale": newScale,
		},
	})
}

// LogConfigLoad logs configuration load
func (l *Logger) LogConfigLoad(configFile string, processCount int) {
	l.Log(Event{
		EventType: EventConfigLoad,
		Actor: Actor{
			Type: "system",
			ID:   "config_loader",
		},
		Action: "load",
		Resource: Resource{
			Type: "config",
			ID:   configFile,
		},
		Status:  StatusSuccess,
		Message: "Configuration loaded",
		Context: map[string]interface{}{
			"process_count": processCount,
		},
	})
}

// LogSystemStart logs system startup
func (l *Logger) LogSystemStart(version string) {
	l.Log(Event{
		EventType: EventSystemStart,
		Actor: Actor{
			Type: "system",
			ID:   "cbox-init",
		},
		Action: "start",
		Resource: Resource{
			Type: "system",
			ID:   "cbox-init",
		},
		Status:  StatusSuccess,
		Message: "PHPeek PM started",
		Context: map[string]interface{}{
			"version": version,
		},
	})
}

// LogSystemShutdown logs system shutdown
func (l *Logger) LogSystemShutdown(reason string, graceful bool) {
	status := StatusSuccess
	if !graceful {
		status = StatusError
	}

	l.Log(Event{
		EventType: EventSystemShutdown,
		Actor: Actor{
			Type: "system",
			ID:   "cbox-init",
		},
		Action: "shutdown",
		Resource: Resource{
			Type: "system",
			ID:   "cbox-init",
		},
		Status:  status,
		Message: "PHPeek PM shutdown",
		Context: map[string]interface{}{
			"reason":   reason,
			"graceful": graceful,
		},
	})
}

// LogSystemError logs system-level error
func (l *Logger) LogSystemError(component string, errorMsg string) {
	l.Log(Event{
		EventType: EventSystemError,
		Actor: Actor{
			Type: "system",
			ID:   component,
		},
		Action: "error",
		Resource: Resource{
			Type: "system",
			ID:   component,
		},
		Status:  StatusError,
		Message: errorMsg,
	})
}

// LogProcessAdded logs when a new process is added
func (l *Logger) LogProcessAdded(name string, command []string, scale int) {
	l.Log(Event{
		EventType: EventProcessStart,
		Actor: Actor{
			Type: "api",
			ID:   "admin",
		},
		Action: "add",
		Resource: Resource{
			Type: "process",
			ID:   name,
		},
		Status:  StatusSuccess,
		Message: fmt.Sprintf("Process %s added", name),
		Context: map[string]interface{}{
			"command": command,
			"scale":   scale,
		},
	})
}

// LogProcessRemoved logs when a process is removed
func (l *Logger) LogProcessRemoved(name string) {
	l.Log(Event{
		EventType: EventProcessStop,
		Actor: Actor{
			Type: "api",
			ID:   "admin",
		},
		Action: "remove",
		Resource: Resource{
			Type: "process",
			ID:   name,
		},
		Status:  StatusSuccess,
		Message: fmt.Sprintf("Process %s removed", name),
	})
}

// LogProcessUpdated logs when a process configuration is updated
func (l *Logger) LogProcessUpdated(name string, command []string, scale int) {
	l.Log(Event{
		EventType: EventProcessRestart,
		Actor: Actor{
			Type: "api",
			ID:   "admin",
		},
		Action: "update",
		Resource: Resource{
			Type: "process",
			ID:   name,
		},
		Status:  StatusSuccess,
		Message: fmt.Sprintf("Process %s updated", name),
		Context: map[string]interface{}{
			"command": command,
			"scale":   scale,
		},
	})
}

// LogConfigSaved logs when configuration is saved to file
func (l *Logger) LogConfigSaved(path string) {
	l.Log(Event{
		EventType: EventSystemStart,
		Actor: Actor{
			Type: "api",
			ID:   "admin",
		},
		Action: "save",
		Resource: Resource{
			Type: "config",
			ID:   path,
		},
		Status:  StatusSuccess,
		Message: fmt.Sprintf("Configuration saved to %s", path),
	})
}

// LogConfigReloaded logs when configuration is reloaded from file
func (l *Logger) LogConfigReloaded(path string) {
	l.Log(Event{
		EventType: EventSystemStart,
		Actor: Actor{
			Type: "api",
			ID:   "admin",
		},
		Action: "reload",
		Resource: Resource{
			Type: "config",
			ID:   path,
		},
		Status:  StatusSuccess,
		Message: fmt.Sprintf("Configuration reloaded from %s", path),
	})
}
