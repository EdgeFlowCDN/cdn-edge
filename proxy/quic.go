package proxy

import (
	"crypto/tls"
	"net/http"

	"github.com/quic-go/quic-go/http3"

	cdnlog "github.com/EdgeFlowCDN/cdn-edge/log"
)

// AltSvcMiddleware wraps an http.Handler to advertise HTTP/3 via the Alt-Svc header.
func AltSvcMiddleware(altSvcValue string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Alt-Svc", altSvcValue)
		next.ServeHTTP(w, r)
	})
}

// ListenAndServeQUIC starts an HTTP/3 (QUIC) server.
func (s *Server) ListenAndServeQUIC(addr string, certStore *CertStore) error {
	cdnlog.Info("starting HTTP/3 QUIC proxy", "addr", addr)

	tlsConfig := &tls.Config{
		GetCertificate: certStore.GetCertificate,
		MinVersion:     tls.VersionTLS13,
		NextProtos:     []string{"h3"},
	}

	srv := &http3.Server{
		Addr:      addr,
		Handler:   s,
		TLSConfig: tlsConfig,
	}

	return srv.ListenAndServe()
}
