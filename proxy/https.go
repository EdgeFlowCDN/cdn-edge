package proxy

import (
	"crypto/tls"
	"net/http"
	"sync"

	cdnlog "github.com/EdgeFlowCDN/cdn-edge/log"
)

// CertStore manages TLS certificates for SNI-based HTTPS.
type CertStore struct {
	mu    sync.RWMutex
	certs map[string]*tls.Certificate // host -> cert
}

// NewCertStore creates a new certificate store.
func NewCertStore() *CertStore {
	return &CertStore{
		certs: make(map[string]*tls.Certificate),
	}
}

// LoadCert loads a certificate for a domain from PEM files.
func (cs *CertStore) LoadCert(host, certFile, keyFile string) error {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return err
	}
	cs.mu.Lock()
	cs.certs[host] = &cert
	cs.mu.Unlock()
	cdnlog.Info("loaded TLS certificate", "host", host)
	return nil
}

// SetCert stores a certificate directly.
func (cs *CertStore) SetCert(host string, cert *tls.Certificate) {
	cs.mu.Lock()
	cs.certs[host] = cert
	cs.mu.Unlock()
}

// GetCertificate implements the tls.Config.GetCertificate callback for SNI.
func (cs *CertStore) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	if cert, ok := cs.certs[hello.ServerName]; ok {
		return cert, nil
	}
	// Return first cert as fallback
	for _, cert := range cs.certs {
		return cert, nil
	}
	return nil, nil
}

// ListenAndServeTLS starts the HTTPS server with SNI support.
func (s *Server) ListenAndServeTLS(addr string, certStore *CertStore) error {
	cdnlog.Info("starting HTTPS proxy", "addr", addr)

	tlsConfig := &tls.Config{
		GetCertificate: certStore.GetCertificate,
		MinVersion:     tls.VersionTLS12,
		NextProtos:     []string{"h2", "http/1.1"},
	}

	srv := &http.Server{
		Addr:      addr,
		Handler:   s,
		TLSConfig: tlsConfig,
	}

	return srv.ListenAndServeTLS("", "")
}
