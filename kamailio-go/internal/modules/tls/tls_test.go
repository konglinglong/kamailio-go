// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the tls module.
 */

package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// genCert generates a self-signed certificate and key, writes them to dir
// with the given name prefix and returns the file paths. domains become the
// certificate's SAN DNS names. notAfter controls the expiry.
func genCert(t *testing.T, dir, name, commonName string, domains []string, notAfter time.Time) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:              domains,
		BasicConstraintsValid: true,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	certFile = filepath.Join(dir, name+".crt")
	keyFile = filepath.Join(dir, name+".key")
	if err := os.WriteFile(certFile, certPEM, 0600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certFile, keyFile
}

// certPEM returns the parsed leaf x509 certificate of c.
func certPEM(c *Certificate) *x509.Certificate {
	if c == nil || c.Cert == nil || len(c.Cert.Certificate) == 0 {
		return nil
	}
	parsed, err := x509.ParseCertificate(c.Cert.Certificate[0])
	if err != nil {
		return nil
	}
	return parsed
}

func TestLoadCertificate(t *testing.T) {
	m := New()
	dir := t.TempDir()
	certFile, keyFile := genCert(t, dir, "srv", "srv.example.com", []string{"srv.example.com", "alt.example.com"}, time.Now().Add(24*time.Hour))

	if err := m.LoadCertificate("server", certFile, keyFile); err != nil {
		t.Fatalf("LoadCertificate() error = %v", err)
	}
	c := m.GetCertificateByName("server")
	if c == nil {
		t.Fatalf("GetCertificateByName() returned nil")
	}
	if c.Name != "server" {
		t.Errorf("Name = %q, want server", c.Name)
	}
	if c.CertFile != certFile {
		t.Errorf("CertFile = %q, want %q", c.CertFile, certFile)
	}
	if c.Cert == nil {
		t.Errorf("Cert is nil")
	}
	if c.Expires.IsZero() {
		t.Errorf("Expires is zero")
	}
	if len(c.Domains) < 2 {
		t.Errorf("Domains len = %d, want >= 2", len(c.Domains))
	}

	// Missing files -> error.
	if err := m.LoadCertificate("bad", filepath.Join(dir, "nope.crt"), filepath.Join(dir, "nope.key")); err == nil {
		t.Errorf("LoadCertificate(missing) should error")
	}
}

func TestGetCertificateBySNI(t *testing.T) {
	m := New()
	dir := t.TempDir()
	certFile, keyFile := genCert(t, dir, "sn", "sn.example.com", []string{"sn.example.com"}, time.Now().Add(24*time.Hour))
	if err := m.LoadCertificate("sn", certFile, keyFile); err != nil {
		t.Fatalf("LoadCertificate() error = %v", err)
	}
	// SNI lookup by SAN domain.
	c := m.GetCertificate("sn.example.com")
	if c == nil {
		t.Fatalf("GetCertificate(sn.example.com) returned nil")
	}
	if c.Name != "sn" {
		t.Errorf("Name = %q, want sn", c.Name)
	}
	// Unknown domain -> nil.
	if m.GetCertificate("nope.example.com") != nil {
		t.Errorf("GetCertificate(unknown) should return nil")
	}
}

func TestGetCertificateByMapping(t *testing.T) {
	m := New()
	dir := t.TempDir()
	certFile, keyFile := genCert(t, dir, "map", "map.example.com", []string{"map.example.com"}, time.Now().Add(24*time.Hour))
	if err := m.LoadCertificate("map", certFile, keyFile); err != nil {
		t.Fatalf("LoadCertificate() error = %v", err)
	}
	m.AddDomainMapping("alias.example.com", "map")
	c := m.GetCertificate("alias.example.com")
	if c == nil {
		t.Fatalf("GetCertificate(alias.example.com) returned nil after mapping")
	}
	if c.Name != "map" {
		t.Errorf("Name = %q, want map", c.Name)
	}
}

func TestListCertificates(t *testing.T) {
	m := New()
	dir := t.TempDir()
	c1, k1 := genCert(t, dir, "a", "a.example.com", []string{"a.example.com"}, time.Now().Add(24*time.Hour))
	c2, k2 := genCert(t, dir, "b", "b.example.com", []string{"b.example.com"}, time.Now().Add(24*time.Hour))
	if err := m.LoadCertificate("a", c1, k1); err != nil {
		t.Fatalf("LoadCertificate(a) error = %v", err)
	}
	if err := m.LoadCertificate("b", c2, k2); err != nil {
		t.Fatalf("LoadCertificate(b) error = %v", err)
	}
	list := m.ListCertificates()
	if len(list) != 2 {
		t.Errorf("ListCertificates() len = %d, want 2", len(list))
	}
}

func TestReloadAll(t *testing.T) {
	m := New()
	dir := t.TempDir()
	certFile, keyFile := genCert(t, dir, "rl", "rl.example.com", []string{"rl.example.com"}, time.Now().Add(24*time.Hour))
	if err := m.LoadCertificate("rl", certFile, keyFile); err != nil {
		t.Fatalf("LoadCertificate() error = %v", err)
	}
	before := m.GetCertificateByName("rl")
	if before == nil {
		t.Fatalf("GetCertificateByName() returned nil")
	}

	// Regenerate the cert files with a new SAN domain and reload.
	certFile2, keyFile2 := genCert(t, dir, "rl2", "rl.example.com", []string{"rl.example.com", "new.example.com"}, time.Now().Add(24*time.Hour))
	// Overwrite the original files so ReloadAll picks up the new content.
	if err := copyFile(certFile2, certFile); err != nil {
		t.Fatalf("copy cert: %v", err)
	}
	if err := copyFile(keyFile2, keyFile); err != nil {
		t.Fatalf("copy key: %v", err)
	}

	if err := m.ReloadAll(); err != nil {
		t.Fatalf("ReloadAll() error = %v", err)
	}
	after := m.GetCertificateByName("rl")
	if after == nil {
		t.Fatalf("GetCertificateByName() returned nil after reload")
	}
	if len(after.Domains) != 2 {
		t.Errorf("after reload Domains len = %d, want 2", len(after.Domains))
	}
}

func TestVerifyPeerValid(t *testing.T) {
	m := New()
	dir := t.TempDir()
	certFile, _ := genCert(t, dir, "vp", "vp.example.com", []string{"vp.example.com"}, time.Now().Add(24*time.Hour))
	// Parse the leaf cert.
	pemBytes, err := os.ReadFile(certFile)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		t.Fatalf("pem decode failed")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	if err := m.VerifyPeer(cert); err != nil {
		t.Errorf("VerifyPeer(valid) error = %v, want nil", err)
	}
}

func TestVerifyPeerExpired(t *testing.T) {
	m := New()
	dir := t.TempDir()
	certFile, _ := genCert(t, dir, "exp", "exp.example.com", []string{"exp.example.com"}, time.Now().Add(-time.Hour))
	pemBytes, _ := os.ReadFile(certFile)
	block, _ := pem.Decode(pemBytes)
	cert, _ := x509.ParseCertificate(block.Bytes)
	if err := m.VerifyPeer(cert); err == nil {
		t.Errorf("VerifyPeer(expired) = nil, want error")
	}
}

func TestVerifyPeerNil(t *testing.T) {
	m := New()
	if err := m.VerifyPeer(nil); err == nil {
		t.Errorf("VerifyPeer(nil) = nil, want error")
	}
}

func TestGetTLSConfig(t *testing.T) {
	m := New()
	dir := t.TempDir()
	certFile, keyFile := genCert(t, dir, "cfg", "cfg.example.com", []string{"cfg.example.com"}, time.Now().Add(24*time.Hour))
	if err := m.LoadCertificate("cfg", certFile, keyFile); err != nil {
		t.Fatalf("LoadCertificate() error = %v", err)
	}
	cfg := m.GetTLSConfig()
	if cfg == nil {
		t.Fatalf("GetTLSConfig() returned nil")
	}
	if cfg.MinVersion == 0 {
		t.Errorf("MinVersion not set")
	}
	// GetCertificate callback should be wired for SNI.
	if cfg.GetCertificate == nil {
		t.Errorf("GetCertificate callback not set")
	}
}

func TestSetVerifyMode(t *testing.T) {
	m := New()
	m.SetVerifyMode(VerifyNone)
	if m.VerifyMode() != VerifyNone {
		t.Errorf("VerifyMode() = %v, want VerifyNone", m.VerifyMode())
	}
	m.SetVerifyMode(VerifyRequired)
	if m.VerifyMode() != VerifyRequired {
		t.Errorf("VerifyMode() = %v, want VerifyRequired", m.VerifyMode())
	}
	// GetTLSConfig should reflect the verify mode.
	cfg := m.GetTLSConfig()
	if cfg.ClientAuth == 0 {
		t.Errorf("ClientAuth not set after VerifyRequired")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultTLS()
	if d == nil {
		t.Fatalf("DefaultTLS() returned nil")
	}
	if DefaultTLS() != d {
		t.Errorf("DefaultTLS() returned different instance")
	}
	dir := t.TempDir()
	certFile, keyFile := genCert(t, dir, "def", "def.example.com", []string{"def.example.com"}, time.Now().Add(24*time.Hour))
	if err := d.LoadCertificate("def", certFile, keyFile); err != nil {
		t.Fatalf("LoadCertificate() error = %v", err)
	}
	Init()
	if len(DefaultTLS().ListCertificates()) != 0 {
		t.Errorf("after Init, ListCertificates() len != 0")
	}
}

func TestConcurrentSafety(t *testing.T) {
	m := New()
	dir := t.TempDir()
	certFile, keyFile := genCert(t, dir, "cc", "cc.example.com", []string{"cc.example.com"}, time.Now().Add(24*time.Hour))
	if err := m.LoadCertificate("cc", certFile, keyFile); err != nil {
		t.Fatalf("LoadCertificate() error = %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.GetCertificate("cc.example.com")
			_ = m.ListCertificates()
			_ = m.GetTLSConfig()
			_ = m.VerifyMode()
		}()
	}
	wg.Wait()
}

// copyFile copies src to dst.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0600)
}
