// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Domain policy module - per-domain policy lookup.
 * Port of the kamailio domainpolicy module (src/modules/domainpolicy).
 *
 * The module stores domain-to-policy mappings and supports add/check/
 * remove/list operations. Check performs an exact domain match. It is
 * safe for concurrent use.
 */

package domainpolicy

import "sync"

// DomainPolicyModule maintains a domain-to-policy mapping.
type DomainPolicyModule struct {
	mu    sync.RWMutex
	rules map[string]string
}

// New creates a DomainPolicyModule with empty storage.
func New() *DomainPolicyModule {
	return &DomainPolicyModule{rules: make(map[string]string)}
}

// AddRule inserts or updates the policy for the given domain.
//
//	C: dp_add_rule()
func (m *DomainPolicyModule) AddRule(domain, policy string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rules == nil {
		m.rules = make(map[string]string)
	}
	m.rules[domain] = policy
}

// Check returns the policy for the given domain and true when present.
//
//	C: dp_check()
func (m *DomainPolicyModule) Check(domain string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.rules[domain]
	return p, ok
}

// RemoveRule deletes the rule for the given domain. Returns true when a
// rule was removed.
//
//	C: dp_remove_rule()
func (m *DomainPolicyModule) RemoveRule(domain string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rules[domain]; !ok {
		return false
	}
	delete(m.rules, domain)
	return true
}

// List returns a copy of all domain/policy pairs.
func (m *DomainPolicyModule) List() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.rules))
	for k, v := range m.rules {
		out[k] = v
	}
	return out
}

// Count returns the number of stored rules.
func (m *DomainPolicyModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.rules)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *DomainPolicyModule
)

// DefaultDomainPolicy returns the process-wide DomainPolicyModule.
func DefaultDomainPolicy() *DomainPolicyModule {
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

// Init (re)initialises the process-wide DomainPolicyModule.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// AddRule is the package-level wrapper around DefaultDomainPolicy().AddRule.
func AddRule(domain, policy string) { DefaultDomainPolicy().AddRule(domain, policy) }

// Check is the package-level wrapper around DefaultDomainPolicy().Check.
func Check(domain string) (string, bool) { return DefaultDomainPolicy().Check(domain) }

// RemoveRule is the package-level wrapper around DefaultDomainPolicy().RemoveRule.
func RemoveRule(domain string) bool { return DefaultDomainPolicy().RemoveRule(domain) }

// List is the package-level wrapper around DefaultDomainPolicy().List.
func List() map[string]string { return DefaultDomainPolicy().List() }
