// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * pdt - prefix-to-domain translation.
 *
 * Maps a numeric prefix to a domain, and translates a dialled number by
 * stripping the longest matching prefix and returning the domain plus the
 * remainder. Mirrors the kamailio pdt module.
 */

package pdt

import "sync"

// PDTModule maps numeric prefixes to domains.
type PDTModule struct {
	mu       sync.RWMutex
	prefixes map[string]string
}

// New returns a new PDTModule.
func New() *PDTModule {
	return &PDTModule{prefixes: make(map[string]string)}
}

// Add registers a prefix -> domain mapping, overwriting any existing value.
func (m *PDTModule) Add(prefix, domain string) {
	if m == nil || prefix == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prefixes[prefix] = domain
}

// Translate resolves number to its domain and the number with the matched
// prefix stripped. Returns (domain, remainder, found).
func (m *PDTModule) Translate(number string) (string, string, bool) {
	if m == nil {
		return "", "", false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var bestPrefix string
	for prefix := range m.prefixes {
		if len(prefix) <= len(number) && number[:len(prefix)] == prefix {
			if len(prefix) > len(bestPrefix) {
				bestPrefix = prefix
			}
		}
	}
	if bestPrefix == "" {
		return "", "", false
	}
	return m.prefixes[bestPrefix], number[len(bestPrefix):], true
}

// Remove deletes the mapping for prefix. Returns true if a mapping existed.
func (m *PDTModule) Remove(prefix string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.prefixes[prefix]; !ok {
		return false
	}
	delete(m.prefixes, prefix)
	return true
}

// List returns a snapshot copy of all prefix -> domain mappings.
func (m *PDTModule) List() map[string]string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.prefixes))
	for k, v := range m.prefixes {
		out[k] = v
	}
	return out
}
