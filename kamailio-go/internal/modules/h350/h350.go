// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * H.350 module - LDAP-based directory entries.
 * Port of the kamailio h350 module (src/modules/h350).
 *
 * The module stores H.350 directory entries keyed by user (UID) and
 * provides lookup and add operations. It is safe for concurrent use.
 */

package h350

import (
	"errors"
	"sync"
)

// H350Entry represents a single H.350 directory entry.
type H350Entry struct {
	DN    string
	UID   string
	Phone string
}

// H350Module maintains a user-indexed store of H.350 entries.
type H350Module struct {
	mu     sync.RWMutex
	entries map[string]*H350Entry
}

// New creates an H350Module with empty storage.
func New() *H350Module {
	return &H350Module{entries: make(map[string]*H350Entry)}
}

// Add stores the given entry, indexed by its UID. It returns an error
// when the entry is nil or has an empty UID.
//
//	C: h350_add()
func (m *H350Module) Add(entry *H350Entry) error {
	if entry == nil {
		return errors.New("h350: nil entry")
	}
	if entry.UID == "" {
		return errors.New("h350: empty UID")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entries == nil {
		m.entries = make(map[string]*H350Entry)
	}
	m.entries[entry.UID] = &H350Entry{
		DN:    entry.DN,
		UID:   entry.UID,
		Phone: entry.Phone,
	}
	return nil
}

// Lookup returns the entry for the given user (UID). It returns an error
// when no entry exists.
//
//	C: h350_lookup()
func (m *H350Module) Lookup(user string) (*H350Entry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.entries[user]
	if !ok {
		return nil, errors.New("h350: user not found: " + user)
	}
	return &H350Entry{DN: e.DN, UID: e.UID, Phone: e.Phone}, nil
}

// Remove deletes the entry for the given user. Returns true when an
// entry was removed.
func (m *H350Module) Remove(user string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entries[user]; !ok {
		return false
	}
	delete(m.entries, user)
	return true
}

// Count returns the number of stored entries.
func (m *H350Module) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *H350Module
)

// DefaultH350 returns the process-wide H350Module.
func DefaultH350() *H350Module {
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

// Init (re)initialises the process-wide H350Module.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// Add is the package-level wrapper around DefaultH350().Add.
func Add(entry *H350Entry) error { return DefaultH350().Add(entry) }

// Lookup is the package-level wrapper around DefaultH350().Lookup.
func Lookup(user string) (*H350Entry, error) { return DefaultH350().Lookup(user) }
