// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * UIDURIDB module - registry of SIP URIs.
 * Port of the kamailio uid_uri_db module (src/modules/uid_uri_db).
 *
 * The uid_uri_db module checks whether a SIP URI is served by the
 * proxy, optionally scoped to a domain id (did). This Go counterpart
 * keeps an in-memory registry of uri -> did mappings.
 *
 * It is safe for concurrent use.
 */

package uid_uri_db

import (
	"sync"
)

// UIDURIDBModule maintains the registry of served URIs.
// C: struct module uid_uri_db
type UIDURIDBModule struct {
	mu   sync.RWMutex
	uris map[string]string
}

// New creates an empty UIDURIDBModule.
func New() *UIDURIDBModule {
	return &UIDURIDBModule{uris: make(map[string]string)}
}

// CheckURI reports whether uri is registered (in any did).
//
//	C: check_uri() / check_uri_realm()
func (m *UIDURIDBModule) CheckURI(uri string) bool {
	if m == nil || uri == "" {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.uris[uri]
	return ok
}

// AddURI registers uri under the given did. Adding an existing uri updates
// its did.
//
//	C: uri_db_add() analogue
func (m *UIDURIDBModule) AddURI(uri, did string) {
	if m == nil || uri == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.uris == nil {
		m.uris = make(map[string]string)
	}
	m.uris[uri] = did
}

// RemoveURI removes uri from the registry. Returns true when a uri was
// removed.
//
//	C: uri_db_remove() analogue
func (m *UIDURIDBModule) RemoveURI(uri string) bool {
	if m == nil || uri == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.uris[uri]; !ok {
		return false
	}
	delete(m.uris, uri)
	return true
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu       sync.RWMutex
	defaultUIDURIDB *UIDURIDBModule
)

// DefaultUIDURIDB returns the process-wide UIDURIDBModule, creating it on
// first use.
func DefaultUIDURIDB() *UIDURIDBModule {
	defaultMu.RLock()
	m := defaultUIDURIDB
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultUIDURIDB == nil {
		defaultUIDURIDB = New()
	}
	return defaultUIDURIDB
}

// Init (re)initialises the process-wide UIDURIDBModule to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultUIDURIDB = New()
}
