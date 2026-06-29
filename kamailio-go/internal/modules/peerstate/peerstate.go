// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * peerstate module - peer liveness state tracking.
 *
 * Tracks the state of peer nodes (e.g. "up", "down", "probe"). IsAlive
 * reports true when a peer's state is "up". The module is safe for
 * concurrent use.
 */

package peerstate

import (
	"sync"
)

// StateUp is the state value indicating a live peer.
const StateUp = "up"

// PeerStateModule tracks per-peer state strings.
type PeerStateModule struct {
	mu     sync.RWMutex
	states map[string]string
}

// New creates a PeerStateModule with no peers.
func New() *PeerStateModule {
	return &PeerStateModule{states: make(map[string]string)}
}

// SetState records state for peer.
//
//	C: peerstate_set_state()
func (m *PeerStateModule) SetState(peer string, state string) {
	if peer == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[peer] = state
}

// GetState returns the state for peer, or an empty string when unknown.
//
//	C: peerstate_get_state()
func (m *PeerStateModule) GetState(peer string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.states[peer]
}

// IsAlive reports whether peer's state is "up".
//
//	C: peerstate_is_alive()
func (m *PeerStateModule) IsAlive(peer string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.states[peer] == StateUp
}

// List returns a snapshot of all peer states.
//
//	C: peerstate_list()
func (m *PeerStateModule) List() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.states))
	for k, v := range m.states {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu        sync.RWMutex
	defaultPeerState *PeerStateModule
)

// DefaultPeerState returns the process-wide PeerStateModule, creating it
// on first use.
func DefaultPeerState() *PeerStateModule {
	defaultMu.RLock()
	m := defaultPeerState
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultPeerState == nil {
		defaultPeerState = New()
	}
	return defaultPeerState
}

// Init (re)initialises the process-wide PeerStateModule to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultPeerState = New()
}
