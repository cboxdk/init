package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/config"
)

type logSink struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (s *logSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *logSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// writeCertValidFor emits a self-signed cert/key pair with the given validity
// window, so expiry handling can be exercised without waiting.
func writeCertValidFor(t *testing.T, notBefore, notAfter time.Time) (certFile, keyFile string) {
	t.Helper()

	dir := t.TempDir()
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}

	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "expiry-test", Organization: []string{"Test"}},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	return certFile, keyFile
}

// TestExpiredCertificateIsReported: LoadX509KeyPair does not check validity, so
// an expired certificate loaded silently and startup looked clean. Every client
// then failed the handshake with an error that says nothing about why, and the
// server log had nothing to connect it to.
func TestExpiredCertificateIsReported(t *testing.T) {
	tests := []struct {
		name        string
		notBefore   time.Time
		notAfter    time.Time
		wantInLog   string
		wantNoError bool
	}{
		{
			name:      "expired",
			notBefore: time.Now().Add(-48 * time.Hour),
			notAfter:  time.Now().Add(-time.Hour),
			wantInLog: "EXPIRED",
		},
		{
			name:      "not yet valid",
			notBefore: time.Now().Add(time.Hour),
			notAfter:  time.Now().Add(48 * time.Hour),
			wantInLog: "not valid yet",
		},
		{
			name:      "expiring soon",
			notBefore: time.Now().Add(-time.Hour),
			notAfter:  time.Now().Add(3 * 24 * time.Hour),
			wantInLog: "expires soon",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			certFile, keyFile := writeCertValidFor(t, tt.notBefore, tt.notAfter)
			sink := &logSink{}

			// Loading must still SUCCEED: the operator may be mid-rotation, and
			// refusing to start would turn a warning into an outage.
			if _, err := NewManager(&config.TLSConfig{
				Enabled: true, CertFile: certFile, KeyFile: keyFile,
			}, slog.New(slog.NewTextHandler(sink, &slog.HandlerOptions{Level: slog.LevelWarn}))); err != nil {
				t.Fatalf("NewManager refused to start: %v", err)
			}

			if got := sink.String(); !strings.Contains(got, tt.wantInLog) {
				t.Errorf("no %q in the log; the certificate problem is invisible "+
					"and clients just fail the handshake:\n%s", tt.wantInLog, got)
			}
		})
	}
}

// TestValidCertificateIsQuiet keeps the signal meaningful.
func TestValidCertificateIsQuiet(t *testing.T) {
	certFile, keyFile := writeCertValidFor(t, time.Now().Add(-time.Hour), time.Now().Add(365*24*time.Hour))
	sink := &logSink{}

	if _, err := NewManager(&config.TLSConfig{
		Enabled: true, CertFile: certFile, KeyFile: keyFile,
	}, slog.New(slog.NewTextHandler(sink, &slog.HandlerOptions{Level: slog.LevelWarn}))); err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	if got := sink.String(); strings.Contains(got, "expire") || strings.Contains(got, "EXPIRED") {
		t.Errorf("a year-long certificate produced an expiry warning:\n%s", got)
	}
}
