package readiness

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"time"
)

// HTTPServer exposes the supervisor's aggregate readiness and liveness over
// HTTP so Kubernetes can use httpGet probes.
//
// Why in addition to the readiness FILE: the file is a passive artifact. If
// cbox-init hangs (event loop wedged) while still alive, a stale file keeps
// reporting "ready". This endpoint is ACTIVE — it is served by cbox-init
// itself, so a wedged supervisor stops answering and the probe fails, which is
// exactly what a liveness/readiness probe must detect.
//
//	/readyz : 200 when all tracked processes are ready, 503 otherwise
//	          (drives the Kubernetes readinessProbe / traffic gating)
//	/livez  : 200 whenever cbox-init can answer at all
//	          (drives the livenessProbe — restart only when the supervisor
//	           itself is unresponsive, NOT merely when the app is unready)
type HTTPServer struct {
	mgr    *Manager
	server *http.Server
	logger *slog.Logger
}

// NewHTTPServer builds the readiness/liveness HTTP server. host defaults to
// 0.0.0.0 (so the kubelet, which probes the pod IP, can reach it).
func NewHTTPServer(mgr *Manager, host string, port int, logger *slog.Logger) *HTTPServer {
	if host == "" {
		host = "0.0.0.0"
	}
	hs := &HTTPServer{mgr: mgr, logger: logger.With("component", "readiness-http")}

	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", hs.handleReadyz)
	mux.HandleFunc("/livez", hs.handleLivez)

	hs.server = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", host, port),
		Handler:           mux,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	return hs
}

func (hs *HTTPServer) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	ready, procs := hs.mgr.Snapshot()
	sort.Slice(procs, func(i, j int) bool { return procs[i].Name < procs[j].Name })

	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{
		"ready":     ready,
		"processes": procs,
	})
}

func (hs *HTTPServer) handleLivez(w http.ResponseWriter, _ *http.Request) {
	// Reaching this handler proves cbox-init's HTTP loop is responsive.
	writeJSON(w, http.StatusOK, map[string]any{"status": "alive"})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// Start runs the server in a background goroutine.
func (hs *HTTPServer) Start() {
	go func() {
		hs.logger.Info("Readiness HTTP server listening",
			"addr", hs.server.Addr, "endpoints", "/readyz,/livez")
		if err := hs.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			hs.logger.Error("Readiness HTTP server stopped", "error", err)
		}
	}()
}

// Stop gracefully shuts the server down.
func (hs *HTTPServer) Stop(ctx context.Context) error {
	return hs.server.Shutdown(ctx)
}
