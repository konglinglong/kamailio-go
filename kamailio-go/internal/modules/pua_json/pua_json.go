// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * pua_json module - Presence User Agent for JSON payloads.
 *
 * Publishes and subscribes to JSON-encoded presence data for users.
 * The last published JSON is stored per user. The module is safe for
 * concurrent use.
 */

package pua_json

import (
	"errors"
	"sync"
)

// PUAJSONModule manages JSON presence state.
type PUAJSONModule struct {
	mu     sync.RWMutex
	states map[string]string
	subs   map[string]bool
}

// New creates a PUAJSONModule with empty state.
func New() *PUAJSONModule {
	return &PUAJSONModule{states: make(map[string]string), subs: make(map[string]bool)}
}

// PublishJSON stores data for user. Returns an error when user is empty.
//
//	C: pua_json_publish()
func (m *PUAJSONModule) PublishJSON(user string, data string) error {
	if user == "" {
		return errors.New("pua_json: empty user")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[user] = data
	return nil
}

// SubscribeJSON registers a subscription for user. Returns an error when
// user is empty.
//
//	C: pua_json_subscribe()
func (m *PUAJSONModule) SubscribeJSON(user string) error {
	if user == "" {
		return errors.New("pua_json: empty user")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subs[user] = true
	return nil
}

// GetJSON returns the last published JSON for user, or an empty string
// when none exists.
//
//	C: pua_json_get()
func (m *PUAJSONModule) GetJSON(user string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.states[user]
}

// IsSubscribed reports whether user has an active subscription.
func (m *PUAJSONModule) IsSubscribed(user string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.subs[user]
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu      sync.RWMutex
	defaultPUAJSON *PUAJSONModule
)

// DefaultPUAJSON returns the process-wide PUAJSONModule, creating it on
// first use.
func DefaultPUAJSON() *PUAJSONModule {
	defaultMu.RLock()
	m := defaultPUAJSON
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultPUAJSON == nil {
		defaultPUAJSON = New()
	}
	return defaultPUAJSON
}

// Init (re)initialises the process-wide PUAJSONModule to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultPUAJSON = New()
}
