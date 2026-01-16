package audit

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// TestLogger_Disabled tests that audit logger does nothing when disabled
func TestLogger_Disabled(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	auditLogger := NewLogger(logger, false) // Disabled

	// Try to log various events
	auditLogger.LogSystemStart("1.0.0")
	auditLogger.LogProcessStart("test", 1234, 3)
	auditLogger.LogAuthFailure("1.2.3.4", "/api/test", "invalid token")

	// Buffer should be empty (no logs emitted)
	output := buf.String()
	if output != "" {
		t.Errorf("Expected no output when disabled, got: %s", output)
	}
}

// TestLogger_SystemStart tests system start audit logging
func TestLogger_SystemStart(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	auditLogger := NewLogger(logger, true) // Enabled
	auditLogger.LogSystemStart("1.0.0")

	// Parse output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	// Verify log entry
	if logEntry["msg"] != "audit_event" {
		t.Errorf("Expected msg='audit_event', got: %v", logEntry["msg"])
	}

	if logEntry["event_type"] != string(EventSystemStart) {
		t.Errorf("Expected event_type='%s', got: %v", EventSystemStart, logEntry["event_type"])
	}

	if logEntry["status"] != string(StatusSuccess) {
		t.Errorf("Expected status='%s', got: %v", StatusSuccess, logEntry["status"])
	}

	// Verify embedded event JSON contains version
	eventJSON := logEntry["event_json"].(string)
	if !strings.Contains(eventJSON, "1.0.0") {
		t.Errorf("Expected event_json to contain version '1.0.0', got: %s", eventJSON)
	}
}

// TestLogger_SystemShutdown tests system shutdown audit logging
func TestLogger_SystemShutdown(t *testing.T) {
	tests := []struct {
		name     string
		reason   string
		graceful bool
		wantLog  string
	}{
		{
			name:     "graceful shutdown",
			reason:   "signal: SIGTERM",
			graceful: true,
			wantLog:  "INFO",
		},
		{
			name:     "ungraceful shutdown",
			reason:   "process manager error",
			graceful: false,
			wantLog:  "ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
			logger := slog.New(handler)

			auditLogger := NewLogger(logger, true)
			auditLogger.LogSystemShutdown(tt.reason, tt.graceful)

			// Parse output
			var logEntry map[string]interface{}
			if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
				t.Fatalf("Failed to parse log output: %v", err)
			}

			// Verify log level
			if logEntry["level"].(string) != tt.wantLog {
				t.Errorf("Expected level='%s', got: %v", tt.wantLog, logEntry["level"])
			}

			// Verify event type
			if logEntry["event_type"] != string(EventSystemShutdown) {
				t.Errorf("Expected event_type='%s', got: %v", EventSystemShutdown, logEntry["event_type"])
			}

			// Verify embedded event contains reason
			eventJSON := logEntry["event_json"].(string)
			if !strings.Contains(eventJSON, tt.reason) {
				t.Errorf("Expected event_json to contain reason '%s', got: %s", tt.reason, eventJSON)
			}
		})
	}
}

// TestLogger_ProcessStart tests process start audit logging
func TestLogger_ProcessStart(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	auditLogger := NewLogger(logger, true)
	auditLogger.LogProcessStart("php-fpm", 1234, 5)

	// Parse output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	// Verify event type
	if logEntry["event_type"] != string(EventProcessStart) {
		t.Errorf("Expected event_type='%s', got: %v", EventProcessStart, logEntry["event_type"])
	}

	if logEntry["resource"] != "php-fpm" {
		t.Errorf("Expected resource='php-fpm', got: %v", logEntry["resource"])
	}

	// Verify embedded event contains PID and scale
	eventJSON := logEntry["event_json"].(string)
	if !strings.Contains(eventJSON, "1234") {
		t.Errorf("Expected event_json to contain PID '1234', got: %s", eventJSON)
	}
	if !strings.Contains(eventJSON, `"scale":5`) {
		t.Errorf("Expected event_json to contain scale '5', got: %s", eventJSON)
	}
}

// TestLogger_ProcessStop tests process stop audit logging
func TestLogger_ProcessStop(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	auditLogger := NewLogger(logger, true)
	auditLogger.LogProcessStop("nginx", 5678, "graceful_shutdown")

	// Parse output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	// Verify event type
	if logEntry["event_type"] != string(EventProcessStop) {
		t.Errorf("Expected event_type='%s', got: %v", EventProcessStop, logEntry["event_type"])
	}

	// Verify embedded event contains reason
	eventJSON := logEntry["event_json"].(string)
	if !strings.Contains(eventJSON, "graceful_shutdown") {
		t.Errorf("Expected event_json to contain reason 'graceful_shutdown', got: %s", eventJSON)
	}
}

// TestLogger_ProcessCrash tests process crash audit logging
func TestLogger_ProcessCrash(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	auditLogger := NewLogger(logger, true)
	auditLogger.LogProcessCrash("horizon", 9999, 137, "SIGKILL")

	// Parse output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	// Verify event type
	if logEntry["event_type"] != string(EventProcessCrash) {
		t.Errorf("Expected event_type='%s', got: %v", EventProcessCrash, logEntry["event_type"])
	}

	// Verify log level (crashes should be logged as errors)
	if logEntry["level"].(string) != "ERROR" {
		t.Errorf("Expected level='ERROR', got: %v", logEntry["level"])
	}

	// Verify status
	if logEntry["status"] != string(StatusError) {
		t.Errorf("Expected status='%s', got: %v", StatusError, logEntry["status"])
	}

	// Verify embedded event contains exit code and signal
	eventJSON := logEntry["event_json"].(string)
	if !strings.Contains(eventJSON, `"exit_code":137`) {
		t.Errorf("Expected event_json to contain exit_code '137', got: %s", eventJSON)
	}
	if !strings.Contains(eventJSON, "SIGKILL") {
		t.Errorf("Expected event_json to contain signal 'SIGKILL', got: %s", eventJSON)
	}
}

// TestLogger_ProcessRestart tests process restart audit logging
func TestLogger_ProcessRestart(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	auditLogger := NewLogger(logger, true)
	auditLogger.LogProcessRestart("queue-worker", 1111, 2222, "crash")

	// Parse output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	// Verify event type
	if logEntry["event_type"] != string(EventProcessRestart) {
		t.Errorf("Expected event_type='%s', got: %v", EventProcessRestart, logEntry["event_type"])
	}

	// Verify embedded event contains PIDs and reason
	eventJSON := logEntry["event_json"].(string)
	if !strings.Contains(eventJSON, `"old_pid":1111`) {
		t.Errorf("Expected event_json to contain old_pid '1111', got: %s", eventJSON)
	}
	if !strings.Contains(eventJSON, `"new_pid":2222`) {
		t.Errorf("Expected event_json to contain new_pid '2222', got: %s", eventJSON)
	}
	if !strings.Contains(eventJSON, "crash") {
		t.Errorf("Expected event_json to contain reason 'crash', got: %s", eventJSON)
	}
}

// TestLogger_ProcessScale tests process scaling audit logging
func TestLogger_ProcessScale(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	auditLogger := NewLogger(logger, true)
	auditLogger.LogProcessScale("worker", 3, 5, "api_request")

	// Parse output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	// Verify event type
	if logEntry["event_type"] != string(EventProcessScale) {
		t.Errorf("Expected event_type='%s', got: %v", EventProcessScale, logEntry["event_type"])
	}

	// Verify embedded event contains scale info
	eventJSON := logEntry["event_json"].(string)
	if !strings.Contains(eventJSON, `"old_scale":3`) {
		t.Errorf("Expected event_json to contain old_scale '3', got: %s", eventJSON)
	}
	if !strings.Contains(eventJSON, `"new_scale":5`) {
		t.Errorf("Expected event_json to contain new_scale '5', got: %s", eventJSON)
	}
}

// TestLogger_APIRequest tests API request audit logging
func TestLogger_APIRequest(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	auditLogger := NewLogger(logger, true)
	auditLogger.LogAPIRequest("10.0.1.5", "GET", "/api/v1/status", "user@example.com")

	// Parse output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	// Verify event type
	if logEntry["event_type"] != string(EventAPIRequest) {
		t.Errorf("Expected event_type='%s', got: %v", EventAPIRequest, logEntry["event_type"])
	}

	// Verify embedded event contains request details
	eventJSON := logEntry["event_json"].(string)
	if !strings.Contains(eventJSON, "10.0.1.5") {
		t.Errorf("Expected event_json to contain IP '10.0.1.5', got: %s", eventJSON)
	}
	if !strings.Contains(eventJSON, "GET") {
		t.Errorf("Expected event_json to contain method 'GET', got: %s", eventJSON)
	}
	if !strings.Contains(eventJSON, "/api/v1/status") {
		t.Errorf("Expected event_json to contain path '/api/v1/status', got: %s", eventJSON)
	}
}

// TestLogger_APIResponse tests API response audit logging
func TestLogger_APIResponse(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantLevel  string
		wantStatus Status
	}{
		{
			name:       "success response",
			statusCode: 200,
			wantLevel:  "info",
			wantStatus: StatusSuccess,
		},
		{
			name:       "client error",
			statusCode: 400,
			wantLevel:  "info",
			wantStatus: StatusFailure,
		},
		{
			name:       "server error",
			statusCode: 500,
			wantLevel:  "info",
			wantStatus: StatusFailure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
			logger := slog.New(handler)

			auditLogger := NewLogger(logger, true)
			auditLogger.LogAPIResponse("10.0.1.5", "POST", "/api/v1/process/start", tt.statusCode, 150*time.Millisecond)

			// Parse output
			var logEntry map[string]interface{}
			if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
				t.Fatalf("Failed to parse log output: %v", err)
			}

			// Verify event type
			if logEntry["event_type"] != string(EventAPIResponse) {
				t.Errorf("Expected event_type='%s', got: %v", EventAPIResponse, logEntry["event_type"])
			}

			// Verify status
			if logEntry["status"] != string(tt.wantStatus) {
				t.Errorf("Expected status='%s', got: %v", tt.wantStatus, logEntry["status"])
			}

			// Verify embedded event contains status code and duration
			eventJSON := logEntry["event_json"].(string)
			if !strings.Contains(eventJSON, `"status_code"`) {
				t.Errorf("Expected event_json to contain status_code, got: %s", eventJSON)
			}
			if !strings.Contains(eventJSON, `"duration_ms"`) {
				t.Errorf("Expected event_json to contain duration_ms, got: %s", eventJSON)
			}
		})
	}
}

// TestLogger_AuthFailure tests authentication failure audit logging
func TestLogger_AuthFailure(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	auditLogger := NewLogger(logger, true)
	auditLogger.LogAuthFailure("192.168.1.100", "/api/v1/admin", "invalid bearer token")

	// Parse output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	// Verify event type
	if logEntry["event_type"] != string(EventAuthFailure) {
		t.Errorf("Expected event_type='%s', got: %v", EventAuthFailure, logEntry["event_type"])
	}

	// Verify status (auth failures should be logged as failures, not errors)
	if logEntry["status"] != string(StatusFailure) {
		t.Errorf("Expected status='%s', got: %v", StatusFailure, logEntry["status"])
	}

	// Auth failures are errors (security events)
	if logEntry["level"].(string) != "ERROR" {
		t.Errorf("Expected level='ERROR', got: %v", logEntry["level"])
	}

	// Verify embedded event contains failure reason
	eventJSON := logEntry["event_json"].(string)
	if !strings.Contains(eventJSON, "invalid bearer token") {
		t.Errorf("Expected event_json to contain reason 'invalid bearer token', got: %s", eventJSON)
	}
}

// TestLogger_ACLDeny tests ACL denial audit logging
func TestLogger_ACLDeny(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	auditLogger := NewLogger(logger, true)
	auditLogger.LogACLDeny("203.0.113.50", "/metrics", "IP not in allow list")

	// Parse output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	// Verify event type
	if logEntry["event_type"] != string(EventACLDeny) {
		t.Errorf("Expected event_type='%s', got: %v", EventACLDeny, logEntry["event_type"])
	}

	// Verify status
	if logEntry["status"] != string(StatusFailure) {
		t.Errorf("Expected status='%s', got: %v", StatusFailure, logEntry["status"])
	}

	// ACL denials are errors (security events)
	if logEntry["level"].(string) != "ERROR" {
		t.Errorf("Expected level='ERROR', got: %v", logEntry["level"])
	}

	// Verify embedded event contains IP and reason
	eventJSON := logEntry["event_json"].(string)
	if !strings.Contains(eventJSON, "203.0.113.50") {
		t.Errorf("Expected event_json to contain IP '203.0.113.50', got: %s", eventJSON)
	}
	if !strings.Contains(eventJSON, "IP not in allow list") {
		t.Errorf("Expected event_json to contain reason, got: %s", eventJSON)
	}
}

// TestLogger_RateLimit tests rate limit audit logging
func TestLogger_RateLimit(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	auditLogger := NewLogger(logger, true)
	auditLogger.LogRateLimit("10.0.2.50", "/api/v1/process/restart")

	// Parse output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	// Verify event type
	if logEntry["event_type"] != string(EventRateLimit) {
		t.Errorf("Expected event_type='%s', got: %v", EventRateLimit, logEntry["event_type"])
	}

	// Verify status
	if logEntry["status"] != string(StatusFailure) {
		t.Errorf("Expected status='%s', got: %v", StatusFailure, logEntry["status"])
	}

	// Rate limits are errors (security/abuse events)
	if logEntry["level"].(string) != "ERROR" {
		t.Errorf("Expected level='ERROR', got: %v", logEntry["level"])
	}
}

// TestLogger_ConfigLoad tests configuration load audit logging
func TestLogger_ConfigLoad(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	auditLogger := NewLogger(logger, true)
	auditLogger.LogConfigLoad("/etc/cbox-init/cbox-init.yaml", 5)

	// Parse output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	// Verify event type
	if logEntry["event_type"] != string(EventConfigLoad) {
		t.Errorf("Expected event_type='%s', got: %v", EventConfigLoad, logEntry["event_type"])
	}

	// Verify embedded event contains process count
	eventJSON := logEntry["event_json"].(string)
	if !strings.Contains(eventJSON, `"process_count":5`) {
		t.Errorf("Expected event_json to contain process_count '5', got: %s", eventJSON)
	}
}

// TestLogger_TimestampAutoSet tests that timestamp is set automatically
func TestLogger_TimestampAutoSet(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	auditLogger := NewLogger(logger, true)

	// Log event without explicitly setting timestamp
	beforeLog := time.Now()
	auditLogger.LogSystemStart("1.0.0")
	afterLog := time.Now()

	// Parse embedded event JSON to check timestamp
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	eventJSON := logEntry["event_json"].(string)
	var event Event
	if err := json.Unmarshal([]byte(eventJSON), &event); err != nil {
		t.Fatalf("Failed to parse event JSON: %v", err)
	}

	// Verify timestamp is within expected range
	if event.Timestamp.Before(beforeLog) || event.Timestamp.After(afterLog) {
		t.Errorf("Timestamp %v is not between %v and %v", event.Timestamp, beforeLog, afterLog)
	}

	// Verify timestamp is not zero
	if event.Timestamp.IsZero() {
		t.Error("Expected timestamp to be set automatically, got zero time")
	}
}

// TestLogger_JSONMarshaling tests that all event fields marshal correctly
func TestLogger_JSONMarshaling(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	auditLogger := NewLogger(logger, true)
	auditLogger.LogProcessStart("test-process", 12345, 3)

	// Parse log entry
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	// Parse embedded event JSON
	eventJSON := logEntry["event_json"].(string)
	var event Event
	if err := json.Unmarshal([]byte(eventJSON), &event); err != nil {
		t.Fatalf("Failed to parse event JSON: %v", err)
	}

	// Verify all fields are populated
	if event.Timestamp.IsZero() {
		t.Error("Expected timestamp to be set")
	}
	if event.EventType != EventProcessStart {
		t.Errorf("Expected event_type='%s', got: %s", EventProcessStart, event.EventType)
	}
	if event.Actor.Type == "" {
		t.Error("Expected actor.type to be set")
	}
	if event.Action == "" {
		t.Error("Expected action to be set")
	}
	if event.Resource.Type == "" {
		t.Error("Expected resource.type to be set")
	}
	if event.Status == "" {
		t.Error("Expected status to be set")
	}
	if event.Message == "" {
		t.Error("Expected message to be set")
	}
	if event.Context == nil {
		t.Error("Expected context to be set")
	}
}

// TestLogger_SystemError tests system error audit logging
func TestLogger_SystemError(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	auditLogger := NewLogger(logger, true)
	auditLogger.LogSystemError("scheduler", "failed to execute cron job")

	// Parse output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	// Verify event type
	if logEntry["event_type"] != string(EventSystemError) {
		t.Errorf("Expected event_type='%s', got: %v", EventSystemError, logEntry["event_type"])
	}

	// Verify status is error
	if logEntry["status"] != string(StatusError) {
		t.Errorf("Expected status='%s', got: %v", StatusError, logEntry["status"])
	}

	// Verify embedded event contains error message
	eventJSON := logEntry["event_json"].(string)
	if !strings.Contains(eventJSON, "failed to execute cron job") {
		t.Errorf("Expected event_json to contain error message, got: %s", eventJSON)
	}
	if !strings.Contains(eventJSON, "scheduler") {
		t.Errorf("Expected event_json to contain component 'scheduler', got: %s", eventJSON)
	}
}

// TestLogger_ProcessAdded tests process added audit logging
func TestLogger_ProcessAdded(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	auditLogger := NewLogger(logger, true)
	auditLogger.LogProcessAdded("new-worker", []string{"php", "artisan", "queue:work"}, 3)

	// Parse output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	// Verify event type (uses EventProcessStart)
	if logEntry["event_type"] != string(EventProcessStart) {
		t.Errorf("Expected event_type='%s', got: %v", EventProcessStart, logEntry["event_type"])
	}

	// Verify resource is the process name
	if logEntry["resource"] != "new-worker" {
		t.Errorf("Expected resource='new-worker', got: %v", logEntry["resource"])
	}

	// Verify status is success
	if logEntry["status"] != string(StatusSuccess) {
		t.Errorf("Expected status='%s', got: %v", StatusSuccess, logEntry["status"])
	}

	// Verify embedded event contains command and scale
	eventJSON := logEntry["event_json"].(string)
	if !strings.Contains(eventJSON, "queue:work") {
		t.Errorf("Expected event_json to contain command 'queue:work', got: %s", eventJSON)
	}
	if !strings.Contains(eventJSON, `"scale":3`) {
		t.Errorf("Expected event_json to contain scale '3', got: %s", eventJSON)
	}
}

// TestLogger_ProcessRemoved tests process removed audit logging
func TestLogger_ProcessRemoved(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	auditLogger := NewLogger(logger, true)
	auditLogger.LogProcessRemoved("old-worker")

	// Parse output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	// Verify event type (uses EventProcessStop)
	if logEntry["event_type"] != string(EventProcessStop) {
		t.Errorf("Expected event_type='%s', got: %v", EventProcessStop, logEntry["event_type"])
	}

	// Verify resource is the process name
	if logEntry["resource"] != "old-worker" {
		t.Errorf("Expected resource='old-worker', got: %v", logEntry["resource"])
	}

	// Verify embedded event contains removal message
	eventJSON := logEntry["event_json"].(string)
	if !strings.Contains(eventJSON, "removed") {
		t.Errorf("Expected event_json to contain 'removed', got: %s", eventJSON)
	}
}

// TestLogger_ProcessUpdated tests process updated audit logging
func TestLogger_ProcessUpdated(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	auditLogger := NewLogger(logger, true)
	auditLogger.LogProcessUpdated("horizon", []string{"php", "artisan", "horizon"}, 5)

	// Parse output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	// Verify event type (uses EventProcessRestart)
	if logEntry["event_type"] != string(EventProcessRestart) {
		t.Errorf("Expected event_type='%s', got: %v", EventProcessRestart, logEntry["event_type"])
	}

	// Verify resource is the process name
	if logEntry["resource"] != "horizon" {
		t.Errorf("Expected resource='horizon', got: %v", logEntry["resource"])
	}

	// Verify embedded event contains updated message
	eventJSON := logEntry["event_json"].(string)
	if !strings.Contains(eventJSON, "updated") {
		t.Errorf("Expected event_json to contain 'updated', got: %s", eventJSON)
	}
	if !strings.Contains(eventJSON, `"scale":5`) {
		t.Errorf("Expected event_json to contain scale '5', got: %s", eventJSON)
	}
}

// TestLogger_ConfigSaved tests config saved audit logging
func TestLogger_ConfigSaved(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	auditLogger := NewLogger(logger, true)
	auditLogger.LogConfigSaved("/etc/cbox-init/config.yaml")

	// Parse output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	// Verify event type (uses EventSystemStart)
	if logEntry["event_type"] != string(EventSystemStart) {
		t.Errorf("Expected event_type='%s', got: %v", EventSystemStart, logEntry["event_type"])
	}

	// Verify status is success
	if logEntry["status"] != string(StatusSuccess) {
		t.Errorf("Expected status='%s', got: %v", StatusSuccess, logEntry["status"])
	}

	// Verify embedded event contains path
	eventJSON := logEntry["event_json"].(string)
	if !strings.Contains(eventJSON, "/etc/cbox-init/config.yaml") {
		t.Errorf("Expected event_json to contain config path, got: %s", eventJSON)
	}
	if !strings.Contains(eventJSON, "saved") {
		t.Errorf("Expected event_json to contain 'saved', got: %s", eventJSON)
	}
}

// TestLogger_ConfigReloaded tests config reloaded audit logging
func TestLogger_ConfigReloaded(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	auditLogger := NewLogger(logger, true)
	auditLogger.LogConfigReloaded("/etc/cbox-init/config.yaml")

	// Parse output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	// Verify event type (uses EventSystemStart)
	if logEntry["event_type"] != string(EventSystemStart) {
		t.Errorf("Expected event_type='%s', got: %v", EventSystemStart, logEntry["event_type"])
	}

	// Verify status is success
	if logEntry["status"] != string(StatusSuccess) {
		t.Errorf("Expected status='%s', got: %v", StatusSuccess, logEntry["status"])
	}

	// Verify embedded event contains path
	eventJSON := logEntry["event_json"].(string)
	if !strings.Contains(eventJSON, "/etc/cbox-init/config.yaml") {
		t.Errorf("Expected event_json to contain config path, got: %s", eventJSON)
	}
	if !strings.Contains(eventJSON, "reloaded") {
		t.Errorf("Expected event_json to contain 'reloaded', got: %s", eventJSON)
	}
}
