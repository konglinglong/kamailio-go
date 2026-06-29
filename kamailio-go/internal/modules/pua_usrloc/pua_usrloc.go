// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * pua_usrloc module - Presence User Agent hook for usrloc events.
 *
 * Receives contact-added/removed notifications from the usrloc module
 * and exposes the list of currently registered Address-of-Record
 * (AoR) subscribers. The module is safe for concurrent use.
 */

package pua_usrloc

import (
	"sync"
)

// PUAUsrlocModule tracks usrloc contact events.
type PUAUsrlocModule struct {
	mu       sync.RWMutex
	contacts map[string]string // aor -> contact
}

// New creates a PUAUsrlocModule with no contacts.
func New() *PUAUsrlocModule {
	return &PUAUsrlocModule{contacts: make(map[string]string)}
}

// OnContactAdded records that contact is now registered for aor.
//
//	C: pua_usrloc_contact_added()
func (m *PUAUsrlocModule) OnContactAdded(aor, contact string) {
	if aor == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.contacts[aor] = contact
}

// OnContactRemoved removes the contact entry for aor.
//
//	C: pua_usrloc_contact_removed()
func (m *PUAUsrlocModule) OnContactRemoved(aor string) {
	if aor == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.contacts, aor)
}

// GetSubscribers returns a snapshot of the AoRs with active contacts.
//
//	C: pua_usrloc_get_subscribers()
func (m *PUAUsrlocModule) GetSubscribers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.contacts))
	for aor := range m.contacts {
		out = append(out, aor)
	}
	return out
}

// GetContact returns the contact for aor, or an empty string when none.
func (m *PUAUsrlocModule) GetContact(aor string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.contacts[aor]
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu        sync.RWMutex
	defaultPUAUsrloc *PUAUsrlocModule
)

// DefaultPUAUsrloc returns the process-wide PUAUsrlocModule, creating it
// on first use.
func DefaultPUAUsrloc() *PUAUsrlocModule {
	defaultMu.RLock()
	m := defaultPUAUsrloc
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultPUAUsrloc == nil {
		defaultPUAUsrloc = New()
	}
	return defaultPUAUsrloc
}

// Init (re)initialises the process-wide PUAUsrlocModule to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultPUAUsrloc = New()
}
