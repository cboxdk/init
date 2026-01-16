package acl

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cboxdk/init/internal/config"
)

// TestNewChecker_Disabled tests that disabled ACL returns nil
func TestNewChecker_Disabled(t *testing.T) {
	cfg := &config.ACLConfig{
		Enabled: false,
	}

	checker, err := NewChecker(cfg)
	if err != nil {
		t.Errorf("Expected no error for disabled ACL, got: %v", err)
	}
	if checker != nil {
		t.Error("Expected nil checker for disabled ACL")
	}
}

// TestNewChecker_NilConfig tests that nil config returns nil
func TestNewChecker_NilConfig(t *testing.T) {
	checker, err := NewChecker(nil)
	if err != nil {
		t.Errorf("Expected no error for nil config, got: %v", err)
	}
	if checker != nil {
		t.Error("Expected nil checker for nil config")
	}
}

// TestNewChecker_InvalidIP tests that invalid IP addresses are rejected
func TestNewChecker_InvalidIP(t *testing.T) {
	cfg := &config.ACLConfig{
		Enabled:   true,
		Mode:      "allow",
		AllowList: []string{"invalid-ip"},
	}

	_, err := NewChecker(cfg)
	if err == nil {
		t.Error("Expected error for invalid IP address")
	}
}

// TestNewChecker_InvalidCIDR tests that invalid CIDR notation is rejected
func TestNewChecker_InvalidCIDR(t *testing.T) {
	cfg := &config.ACLConfig{
		Enabled:   true,
		Mode:      "allow",
		AllowList: []string{"192.168.1.0/99"},
	}

	_, err := NewChecker(cfg)
	if err == nil {
		t.Error("Expected error for invalid CIDR")
	}
}

// TestIsAllowed_AllowMode tests whitelist mode
func TestIsAllowed_AllowMode(t *testing.T) {
	cfg := &config.ACLConfig{
		Enabled:   true,
		Mode:      "allow",
		AllowList: []string{"192.168.1.100", "10.0.0.0/8"},
	}

	checker, err := NewChecker(cfg)
	if err != nil {
		t.Fatalf("Failed to create checker: %v", err)
	}

	tests := []struct {
		name    string
		ip      string
		allowed bool
	}{
		{"allowed IP", "192.168.1.100", true},
		{"allowed CIDR", "10.0.5.25", true},
		{"denied IP", "192.168.1.101", false},
		{"denied IP outside range", "172.16.0.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("Invalid test IP: %s", tt.ip)
			}
			allowed := checker.IsAllowed(ip)
			if allowed != tt.allowed {
				t.Errorf("IsAllowed(%s) = %v, want %v", tt.ip, allowed, tt.allowed)
			}
		})
	}
}

// TestIsAllowed_DenyMode tests blacklist mode
func TestIsAllowed_DenyMode(t *testing.T) {
	cfg := &config.ACLConfig{
		Enabled:  true,
		Mode:     "deny",
		DenyList: []string{"192.168.1.100", "10.0.0.0/8"},
	}

	checker, err := NewChecker(cfg)
	if err != nil {
		t.Fatalf("Failed to create checker: %v", err)
	}

	tests := []struct {
		name    string
		ip      string
		allowed bool
	}{
		{"denied IP", "192.168.1.100", false},
		{"denied CIDR", "10.0.5.25", false},
		{"allowed IP", "192.168.1.101", true},
		{"allowed IP outside range", "172.16.0.1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("Invalid test IP: %s", tt.ip)
			}
			allowed := checker.IsAllowed(ip)
			if allowed != tt.allowed {
				t.Errorf("IsAllowed(%s) = %v, want %v", tt.ip, allowed, tt.allowed)
			}
		})
	}
}

// TestExtractIP_RemoteAddr tests IP extraction from RemoteAddr
func TestExtractIP_RemoteAddr(t *testing.T) {
	cfg := &config.ACLConfig{
		Enabled:    true,
		Mode:       "allow",
		TrustProxy: false,
	}

	checker, err := NewChecker(cfg)
	if err != nil {
		t.Fatalf("Failed to create checker: %v", err)
	}

	tests := []struct {
		name       string
		remoteAddr string
		wantIP     string
		wantErr    bool
	}{
		{"IP with port", "192.168.1.100:12345", "192.168.1.100", false},
		{"IP only", "192.168.1.100", "192.168.1.100", false},
		{"IPv6 with port", "[2001:db8::1]:12345", "2001:db8::1", false},
		{"IPv6 only", "2001:db8::1", "2001:db8::1", false},
		{"invalid", "invalid", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr

			ip, err := checker.ExtractIP(req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractIP() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && ip.String() != tt.wantIP {
				t.Errorf("ExtractIP() = %v, want %v", ip, tt.wantIP)
			}
		})
	}
}

// TestExtractIP_XForwardedFor tests X-Forwarded-For header handling
func TestExtractIP_XForwardedFor(t *testing.T) {
	cfg := &config.ACLConfig{
		Enabled:    true,
		Mode:       "allow",
		TrustProxy: true,
	}

	checker, err := NewChecker(cfg)
	if err != nil {
		t.Fatalf("Failed to create checker: %v", err)
	}

	tests := []struct {
		name       string
		xff        string
		remoteAddr string
		wantIP     string
	}{
		{"single IP", "192.168.1.100", "10.0.0.1:12345", "192.168.1.100"},
		{"multiple IPs (use first)", "192.168.1.100, 10.0.0.1, 10.0.0.2", "10.0.0.3:12345", "192.168.1.100"},
		{"no XFF header", "", "10.0.0.1:12345", "10.0.0.1"},
		{"XFF with spaces", "  192.168.1.100  ", "10.0.0.1:12345", "192.168.1.100"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}

			ip, err := checker.ExtractIP(req)
			if err != nil {
				t.Fatalf("ExtractIP() error = %v", err)
			}
			if ip.String() != tt.wantIP {
				t.Errorf("ExtractIP() = %v, want %v", ip, tt.wantIP)
			}
		})
	}
}

// TestExtractIP_TrustProxyDisabled tests that XFF is ignored when TrustProxy is false
func TestExtractIP_TrustProxyDisabled(t *testing.T) {
	cfg := &config.ACLConfig{
		Enabled:    true,
		Mode:       "allow",
		TrustProxy: false,
	}

	checker, err := NewChecker(cfg)
	if err != nil {
		t.Fatalf("Failed to create checker: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "192.168.1.100")

	ip, err := checker.ExtractIP(req)
	if err != nil {
		t.Fatalf("ExtractIP() error = %v", err)
	}

	// Should use RemoteAddr, not XFF
	if ip.String() != "10.0.0.1" {
		t.Errorf("ExtractIP() = %v, want 10.0.0.1 (XFF should be ignored)", ip)
	}
}

// TestMiddleware_Allowed tests that allowed IPs pass through
func TestMiddleware_Allowed(t *testing.T) {
	cfg := &config.ACLConfig{
		Enabled:   true,
		Mode:      "allow",
		AllowList: []string{"192.168.1.100"},
	}

	checker, err := NewChecker(cfg)
	if err != nil {
		t.Fatalf("Failed to create checker: %v", err)
	}

	handler := checker.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != "OK" {
		t.Errorf("Expected body 'OK', got '%s'", rec.Body.String())
	}
}

// TestMiddleware_Denied tests that denied IPs get 403
func TestMiddleware_Denied(t *testing.T) {
	cfg := &config.ACLConfig{
		Enabled:   true,
		Mode:      "allow",
		AllowList: []string{"192.168.1.100"},
	}

	checker, err := NewChecker(cfg)
	if err != nil {
		t.Fatalf("Failed to create checker: %v", err)
	}

	handler := checker.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.101:12345" // Not in allow list
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", rec.Code)
	}
}

// TestMiddleware_Disabled tests that disabled ACL allows all
func TestMiddleware_Disabled(t *testing.T) {
	var checker *Checker // nil checker = disabled

	handler := checker.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200 (ACL disabled), got %d", rec.Code)
	}
}

// TestIPv6Support tests IPv6 address handling
func TestIPv6Support(t *testing.T) {
	cfg := &config.ACLConfig{
		Enabled:   true,
		Mode:      "allow",
		AllowList: []string{"2001:db8::1", "fe80::/10"},
	}

	checker, err := NewChecker(cfg)
	if err != nil {
		t.Fatalf("Failed to create checker: %v", err)
	}

	tests := []struct {
		name    string
		ip      string
		allowed bool
	}{
		{"allowed IPv6", "2001:db8::1", true},
		{"allowed IPv6 CIDR", "fe80::1", true},
		{"denied IPv6", "2001:db8::2", false},
		{"denied IPv6 outside range", "2001:db9::1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("Invalid test IP: %s", tt.ip)
			}
			allowed := checker.IsAllowed(ip)
			if allowed != tt.allowed {
				t.Errorf("IsAllowed(%s) = %v, want %v", tt.ip, allowed, tt.allowed)
			}
		})
	}
}

// TestNewChecker_InvalidDenyListEntry tests that invalid deny list entries return error
func TestNewChecker_InvalidDenyListEntry(t *testing.T) {
	cfg := &config.ACLConfig{
		Enabled:  true,
		Mode:     "deny",
		DenyList: []string{"invalid-ip"},
	}

	_, err := NewChecker(cfg)
	if err == nil {
		t.Error("Expected error for invalid deny list entry")
	}
}

// TestNewChecker_InvalidDenyListCIDR tests that invalid deny list CIDR returns error
func TestNewChecker_InvalidDenyListCIDR(t *testing.T) {
	cfg := &config.ACLConfig{
		Enabled:  true,
		Mode:     "deny",
		DenyList: []string{"192.168.1.0/99"},
	}

	_, err := NewChecker(cfg)
	if err == nil {
		t.Error("Expected error for invalid deny list CIDR")
	}
}

// TestIsAllowed_NilChecker tests that nil checker allows all IPs
func TestIsAllowed_NilChecker(t *testing.T) {
	var checker *Checker // nil checker

	ip := net.ParseIP("192.168.1.100")
	if !checker.IsAllowed(ip) {
		t.Error("Nil checker should allow all IPs")
	}
}

// TestMiddleware_InvalidIP tests middleware with invalid IP in RemoteAddr
func TestMiddleware_InvalidIP(t *testing.T) {
	cfg := &config.ACLConfig{
		Enabled:   true,
		Mode:      "allow",
		AllowList: []string{"192.168.1.100"},
	}

	checker, err := NewChecker(cfg)
	if err != nil {
		t.Fatalf("Failed to create checker: %v", err)
	}

	handler := checker.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "invalid-ip-address" // Invalid IP
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for invalid IP, got %d", rec.Code)
	}
}

// TestParseAndAddEntry_EmptyEntry tests that empty entries are skipped
func TestParseAndAddEntry_EmptyEntry(t *testing.T) {
	cfg := &config.ACLConfig{
		Enabled:   true,
		Mode:      "allow",
		AllowList: []string{"", "  ", "192.168.1.100"},
	}

	checker, err := NewChecker(cfg)
	if err != nil {
		t.Fatalf("Failed to create checker: %v", err)
	}

	// Should only have one IP in allow list
	if len(checker.allowIPs) != 1 {
		t.Errorf("Expected 1 allowed IP, got %d", len(checker.allowIPs))
	}
}

// TestExtractIP_InvalidXFFWithFallback tests XFF fallback when first IP is invalid
func TestExtractIP_InvalidXFFWithFallback(t *testing.T) {
	cfg := &config.ACLConfig{
		Enabled:    true,
		Mode:       "allow",
		TrustProxy: true,
	}

	checker, err := NewChecker(cfg)
	if err != nil {
		t.Fatalf("Failed to create checker: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "invalid-ip")

	ip, err := checker.ExtractIP(req)
	if err != nil {
		t.Fatalf("ExtractIP() error = %v", err)
	}
	// Should fallback to RemoteAddr
	if ip.String() != "10.0.0.1" {
		t.Errorf("ExtractIP() = %v, want 10.0.0.1 (fallback)", ip)
	}
}

// TestParseAndAddEntry_DenyCIDR tests adding CIDR to deny list
func TestParseAndAddEntry_DenyCIDR(t *testing.T) {
	cfg := &config.ACLConfig{
		Enabled:  true,
		Mode:     "deny",
		DenyList: []string{"192.168.0.0/16", "10.0.0.0/8"},
	}

	checker, err := NewChecker(cfg)
	if err != nil {
		t.Fatalf("Failed to create checker: %v", err)
	}

	// Should have two deny networks
	if len(checker.denyNets) != 2 {
		t.Errorf("Expected 2 deny networks, got %d", len(checker.denyNets))
	}
}
