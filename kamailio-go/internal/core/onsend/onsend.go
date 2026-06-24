// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * On-send hook framework - matching C onsend.c
 *
 * Provides hooks invoked before a SIP message is sent on the wire.
 * Mirrors run_onsend() from the C core, exposed as a thread-safe Go
 * OnSendManager plus package-level helpers backed by a default
 * singleton manager.
 */

package onsend

import (
	"net"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// OnSendHook is the function signature invoked before a message is
// sent. It returns 1 to allow sending and 0 to block (drop) it.
//
// This mirrors the run_onsend() return contract documented in C
// onsend.c:
//   - 0  -> drop the message (block)
//   - >0 -> ok, forward (allow)
//   - <0 -> error, but still forward (allow)
//
// Consequently Execute treats any non-zero return as "allow" and a
// zero return as "block".
type OnSendHook func(msg *parser.SIPMsg, dst net.Addr, isRequest bool) int

// hookEntry is a single registered on-send hook.
type hookEntry struct {
	ID   int
	Hook OnSendHook
}

// OnSendManager manages on-send hooks.
// C counterpart: the onsend route list plus run_onsend() in onsend.c.
type OnSendManager struct {
	mu      sync.RWMutex
	nextID  int
	entries []hookEntry
}

// NewOnSendManager creates a new on-send hook manager.
func NewOnSendManager() *OnSendManager {
	return &OnSendManager{
		nextID: 1,
	}
}

// Register adds an on-send hook and returns its ID. A nil hook is
// rejected with -1. IDs are unique and monotonically increasing.
func (m *OnSendManager) Register(hook OnSendHook) int {
	if hook == nil {
		return -1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.nextID
	m.nextID++
	m.entries = append(m.entries, hookEntry{ID: id, Hook: hook})
	return id
}

// Unregister removes the hook with the given ID. Removing an unknown
// or already-removed ID is a safe no-op.
func (m *OnSendManager) Unregister(hookID int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, e := range m.entries {
		if e.ID == hookID {
			m.entries = append(m.entries[:i], m.entries[i+1:]...)
			return
		}
	}
}

// Execute runs every registered hook in registration (FIFO) order and
// returns true only if all hooks allow sending. The first hook that
// blocks (returns 0) short-circuits and makes Execute return false,
// matching the "drop the message" behaviour of run_onsend().
func (m *OnSendManager) Execute(msg *parser.SIPMsg, dst net.Addr, isRequest bool) bool {
	m.mu.RLock()
	// Snapshot so execution is unaffected by concurrent mutation
	// and so we never hold the lock while running user callbacks.
	snapshot := make([]hookEntry, len(m.entries))
	copy(snapshot, m.entries)
	m.mu.RUnlock()

	for _, e := range snapshot {
		if e.Hook(msg, dst, isRequest) == 0 {
			return false
		}
	}
	return true
}

// Count returns the number of hooks currently registered.
func (m *OnSendManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}

// Clear removes all registered hooks.
func (m *OnSendManager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = nil
}

var (
	defaultMu      sync.RWMutex
	defaultManager *OnSendManager
)

// DefaultOnSendManager returns the process-wide on-send hook manager,
// creating it on first use.
func DefaultOnSendManager() *OnSendManager {
	defaultMu.RLock()
	dm := defaultManager
	defaultMu.RUnlock()
	if dm == nil {
		defaultMu.Lock()
		if defaultManager == nil {
			defaultManager = NewOnSendManager()
		}
		dm = defaultManager
		defaultMu.Unlock()
	}
	return dm
}

// Init (re)initializes the default on-send hook manager.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultManager = NewOnSendManager()
}

// Register adds a hook to the default manager.
func Register(hook OnSendHook) int {
	return DefaultOnSendManager().Register(hook)
}

// Execute runs hooks on the default manager.
func Execute(msg *parser.SIPMsg, dst net.Addr, isRequest bool) bool {
	return DefaultOnSendManager().Execute(msg, dst, isRequest)
}
