// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * PVHeaders module - per-message pseudo-variable header store.
 * Port of the kamailio pv_headers module (src/modules/pv_headers).
 *
 * pv_headers keeps a per-message map of header name to value so scripts
 * can stash and retrieve header-like values alongside a parsed SIP
 * message without mutating the on-wire headers. The store is keyed by the
 * message pointer; entries are guarded by a single mutex.
 *
 * The module is safe for concurrent use.
 */

package pv_headers

import (
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// PVHeadersModule is a per-message header store.
type PVHeadersModule struct {
	mu      sync.Mutex
	storage map[*parser.SIPMsg]map[string]string
}

// New creates an empty PVHeadersModule.
func New() *PVHeadersModule {
	return &PVHeadersModule{storage: make(map[*parser.SIPMsg]map[string]string)}
}

// Get returns the value for name on msg, or "" when absent (or msg is nil).
func (m *PVHeadersModule) Get(msg *parser.SIPMsg, name string) string {
	if msg == nil || name == "" {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.storage[msg]; ok {
		return h[name]
	}
	return ""
}

// Set stores value for name on msg. A nil msg or empty name is ignored.
func (m *PVHeadersModule) Set(msg *parser.SIPMsg, name, value string) {
	if msg == nil || name == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.storage[msg]
	if !ok {
		h = make(map[string]string)
		m.storage[msg] = h
	}
	h[name] = value
}

// Remove deletes name from msg's store. Returns true when a value was
// removed.
func (m *PVHeadersModule) Remove(msg *parser.SIPMsg, name string) bool {
	if msg == nil || name == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.storage[msg]
	if !ok {
		return false
	}
	if _, ok := h[name]; !ok {
		return false
	}
	delete(h, name)
	return true
}

// Count returns the number of stored headers for msg.
func (m *PVHeadersModule) Count(msg *parser.SIPMsg) int {
	if msg == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.storage[msg])
}

// Purge removes all stored headers for msg.
func (m *PVHeadersModule) Purge(msg *parser.SIPMsg) {
	if msg == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.storage, msg)
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *PVHeadersModule
)

// DefaultPVHeaders returns the process-wide module, creating it on first use.
func DefaultPVHeaders() *PVHeadersModule {
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

// Init (re)initialises the process-wide module to a fresh state.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// Get is the package-level wrapper.
func Get(msg *parser.SIPMsg, name string) string { return DefaultPVHeaders().Get(msg, name) }

// Set is the package-level wrapper.
func Set(msg *parser.SIPMsg, name, value string) { DefaultPVHeaders().Set(msg, name, value) }

// Remove is the package-level wrapper.
func Remove(msg *parser.SIPMsg, name string) bool { return DefaultPVHeaders().Remove(msg, name) }

// Count is the package-level wrapper.
func Count(msg *parser.SIPMsg) int { return DefaultPVHeaders().Count(msg) }
