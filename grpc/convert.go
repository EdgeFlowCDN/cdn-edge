package grpc

import "github.com/EdgeFlowCDN/cdn-edge/config"

// ToEdgeConfigs converts gRPC domain configs to edge config format.
func ToEdgeConfigs(domains []DomainConfig) []config.DomainConfig {
	result := make([]config.DomainConfig, len(domains))
	for i, d := range domains {
		origins := make([]config.OriginConfig, len(d.Origins))
		for j, o := range d.Origins {
			origins[j] = config.OriginConfig{
				Addr:     o.Addr,
				Weight:   o.Weight,
				Priority: o.Priority,
			}
		}
		result[i] = config.DomainConfig{
			Host:    d.Host,
			Origins: origins,
			Cache: config.DomainCacheConfig{
				DefaultTTL:  d.Cache.DefaultTTL,
				IgnoreQuery: d.Cache.IgnoreQuery,
				ForceTTL:    d.Cache.ForceTTL,
			},
		}
	}
	return result
}
