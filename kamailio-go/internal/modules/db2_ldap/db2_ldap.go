// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * DB2 LDAP module - in-memory LDAP directory mock.
 * Port of the kamailio db2_ldap module (src/modules/db2_ldap).
 *
 * The module stores LDAP entries keyed by distinguished name (DN) and
 * supports search/add/delete operations. Search matches entries whose DN
 * starts with the given base and whose attributes satisfy a simple
 * "attr=value" filter. It is safe for concurrent use.
 */

package db2_ldap

import (
	"errors"
	"strings"
	"sync"
)

// DB2LDAPModule maintains an in-memory LDAP directory.
type DB2LDAPModule struct {
	mu       sync.RWMutex
	server   string
	entries  map[string]map[string]string
}

// New creates a DB2LDAPModule with empty storage.
func New() *DB2LDAPModule {
	return &DB2LDAPModule{entries: make(map[string]map[string]string)}
}

// Init configures the LDAP server address and marks the module connected.
//
//	C: db2_ldap_init()
func (m *DB2LDAPModule) Init(server string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.server = server
}

// IsConnected returns true when a server address has been configured.
func (m *DB2LDAPModule) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.server != ""
}

// Add stores a new entry under the given DN. It returns an error when
// the DN already exists.
//
//	C: db2_ldap_add()
func (m *DB2LDAPModule) Add(dn string, attrs map[string]string) error {
	if dn == "" {
		return errors.New("db2_ldap: empty DN")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entries == nil {
		m.entries = make(map[string]map[string]string)
	}
	if _, ok := m.entries[dn]; ok {
		return errors.New("db2_ldap: entry already exists: " + dn)
	}
	cp := make(map[string]string, len(attrs))
	for k, v := range attrs {
		cp[k] = v
	}
	m.entries[dn] = cp
	return nil
}

// Delete removes the entry identified by DN. It returns an error when
// the DN is not found.
//
//	C: db2_ldap_delete()
func (m *DB2LDAPModule) Delete(dn string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entries[dn]; !ok {
		return errors.New("db2_ldap: entry not found: " + dn)
	}
	delete(m.entries, dn)
	return nil
}

// Search returns the entries whose DN starts with base and whose
// attributes satisfy the given filter. The filter is a simple
// "attr=value" string; an empty filter matches all entries under base.
//
//	C: db2_ldap_search()
func (m *DB2LDAPModule) Search(base, filter string) ([]map[string]string, error) {
	attr, val := parseFilter(filter)
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []map[string]string
	for dn, attrs := range m.entries {
		if base != "" && !strings.HasPrefix(dn, base) {
			continue
		}
		if attr != "" {
			if attrs[attr] != val {
				continue
			}
		}
		cp := make(map[string]string, len(attrs)+1)
		for k, v := range attrs {
			cp[k] = v
		}
		cp["dn"] = dn
		out = append(out, cp)
	}
	return out, nil
}

// Count returns the number of stored entries.
func (m *DB2LDAPModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}

// parseFilter splits a "attr=value" filter into its components.
func parseFilter(filter string) (attr, val string) {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return "", ""
	}
	if i := strings.Index(filter, "="); i >= 0 {
		return strings.TrimSpace(filter[:i]), strings.TrimSpace(filter[i+1:])
	}
	return "", ""
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *DB2LDAPModule
)

// DefaultDB2LDAP returns the process-wide DB2LDAPModule.
func DefaultDB2LDAP() *DB2LDAPModule {
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

// Init is the package-level wrapper that (re)initialises the process-wide
// DB2LDAPModule to a fresh state and configures the server address.
func Init(server string) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
	defaultM.server = server
}

// Search is the package-level wrapper around DefaultDB2LDAP().Search.
func Search(base, filter string) ([]map[string]string, error) {
	return DefaultDB2LDAP().Search(base, filter)
}

// Add is the package-level wrapper around DefaultDB2LDAP().Add.
func Add(dn string, attrs map[string]string) error { return DefaultDB2LDAP().Add(dn, attrs) }

// Delete is the package-level wrapper around DefaultDB2LDAP().Delete.
func Delete(dn string) error { return DefaultDB2LDAP().Delete(dn) }

// IsConnected is the package-level wrapper around DefaultDB2LDAP().IsConnected.
func IsConnected() bool { return DefaultDB2LDAP().IsConnected() }
