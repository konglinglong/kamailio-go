// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SipRepo module - in-memory SIP message repository.
 * Port of the kamailio siprepo module (src/modules/siprepo).
 *
 * Stores parsed SIP messages under a generated id so the routing script can
 * stash a message, continue processing, and pull it back later. The id is a
 * monotonically increasing counter formatted as a hex string.
 *
 * It is safe for concurrent use.
 */
package siprepo

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// SipRepoModule implements the siprepo module.
// C: struct module siprepo
type SipRepoModule struct {
	mu      sync.RWMutex
	store   map[string]*parser.SIPMsg
	order   []string // ids in insertion order
	nextID  atomic.Int64
}

// NewSipRepoModule creates a SipRepoModule.
func NewSipRepoModule() *SipRepoModule {
	return &SipRepoModule{
		store: make(map[string]*parser.SIPMsg),
	}
}

// Store saves msg under a freshly generated id and returns that id.
// Returns an empty string when msg is nil.
// C: siprepo_store()
func (m *SipRepoModule) Store(msg *parser.SIPMsg) string {
	if msg == nil {
		return ""
	}
	id := fmt.Sprintf("msg-%x", m.nextID.Add(1))
	m.mu.Lock()
	if m.store == nil {
		m.store = make(map[string]*parser.SIPMsg)
	}
	m.store[id] = msg
	m.order = append(m.order, id)
	m.mu.Unlock()
	return id
}

// Retrieve returns the message stored under id, or an error when no such
// id exists.
// C: siprepo_retrieve()
func (m *SipRepoModule) Retrieve(id string) (*parser.SIPMsg, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	msg, ok := m.store[id]
	if !ok {
		return nil, fmt.Errorf("siprepo: unknown id %q", id)
	}
	return msg, nil
}

// Delete removes the message stored under id. Returns true when something
// was removed.
// C: siprepo_delete()
func (m *SipRepoModule) Delete(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.store[id]; !ok {
		return false
	}
	delete(m.store, id)
	for i, x := range m.order {
		if x == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	return true
}

// Count returns the number of stored messages.
func (m *SipRepoModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.store)
}

// List returns the ids of every stored message in insertion order.
func (m *SipRepoModule) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, len(m.order))
	copy(out, m.order)
	return out
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu      sync.RWMutex
	defaultSipRepo *SipRepoModule
)

// DefaultSipRepo returns the process-wide SipRepoModule, creating one on
// first use.
func DefaultSipRepo() *SipRepoModule {
	defaultMu.RLock()
	m := defaultSipRepo
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultSipRepo == nil {
		defaultSipRepo = NewSipRepoModule()
	}
	return defaultSipRepo
}

// Init (re)initialises the process-wide SipRepoModule to a fresh state.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultSipRepo = NewSipRepoModule()
}
