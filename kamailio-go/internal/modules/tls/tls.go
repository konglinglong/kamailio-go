// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * tls module - TLS certificate management and SNI dispatch, matching
 * C tls_domain.c / tls_mod.c.
 *
 * Loads TLS certificates from disk, exposes SNI-based certificate selection
 * (GetCertificate), supports hot reload (ReloadAll) and peer-certificate
 * verification (VerifyPeer). The module is safe for concurrent use.
 */

package tls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"sync"
	"time"
)

// VerifyMode controls client-certificate verification, mirroring Kamailio's
// verify_client options.
type VerifyMode int

const (
	// VerifyNone does not request or verify client certificates.
	VerifyNone VerifyMode = iota
	// VerifyOptional requests but does not require a client certificate.
	VerifyOptional
	// VerifyRequired requires and verifies a client certificate.
	VerifyRequired
)

// Certificate holds a loaded TLS certificate together with its metadata.
type Certificate struct {
	Name     string
	CertFile string
	KeyFile  string
	Cert     *tls.Certificate
	Expires  time.Time
	Domains  []string // SAN DNS names
}

// TLSModule manages TLS certificates and provides SNI dispatch and peer
// verification. It is safe for concurrent use.
type TLSModule struct {
	mu        sync.RWMutex
	config    *tls.Config
	certs     map[string]*Certificate
	domainMap map[string]string // domain -> cert name
	verify    VerifyMode
}

// New creates a TLSModule with empty state and a default verify mode of
// VerifyNone.
func New() *TLSModule {
	return &TLSModule{
		certs:     make(map[string]*Certificate),
		domainMap: make(map[string]string),
		verify:    VerifyNone,
	}
}

// LoadCertificate loads a PEM certificate/key pair from certFile and keyFile
// and registers it under name. The certificate's expiry and SAN domains are
// parsed from the leaf certificate.
//
//	C: tls_load_cert() / tls_add_certificate()
func (m *TLSModule) LoadCertificate(name, certFile, keyFile string) error {
	if name == "" {
		return fmt.Errorf("tls: empty certificate name")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("tls: load key pair %q: %w", name, err)
	}
	if len(cert.Certificate) == 0 {
		return fmt.Errorf("tls: certificate %q has no leaf", name)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return fmt.Errorf("tls: parse leaf %q: %w", name, err)
	}

	c := &Certificate{
		Name:     name,
		CertFile: certFile,
		KeyFile:  keyFile,
		Cert:     &cert,
		Expires:  leaf.NotAfter,
		Domains:  append([]string(nil), leaf.DNSNames...),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.certs == nil {
		m.certs = make(map[string]*Certificate)
	}
	m.certs[name] = c
	return nil
}

// GetCertificateByName returns the certificate registered under name, or
// nil if none exists.
func (m *TLSModule) GetCertificateByName(name string) *Certificate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.certs[name]
}

// GetCertificate returns the certificate matching the given domain (SNI).
// The lookup first consults the explicit domain map and then falls back to
// the certificates' SAN DNS names.
//
//	C: tls_get_certificate() / SNI callback
func (m *TLSModule) GetCertificate(domain string) *Certificate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// explicit domain mapping
	if name, ok := m.domainMap[domain]; ok {
		if c, ok := m.certs[name]; ok {
			return c
		}
	}
	// SAN DNS name match
	for _, c := range m.certs {
		for _, d := range c.Domains {
			if d == domain {
				return c
			}
		}
	}
	return nil
}

// ReloadAll reloads every registered certificate from its on-disk files.
//
//	C: tls_reload() / tls_rpc_reload
func (m *TLSModule) ReloadAll() error {
	m.mu.RLock()
	names := make([]string, 0, len(m.certs))
	snap := make(map[string]*Certificate, len(m.certs))
	for name, c := range m.certs {
		names = append(names, name)
		snap[name] = &Certificate{
			Name:     c.Name,
			CertFile: c.CertFile,
			KeyFile:  c.KeyFile,
		}
	}
	m.mu.RUnlock()

	// Reload each certificate outside the lock, then swap in the results.
	loaded := make(map[string]*Certificate, len(names))
	for _, name := range names {
		old := snap[name]
		cert, err := tls.LoadX509KeyPair(old.CertFile, old.KeyFile)
		if err != nil {
			return fmt.Errorf("tls: reload %q: %w", name, err)
		}
		leaf, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return fmt.Errorf("tls: reload parse leaf %q: %w", name, err)
		}
		loaded[name] = &Certificate{
			Name:     old.Name,
			CertFile: old.CertFile,
			KeyFile:  old.KeyFile,
			Cert:     &cert,
			Expires:  leaf.NotAfter,
			Domains:  append([]string(nil), leaf.DNSNames...),
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for name, c := range loaded {
		m.certs[name] = c
	}
	return nil
}

// VerifyPeer validates a peer (client) certificate. It rejects nil or
// expired/ not-yet-valid certificates.
//
//	C: tls_verify_peer()
func (m *TLSModule) VerifyPeer(cert *x509.Certificate) error {
	if cert == nil {
		return fmt.Errorf("tls: nil peer certificate")
	}
	now := time.Now()
	if now.Before(cert.NotBefore) {
		return fmt.Errorf("tls: peer certificate not yet valid (NotBefore %s)", cert.NotBefore)
	}
	if now.After(cert.NotAfter) {
		return fmt.Errorf("tls: peer certificate expired (NotAfter %s)", cert.NotAfter)
	}
	return nil
}

// GetTLSConfig returns a *tls.Config suitable for net.Listen / tls.Listen.
// The returned config wires a GetCertificate callback for SNI dispatch and
// honours the configured verify mode.
//
//	C: tls_get_cfg() / tls_domain_t
func (m *TLSModule) GetTLSConfig() *tls.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"sip"},
	}
	switch m.verify {
	case VerifyOptional:
		cfg.ClientAuth = tls.VerifyClientCertIfGiven
	case VerifyRequired:
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	default:
		cfg.ClientAuth = tls.NoClientCert
	}
	// Provide a static certificate list (first cert is the default) plus a
	// SNI callback for multi-domain dispatch.
	for _, c := range m.certs {
		if c.Cert != nil {
			cfg.Certificates = append(cfg.Certificates, *c.Cert)
		}
	}
	// Snapshot the module reference for the callback closure.
	mod := m
	cfg.GetCertificate = func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		c := mod.GetCertificate(hello.ServerName)
		if c == nil {
			// fall back to the first registered certificate
			c = mod.firstCertificate()
		}
		if c == nil {
			return nil, fmt.Errorf("tls: no certificate for %q", hello.ServerName)
		}
		return c.Cert, nil
	}
	return cfg
}

// firstCertificate returns the first registered certificate (for fallback).
func (m *TLSModule) firstCertificate() *Certificate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, c := range m.certs {
		return c
	}
	return nil
}

// SetVerifyMode configures the client-certificate verification mode.
//
//	C: verify_client parameter
func (m *TLSModule) SetVerifyMode(mode VerifyMode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.verify = mode
}

// VerifyMode returns the currently configured verification mode.
func (m *TLSModule) VerifyMode() VerifyMode {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.verify
}

// ListCertificates returns a snapshot of all registered certificates.
func (m *TLSModule) ListCertificates() []Certificate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Certificate, 0, len(m.certs))
	for _, c := range m.certs {
		out = append(out, *c)
	}
	return out
}

// AddDomainMapping maps a domain to a registered certificate name, used by
// SNI dispatch when the certificate's SAN list does not cover the domain.
func (m *TLSModule) AddDomainMapping(domain, certName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.domainMap == nil {
		m.domainMap = make(map[string]string)
	}
	m.domainMap[domain] = certName
}

// RemoveCertificate removes the certificate registered under name. Returns
// true when a certificate was removed.
func (m *TLSModule) RemoveCertificate(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.certs[name]; !ok {
		return false
	}
	delete(m.certs, name)
	return true
}

// -----------------------------------------------------------------------
// process-wide singleton (mirrors the C module's global state)
// -----------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultTM *TLSModule
)

// DefaultTLS returns the process-wide TLSModule, creating it on first use.
func DefaultTLS() *TLSModule {
	defaultMu.RLock()
	m := defaultTM
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultTM == nil {
		defaultTM = New()
	}
	return defaultTM
}

// Init (re)initialises the process-wide TLSModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultTM = New()
}

// LoadCertificate is the package-level wrapper.
func LoadCertificate(name, certFile, keyFile string) error {
	return DefaultTLS().LoadCertificate(name, certFile, keyFile)
}

// GetCertificate is the package-level wrapper.
func GetCertificate(domain string) *Certificate { return DefaultTLS().GetCertificate(domain) }

// ReloadAll is the package-level wrapper.
func ReloadAll() error { return DefaultTLS().ReloadAll() }

// VerifyPeer is the package-level wrapper.
func VerifyPeer(cert *x509.Certificate) error { return DefaultTLS().VerifyPeer(cert) }

// GetTLSConfig is the package-level wrapper.
func GetTLSConfig() *tls.Config { return DefaultTLS().GetTLSConfig() }

// SetVerifyMode is the package-level wrapper.
func SetVerifyMode(mode VerifyMode) { DefaultTLS().SetVerifyMode(mode) }

// ListCertificates is the package-level wrapper.
func ListCertificates() []Certificate { return DefaultTLS().ListCertificates() }

// AddDomainMapping is the package-level wrapper.
func AddDomainMapping(domain, certName string) { DefaultTLS().AddDomainMapping(domain, certName) }
