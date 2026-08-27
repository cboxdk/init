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

	// An unrecognised mode used to fall through to deny-mode semantics, which
	// with an empty deny_list means "allow everyone" — so a typo like
	// mode: "Allow" or "whitelist" silently turned a whitelist into allow-all.
	switch cfg.Mode {
	case "allow", "deny":
	case "":
		// Empty is filled in by SetDefaults; treat it as the safe direction.
	default:
		return nil, fmt.Errorf("invalid acl mode %q (valid: allow, deny)", cfg.Mode)
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

	// An explicit deny always wins, in both modes. Previously deny_list was
	// parsed and then ignored in allow mode, so the natural
	// "allow 10.0.0.0/8 except 10.0.0.66" config silently allowed 10.0.0.66.
	if c.isInDenyList(ip) {
		return false
	}

	// Mode: "allow" (whitelist) - deny all except allowed
	// Mode: "deny" (blacklist) - allow all except denied
	if c.config.Mode == "allow" {
		return c.isInAllowList(ip)
	}
	return true
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
		if ip := c.clientFromXFF(r.Header.Get("X-Forwarded-For")); ip != nil {
			return ip, nil
		}
	}

	return peer, nil
}

// maxXFFHops bounds how far back through X-Forwarded-For we will walk, so a
// header stuffed with thousands of entries cannot turn each request into a long
// scan.
const maxXFFHops = 32

// clientFromXFF picks the real client out of an X-Forwarded-For chain, reading
// RIGHT TO LEFT.
//
// The list is "client, proxy1, proxy2": every standard proxy APPENDS the peer it
// saw (nginx's proxy_add_x_forwarded_for, HAProxy, ingress-nginx), so everything
// to the left of the entries our own trusted proxies added is supplied by the
// client and can say anything. Taking the leftmost value therefore let an
// attacker pick their own source address: sending `X-Forwarded-For: 10.0.0.1`
// through the proxy yields `10.0.0.1, <attacker>` and the ACL saw 10.0.0.1.
//
// Walking from the right and discarding addresses that are themselves trusted
// proxies leaves the first address we did not vouch for — the closest untrusted
// hop, which is the real client. If every entry is a trusted proxy, there is no
// client address to believe and the caller falls back to the peer.
func (c *Checker) clientFromXFF(xff string) net.IP {
	if xff == "" {
		return nil
	}
	parts := strings.Split(xff, ",")
	if len(parts) > maxXFFHops {
		parts = parts[len(parts)-maxXFFHops:]
	}
	for i := len(parts) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(parts[i]))
		if ip == nil {
			// A malformed entry is not something to trust past: stop here rather
			// than skipping over it into client-controlled territory.
			return nil
		}
		if c.isTrustedProxy(ip) {
			continue // one of ours; keep walking left
		}
		return ip
	}
	return nil
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
