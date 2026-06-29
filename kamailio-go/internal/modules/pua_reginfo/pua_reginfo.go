// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * pua_reginfo module - Presence User Agent for Registration Info.
 *
 * Publishes and subscribes to registration information (RFC 3680) for
 * users. The last published info is stored per user. The module is
 * safe for concurrent use.
 */

package pua_reginfo

import (
	"errors"
	"sync"
)

// PUARegInfoModule manages reg-info presence state.
type PUARegInfoModule struct {
	mu     sync.RWMutex
	states map[string]string
	subs   map[string]bool
}

// New creates a PUARegInfoModule with empty state.
func New() *PUARegInfoModule {
	return &PUARegInfoModule{states: make(map[string]string), subs: make(map[string]bool)}
}

// PublishRegInfo stores info for user. Returns an error when user is
// empty.
//
//	C: pua_reginfo_publish()
func (m *PUARegInfoModule) PublishRegInfo(user string, info string) error {
	if user == "" {
		return errors.New("pua_reginfo: empty user")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[user] = info
	return nil
}

// SubscribeRegInfo registers a subscription for user. Returns an error
// when user is empty.
//
//	C: pua_reginfo_subscribe()
func (m *PUARegInfoModule) SubscribeRegInfo(user string) error {
	if user == "" {
		return errors.New("pua_reginfo: empty user")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subs[user] = true
	return nil
}

// GetRegInfo returns the last published info for user, or an empty
// string when none exists.
//
//	C: pua_reginfo_get()
func (m *PUARegInfoModule) GetRegInfo(user string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.states[user]
}

// IsSubscribed reports whether user has an active subscription.
func (m *PUARegInfoModule) IsSubscribed(user string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.subs[user]
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu         sync.RWMutex
	defaultPUARegInfo *PUARegInfoModule
)

// DefaultPUARegInfo returns the process-wide PUARegInfoModule, creating
// it on first use.
func DefaultPUARegInfo() *PUARegInfoModule {
	defaultMu.RLock()
	m := defaultPUARegInfo
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultPUARegInfo == nil {
		defaultPUARegInfo = New()
	}
	return defaultPUARegInfo
}

// Init (re)initialises the process-wide PUARegInfoModule to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultPUARegInfo = New()
}
