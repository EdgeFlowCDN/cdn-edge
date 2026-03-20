package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

// ACMEManager handles automatic certificate issuance and renewal via Let's Encrypt.
type ACMEManager struct {
	manager   *autocert.Manager
	certStore *CertStore
}

// NewACMEManager creates an ACME manager for automatic certificate management.
// cacheDir is where certs are stored on disk. allowedHosts limits which domains can get certs.
func NewACMEManager(cacheDir string, allowedHosts []string, certStore *CertStore) *ACMEManager {
	hostPolicy := autocert.HostWhitelist(allowedHosts...)

	m := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(cacheDir),
		HostPolicy: hostPolicy,
	}

	return &ACMEManager{
		manager:   m,
		certStore: certStore,
	}
}

// GetCertificate returns a certificate for the given ClientHello.
// It first checks the CertStore for manually uploaded certs, then falls back to ACME.
func (am *ACMEManager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	// Check manually uploaded certs first
	cert, err := am.certStore.GetCertificate(hello)
	if err == nil && cert != nil {
		return cert, nil
	}

	// Fall back to ACME
	return am.manager.GetCertificate(hello)
}

// HTTPHandler returns an HTTP handler for ACME HTTP-01 challenges.
// This should be registered on port 80.
func (am *ACMEManager) HTTPHandler(fallback http.Handler) http.Handler {
	return am.manager.HTTPHandler(fallback)
}

// CertRenewer periodically checks and renews certificates that are close to expiry.
type CertRenewer struct {
	mu         sync.Mutex
	certStore  *CertStore
	acmeClient *acme.Client
	accountKey *ecdsa.PrivateKey
	stopCh     chan struct{}
}

// NewCertRenewer creates a certificate renewal checker.
func NewCertRenewer(certStore *CertStore) *CertRenewer {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	return &CertRenewer{
		certStore:  certStore,
		acmeClient: &acme.Client{Key: key},
		accountKey: key,
		stopCh:     make(chan struct{}),
	}
}

// Start begins periodic certificate renewal checks.
func (cr *CertRenewer) Start() {
	go cr.renewLoop()
}

// Stop stops the renewer.
func (cr *CertRenewer) Stop() {
	close(cr.stopCh)
}

func (cr *CertRenewer) renewLoop() {
	// Check daily
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-cr.stopCh:
			return
		case <-ticker.C:
			cr.checkRenewals()
		}
	}
}

func (cr *CertRenewer) checkRenewals() {
	cr.certStore.mu.RLock()
	defer cr.certStore.mu.RUnlock()

	renewBefore := 30 * 24 * time.Hour // renew 30 days before expiry

	for host, cert := range cr.certStore.certs {
		if cert.Leaf == nil {
			leaf, err := x509.ParseCertificate(cert.Certificate[0])
			if err != nil {
				continue
			}
			cert.Leaf = leaf
		}

		if time.Until(cert.Leaf.NotAfter) < renewBefore {
			log.Printf("[acme] certificate for %s expires in %v, needs renewal",
				host, time.Until(cert.Leaf.NotAfter).Round(time.Hour))
		}
	}
}

// GenerateSelfSignedCert creates a self-signed certificate for development/testing.
func GenerateSelfSignedCert(host string) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{host},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, _ := x509.MarshalECPrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	return &tlsCert, nil
}
