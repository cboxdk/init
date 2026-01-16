package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/cboxdk/init/internal/acl"
	"github.com/cboxdk/init/internal/config"
	tlsmgr "github.com/cboxdk/init/internal/tls"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server serves Prometheus metrics
type Server struct {
	port       int
	path       string
	server     *http.Server
	mu         sync.RWMutex // protects server field
	logger     *slog.Logger
	aclConfig  *config.ACLConfig
	aclChecker *acl.Checker
	tlsConfig  *config.TLSConfig
	tlsManager *tlsmgr.Manager
}

// NewServer creates a new metrics server
func NewServer(port int, path string, aclCfg *config.ACLConfig, tlsCfg *config.TLSConfig, log *slog.Logger) *Server {
	if path == "" {
		path = "/metrics"
	}

	// Create ACL checker if enabled
	var aclChecker *acl.Checker
	if aclCfg != nil && aclCfg.Enabled {
		checker, err := acl.NewChecker(aclCfg)
		if err != nil {
			log.Error("Failed to create ACL checker", "error", err)
		} else {
			aclChecker = checker
			log.Info("ACL enabled for metrics", "mode", aclCfg.Mode, "allow_count", len(aclCfg.AllowList), "deny_count", len(aclCfg.DenyList))
		}
	}

	return &Server{
		port:       port,
		path:       path,
		aclConfig:  aclCfg,
		aclChecker: aclChecker,
		tlsConfig:  tlsCfg,
		logger:     log,
	}
}

// Start starts the metrics server
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Prometheus metrics endpoint
	mux.Handle(s.path, promhttp.Handler())

	// Health endpoint for the metrics server itself
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Wrap mux with ACL middleware if enabled (applied to all routes)
	var handler http.Handler = mux
	if s.aclChecker != nil {
		handler = s.aclChecker.Middleware(mux)
		s.logger.Info("ACL middleware enabled for metrics server")
	}

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Setup TLS if enabled
	var scheme string
	if s.tlsConfig != nil && s.tlsConfig.Enabled {
		tlsMgr, err := tlsmgr.NewManager(s.tlsConfig, s.logger)
		if err != nil {
			return fmt.Errorf("failed to create TLS manager: %w", err)
		}

		tlsConf, err := tlsMgr.GetTLSConfig()
		if err != nil {
			return fmt.Errorf("failed to get TLS config: %w", err)
		}

		server.TLSConfig = tlsConf
		s.tlsManager = tlsMgr
		scheme = "https"
	} else {
		scheme = "http"
	}

	// Store server reference under lock
	s.mu.Lock()
	s.server = server
	s.mu.Unlock()

	s.logger.Info("Starting metrics server",
		"scheme", scheme,
		"port", s.port,
		"path", s.path,
		"tls_enabled", s.tlsConfig != nil && s.tlsConfig.Enabled,
	)

	// Start server in background
	go func() {
		var err error
		if s.tlsConfig != nil && s.tlsConfig.Enabled {
			// TLS is configured in server.TLSConfig, but we still need to call ListenAndServeTLS
			// Pass empty cert/key since we're using GetCertificate callback
			err = server.ListenAndServeTLS("", "")
		} else {
			err = server.ListenAndServe()
		}

		if err != nil && err != http.ErrServerClosed {
			s.logger.Error("Metrics server failed", "error", err)
		}
	}()

	return nil
}

// Stop gracefully stops the metrics server
func (s *Server) Stop(ctx context.Context) error {
	s.mu.RLock()
	server := s.server
	s.mu.RUnlock()

	if server == nil {
		return nil
	}

	s.logger.Info("Stopping metrics server")

	// Stop TLS manager if running
	if s.tlsManager != nil {
		s.tlsManager.Stop()
	}

	if err := server.Shutdown(ctx); err != nil {
		s.logger.Error("Failed to stop metrics server gracefully", "error", err)
		return err
	}

	s.logger.Info("Metrics server stopped")
	return nil
}

// Port returns the port the server is listening on
func (s *Server) Port() int {
	return s.port
}
