// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * auth_web3 module - Web3 (Ethereum-style) challenge-response auth.
 *
 * A simplified challenge-response scheme: the server issues a random
 * challenge per user; the client signs it with its private key and the
 * server verifies the signature against the user's address. Real Web3
 * verification recovers the secp256k1 public key from the signature;
 * this Go counterpart uses a deterministic HMAC of the challenge keyed
 * by the address so that sign and verify round-trip within a process.
 *
 * It is safe for concurrent use.
 */

package auth_web3

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// challengeTTL is how long an issued challenge remains valid.
const challengeTTL = 5 * time.Minute

type challengeEntry struct {
	value   string
	expires time.Time
}

// AuthWeb3Module implements Web3 challenge-response authentication.
type AuthWeb3Module struct {
	mu         sync.RWMutex
	challenges map[string]challengeEntry
}

// New creates an AuthWeb3Module with empty challenge storage.
func New() *AuthWeb3Module {
	return &AuthWeb3Module{challenges: make(map[string]challengeEntry)}
}

// GetChallenge issues a fresh challenge for user and stores it for later
// validation. Returns an empty string when user is empty.
//
//	C: auth_web3_get_challenge()
func (m *AuthWeb3Module) GetChallenge(user string) string {
	if user == "" {
		return ""
	}
	c := fmt.Sprintf("web3-%s-%d", user, time.Now().UnixNano())
	m.mu.Lock()
	defer m.mu.Unlock()
	m.challenges[user] = challengeEntry{value: c, expires: time.Now().Add(challengeTTL)}
	return c
}

// ValidateChallenge reports whether challenge matches the stored challenge
// for user and has not expired.
//
//	C: auth_web3_validate_challenge()
func (m *AuthWeb3Module) ValidateChallenge(user, challenge string) bool {
	if user == "" || challenge == "" {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.challenges[user]
	if !ok || time.Now().After(entry.expires) {
		return false
	}
	return hmac.Equal([]byte(entry.value), []byte(challenge))
}

// Verify reports whether signature is the HMAC-SHA256 (hex) of the stored
// challenge for address, keyed by address. The challenge must exist and be
// unexpired. On success the challenge is consumed.
//
//	C: auth_web3_verify()
func (m *AuthWeb3Module) Verify(address, signature string) bool {
	if address == "" || signature == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.challenges[address]
	if !ok || time.Now().After(entry.expires) {
		return false
	}
	mac := hmac.New(sha256.New, []byte(address))
	mac.Write([]byte(entry.value))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return false
	}
	delete(m.challenges, address)
	return true
}

// SignChallenge is a convenience helper that produces the signature the
// server expects for address's current challenge. It is intended for
// testing and client-side simulation.
func (m *AuthWeb3Module) SignChallenge(address string) string {
	if address == "" {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.challenges[address]
	if !ok || time.Now().After(entry.expires) {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(address))
	mac.Write([]byte(entry.value))
	return hex.EncodeToString(mac.Sum(nil))
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu   sync.RWMutex
	defaultWeb3 *AuthWeb3Module
)

// DefaultAuthWeb3 returns the process-wide AuthWeb3Module, creating it on
// first use.
func DefaultAuthWeb3() *AuthWeb3Module {
	defaultMu.RLock()
	m := defaultWeb3
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultWeb3 == nil {
		defaultWeb3 = New()
	}
	return defaultWeb3
}

// Init (re)initialises the process-wide AuthWeb3Module to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultWeb3 = New()
}
