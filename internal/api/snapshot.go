package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/cboxdk/init/internal/snapshot"
)

// SnapshotControl is the existing coordinator, never an independent checkpoint path.
type SnapshotControl interface {
	Sleep(context.Context) error
	Wake(context.Context) error
	Status() snapshot.Status
}

// SetSnapshotControl binds the qualified warm tier before either listener starts.
func (s *Server) SetSnapshotControl(control SnapshotControl) *Server {
	s.snapshot = control
	return s
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == "/api/v1/snapshot" {
		if r.Method != http.MethodGet {
			s.respondMethodNotAllowed(w, http.MethodGet)
			return
		}
		status := snapshot.Status{State: "unavailable"}
		if s.snapshot != nil {
			status = s.snapshot.Status()
		}
		w.Header().Set("Cache-Control", "no-store")
		s.respondJSON(w, http.StatusOK, status)
		return
	}
	if r.Method != http.MethodPost {
		s.respondMethodNotAllowed(w, http.MethodPost)
		return
	}
	if r.ContentLength != 0 || r.URL.RawQuery != "" {
		s.respondError(w, http.StatusBadRequest, "snapshot actions do not accept options or a force override")
		return
	}
	if s.snapshot == nil {
		s.respondError(w, http.StatusConflict, "sleep is not available for this workload")
		return
	}
	// Keep the ordinary short API deadlines for all other routes. A checkpoint
	// has the same bounded control budget as automatic idling, with time to
	// return its result before the HTTP connection's write deadline.
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(70 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		s.respondError(w, http.StatusServiceUnavailable, "snapshot response deadline could not be established")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), snapshot.DefaultRequestTimeout)
	defer cancel()
	var err error
	if r.URL.Path == "/api/v1/snapshot/sleep" {
		err = s.snapshot.Sleep(ctx)
	} else {
		err = s.snapshot.Wake(ctx)
	}
	if err != nil {
		s.respondSnapshotError(w, r.URL.Path, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.respondJSON(w, http.StatusOK, s.snapshot.Status())
}

// Cause codes for refusals that are not warm-tier outcomes. They appear only
// in error responses; outcome causes come from the snapshot package.
const (
	snapshotCauseBusy        = "workload_busy"
	snapshotCauseUnavailable = "sleep_unavailable"
	snapshotCauseTimeout     = "operation_timeout"
)

// respondSnapshotError answers a failed sleep or wake with why it failed: a
// machine-readable cause, a sentence, and the status observed afterwards. A
// bare "failed" leaves the caller to guess or to go read the logs.
func (s *Server) respondSnapshotError(w http.ResponseWriter, action string, err error) {
	status := s.snapshot.Status()
	code := http.StatusServiceUnavailable
	cause := status.Cause
	// The raw error stays in the log: it can carry internal detail. The
	// response carries the coordinator's own explanation instead, which is the
	// same text the status endpoint reports.
	message := "snapshot operation failed; read runtime state before retrying"
	switch {
	case errors.Is(err, snapshot.ErrWorkloadBusy):
		code, cause, message = http.StatusConflict, snapshotCauseBusy, err.Error()
	case errors.Is(err, snapshot.ErrSleepUnavailable):
		code, cause, message = http.StatusConflict, snapshotCauseUnavailable, err.Error()
		if status.DisabledCause != "" {
			cause = status.DisabledCause
		}
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		code, cause = http.StatusGatewayTimeout, snapshotCauseTimeout
		message = "snapshot operation timed out before it finished; read runtime state before retrying"
	default:
		if status.Message != "" {
			message = status.Message
		}
		s.logger.Warn("Snapshot control failed", "action", action, "cause", cause, "error", err)
	}
	body := map[string]any{"error": message, "snapshot": status}
	if cause != "" {
		body["cause"] = cause
	}
	s.respondJSON(w, code, body)
}
