// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * UIDDomain module - registry of known SIP domains.
 * Port of the kamailio uid_domain module (src/modules/uid_domain).
 *
 * The uid_domain module maintains the list of domains the proxy is
 * responsible for. Requests whose Request-URI host part is not a known
 * domain are rejected. This Go counterpart keeps an in-memory registry
 * of domain names.
 *
 * It is safe for concurrent use.
 */

package uid_domain

import (
	"sort"
	"sync"
)

// UIDDomainModule maintains the registry of known domains.
// C: struct module uid_domain
type UIDDomainModule struct {
	mu      sync.RWMutex
	domains map[string]bool
}

// New creates an empty UIDDomainModule.
func New() *UIDDomainModule {
	return &UIDDomainModule{domains: make(map[string]bool)}
}

// AddDomain registers name as a known domain. Adding an existing domain is
// a no-op.
//
//	C: domain_add()
func (m *UIDDomainModule) AddDomain(name string) {
	if m == nil || name == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.domains == nil {
		m.domains = make(map[string]bool)
	}
	m.domains[name] = true
}

// RemoveDomain removes name from the registry. Returns true when a domain
// was removed.
//
//	C: domain_remove()
func (m *UIDDomainModule) RemoveDomain(name string) bool {
	if m == nil || name == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.domains[name]; !ok {
		return false
	}
	delete(m.domains, name)
	return true
}

// IsKnown reports whether name is a registered domain.
//
//	C: is_domain_local() / is_from_local()
func (m *UIDDomainModule) IsKnown(name string) bool {
	if m == nil || name == "" {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.domains[name]
}

// List returns the known domain names in sorted order.
func (m *UIDDomainModule) List() []string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.domains))
	for name := range m.domains {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu        sync.RWMutex
	defaultUIDDomain *UIDDomainModule
)

// DefaultUIDDomain returns the process-wide UIDDomainModule, creating it on
// first use.
func DefaultUIDDomain() *UIDDomainModule {
	defaultMu.RLock()
	m := defaultUIDDomain
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultUIDDomain == nil {
		defaultUIDDomain = New()
	}
	return defaultUIDDomain
}

// Init (re)initialises the process-wide UIDDomainModule to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultUIDDomain = New()
}
