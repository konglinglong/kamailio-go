// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * pua_bla module - Presence User Agent for Bridged Line Appearance.
 *
 * Publishes and subscribes to BLA (Bridged Line Appearance) state for
 * users. State is stored per user so GetBLAState returns the last
 * published value. The module is safe for concurrent use.
 */

package pua_bla

import (
	"errors"
	"sync"
)

// PUABLAModule manages BLA presence state.
type PUABLAModule struct {
	mu     sync.RWMutex
	states map[string]string
	subs   map[string]bool
}

// New creates a PUABLAModule with empty state.
func New() *PUABLAModule {
	return &PUABLAModule{states: make(map[string]string), subs: make(map[string]bool)}
}

// PublishBLA stores state for user and marks it published. Returns an
// error when user is empty.
//
//	C: pua_bla_publish()
func (m *PUABLAModule) PublishBLA(user, state string) error {
	if user == "" {
		return errors.New("pua_bla: empty user")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[user] = state
	return nil
}

// SubscribeBLA registers a subscription for user. Returns an error when
// user is empty.
//
//	C: pua_bla_subscribe()
func (m *PUABLAModule) SubscribeBLA(user string) error {
	if user == "" {
		return errors.New("pua_bla: empty user")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subs[user] = true
	return nil
}

// GetBLAState returns the last published state for user, or an empty
// string when none exists.
//
//	C: pua_bla_get_state()
func (m *PUABLAModule) GetBLAState(user string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.states[user]
}

// IsSubscribed reports whether user has an active subscription.
func (m *PUABLAModule) IsSubscribed(user string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.subs[user]
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu     sync.RWMutex
	defaultPUABLA *PUABLAModule
)

// DefaultPUABLA returns the process-wide PUABLAModule, creating it on
// first use.
func DefaultPUABLA() *PUABLAModule {
	defaultMu.RLock()
	m := defaultPUABLA
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultPUABLA == nil {
		defaultPUABLA = New()
	}
	return defaultPUABLA
}

// Init (re)initialises the process-wide PUABLAModule to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultPUABLA = New()
}
