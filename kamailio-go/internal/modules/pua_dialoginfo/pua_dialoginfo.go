// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * pua_dialoginfo module - Presence User Agent for Dialog Info.
 *
 * Publishes and subscribes to dialog information (RFC 4235) for users.
 * The last published info is stored per user. The module is safe for
 * concurrent use.
 */

package pua_dialoginfo

import (
	"errors"
	"sync"
)

// PUADialogInfoModule manages dialog-info presence state.
type PUADialogInfoModule struct {
	mu     sync.RWMutex
	states map[string]string
	subs   map[string]bool
}

// New creates a PUADialogInfoModule with empty state.
func New() *PUADialogInfoModule {
	return &PUADialogInfoModule{states: make(map[string]string), subs: make(map[string]bool)}
}

// PublishDialogInfo stores info for user. Returns an error when user is
// empty.
//
//	C: pua_dialoginfo_publish()
func (m *PUADialogInfoModule) PublishDialogInfo(user string, info string) error {
	if user == "" {
		return errors.New("pua_dialoginfo: empty user")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[user] = info
	return nil
}

// SubscribeDialogInfo registers a subscription for user. Returns an error
// when user is empty.
//
//	C: pua_dialoginfo_subscribe()
func (m *PUADialogInfoModule) SubscribeDialogInfo(user string) error {
	if user == "" {
		return errors.New("pua_dialoginfo: empty user")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subs[user] = true
	return nil
}

// GetDialogInfo returns the last published info for user, or an empty
// string when none exists.
//
//	C: pua_dialoginfo_get_info()
func (m *PUADialogInfoModule) GetDialogInfo(user string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.states[user]
}

// IsSubscribed reports whether user has an active subscription.
func (m *PUADialogInfoModule) IsSubscribed(user string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.subs[user]
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu            sync.RWMutex
	defaultPUADialogInfo *PUADialogInfoModule
)

// DefaultPUADialogInfo returns the process-wide PUADialogInfoModule,
// creating it on first use.
func DefaultPUADialogInfo() *PUADialogInfoModule {
	defaultMu.RLock()
	m := defaultPUADialogInfo
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultPUADialogInfo == nil {
		defaultPUADialogInfo = New()
	}
	return defaultPUADialogInfo
}

// Init (re)initialises the process-wide PUADialogInfoModule to a fresh
// state. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultPUADialogInfo = New()
}
