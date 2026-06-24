// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Auth ephemeral module - ephemeral authentication tokens.
 * Port of the kamailio auth_ephemeral module (src/modules/auth_ephemeral).
 *
 * The module issues short-lived per-user tokens that can be validated and
 * revoked. Tokens are generated with a random nonce and stored in memory.
 * It is safe for concurrent use.
 */

package auth_ephemeral

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
)

// AuthEphemeralModule maintains a per-user ephemeral token store.
type AuthEphemeralModule struct {
	mu     sync.RWMutex
	tokens map[string]string
}

// New creates an AuthEphemeralModule with empty storage.
func New() *AuthEphemeralModule {
	return &AuthEphemeralModule{tokens: make(map[string]string)}
}

// Generate issues a new ephemeral token for the given user and returns it.
// A previously issued token for the same user is replaced.
//
//	C: autheph_generate()
func (m *AuthEphemeralModule) Generate(user string) (string, error) {
	if user == "" {
		return "", errors.New("auth_ephemeral: empty user")
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	token := user + ":" + hex.EncodeToString(nonce)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tokens == nil {
		m.tokens = make(map[string]string)
	}
	m.tokens[user] = token
	return token, nil
}

// Validate returns true when the given token matches the currently
// stored token for the user.
//
//	C: autheph_validate()
func (m *AuthEphemeralModule) Validate(user, token string) bool {
	if user == "" {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	stored, ok := m.tokens[user]
	if !ok {
		return false
	}
	return stored == token
}

// Revoke removes the token for the given user, if any.
//
//	C: autheph_revoke()
func (m *AuthEphemeralModule) Revoke(user string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tokens, user)
}

// Count returns the number of currently issued tokens.
func (m *AuthEphemeralModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.tokens)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *AuthEphemeralModule
)

// DefaultAuthEphemeral returns the process-wide AuthEphemeralModule.
func DefaultAuthEphemeral() *AuthEphemeralModule {
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

// Init (re)initialises the process-wide AuthEphemeralModule.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// Generate is the package-level wrapper around DefaultAuthEphemeral().Generate.
func Generate(user string) (string, error) { return DefaultAuthEphemeral().Generate(user) }

// Validate is the package-level wrapper around DefaultAuthEphemeral().Validate.
func Validate(user, token string) bool { return DefaultAuthEphemeral().Validate(user, token) }

// Revoke is the package-level wrapper around DefaultAuthEphemeral().Revoke.
func Revoke(user string) { DefaultAuthEphemeral().Revoke(user) }
