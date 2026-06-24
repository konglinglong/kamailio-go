// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * auth_arnacon module - Arnacon token-based authentication.
 *
 * Issues opaque per-user tokens and authenticates requests by looking
 * the token up in an in-memory table. Tokens can be revoked. The module
 * is safe for concurrent use.
 */

package auth_arnacon

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// tokenTTL is how long a token remains valid.
const tokenTTL = 1 * time.Hour

type tokenEntry struct {
	value   string
	expires time.Time
}

// AuthArnaconModule implements Arnacon token-based authentication.
type AuthArnaconModule struct {
	mu     sync.RWMutex
	tokens map[string]tokenEntry
}

// New creates an AuthArnaconModule with empty token storage.
func New() *AuthArnaconModule {
	return &AuthArnaconModule{tokens: make(map[string]tokenEntry)}
}

// GenerateToken issues a fresh opaque token for user and stores it.
// Returns an empty string when user is empty.
//
//	C: auth_arnacon_generate_token()
func (m *AuthArnaconModule) GenerateToken(user string) string {
	if user == "" {
		return ""
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// Fall back to a time-derived value; rand.Read practically never
		// fails on modern systems.
		return fmt.Sprintf("arnacon-%s-%d", user, time.Now().UnixNano())
	}
	tok := "arnacon-" + hex.EncodeToString(raw[:])
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[user] = tokenEntry{value: tok, expires: time.Now().Add(tokenTTL)}
	return tok
}

// Authenticate reports whether token is valid for user and unexpired.
//
//	C: auth_arnacon_authenticate()
func (m *AuthArnaconModule) Authenticate(user, token string) bool {
	if user == "" || token == "" {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.tokens[user]
	if !ok || time.Now().After(entry.expires) {
		return false
	}
	return entry.value == token
}

// RevokeToken removes the token for user. It is not an error if no token
// exists.
//
//	C: auth_arnacon_revoke_token()
func (m *AuthArnaconModule) RevokeToken(user string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tokens, user)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu      sync.RWMutex
	defaultArnacon *AuthArnaconModule
)

// DefaultAuthArnacon returns the process-wide AuthArnaconModule, creating
// it on first use.
func DefaultAuthArnacon() *AuthArnaconModule {
	defaultMu.RLock()
	m := defaultArnacon
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultArnacon == nil {
		defaultArnacon = New()
	}
	return defaultArnacon
}

// Init (re)initialises the process-wide AuthArnaconModule to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultArnacon = New()
}
