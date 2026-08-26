package tls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/cboxdk/init/internal/config"
)

// Manager manages TLS certificates with auto-reload capability
type Manager struct {
	config   *config.TLSConfig
	logger   *slog.Logger
	certFile string
	keyFile  string
	caFile   string

	mu          sync.RWMutex
	certificate *tls.Certificate
	certPool    *x509.CertPool
	stopCh      chan struct{}
	wg          sync.WaitGroup
}

// NewManager creates a new TLS certificate manager
func NewManager(cfg *config.TLSConfig, logger *slog.Logger) (*Manager, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, fmt.Errorf("TLS not enabled")
	}

	if cfg.CertFile == "" || cfg.KeyFile == "" {
		return nil, fmt.Errorf("cert_file and key_file are required for TLS")
	}

	m := &Manager{
		config:   cfg,
		logger:   logger,
		certFile: cfg.CertFile,
		keyFile:  cfg.KeyFile,
		caFile:   cfg.CAFile,
		stopCh:   make(chan struct{}),
	}

	// A configured min_version below the 1.2 floor is clamped, not obeyed; say
	// so once at startup so the operator is not misled into thinking the weaker
	// floor took effect.
	if isWeakTLSVersion(cfg.MinVersion) {
		logger.Warn("Configured TLS min_version is below the enforced floor; raising to TLS 1.2",
			"configured", cfg.MinVersion,
			"enforced", "TLS 1.2",
		)
	}

	// Load initial certificates
	if err := m.loadCertificates(); err != nil {
		return nil, fmt.Errorf("failed to load certificates: %w", err)
	}

	// Start auto-reload if enabled
	if cfg.AutoReload {
		m.wg.Add(1)
		go m.autoReloadLoop()
	}

	return m, nil
}

// loadCertificates loads certificate and key from files
func (m *Manager) loadCertificates() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Load certificate and key
	cert, err := tls.LoadX509KeyPair(m.certFile, m.keyFile)
	if err != nil {
		return fmt.Errorf("failed to load certificate and key: %w", err)
	}

	m.certificate = &cert

	m.logger.Info("TLS certificates loaded",
		"cert_file", m.certFile,
		"key_file", m.keyFile,
	)

	// Load CA certificate if specified (for mTLS)
	if m.caFile != "" {
		caData, err := os.ReadFile(m.caFile)
		if err != nil {
			return fmt.Errorf("failed to read CA file: %w", err)
		}

		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caData) {
			return fmt.Errorf("failed to parse CA certificate")
		}

		m.certPool = certPool

		m.logger.Info("TLS CA certificate loaded",
			"ca_file", m.caFile,
		)
	}

	return nil
}

// GetCertificate returns the current certificate (for tls.Config.GetCertificate)
func (m *Manager) GetCertificate(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.certificate == nil {
		return nil, fmt.Errorf("no certificate loaded")
	}

	return m.certificate, nil
}

// GetTLSConfig returns a tls.Config configured according to the TLSConfig
func (m *Manager) GetTLSConfig() (*tls.Config, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tlsConfig := &tls.Config{
		GetCertificate: m.GetCertificate,
		MinVersion:     m.parseTLSVersion(m.config.MinVersion),
		ClientAuth:     m.parseClientAuth(m.config.ClientAuth),
	}

	// Set CA pool for client certificate verification (mTLS)
	if m.certPool != nil {
		tlsConfig.ClientCAs = m.certPool
	}

	// Set cipher suites if specified
	if len(m.config.CipherSuites) > 0 {
		ciphers, err := m.parseCipherSuites(m.config.CipherSuites)
		if err != nil {
			return nil, fmt.Errorf("invalid cipher suites: %w", err)
		}
		tlsConfig.CipherSuites = ciphers
	}

	return tlsConfig, nil
}

// parseTLSVersion converts string version to a tls constant, with a hard floor
// at TLS 1.2. TLS 1.0 and 1.1 are deprecated (RFC 8996) and broken; honoring a
// `min_version: "TLS 1.0"` would let a misconfiguration silently downgrade the
// management API onto a protocol with known attacks. An explicit request for a
// sub-1.2 floor is clamped to 1.2 rather than obeyed — the caller logs a warning
// once at startup (see clampedMinVersion). Unknown/empty values also default to
// 1.2.
func (m *Manager) parseTLSVersion(version string) uint16 {
	switch version {
	case "TLS 1.3":
		return tls.VersionTLS13
	default:
		// "TLS 1.2", "TLS 1.1", "TLS 1.0", "", and anything unrecognized all
		// resolve to the 1.2 floor.
		return tls.VersionTLS12
	}
}

// isWeakTLSVersion reports whether the configured min_version names a protocol
// below the enforced 1.2 floor, so the caller can warn that it is being raised.
func isWeakTLSVersion(version string) bool {
	return version == "TLS 1.1" || version == "TLS 1.0"
}

// parseClientAuth converts string to tls.ClientAuthType
func (m *Manager) parseClientAuth(auth string) tls.ClientAuthType {
	switch auth {
	case "request":
		return tls.RequestClientCert
	case "require":
		return tls.RequireAnyClientCert
	case "verify":
		return tls.RequireAndVerifyClientCert
	case "none":
		fallthrough
	default:
		return tls.NoClientCert
	}
}

// parseCipherSuites converts cipher suite names to tls constants
func (m *Manager) parseCipherSuites(suites []string) ([]uint16, error) {
	var ciphers []uint16

	cipherMap := map[string]uint16{
		"TLS_RSA_WITH_AES_128_CBC_SHA":            tls.TLS_RSA_WITH_AES_128_CBC_SHA,
		"TLS_RSA_WITH_AES_256_CBC_SHA":            tls.TLS_RSA_WITH_AES_256_CBC_SHA,
		"TLS_RSA_WITH_AES_128_GCM_SHA256":         tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
		"TLS_RSA_WITH_AES_256_GCM_SHA384":         tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA":    tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
		"TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA":    tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
		"TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA":      tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
		"TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA":      tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256": tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384": tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256":   tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384":   tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305":    tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
		"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305":  tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
		// TLS 1.3 cipher suites
		"TLS_AES_128_GCM_SHA256":       tls.TLS_AES_128_GCM_SHA256,
		"TLS_AES_256_GCM_SHA384":       tls.TLS_AES_256_GCM_SHA384,
		"TLS_CHACHA20_POLY1305_SHA256": tls.TLS_CHACHA20_POLY1305_SHA256,
	}

	for _, suite := range suites {
		cipher, ok := cipherMap[suite]
		if !ok {
			return nil, fmt.Errorf("unknown cipher suite: %s", suite)
		}
		ciphers = append(ciphers, cipher)
	}

	return ciphers, nil
}

// autoReloadLoop periodically checks for certificate changes and reloads
func (m *Manager) autoReloadLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(time.Duration(m.config.AutoReloadInterval) * time.Second)
	defer ticker.Stop()

	m.logger.Info("TLS auto-reload enabled",
		"interval", fmt.Sprintf("%ds", m.config.AutoReloadInterval),
	)

	for {
		select {
		case <-ticker.C:
			if err := m.Reload(); err != nil {
				m.logger.Error("Failed to reload TLS certificates", "error", err)
			}
		case <-m.stopCh:
			m.logger.Info("TLS auto-reload stopped")
			return
		}
	}
}

// Reload reloads certificates from disk
func (m *Manager) Reload() error {
	m.logger.Debug("Reloading TLS certificates")

	if err := m.loadCertificates(); err != nil {
		return fmt.Errorf("failed to reload certificates: %w", err)
	}

	m.logger.Info("TLS certificates reloaded successfully")
	return nil
}

// Stop stops the auto-reload goroutine
func (m *Manager) Stop() {
	if m.config.AutoReload {
		close(m.stopCh)
		m.wg.Wait()
	}
}
