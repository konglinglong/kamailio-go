// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * auth_diameter module - Diameter (RFC 6733) authentication client.
 *
 * A simplified Diameter Authentication-Authorization client. Init opens
 * a "connection" to a Diameter server (tracked as a flag); Authenticate
 * validates user/password against an in-memory credential table so that
 * the module is functional without a live server. It is safe for
 * concurrent use.
 */

package auth_diameter

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
)

// AuthDiameterModule implements Diameter-based authentication.
type AuthDiameterModule struct {
	mu        sync.RWMutex
	server    string
	connected bool
	creds     map[string]string // user -> hex(sha256(password))
}

// New creates an AuthDiameterModule with no server configured.
func New() *AuthDiameterModule {
	return &AuthDiameterModule{creds: make(map[string]string)}
}

// hashPassword returns the hex-encoded SHA-256 digest of password.
func hashPassword(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}

// AddCredential registers a user/password pair in the local credential
// table. This mirrors seeding a Diameter user database.
func (m *AuthDiameterModule) AddCredential(user, password string) {
	if user == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.creds[user] = hashPassword(password)
}

// Init configures the Diameter server address and marks the module as
// connected. An empty server leaves the module disconnected.
//
//	C: auth_diameter_init()
func (m *AuthDiameterModule) Init(server string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.server = server
	m.connected = server != ""
}

// IsConnected reports whether the module has an active server connection.
//
//	C: auth_diameter_is_connected()
func (m *AuthDiameterModule) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// Authenticate validates user/password against the local credential table.
// It returns an error when the module is not connected or the credentials
// are invalid.
//
//	C: auth_diameter_authenticate()
func (m *AuthDiameterModule) Authenticate(user, password string) (bool, error) {
	if user == "" || password == "" {
		return false, errors.New("auth_diameter: empty user or password")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.connected {
		return false, errors.New("auth_diameter: not connected")
	}
	want, ok := m.creds[user]
	if !ok {
		return false, nil
	}
	return want == hashPassword(password), nil
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu       sync.RWMutex
	defaultDiameter *AuthDiameterModule
)

// DefaultAuthDiameter returns the process-wide AuthDiameterModule, creating
// it on first use.
func DefaultAuthDiameter() *AuthDiameterModule {
	defaultMu.RLock()
	m := defaultDiameter
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultDiameter == nil {
		defaultDiameter = New()
	}
	return defaultDiameter
}

// Init (re)initialises the process-wide AuthDiameterModule to a fresh,
// unconfigured state. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultDiameter = New()
}
