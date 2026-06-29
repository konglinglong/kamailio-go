// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Auth xkeys module - shared-key authentication.
 * Port of the kamailio auth_xkeys module (src/modules/auth_xkeys).
 *
 * The module stores named shared keys and validates SIP messages by
 * comparing the value of the "X-Auth-Key" header against the stored key.
 * It is safe for concurrent use.
 */

package auth_xkeys

import (
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// AuthHeaderName is the header inspected by Validate.
const AuthHeaderName = "X-Auth-Key"

// AuthXKeysModule maintains a registry of named shared keys.
type AuthXKeysModule struct {
	mu   sync.RWMutex
	keys map[string]string
}

// New creates an AuthXKeysModule with empty storage.
func New() *AuthXKeysModule {
	return &AuthXKeysModule{keys: make(map[string]string)}
}

// SetKey stores or replaces the shared key for the given name.
//
//	C: xkeys_set_key()
func (m *AuthXKeysModule) SetKey(name, key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.keys == nil {
		m.keys = make(map[string]string)
	}
	m.keys[name] = key
}

// GetKey returns the shared key for the given name.
func (m *AuthXKeysModule) GetKey(name string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	k, ok := m.keys[name]
	return k, ok
}

// Validate returns true when msg carries an "X-Auth-Key" header whose
// value matches the shared key registered for keyName.
//
//	C: xkeys_validate()
func (m *AuthXKeysModule) Validate(msg *parser.SIPMsg, keyName string) bool {
	if msg == nil {
		return false
	}
	stored, ok := m.GetKey(keyName)
	if !ok || stored == "" {
		return false
	}
	for _, h := range msg.Headers {
		if h == nil {
			continue
		}
		if strings.EqualFold(h.Name.String(), AuthHeaderName) {
			return strings.TrimSpace(h.Body.String()) == stored
		}
	}
	return false
}

// RemoveKey deletes the shared key for the given name. Returns true when
// a key was removed.
func (m *AuthXKeysModule) RemoveKey(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.keys[name]; !ok {
		return false
	}
	delete(m.keys, name)
	return true
}

// Count returns the number of stored keys.
func (m *AuthXKeysModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.keys)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *AuthXKeysModule
)

// DefaultAuthXKeys returns the process-wide AuthXKeysModule.
func DefaultAuthXKeys() *AuthXKeysModule {
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

// Init (re)initialises the process-wide AuthXKeysModule.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// SetKey is the package-level wrapper around DefaultAuthXKeys().SetKey.
func SetKey(name, key string) { DefaultAuthXKeys().SetKey(name, key) }

// GetKey is the package-level wrapper around DefaultAuthXKeys().GetKey.
func GetKey(name string) (string, bool) { return DefaultAuthXKeys().GetKey(name) }

// Validate is the package-level wrapper around DefaultAuthXKeys().Validate.
func Validate(msg *parser.SIPMsg, keyName string) bool {
	return DefaultAuthXKeys().Validate(msg, keyName)
}

// RemoveKey is the package-level wrapper around DefaultAuthXKeys().RemoveKey.
func RemoveKey(name string) bool { return DefaultAuthXKeys().RemoveKey(name) }
