package proxy

import (
	"sync"

	"github.com/EdgeFlowCDN/cdn-edge/config"
)

// ConfigReloader provides thread-safe hot-reloading of domain configurations.
type ConfigReloader struct {
	mu      sync.RWMutex
	domains map[string]*config.DomainConfig
}

// NewConfigReloader creates a ConfigReloader initialized with the given domains.
func NewConfigReloader(domains []config.DomainConfig) *ConfigReloader {
	cr := &ConfigReloader{}
	cr.ReloadDomains(domains)
	return cr
}

// ReloadDomains replaces the entire domains map with a new set of domain configs.
func (cr *ConfigReloader) ReloadDomains(domains []config.DomainConfig) {
	m := make(map[string]*config.DomainConfig, len(domains))
	for i := range domains {
		m[domains[i].Host] = &domains[i]
	}
	cr.mu.Lock()
	cr.domains = m
	cr.mu.Unlock()
}

// GetDomain returns the DomainConfig for the given host and whether it was found.
func (cr *ConfigReloader) GetDomain(host string) (*config.DomainConfig, bool) {
	cr.mu.RLock()
	d, ok := cr.domains[host]
	cr.mu.RUnlock()
	return d, ok
}
