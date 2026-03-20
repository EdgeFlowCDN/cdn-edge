package proxy

import (
	"net"
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
	metrics      *Metrics
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
		metrics:      NewMetrics(),
	}
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	addr := s.cfg.Server.Listen
	cdnlog.Info("starting edge proxy", "addr", addr, "domains", len(s.domains))

	srv := &http.Server{
		Addr:    addr,
		Handler: s,
		ConnState: func(conn net.Conn, state http.ConnState) {
			switch state {
			case http.StateNew:
				s.metrics.ConnOpen()
			case http.StateClosed, http.StateHijacked:
				s.metrics.ConnClose()
			}
		},
	}
	return srv.ListenAndServe()
}

// Metrics returns the server's metrics instance.
func (s *Server) Metrics() *Metrics { return s.metrics }

// StartMetricsServer starts the /metrics and /health endpoints on a separate port.
func (s *Server) StartMetricsServer(addr string) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", s.metrics.MetricsHandler())
	mux.HandleFunc("/health", HealthHandler())
	cdnlog.Info("starting metrics server", "addr", addr)
	return http.ListenAndServe(addr, mux)
}
