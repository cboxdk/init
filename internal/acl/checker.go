package acl

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/cboxdk/init/internal/config"
)

// Checker validates IP addresses against ACL rules
type Checker struct {
	config      *config.ACLConfig
	allowNets   []*net.IPNet
	allowIPs    []net.IP
	denyNets    []*net.IPNet
	denyIPs     []net.IP
	trustProxy  bool
	trustedNets []*net.IPNet
	trustedIPs  []net.IP
}

// NewChecker creates a new ACL checker from configuration
func NewChecker(cfg *config.ACLConfig) (*Checker, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil // ACL disabled
	}

	checker := &Checker{
		config:     cfg,
		trustProxy: cfg.TrustProxy,
	}

	// Parse allow list
	for _, entry := range cfg.AllowList {
		if err := checker.parseAndAddEntry(entry, true); err != nil {
			return nil, fmt.Errorf("invalid allow list entry %q: %w", entry, err)
		}
	}

	// Parse deny list
	for _, entry := range cfg.DenyList {
		if err := checker.parseAndAddEntry(entry, false); err != nil {
			return nil, fmt.Errorf("invalid deny list entry %q: %w", entry, err)
		}
	}

	// Parse trusted-proxy list (whose X-Forwarded-For we will honor).
	for _, entry := range cfg.TrustedProxies {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			_, ipnet, err := net.ParseCIDR(entry)
			if err != nil {
				return nil, fmt.Errorf("invalid trusted_proxies CIDR %q: %w", entry, err)
			}
			checker.trustedNets = append(checker.trustedNets, ipnet)
		} else {
			ip := net.ParseIP(entry)
			if ip == nil {
				return nil, fmt.Errorf("invalid trusted_proxies IP %q", entry)
			}
			checker.trustedIPs = append(checker.trustedIPs, ip)
		}
	}

	return checker, nil
}

// isTrustedProxy reports whether ip is a configured trusted reverse proxy.
func (c *Checker) isTrustedProxy(ip net.IP) bool {
	for _, tip := range c.trustedIPs {
		if ip.Equal(tip) {
			return true
		}
	}
	for _, tnet := range c.trustedNets {
		if tnet.Contains(ip) {
			return true
		}
	}
	return false
}

// parseAndAddEntry parses an IP or CIDR and adds to appropriate list
func (c *Checker) parseAndAddEntry(entry string, isAllow bool) error {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return nil
	}

	// Try parsing as CIDR first
	if strings.Contains(entry, "/") {
		_, ipnet, err := net.ParseCIDR(entry)
		if err != nil {
			return fmt.Errorf("invalid CIDR: %w", err)
		}
		if isAllow {
			c.allowNets = append(c.allowNets, ipnet)
		} else {
			c.denyNets = append(c.denyNets, ipnet)
		}
		return nil
	}

	// Parse as single IP
	ip := net.ParseIP(entry)
	if ip == nil {
		return fmt.Errorf("invalid IP address")
	}
	if isAllow {
		c.allowIPs = append(c.allowIPs, ip)
	} else {
		c.denyIPs = append(c.denyIPs, ip)
	}
	return nil
}

// IsAllowed checks if an IP address is allowed by ACL rules
func (c *Checker) IsAllowed(ip net.IP) bool {
	if c == nil {
		return true // ACL disabled
	}

	// Mode: "allow" (whitelist) - deny all except allowed
	// Mode: "deny" (blacklist) - allow all except denied
	if c.config.Mode == "allow" {
		return c.isInAllowList(ip)
	}
	return !c.isInDenyList(ip)
}

// isInAllowList checks if IP is in allow list (IPs or CIDRs)
func (c *Checker) isInAllowList(ip net.IP) bool {
	// Check individual IPs
	for _, allowIP := range c.allowIPs {
		if ip.Equal(allowIP) {
			return true
		}
	}
	// Check CIDR ranges
	for _, allowNet := range c.allowNets {
		if allowNet.Contains(ip) {
			return true
		}
	}
	return false
}

// isInDenyList checks if IP is in deny list (IPs or CIDRs)
func (c *Checker) isInDenyList(ip net.IP) bool {
	// Check individual IPs
	for _, denyIP := range c.denyIPs {
		if ip.Equal(denyIP) {
			return true
		}
	}
	// Check CIDR ranges
	for _, denyNet := range c.denyNets {
		if denyNet.Contains(ip) {
			return true
		}
	}
	return false
}

// ExtractIP extracts the real client IP from an HTTP request.
func (c *Checker) ExtractIP(r *http.Request) (net.IP, error) {
	// The direct TCP peer.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// No port, treat as IP directly.
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil {
		return nil, fmt.Errorf("invalid IP address: %s", host)
	}

	// Honor X-Forwarded-For only when trust_proxy is enabled AND the direct peer
	// is a configured trusted proxy. Without the peer check, any client
	// connecting directly could spoof X-Forwarded-For to forge an allowed source
	// IP and bypass the ACL. When trusted_proxies is empty, no peer is trusted,
	// so the header is ignored (fail closed).
	if c != nil && c.trustProxy && c.isTrustedProxy(peer) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// "client, proxy1, proxy2" — the leftmost is the original client.
			ips := strings.Split(xff, ",")
			clientIP := strings.TrimSpace(ips[0])
			if ip := net.ParseIP(clientIP); ip != nil {
				return ip, nil
			}
		}
	}

	return peer, nil
}

// Middleware returns an HTTP middleware that enforces ACL rules
func (c *Checker) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If ACL disabled, pass through
		if c == nil {
			next.ServeHTTP(w, r)
			return
		}

		// Extract client IP
		ip, err := c.ExtractIP(r)
		if err != nil {
			http.Error(w, "Unable to determine client IP", http.StatusBadRequest)
			return
		}

		// Check ACL
		if !c.IsAllowed(ip) {
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}

		// IP allowed, continue to next handler
		next.ServeHTTP(w, r)
	})
}
