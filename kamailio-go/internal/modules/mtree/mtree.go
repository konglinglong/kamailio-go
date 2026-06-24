// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * mtree - in-memory prefix tree (longest-prefix match).
 *
 * Stores prefix -> value mappings and resolves a key to the value of its
 * longest matching prefix. Mirrors the kamailio mtree module.
 */

package mtree

import "sync"

// MTreeModule is a longest-prefix-match string map.
type MTreeModule struct {
	mu   sync.RWMutex
	data map[string]string
}

// New returns a new MTreeModule.
func New() *MTreeModule {
	return &MTreeModule{data: make(map[string]string)}
}

// Insert stores value for the given prefix, overwriting any existing value.
func (m *MTreeModule) Insert(prefix, value string) {
	if m == nil || prefix == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[prefix] = value
}

// Lookup returns the value of the longest stored prefix that is a prefix of
// key, together with a found flag.
func (m *MTreeModule) Lookup(key string) (string, bool) {
	if m == nil {
		return "", false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var bestPrefix string
	for prefix := range m.data {
		if len(prefix) <= len(key) && key[:len(prefix)] == prefix {
			if len(prefix) > len(bestPrefix) {
				bestPrefix = prefix
			}
		}
	}
	if bestPrefix == "" {
		return "", false
	}
	return m.data[bestPrefix], true
}

// Delete removes the entry for the given prefix. Returns true if an entry
// was removed.
func (m *MTreeModule) Delete(prefix string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[prefix]; !ok {
		return false
	}
	delete(m.data, prefix)
	return true
}

// Size returns the number of stored prefixes.
func (m *MTreeModule) Size() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.data)
}

// List returns a snapshot copy of all prefix -> value mappings.
func (m *MTreeModule) List() map[string]string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.data))
	for k, v := range m.data {
		out[k] = v
	}
	return out
}
