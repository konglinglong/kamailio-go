// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * DNSSEC module - DNS Security Extensions verification.
 * Port of the kamailio dnssec module (src/modules/dnssec).
 *
 * The module stores DNSKEY and RRSIG records per domain and verifies
 * that a domain has both records present. It is safe for concurrent use.
 */

package dnssec

import (
	"errors"
	"sync"
)

// DNSSECModule maintains DNSKEY and RRSIG records per domain.
type DNSSECModule struct {
	mu      sync.RWMutex
	dnskeys map[string][]byte
	rrsigs  map[string][]byte
}

// New creates a DNSSECModule with empty storage.
func New() *DNSSECModule {
	return &DNSSECModule{
		dnskeys: make(map[string][]byte),
		rrsigs:  make(map[string][]byte),
	}
}

// SetDNSKEY stores the DNSKEY record for the given domain.
func (m *DNSSECModule) SetDNSKEY(domain string, key []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dnskeys == nil {
		m.dnskeys = make(map[string][]byte)
	}
	cp := make([]byte, len(key))
	copy(cp, key)
	m.dnskeys[domain] = cp
}

// SetRRSIG stores the RRSIG record for the given domain.
func (m *DNSSECModule) SetRRSIG(domain string, sig []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rrsigs == nil {
		m.rrsigs = make(map[string][]byte)
	}
	cp := make([]byte, len(sig))
	copy(cp, sig)
	m.rrsigs[domain] = cp
}

// Verify returns true when the given domain has both a DNSKEY and an
// RRSIG record stored.
//
//	C: dnssec_verify()
func (m *DNSSECModule) Verify(domain string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, hasKey := m.dnskeys[domain]
	_, hasSig := m.rrsigs[domain]
	return hasKey && hasSig
}

// GetDNSKEY returns the stored DNSKEY record for the given domain.
//
//	C: dnssec_get_dnskey()
func (m *DNSSECModule) GetDNSKEY(domain string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key, ok := m.dnskeys[domain]
	if !ok {
		return nil, errors.New("dnssec: no DNSKEY for " + domain)
	}
	out := make([]byte, len(key))
	copy(out, key)
	return out, nil
}

// GetRRSIG returns the stored RRSIG record for the given domain.
//
//	C: dnssec_get_rrsig()
func (m *DNSSECModule) GetRRSIG(domain string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sig, ok := m.rrsigs[domain]
	if !ok {
		return nil, errors.New("dnssec: no RRSIG for " + domain)
	}
	out := make([]byte, len(sig))
	copy(out, sig)
	return out, nil
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *DNSSECModule
)

// DefaultDNSSEC returns the process-wide DNSSECModule.
func DefaultDNSSEC() *DNSSECModule {
	defaultMu.RLock()
	m := defaultM
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM == nil {
		defaultM = New()
	}
	return defaultM
}

// Init (re)initialises the process-wide DNSSECModule.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// Verify is the package-level wrapper around DefaultDNSSEC().Verify.
func Verify(domain string) bool { return DefaultDNSSEC().Verify(domain) }

// GetDNSKEY is the package-level wrapper around DefaultDNSSEC().GetDNSKEY.
func GetDNSKEY(domain string) ([]byte, error) { return DefaultDNSSEC().GetDNSKEY(domain) }

// GetRRSIG is the package-level wrapper around DefaultDNSSEC().GetRRSIG.
func GetRRSIG(domain string) ([]byte, error) { return DefaultDNSSEC().GetRRSIG(domain) }
