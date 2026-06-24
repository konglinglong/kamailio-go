// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * peering - SIP peer registry and token validation.
 *
 * Maintains a registry of trusted peer hosts with shared-secret tokens and
 * validates incoming requests against them. Mirrors the kamailio peering
 * module.
 */

package peering

import "sync"

// PeeringModule tracks trusted peers and their tokens.
type PeeringModule struct {
	mu    sync.RWMutex
	peers map[string]string // host -> token
}

// New returns a new PeeringModule.
func New() *PeeringModule {
	return &PeeringModule{peers: make(map[string]string)}
}

// CheckPeer reports whether host is a registered peer.
func (m *PeeringModule) CheckPeer(host string) bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.peers[host]
	return ok
}

// AddPeer registers host with the given token, overwriting any previous.
func (m *PeeringModule) AddPeer(host, token string) {
	if m == nil || host == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.peers[host] = token
}

// RemovePeer removes host from the registry. Returns true if it existed.
func (m *PeeringModule) RemovePeer(host string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.peers[host]; !ok {
		return false
	}
	delete(m.peers, host)
	return true
}

// ValidateToken reports whether host is registered with exactly token.
func (m *PeeringModule) ValidateToken(host, token string) bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	got, ok := m.peers[host]
	return ok && got == token
}

// ListPeers returns a snapshot of all registered peer hosts.
func (m *PeeringModule) ListPeers() []string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.peers))
	for host := range m.peers {
		out = append(out, host)
	}
	return out
}
