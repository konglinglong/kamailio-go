// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SpeedDial module - short-code to target URI mapping.
 * Port of the kamailio speeddial module (src/modules/speeddial).
 *
 * Maintains a per-instance table of short codes (e.g. "**1") that expand to
 * a destination URI, letting users dial a memorable short code instead of
 * a full address. The mapping is held in memory.
 *
 * It is safe for concurrent use.
 */
package speeddial

import (
	"sync"
)

// SpeedDialModule implements the speeddial module functionality.
// C: struct module speeddial
type SpeedDialModule struct {
	mu     sync.RWMutex
	entries map[string]string
}

// NewSpeedDialModule creates a SpeedDialModule.
func NewSpeedDialModule() *SpeedDialModule {
	return &SpeedDialModule{entries: make(map[string]string)}
}

// Add registers (or overwrites) the mapping shortcode -> target.
// C: speeddial_add()
func (m *SpeedDialModule) Add(shortcode, target string) {
	if shortcode == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[shortcode] = target
}

// Lookup resolves shortcode to its target. The bool result is false when the
// shortcode is not registered.
// C: speeddial_lookup()
func (m *SpeedDialModule) Lookup(shortcode string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	target, ok := m.entries[shortcode]
	return target, ok
}

// Remove deletes the mapping for shortcode. Returns true when something was
// removed.
// C: speeddial_remove()
func (m *SpeedDialModule) Remove(shortcode string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entries[shortcode]; !ok {
		return false
	}
	delete(m.entries, shortcode)
	return true
}

// List returns a copy of every shortcode -> target mapping.
func (m *SpeedDialModule) List() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.entries))
	for k, v := range m.entries {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu         sync.RWMutex
	defaultSpeedDial *SpeedDialModule
)

// DefaultSpeedDial returns the process-wide SpeedDialModule, creating one on
// first use.
func DefaultSpeedDial() *SpeedDialModule {
	defaultMu.RLock()
	m := defaultSpeedDial
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultSpeedDial == nil {
		defaultSpeedDial = NewSpeedDialModule()
	}
	return defaultSpeedDial
}

// Init (re)initialises the process-wide SpeedDialModule to a fresh state.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultSpeedDial = NewSpeedDialModule()
}
