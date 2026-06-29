// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * URIDB module - SIP URI lookup database.
 * Port of the kamailio uri_db module (src/modules/uri_db).
 *
 * The uri_db module checks whether a SIP URI is handled by the proxy
 * and resolves the domain it belongs to. This Go counterpart keeps an
 * in-memory registry of uri -> domain mappings.
 *
 * It is safe for concurrent use.
 */

package uri_db

import (
	"sync"
)

// URIDBModule maintains the registry of served URIs and their domains.
// C: struct module uri_db
type URIDBModule struct {
	mu   sync.RWMutex
	uris map[string]string
}

// New creates an empty URIDBModule.
func New() *URIDBModule {
	return &URIDBModule{uris: make(map[string]string)}
}

// AddURI registers uri as served, belonging to domain. Adding an existing
// uri updates its domain. It is the in-memory equivalent of populating the
// uri table.
func (m *URIDBModule) AddURI(uri, domain string) {
	if m == nil || uri == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.uris == nil {
		m.uris = make(map[string]string)
	}
	m.uris[uri] = domain
}

// CheckURI reports whether uri is registered and non-empty.
//
//	C: check_uri()
func (m *URIDBModule) CheckURI(uri string) bool {
	if m == nil || uri == "" {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.uris[uri]
	return ok
}

// DoesURIExist reports whether uri is registered.
//
//	C: does_uri_exist()
func (m *URIDBModule) DoesURIExist(uri string) bool {
	if m == nil || uri == "" {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.uris[uri]
	return ok
}

// GetDomain returns the domain registered for uri and whether it existed.
//
//	C: get_domain() analogue
func (m *URIDBModule) GetDomain(uri string) (string, bool) {
	if m == nil || uri == "" {
		return "", false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	domain, ok := m.uris[uri]
	return domain, ok
}

// RemoveURI removes uri from the registry. Returns true when a uri was
// removed.
func (m *URIDBModule) RemoveURI(uri string) bool {
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
	defaultMu   sync.RWMutex
	defaultURIDB *URIDBModule
)

// DefaultURIDB returns the process-wide URIDBModule, creating it on first use.
func DefaultURIDB() *URIDBModule {
	defaultMu.RLock()
	m := defaultURIDB
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultURIDB == nil {
		defaultURIDB = New()
	}
	return defaultURIDB
}

// Init (re)initialises the process-wide URIDBModule to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultURIDB = New()
}
