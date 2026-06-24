// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * pua_xmpp module - Presence User Agent XMPP gateway.
 *
 * Bridges SIP presence to XMPP. PublishXMPP sends a message to a user's
 * XMPP endpoint; SubscribeXMPP registers an XMPP subscription. The
 * module tracks a connected flag and the last published message per
 * user. It is safe for concurrent use.
 */

package pua_xmpp

import (
	"errors"
	"sync"
)

// PUAXmppModule bridges presence to XMPP.
type PUAXmppModule struct {
	mu        sync.RWMutex
	connected bool
	messages  map[string]string
	subs      map[string]bool
}

// New creates a PUAXmppModule, disconnected by default.
func New() *PUAXmppModule {
	return &PUAXmppModule{messages: make(map[string]string), subs: make(map[string]bool)}
}

// Connect marks the XMPP gateway as connected.
func (m *PUAXmppModule) Connect() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = true
}

// Disconnect marks the XMPP gateway as disconnected.
func (m *PUAXmppModule) Disconnect() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false
}

// IsConnected reports whether the XMPP gateway is connected.
//
//	C: pua_xmpp_is_connected()
func (m *PUAXmppModule) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// PublishXMPP stores message for user. Returns an error when not
// connected or user is empty.
//
//	C: pua_xmpp_publish()
func (m *PUAXmppModule) PublishXMPP(user, message string) error {
	if user == "" {
		return errors.New("pua_xmpp: empty user")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return errors.New("pua_xmpp: not connected")
	}
	m.messages[user] = message
	return nil
}

// SubscribeXMPP registers a subscription for user. Returns an error when
// not connected or user is empty.
//
//	C: pua_xmpp_subscribe()
func (m *PUAXmppModule) SubscribeXMPP(user string) error {
	if user == "" {
		return errors.New("pua_xmpp: empty user")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return errors.New("pua_xmpp: not connected")
	}
	m.subs[user] = true
	return nil
}

// GetMessage returns the last published message for user.
func (m *PUAXmppModule) GetMessage(user string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.messages[user]
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu      sync.RWMutex
	defaultPUAXmpp *PUAXmppModule
)

// DefaultPUAXmpp returns the process-wide PUAXmppModule, creating it on
// first use.
func DefaultPUAXmpp() *PUAXmppModule {
	defaultMu.RLock()
	m := defaultPUAXmpp
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultPUAXmpp == nil {
		defaultPUAXmpp = New()
	}
	return defaultPUAXmpp
}

// Init (re)initialises the process-wide PUAXmppModule to a fresh,
// disconnected state. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultPUAXmpp = New()
}
