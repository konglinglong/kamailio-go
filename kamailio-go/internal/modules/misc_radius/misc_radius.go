// SPDX-License-Identifier-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * MiscRadius module - miscellaneous RADIUS attribute handling.
 * Port of the kamailio misc_radius module (src/modules/misc_radius).
 *
 * misc_radius sends user/attribute pairs to a configured RADIUS server.
 * This implementation buffers the requests in memory (keyed by user) so
 * tests can inspect what would have been sent. Init establishes the
 * "connection" (records server + secret); Send records the attributes.
 *
 * The module is safe for concurrent use.
 */

package misc_radius

import (
	"errors"
	"sync"
)

// Request is a buffered RADIUS request.
type Request struct {
	User string
	Attrs map[string]string
}

// MiscRadiusModule buffers RADIUS requests.
type MiscRadiusModule struct {
	mu       sync.RWMutex
	server   string
	secret   string
	requests []Request
	byUser   map[string][]Request
	connected bool
}

// New creates a MiscRadiusModule that is not yet connected.
func New() *MiscRadiusModule {
	return &MiscRadiusModule{byUser: make(map[string][]Request)}
}

// Init configures the server and shared secret and marks the module
// connected. It mirrors Kamailio's mod_init.
func (m *MiscRadiusModule) Init(server, secret string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.server = server
	m.secret = secret
	m.connected = true
	m.requests = nil
	m.byUser = make(map[string][]Request)
}

// IsConnected reports whether Init has been called.
func (m *MiscRadiusModule) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// Send records a RADIUS request for user with the given attributes. It
// returns an error when the module is not connected or user is empty.
func (m *MiscRadiusModule) Send(user string, attrs map[string]string) error {
	if user == "" {
		return errors.New("misc_radius: empty user")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return errors.New("misc_radius: not connected")
	}
	cp := make(map[string]string, len(attrs))
	for k, v := range attrs {
		cp[k] = v
	}
	req := Request{User: user, Attrs: cp}
	m.requests = append(m.requests, req)
	m.byUser[user] = append(m.byUser[user], req)
	return nil
}

// Requests returns a copy of all buffered requests. The Attrs maps are
// deep-copied so callers cannot mutate the stored entries.
func (m *MiscRadiusModule) Requests() []Request {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Request, len(m.requests))
	for i, r := range m.requests {
		out[i] = Request{User: r.User, Attrs: copyAttrs(r.Attrs)}
	}
	return out
}

// RequestsForUser returns the buffered requests for user. The Attrs maps
// are deep-copied.
func (m *MiscRadiusModule) RequestsForUser(user string) []Request {
	m.mu.RLock()
	defer m.mu.RUnlock()
	src := m.byUser[user]
	out := make([]Request, len(src))
	for i, r := range src {
		out[i] = Request{User: r.User, Attrs: copyAttrs(r.Attrs)}
	}
	return out
}

// copyAttrs returns a shallow copy of attrs.
func copyAttrs(attrs map[string]string) map[string]string {
	if attrs == nil {
		return nil
	}
	out := make(map[string]string, len(attrs))
	for k, v := range attrs {
		out[k] = v
	}
	return out
}

// Count returns the total number of buffered requests.
func (m *MiscRadiusModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.requests)
}

// Close marks the module disconnected and clears buffers.
func (m *MiscRadiusModule) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *MiscRadiusModule
)

// DefaultMiscRadius returns the process-wide module, creating it on first use.
func DefaultMiscRadius() *MiscRadiusModule {
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

// Init is the package-level (re)initialiser.
func Init(server, secret string) {
	DefaultMiscRadius().Init(server, secret)
}

// Send is the package-level wrapper.
func Send(user string, attrs map[string]string) error {
	return DefaultMiscRadius().Send(user, attrs)
}

// IsConnected is the package-level wrapper.
func IsConnected() bool { return DefaultMiscRadius().IsConnected() }
