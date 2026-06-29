// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * PUsrloc module - persistent user location store.
 * Port of the kamailio p_usrloc module (src/modules/p_usrloc).
 *
 * p_usrloc keeps an in-memory map of Address-of-Record (AOR) to contact
 * string so registrations can be stored and looked up. Store inserts or
 * replaces a binding; Load fetches it; Delete removes it; Count reports
 * the number of bindings.
 *
 * The module is safe for concurrent use.
 */

package p_usrloc

import "sync"

// PUsrlocModule is a persistent user-location store.
type PUsrlocModule struct {
	mu       sync.RWMutex
	bindings map[string]string
}

// New creates an empty PUsrlocModule.
func New() *PUsrlocModule {
	return &PUsrlocModule{bindings: make(map[string]string)}
}

// Store inserts (or replaces) the contact for aor.
func (m *PUsrlocModule) Store(aor, contact string) {
	if aor == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bindings[aor] = contact
}

// Load returns the contact for aor. The boolean is false when aor is
// unknown.
func (m *PUsrlocModule) Load(aor string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.bindings[aor]
	return c, ok
}

// Delete removes the binding for aor. Returns true when a binding was
// removed.
func (m *PUsrlocModule) Delete(aor string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.bindings[aor]; !ok {
		return false
	}
	delete(m.bindings, aor)
	return true
}

// Count returns the number of stored bindings.
func (m *PUsrlocModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.bindings)
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *PUsrlocModule
)

// DefaultPUsrloc returns the process-wide module, creating it on first use.
func DefaultPUsrloc() *PUsrlocModule {
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

// Store is the package-level wrapper.
func Store(aor, contact string) { DefaultPUsrloc().Store(aor, contact) }

// Load is the package-level wrapper.
func Load(aor string) (string, bool) { return DefaultPUsrloc().Load(aor) }

// Delete is the package-level wrapper.
func Delete(aor string) bool { return DefaultPUsrloc().Delete(aor) }

// Count is the package-level wrapper.
func Count() int { return DefaultPUsrloc().Count() }
