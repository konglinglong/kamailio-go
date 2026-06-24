// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * tlsa module - TLSA (RFC 6698) record generation and verification.
 *
 * Generates DANE TLSA records from a certificate's DER bytes and
 * verifies a certificate against a stored record set. The matching
 * type 1 (SHA-256) is implemented. The module is safe for concurrent
 * use.
 */

package tlsa

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
)

// TLSARecord is a DANE TLSA record (RFC 6698).
type TLSARecord struct {
	Usage        uint8
	Selector     uint8
	MatchingType uint8
	Certificate  string
}

// Default usage/selector/matching type values.
const (
	DefaultUsage        uint8 = 3 // Domain-issued certificate
	DefaultSelector     uint8 = 0 // Full certificate
	DefaultMatchingType uint8 = 1 // SHA-256
)

// TLSAModule generates and verifies TLSA records.
type TLSAModule struct {
	mu      sync.RWMutex
	records map[string][]TLSARecord // domain -> records
}

// New creates a TLSAModule with empty record storage.
func New() *TLSAModule {
	return &TLSAModule{records: make(map[string][]TLSARecord)}
}

// matchingValue computes the matching value for cert bytes given a
// matching type. Only SHA-256 (1) is supported.
func matchingValue(cert []byte, matchingType uint8) (string, error) {
	switch matchingType {
	case 1:
		sum := sha256.Sum256(cert)
		return hex.EncodeToString(sum[:]), nil
	case 0:
		return hex.EncodeToString(cert), nil
	default:
		return "", errors.New("tlsa: unsupported matching type")
	}
}

// Generate builds a TLSARecord from cert bytes using the default
// usage/selector/matching type and stores it under domain. When domain
// is empty the record is returned but not stored.
//
//	C: tlsa_generate()
func (m *TLSAModule) Generate(cert []byte) (*TLSARecord, error) {
	if len(cert) == 0 {
		return nil, errors.New("tlsa: empty certificate")
	}
	val, err := matchingValue(cert, DefaultMatchingType)
	if err != nil {
		return nil, err
	}
	rec := &TLSARecord{
		Usage:        DefaultUsage,
		Selector:     DefaultSelector,
		MatchingType: DefaultMatchingType,
		Certificate:  val,
	}
	return rec, nil
}

// Store associates rec with domain.
func (m *TLSAModule) Store(domain string, rec TLSARecord) {
	if domain == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[domain] = append(m.records[domain], rec)
}

// Verify reports whether cert matches any stored TLSA record for domain.
//
//	C: tlsa_verify()
func (m *TLSAModule) Verify(domain string, cert []byte) bool {
	if domain == "" || len(cert) == 0 {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	recs := m.records[domain]
	for _, rec := range recs {
		val, err := matchingValue(cert, rec.MatchingType)
		if err != nil {
			continue
		}
		if val == rec.Certificate {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu   sync.RWMutex
	defaultTLSA *TLSAModule
)

// DefaultTLSA returns the process-wide TLSAModule, creating it on first use.
func DefaultTLSA() *TLSAModule {
	defaultMu.RLock()
	m := defaultTLSA
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultTLSA == nil {
		defaultTLSA = New()
	}
	return defaultTLSA
}

// Init (re)initialises the process-wide TLSAModule to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultTLSA = New()
}
