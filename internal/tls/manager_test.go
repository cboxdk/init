package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/config"
)

// Helper function to create test certificates
func createTestCertificates(t *testing.T) (certFile, keyFile, caFile string, cleanup func()) {
	t.Helper()

	tmpDir := t.TempDir()
	certFile = filepath.Join(tmpDir, "cert.pem")
	keyFile = filepath.Join(tmpDir, "key.pem")
	caFile = filepath.Join(tmpDir, "ca.pem")

	// Generate private key
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	// Create certificate template
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("Failed to generate serial number: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Test Organization"},
			CommonName:   "localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
	}

	// Create self-signed certificate
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	// Write certificate
	certOut, err := os.Create(certFile)
	if err != nil {
		t.Fatalf("Failed to create cert file: %v", err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		t.Fatalf("Failed to write cert: %v", err)
	}
	certOut.Close()

	// Write private key
	keyOut, err := os.Create(keyFile)
	if err != nil {
		t.Fatalf("Failed to create key file: %v", err)
	}
	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("Failed to marshal private key: %v", err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes}); err != nil {
		t.Fatalf("Failed to write key: %v", err)
	}
	keyOut.Close()

	// Create CA certificate (same as server cert for simplicity)
	caOut, err := os.Create(caFile)
	if err != nil {
		t.Fatalf("Failed to create CA file: %v", err)
	}
	if err := pem.Encode(caOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		t.Fatalf("Failed to write CA cert: %v", err)
	}
	caOut.Close()

	cleanup = func() {
		// Cleanup is handled by t.TempDir()
	}

	return certFile, keyFile, caFile, cleanup
}

// TestNewManager_Disabled tests that disabled TLS returns error
func TestNewManager_Disabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	cfg := &config.TLSConfig{
		Enabled: false,
	}

	_, err := NewManager(cfg, logger)
	if err == nil {
		t.Error("Expected error when TLS is disabled, got nil")
	}
}

// TestNewManager_NilConfig tests that nil config returns error
func TestNewManager_NilConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	_, err := NewManager(nil, logger)
	if err == nil {
		t.Error("Expected error when config is nil, got nil")
	}
}

// TestNewManager_MissingCertFile tests that missing cert file returns error
func TestNewManager_MissingCertFile(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	cfg := &config.TLSConfig{
		Enabled: true,
		KeyFile: "key.pem",
	}

	_, err := NewManager(cfg, logger)
	if err == nil {
		t.Error("Expected error when cert_file is missing, got nil")
	}
}

// TestNewManager_MissingKeyFile tests that missing key file returns error
func TestNewManager_MissingKeyFile(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	cfg := &config.TLSConfig{
		Enabled:  true,
		CertFile: "cert.pem",
	}

	_, err := NewManager(cfg, logger)
	if err == nil {
		t.Error("Expected error when key_file is missing, got nil")
	}
}

// TestNewManager_MissingFiles tests that missing cert/key returns error
func TestNewManager_MissingFiles(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	cfg := &config.TLSConfig{
		Enabled:  true,
		CertFile: "/nonexistent/cert.pem",
		KeyFile:  "/nonexistent/key.pem",
	}

	_, err := NewManager(cfg, logger)
	if err == nil {
		t.Error("Expected error when cert/key files don't exist, got nil")
	}
}

// TestNewManager_Success tests successful manager creation
func TestNewManager_Success(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	certFile, keyFile, _, cleanup := createTestCertificates(t)
	defer cleanup()

	cfg := &config.TLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	mgr, err := NewManager(cfg, logger)
	if err != nil {
		t.Fatalf("Unexpected error creating manager: %v", err)
	}

	if mgr == nil {
		t.Fatal("Expected non-nil manager")
	}

	if mgr.certificate == nil {
		t.Error("Expected certificate to be loaded")
	}
}

// TestNewManager_WithCA tests manager creation with CA certificate
func TestNewManager_WithCA(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	certFile, keyFile, caFile, cleanup := createTestCertificates(t)
	defer cleanup()

	cfg := &config.TLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
		CAFile:   caFile,
	}

	mgr, err := NewManager(cfg, logger)
	if err != nil {
		t.Fatalf("Unexpected error creating manager: %v", err)
	}

	if mgr.certPool == nil {
		t.Error("Expected cert pool to be loaded")
	}
}

// TestNewManager_InvalidCAFile tests that invalid CA file returns error
func TestNewManager_InvalidCAFile(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	certFile, keyFile, _, cleanup := createTestCertificates(t)
	defer cleanup()

	cfg := &config.TLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
		CAFile:   "/nonexistent/ca.pem",
	}

	_, err := NewManager(cfg, logger)
	if err == nil {
		t.Error("Expected error when CA file doesn't exist, got nil")
	}
}

// TestNewManager_InvalidCAPEM tests that invalid CA PEM returns error
func TestNewManager_InvalidCAPEM(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	certFile, keyFile, _, cleanup := createTestCertificates(t)
	defer cleanup()

	// Create invalid CA file
	tmpDir := filepath.Dir(certFile)
	invalidCAFile := filepath.Join(tmpDir, "invalid-ca.pem")
	if err := os.WriteFile(invalidCAFile, []byte("invalid pem data"), 0644); err != nil {
		t.Fatalf("Failed to write invalid CA file: %v", err)
	}

	cfg := &config.TLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
		CAFile:   invalidCAFile,
	}

	_, err := NewManager(cfg, logger)
	if err == nil {
		t.Error("Expected error when CA PEM is invalid, got nil")
	}
}

// TestNewManager_WithAutoReload tests manager creation with auto-reload enabled
func TestNewManager_WithAutoReload(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	certFile, keyFile, _, cleanup := createTestCertificates(t)
	defer cleanup()

	cfg := &config.TLSConfig{
		Enabled:            true,
		CertFile:           certFile,
		KeyFile:            keyFile,
		AutoReload:         true,
		AutoReloadInterval: 1, // 1 second for testing
	}

	mgr, err := NewManager(cfg, logger)
	if err != nil {
		t.Fatalf("Unexpected error creating manager: %v", err)
	}
	defer mgr.Stop()

	if mgr == nil {
		t.Fatal("Expected non-nil manager")
	}
}

// TestGetCertificate tests GetCertificate method
func TestGetCertificate(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	certFile, keyFile, _, cleanup := createTestCertificates(t)
	defer cleanup()

	cfg := &config.TLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	mgr, err := NewManager(cfg, logger)
	if err != nil {
		t.Fatalf("Unexpected error creating manager: %v", err)
	}

	cert, err := mgr.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate failed: %v", err)
	}

	if cert == nil {
		t.Error("Expected non-nil certificate")
	}
}

// TestGetCertificate_NoCertificate tests GetCertificate with no certificate loaded
func TestGetCertificate_NoCertificate(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	mgr := &Manager{
		config:      &config.TLSConfig{},
		logger:      logger,
		certificate: nil,
	}

	_, err := mgr.GetCertificate(nil)
	if err == nil {
		t.Error("Expected error when no certificate is loaded, got nil")
	}
}

// TestGetTLSConfig tests GetTLSConfig method
func TestGetTLSConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	certFile, keyFile, _, cleanup := createTestCertificates(t)
	defer cleanup()

	tests := []struct {
		name      string
		config    *config.TLSConfig
		wantErr   bool
		checkFunc func(*testing.T, *tls.Config)
	}{
		{
			name: "basic config",
			config: &config.TLSConfig{
				Enabled:  true,
				CertFile: certFile,
				KeyFile:  keyFile,
			},
			wantErr: false,
			checkFunc: func(t *testing.T, cfg *tls.Config) {
				if cfg.MinVersion != tls.VersionTLS12 {
					t.Errorf("Expected MinVersion TLS 1.2, got %v", cfg.MinVersion)
				}
				if cfg.ClientAuth != tls.NoClientCert {
					t.Errorf("Expected NoClientCert, got %v", cfg.ClientAuth)
				}
			},
		},
		{
			name: "with min version TLS 1.3",
			config: &config.TLSConfig{
				Enabled:    true,
				CertFile:   certFile,
				KeyFile:    keyFile,
				MinVersion: "TLS 1.3",
			},
			wantErr: false,
			checkFunc: func(t *testing.T, cfg *tls.Config) {
				if cfg.MinVersion != tls.VersionTLS13 {
					t.Errorf("Expected MinVersion TLS 1.3, got %v", cfg.MinVersion)
				}
			},
		},
		{
			name: "with client auth request",
			config: &config.TLSConfig{
				Enabled:    true,
				CertFile:   certFile,
				KeyFile:    keyFile,
				ClientAuth: "request",
			},
			wantErr: false,
			checkFunc: func(t *testing.T, cfg *tls.Config) {
				if cfg.ClientAuth != tls.RequestClientCert {
					t.Errorf("Expected RequestClientCert, got %v", cfg.ClientAuth)
				}
			},
		},
		{
			name: "with valid cipher suites",
			config: &config.TLSConfig{
				Enabled:      true,
				CertFile:     certFile,
				KeyFile:      keyFile,
				CipherSuites: []string{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, cfg *tls.Config) {
				if len(cfg.CipherSuites) != 2 {
					t.Errorf("Expected 2 cipher suites, got %d", len(cfg.CipherSuites))
				}
			},
		},
		{
			name: "with invalid cipher suite",
			config: &config.TLSConfig{
				Enabled:      true,
				CertFile:     certFile,
				KeyFile:      keyFile,
				CipherSuites: []string{"TLS_INVALID_CIPHER"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, err := NewManager(tt.config, logger)
			if err != nil {
				t.Fatalf("Failed to create manager: %v", err)
			}

			tlsConfig, err := mgr.GetTLSConfig()
			if (err != nil) != tt.wantErr {
				t.Errorf("GetTLSConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.checkFunc != nil {
				tt.checkFunc(t, tlsConfig)
			}
		})
	}
}

// TestGetTLSConfig_WithCA tests GetTLSConfig with CA certificate
func TestGetTLSConfig_WithCA(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	certFile, keyFile, caFile, cleanup := createTestCertificates(t)
	defer cleanup()

	cfg := &config.TLSConfig{
		Enabled:    true,
		CertFile:   certFile,
		KeyFile:    keyFile,
		CAFile:     caFile,
		ClientAuth: "verify",
	}

	mgr, err := NewManager(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	tlsConfig, err := mgr.GetTLSConfig()
	if err != nil {
		t.Fatalf("GetTLSConfig failed: %v", err)
	}

	if tlsConfig.ClientCAs == nil {
		t.Error("Expected ClientCAs to be set")
	}

	if tlsConfig.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Errorf("Expected RequireAndVerifyClientCert, got %v", tlsConfig.ClientAuth)
	}
}

// TestParseTLSVersion tests TLS version parsing
func TestParseTLSVersion(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	m := &Manager{
		config: &config.TLSConfig{},
		logger: logger,
	}

	tests := []struct {
		version  string
		expected uint16
	}{
		{"TLS 1.0", tls.VersionTLS10},
		{"TLS 1.1", tls.VersionTLS11},
		{"TLS 1.2", tls.VersionTLS12},
		{"TLS 1.3", tls.VersionTLS13},
		{"invalid", tls.VersionTLS12}, // Default
		{"", tls.VersionTLS12},        // Empty defaults to TLS 1.2
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := m.parseTLSVersion(tt.version)
			if got != tt.expected {
				t.Errorf("parseTLSVersion(%s) = %v, want %v", tt.version, got, tt.expected)
			}
		})
	}
}

// TestParseClientAuth tests client auth parsing
func TestParseClientAuth(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	m := &Manager{
		config: &config.TLSConfig{},
		logger: logger,
	}

	tests := []struct {
		auth     string
		expected tls.ClientAuthType
	}{
		{"none", tls.NoClientCert},
		{"request", tls.RequestClientCert},
		{"require", tls.RequireAnyClientCert},
		{"verify", tls.RequireAndVerifyClientCert},
		{"invalid", tls.NoClientCert}, // Default
		{"", tls.NoClientCert},        // Empty defaults to NoClientCert
	}

	for _, tt := range tests {
		t.Run(tt.auth, func(t *testing.T) {
			got := m.parseClientAuth(tt.auth)
			if got != tt.expected {
				t.Errorf("parseClientAuth(%s) = %v, want %v", tt.auth, got, tt.expected)
			}
		})
	}
}

// TestParseCipherSuites tests cipher suite parsing
func TestParseCipherSuites(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	m := &Manager{
		config: &config.TLSConfig{},
		logger: logger,
	}

	tests := []struct {
		name        string
		suites      []string
		shouldError bool
		expectedLen int
	}{
		{
			name:        "valid TLS 1.2 suites",
			suites:      []string{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"},
			shouldError: false,
			expectedLen: 2,
		},
		{
			name:        "valid TLS 1.3 suites",
			suites:      []string{"TLS_AES_128_GCM_SHA256", "TLS_AES_256_GCM_SHA384"},
			shouldError: false,
			expectedLen: 2,
		},
		{
			name:        "mixed TLS 1.2 and 1.3 suites",
			suites:      []string{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", "TLS_AES_128_GCM_SHA256"},
			shouldError: false,
			expectedLen: 2,
		},
		{
			name: "all supported cipher suites",
			suites: []string{
				"TLS_RSA_WITH_AES_128_CBC_SHA",
				"TLS_RSA_WITH_AES_256_CBC_SHA",
				"TLS_RSA_WITH_AES_128_GCM_SHA256",
				"TLS_RSA_WITH_AES_256_GCM_SHA384",
				"TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA",
				"TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA",
				"TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA",
				"TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA",
				"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
				"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
				"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
				"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305",
				"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305",
				"TLS_AES_128_GCM_SHA256",
				"TLS_AES_256_GCM_SHA384",
				"TLS_CHACHA20_POLY1305_SHA256",
			},
			shouldError: false,
			expectedLen: 17,
		},
		{
			name:        "invalid suite",
			suites:      []string{"TLS_INVALID_CIPHER"},
			shouldError: true,
		},
		{
			name:        "empty",
			suites:      []string{},
			shouldError: false,
			expectedLen: 0,
		},
		{
			name:        "nil",
			suites:      nil,
			shouldError: false,
			expectedLen: 0,
		},
		{
			name:        "one valid one invalid",
			suites:      []string{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", "TLS_INVALID_CIPHER"},
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ciphers, err := m.parseCipherSuites(tt.suites)
			if tt.shouldError {
				if err == nil {
					t.Error("Expected error for invalid cipher suite, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if len(ciphers) != tt.expectedLen {
					t.Errorf("Expected %d ciphers, got %d", tt.expectedLen, len(ciphers))
				}
			}
		})
	}
}

// TestReload tests certificate reload functionality
func TestReload(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	certFile, keyFile, _, cleanup := createTestCertificates(t)
	defer cleanup()

	cfg := &config.TLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	mgr, err := NewManager(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Reload should succeed
	if err := mgr.Reload(); err != nil {
		t.Errorf("Reload failed: %v", err)
	}

	// Certificate should still be loaded
	if mgr.certificate == nil {
		t.Error("Certificate should still be loaded after reload")
	}
}

// TestReload_InvalidCert tests reload with invalid certificate
func TestReload_InvalidCert(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	certFile, keyFile, _, cleanup := createTestCertificates(t)
	defer cleanup()

	cfg := &config.TLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	mgr, err := NewManager(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Corrupt the certificate file
	if err := os.WriteFile(certFile, []byte("invalid cert data"), 0644); err != nil {
		t.Fatalf("Failed to write invalid cert: %v", err)
	}

	// Reload should fail
	if err := mgr.Reload(); err == nil {
		t.Error("Expected reload to fail with invalid certificate, got nil")
	}
}

// TestAutoReload tests auto-reload functionality
func TestAutoReload(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	certFile, keyFile, _, cleanup := createTestCertificates(t)
	defer cleanup()

	cfg := &config.TLSConfig{
		Enabled:            true,
		CertFile:           certFile,
		KeyFile:            keyFile,
		AutoReload:         true,
		AutoReloadInterval: 1, // 1 second
	}

	mgr, err := NewManager(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Wait a bit to ensure auto-reload goroutine is running
	time.Sleep(100 * time.Millisecond)

	// Stop should wait for auto-reload to finish
	mgr.Stop()
}

// TestAutoReload_ErrorHandling tests that auto-reload handles errors gracefully
func TestAutoReload_ErrorHandling(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	certFile, keyFile, _, cleanup := createTestCertificates(t)
	defer cleanup()

	cfg := &config.TLSConfig{
		Enabled:            true,
		CertFile:           certFile,
		KeyFile:            keyFile,
		AutoReload:         true,
		AutoReloadInterval: 1, // 1 second
	}

	mgr, err := NewManager(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Stop()

	// Corrupt the certificate file after manager is created
	if err := os.WriteFile(certFile, []byte("invalid cert data"), 0644); err != nil {
		t.Fatalf("Failed to write invalid cert: %v", err)
	}

	// Wait for auto-reload to trigger (should log error but not crash)
	time.Sleep(1500 * time.Millisecond)

	// Manager should still be functional (original cert still loaded)
	cert, err := mgr.GetCertificate(nil)
	if err != nil {
		t.Errorf("GetCertificate should still work with original cert: %v", err)
	}
	if cert == nil {
		t.Error("Expected original certificate to still be available")
	}
}

// TestStop_WithoutAutoReload tests Stop when auto-reload is disabled
func TestStop_WithoutAutoReload(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	certFile, keyFile, _, cleanup := createTestCertificates(t)
	defer cleanup()

	cfg := &config.TLSConfig{
		Enabled:    true,
		CertFile:   certFile,
		KeyFile:    keyFile,
		AutoReload: false,
	}

	mgr, err := NewManager(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Stop should be safe even when auto-reload is not running
	mgr.Stop()
}

// TestConcurrentAccess tests concurrent access to certificate
func TestConcurrentAccess(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	certFile, keyFile, _, cleanup := createTestCertificates(t)
	defer cleanup()

	cfg := &config.TLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	mgr, err := NewManager(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Simulate concurrent reads
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				cert, err := mgr.GetCertificate(nil)
				if err != nil {
					t.Errorf("GetCertificate failed: %v", err)
				}
				if cert == nil {
					t.Error("Expected non-nil certificate")
				}
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestConcurrentReload tests concurrent reload operations
func TestConcurrentReload(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	certFile, keyFile, _, cleanup := createTestCertificates(t)
	defer cleanup()

	cfg := &config.TLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	mgr, err := NewManager(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Simulate concurrent reloads
	done := make(chan bool)
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				if err := mgr.Reload(); err != nil {
					t.Errorf("Reload failed: %v", err)
				}
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 5; i++ {
		<-done
	}
}
