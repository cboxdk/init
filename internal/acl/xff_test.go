package acl

import (
	"net"
	"net/http/httptest"
	"testing"

	"github.com/cboxdk/init/internal/config"
)

func newTestChecker(t *testing.T, cfg *config.ACLConfig) *Checker {
	t.Helper()
	c, err := NewChecker(cfg)
	if err != nil {
		t.Fatalf("NewChecker: %v", err)
	}
	return c
}

// TestExtractIPForwardedForIsReadRightToLeft covers the spoofing shape that made
// X-Forwarded-For unusable as an access-control input: proxies APPEND the peer
// they saw, so anything a client puts in the header ends up to the LEFT of its
// own address. Reading the leftmost entry let a client name its own source IP.
func TestExtractIPForwardedForIsReadRightToLeft(t *testing.T) {
	c := newTestChecker(t, &config.ACLConfig{
		Enabled:        true,
		Mode:           "allow",
		AllowList:      []string{"10.0.0.0/24"},
		TrustProxy:     true,
		TrustedProxies: []string{"192.168.1.10", "192.168.1.11"},
	})

	tests := []struct {
		name   string
		peer   string
		xff    string
		wantIP string
		allow  bool
	}{
		{
			name:   "client forges an allowed address to the left of its own",
			peer:   "192.168.1.10:5555",
			xff:    "10.0.0.5, 203.0.113.7",
			wantIP: "203.0.113.7",
			allow:  false,
		},
		{
			name:   "single proxy hop resolves the real client",
			peer:   "192.168.1.10:5555",
			xff:    "10.0.0.5",
			wantIP: "10.0.0.5",
			allow:  true,
		},
		{
			name:   "chained proxies are skipped to reach the client",
			peer:   "192.168.1.10:5555",
			xff:    "10.0.0.5, 192.168.1.11",
			wantIP: "10.0.0.5",
			allow:  true,
		},
		{
			name:   "an all-proxy chain falls back to the peer",
			peer:   "192.168.1.10:5555",
			xff:    "192.168.1.11, 192.168.1.11",
			wantIP: "192.168.1.10",
			allow:  false,
		},
		{
			name:   "a garbage entry is not walked past",
			peer:   "192.168.1.10:5555",
			xff:    "10.0.0.5, not-an-ip",
			wantIP: "192.168.1.10",
			allow:  false,
		},
		{
			name:   "an untrusted peer's header is ignored entirely",
			peer:   "203.0.113.7:5555",
			xff:    "10.0.0.5",
			wantIP: "203.0.113.7",
			allow:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.peer
			req.Header.Set("X-Forwarded-For", tt.xff)

			ip, err := c.ExtractIP(req)
			if err != nil {
				t.Fatalf("ExtractIP: %v", err)
			}
			if ip.String() != tt.wantIP {
				t.Errorf("extracted %s, want %s", ip, tt.wantIP)
			}
			if got := c.IsAllowed(ip); got != tt.allow {
				t.Errorf("IsAllowed(%s) = %v, want %v", ip, got, tt.allow)
			}
		})
	}
}

// TestExtractIPForwardedForHopCap keeps a header stuffed with entries from
// turning every request into a long scan.
func TestExtractIPForwardedForHopCap(t *testing.T) {
	c := newTestChecker(t, &config.ACLConfig{
		Enabled: true, Mode: "allow", AllowList: []string{"10.0.0.0/8"},
		TrustProxy: true, TrustedProxies: []string{"192.168.1.10"},
	})

	xff := "10.0.0.5"
	for i := 0; i < 200; i++ {
		xff += ", 192.168.1.10"
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.10:5555"
	req.Header.Set("X-Forwarded-For", xff)

	// Every entry within the cap is a trusted proxy, so the walk exhausts and
	// falls back to the peer rather than scanning the whole list.
	ip, err := c.ExtractIP(req)
	if err != nil {
		t.Fatalf("ExtractIP: %v", err)
	}
	if ip.String() != "192.168.1.10" {
		t.Errorf("extracted %s, want the peer 192.168.1.10", ip)
	}
}

// TestNewCheckerRejectsUnknownMode: an unrecognised mode used to fall through to
// deny semantics, so mode: "Allow" turned a whitelist into allow-all.
func TestNewCheckerRejectsUnknownMode(t *testing.T) {
	for _, mode := range []string{"Allow", "whitelist", "allowlist", "ALLOW", "block"} {
		t.Run(mode, func(t *testing.T) {
			_, err := NewChecker(&config.ACLConfig{
				Enabled: true, Mode: mode, AllowList: []string{"10.0.0.1"},
			})
			if err == nil {
				t.Fatalf("mode %q accepted; a typo must not silently allow every address", mode)
			}
		})
	}
}

// TestDenyListAppliesInAllowMode: deny_list was parsed and then ignored in allow
// mode, so "allow this subnet except this host" silently allowed the host.
func TestDenyListAppliesInAllowMode(t *testing.T) {
	c := newTestChecker(t, &config.ACLConfig{
		Enabled:   true,
		Mode:      "allow",
		AllowList: []string{"10.0.0.0/8"},
		DenyList:  []string{"10.0.0.66"},
	})

	if c.IsAllowed(mustIP(t, "10.0.0.5")) != true {
		t.Error("10.0.0.5 is inside the allow list and not denied; want allowed")
	}
	if c.IsAllowed(mustIP(t, "10.0.0.66")) != false {
		t.Error("10.0.0.66 is explicitly denied; want denied even in allow mode")
	}
}

func mustIP(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	if ip == nil {
		t.Fatalf("bad test IP %q", s)
	}
	return ip
}
