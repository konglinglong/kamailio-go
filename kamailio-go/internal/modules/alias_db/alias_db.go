// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Alias DB module - alias-to-contact lookup store.
 * Port of the kamailio alias_db module (src/modules/alias_db).
 *
 * The alias_db module maintains an in-memory map of aliases to contact
 * strings, providing add/lookup/remove operations. It is safe for
 * concurrent use.
 */

package alias_db

import "sync"

// AliasDBModule maintains an alias-to-contact mapping.
type AliasDBModule struct {
	mu      sync.RWMutex
	aliases map[string]string
}

// New creates an AliasDBModule with empty storage.
func New() *AliasDBModule {
	return &AliasDBModule{aliases: make(map[string]string)}
}

// Add inserts or updates the contact for the given alias.
//
//	C: alias_db_add()
func (m *AliasDBModule) Add(alias, contact string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.aliases == nil {
		m.aliases = make(map[string]string)
	}
	m.aliases[alias] = contact
}

// Lookup returns the contact for the given alias and true when present.
//
//	C: alias_db_lookup()
func (m *AliasDBModule) Lookup(alias string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.aliases[alias]
	return c, ok
}

// Remove deletes the given alias. Returns true when an entry was removed.
//
//	C: alias_db_remove()
func (m *AliasDBModule) Remove(alias string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.aliases[alias]; !ok {
		return false
	}
	delete(m.aliases, alias)
	return true
}

// Count returns the number of stored aliases.
func (m *AliasDBModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.aliases)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *AliasDBModule
)

// DefaultAliasDB returns the process-wide AliasDBModule, creating it on
// first use.
func DefaultAliasDB() *AliasDBModule {
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

// Init (re)initialises the process-wide AliasDBModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// Add is the package-level wrapper around DefaultAliasDB().Add.
func Add(alias, contact string) { DefaultAliasDB().Add(alias, contact) }

// Lookup is the package-level wrapper around DefaultAliasDB().Lookup.
func Lookup(alias string) (string, bool) { return DefaultAliasDB().Lookup(alias) }

// Remove is the package-level wrapper around DefaultAliasDB().Remove.
func Remove(alias string) bool { return DefaultAliasDB().Remove(alias) }

// Count is the package-level wrapper around DefaultAliasDB().Count.
func Count() int { return DefaultAliasDB().Count() }
