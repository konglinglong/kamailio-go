// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * UIDGFlags module - named global integer flags.
 * Port of the kamailio uid_gflags module (src/modules/uid_gflags).
 *
 * The uid_gflags module exposes a set of named global integer flags
 * that can be set, queried and tested from the config script. They
 * persist for the lifetime of the process and are shared across all
 * requests.
 *
 * It is safe for concurrent use.
 */

package uid_gflags

import (
	"sync"
)

// UIDGFlagsModule maintains named global integer flags.
// C: struct module uid_gflags
type UIDGFlagsModule struct {
	mu    sync.RWMutex
	flags map[string]int
}

// New creates an empty UIDGFlagsModule.
func New() *UIDGFlagsModule {
	return &UIDGFlagsModule{flags: make(map[string]int)}
}

// Set assigns value to the named flag, creating it when necessary.
//
//	C: set_gflag()
func (m *UIDGFlagsModule) Set(name string, value int) {
	if m == nil || name == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.flags == nil {
		m.flags = make(map[string]int)
	}
	m.flags[name] = value
}

// Get returns the value of the named flag, or 0 when the flag has not been
// set.
//
//	C: get_gflag()
func (m *UIDGFlagsModule) Get(name string) int {
	if m == nil || name == "" {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.flags[name]
}

// IsSet reports whether the named flag has been set.
//
//	C: is_gflag_set()
func (m *UIDGFlagsModule) IsSet(name string) bool {
	if m == nil || name == "" {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.flags[name]
	return ok
}

// List returns a copy of every flag and its value.
func (m *UIDGFlagsModule) List() map[string]int {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]int, len(m.flags))
	for k, v := range m.flags {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu        sync.RWMutex
	defaultUIDGFlags *UIDGFlagsModule
)

// DefaultUIDGFlags returns the process-wide UIDGFlagsModule, creating it on
// first use.
func DefaultUIDGFlags() *UIDGFlagsModule {
	defaultMu.RLock()
	m := defaultUIDGFlags
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultUIDGFlags == nil {
		defaultUIDGFlags = New()
	}
	return defaultUIDGFlags
}

// Init (re)initialises the process-wide UIDGFlagsModule to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultUIDGFlags = New()
}
