package proxy

import (
	"net/http"

	"github.com/EdgeFlowCDN/cdn-edge/cache"
	"github.com/EdgeFlowCDN/cdn-edge/config"
	cdnlog "github.com/EdgeFlowCDN/cdn-edge/log"
	"github.com/EdgeFlowCDN/cdn-edge/origin"
)

// Server is the edge proxy server.
type Server struct {
	cfg          *config.Config
	domains      map[string]*config.DomainConfig
	cacheManager *cache.Manager
	fetcher      *origin.Fetcher
	accessLogger *cdnlog.AccessLogger
}

// NewServer creates a new proxy server.
func NewServer(cfg *config.Config, cacheManager *cache.Manager, accessLogger *cdnlog.AccessLogger) *Server {
	domains := make(map[string]*config.DomainConfig)
	for i := range cfg.Domains {
		domains[cfg.Domains[i].Host] = &cfg.Domains[i]
	}

	return &Server{
		cfg:          cfg,
		domains:      domains,
		cacheManager: cacheManager,
		fetcher:      origin.NewFetcher("round-robin"),
		accessLogger: accessLogger,
	}
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	addr := s.cfg.Server.Listen
	cdnlog.Info("starting edge proxy", "addr", addr, "domains", len(s.domains))

	srv := &http.Server{
		Addr:    addr,
		Handler: s,
	}
	return srv.ListenAndServe()
}
