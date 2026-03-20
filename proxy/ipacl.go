package proxy

import (
	"net"
	"net/http"
	"sync"

	cdnlog "github.com/EdgeFlowCDN/cdn-edge/log"
)

// IPACLMode defines whether the ACL is a whitelist or blacklist.
type IPACLMode string

const (
	IPACLBlacklist IPACLMode = "blacklist"
	IPACLWhitelist IPACLMode = "whitelist"
)

// IPACL manages IP-based access control lists.
type IPACL struct {
	mu      sync.RWMutex
	mode    IPACLMode
	ips     map[string]bool  // exact IPs
	cidrs   []*net.IPNet     // CIDR ranges
}

// NewIPACL creates a new IP access control list.
func NewIPACL(mode IPACLMode) *IPACL {
	return &IPACL{
		mode: mode,
		ips:  make(map[string]bool),
	}
}

// AddIP adds an IP or CIDR range to the ACL.
func (acl *IPACL) AddIP(ipOrCIDR string) {
	acl.mu.Lock()
	defer acl.mu.Unlock()

	if _, cidr, err := net.ParseCIDR(ipOrCIDR); err == nil {
		acl.cidrs = append(acl.cidrs, cidr)
		return
	}
	if ip := net.ParseIP(ipOrCIDR); ip != nil {
		acl.ips[ip.String()] = true
	}
}

// RemoveIP removes an exact IP from the ACL.
func (acl *IPACL) RemoveIP(ip string) {
	acl.mu.Lock()
	defer acl.mu.Unlock()
	delete(acl.ips, ip)
}

// Contains checks if an IP matches the ACL.
func (acl *IPACL) Contains(ipStr string) bool {
	acl.mu.RLock()
	defer acl.mu.RUnlock()

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	if acl.ips[ip.String()] {
		return true
	}
	for _, cidr := range acl.cidrs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// IsAllowed returns whether a request from the given IP should be allowed.
func (acl *IPACL) IsAllowed(ipStr string) bool {
	matched := acl.Contains(ipStr)
	switch acl.mode {
	case IPACLWhitelist:
		return matched // only allow listed IPs
	case IPACLBlacklist:
		return !matched // block listed IPs
	}
	return true
}

// IPACLMiddleware wraps a handler with IP ACL checking.
func IPACLMiddleware(acl *IPACL, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !acl.IsAllowed(ip) {
			cdnlog.Debug("IP ACL blocked", "ip", ip, "mode", string(acl.mode))
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// PerDomainIPACL manages IP ACLs per domain.
type PerDomainIPACL struct {
	mu   sync.RWMutex
	acls map[string]*IPACL // domain -> ACL
}

// NewPerDomainIPACL creates a per-domain IP ACL manager.
func NewPerDomainIPACL() *PerDomainIPACL {
	return &PerDomainIPACL{
		acls: make(map[string]*IPACL),
	}
}

// SetACL sets the ACL for a domain.
func (pd *PerDomainIPACL) SetACL(domain string, acl *IPACL) {
	pd.mu.Lock()
	pd.acls[domain] = acl
	pd.mu.Unlock()
}

// IsAllowed checks if a request is allowed for the given domain and IP.
func (pd *PerDomainIPACL) IsAllowed(domain, ip string) bool {
	pd.mu.RLock()
	acl, exists := pd.acls[domain]
	pd.mu.RUnlock()
	if !exists {
		return true // no ACL configured = allow all
	}
	return acl.IsAllowed(ip)
}
