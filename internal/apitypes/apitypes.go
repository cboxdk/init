// Package apitypes holds the wire types shared by the management API server
// (internal/api) and its client (internal/apiclient). Defining the response
// shapes once here — instead of the server building an ad-hoc map[string]any
// and the client decoding into a separate anonymous struct — gives the two
// sides compile-time linkage, so a field rename can't silently break the client.
package apitypes

import (
	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/logger"
	"github.com/cboxdk/init/internal/process"
)

// ProcessListResponse is the body of GET /api/v1/processes.
type ProcessListResponse struct {
	Processes []process.ProcessInfo `json:"processes"`
}

// ProcessDetailResponse is the body of GET /api/v1/processes/{name}.
type ProcessDetailResponse struct {
	Process string          `json:"process"`
	Config  *config.Process `json:"config"`
}

// LogsResponse is the body of the log endpoints. The per-process endpoint sets
// Process; the stack endpoint sets Scope; both set Limit/Count/Logs. The client
// only reads Logs.
type LogsResponse struct {
	Process string            `json:"process,omitempty"`
	Scope   string            `json:"scope,omitempty"`
	Limit   int               `json:"limit"`
	Count   int               `json:"count"`
	Logs    []logger.LogEntry `json:"logs"`
}

// OneshotHistoryResponse is the body of GET /api/v1/oneshot/history.
type OneshotHistoryResponse struct {
	Executions []process.OneshotExecution `json:"executions"`
	Count      int                        `json:"count"`
	Limit      int                        `json:"limit"`
}
