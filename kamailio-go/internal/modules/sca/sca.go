// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SCA module - Shared Call Appearance.
 * Port of the kamailio sca module (src/modules/sca).
 *
 * Tracks the calls currently active on a shared line appearance so the
 * routing script can implement hold/retrieve semantics and limit the
 * number of appearances in use.
 *
 * It is safe for concurrent use.
 */
package sca

import (
	"sync"
	"sync/atomic"
)

// callState tracks a single appearance on a shared line.
type callState struct {
	line   string
	onHold bool
}

// SCAModule implements the Shared Call Appearance module.
// C: struct module sca
type SCAModule struct {
	mu     sync.RWMutex
	calls  map[string]*callState // call-id -> state
	lines  map[string]map[string]bool
	nextID atomic.Int64
}

// NewSCAModule creates a new SCAModule.
func NewSCAModule() *SCAModule {
	return &SCAModule{
		calls: make(map[string]*callState),
		lines: make(map[string]map[string]bool),
	}
}

// addCall registers callID on line. It is a no-op if the call already
// exists. The line association is what GetActiveCalls inspects.
func (m *SCAModule) addCall(callID, line string) {
	if _, ok := m.calls[callID]; ok {
		return
	}
	m.calls[callID] = &callState{line: line}
	if m.lines[line] == nil {
		m.lines[line] = make(map[string]bool)
	}
	m.lines[line][callID] = true
}

// Hold marks the call identified by callID as on hold. If the call is
// not yet known it is registered on the default line "default".
// C: sca_hold()
func (m *SCAModule) Hold(callID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.calls[callID]
	if !ok {
		m.addCall(callID, "default")
		st = m.calls[callID]
	}
	st.onHold = true
}

// Retrieve clears the hold flag for callID. Unknown calls are ignored.
// C: sca_retrieve()
func (m *SCAModule) Retrieve(callID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.calls[callID]; ok {
		st.onHold = false
	}
}

// IsOnHold reports whether callID is currently on hold.
// C: sca_is_on_hold()
func (m *SCAModule) IsOnHold(callID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if st, ok := m.calls[callID]; ok {
		return st.onHold
	}
	return false
}

// GetActiveCalls returns the call-ids of every appearance registered on
// line, in insertion-stable order. Returns nil for an unknown line.
// C: sca_get_active_calls()
func (m *SCAModule) GetActiveCalls(line string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	set := m.lines[line]
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	return out
}

// Count returns the total number of appearances currently tracked.
func (m *SCAModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.calls)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu  sync.RWMutex
	defaultSCA *SCAModule
)

// DefaultSCA returns the process-wide SCAModule, creating one on first use.
func DefaultSCA() *SCAModule {
	defaultMu.RLock()
	m := defaultSCA
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultSCA == nil {
		defaultSCA = NewSCAModule()
	}
	return defaultSCA
}

// Init (re)initialises the process-wide SCAModule to a fresh state.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultSCA = NewSCAModule()
}
