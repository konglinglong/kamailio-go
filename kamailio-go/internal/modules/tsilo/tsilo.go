// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Transaction silo (tsilo) module - per-RURI transaction storage.
 * Port of the kamailio tsilo module (src/modules/tsilo).
 *
 * The tsilo module stores SIP transactions keyed by Request-URI so that
 * they can be delivered later, once the target registers or becomes
 * reachable. Each stored entry records the RURI, Call-ID, From tag and
 * message body, plus the time it was stored so that expired entries can be
 * purged.
 *
 * It is safe for concurrent use: the entry map is guarded by a read/write
 * lock and the process-wide singleton is guarded by a mutex.
 */

package tsilo

import (
	"errors"
	"sync"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// TSiloEntry describes a single stored transaction awaiting delivery.
type TSiloEntry struct {
	RURI     string
	CallID   string
	FromTag  string
	Body     string
	StoredAt time.Time
}

// TSiloModule implements the tsilo module functionality.
// C: struct module tsilo
type TSiloModule struct {
	mu      sync.RWMutex
	entries map[string]*TSiloEntry
}

// New creates a TSiloModule with empty storage.
func New() *TSiloModule {
	return &TSiloModule{entries: make(map[string]*TSiloEntry)}
}

// Store records the transaction described by msg under ruri for later
// delivery. The Call-ID, From tag and body are extracted from msg.
// Returns 0 on success or -1 when msg is nil or ruri is empty.
//
//	C: ts_store() / t_store()
func (m *TSiloModule) Store(ruri string, msg *parser.SIPMsg) int {
	if msg == nil || ruri == "" {
		return -1
	}
	entry := &TSiloEntry{
		RURI:     ruri,
		CallID:   extractCallID(msg),
		FromTag:  extractFromTag(msg),
		Body:     extractBody(msg),
		StoredAt: time.Now(),
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entries == nil {
		m.entries = make(map[string]*TSiloEntry)
	}
	m.entries[ruri] = entry
	return 0
}

// Retrieve returns the entry stored under ruri. Returns an error when no
// entry is stored for ruri.
//
//	C: ts_retrieve() analogue
func (m *TSiloModule) Retrieve(ruri string) (*TSiloEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.entries[ruri]
	if !ok {
		return nil, errors.New("tsilo: no entry for " + ruri)
	}
	return e, nil
}

// Delete removes the entry stored under ruri. Returns true when an entry
// was removed.
//
//	C: ts_delete() analogue
func (m *TSiloModule) Delete(ruri string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entries[ruri]; !ok {
		return false
	}
	delete(m.entries, ruri)
	return true
}

// Count returns the number of stored entries.
func (m *TSiloModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}

// List returns a snapshot of every stored entry.
func (m *TSiloModule) List() []*TSiloEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*TSiloEntry, 0, len(m.entries))
	for _, e := range m.entries {
		out = append(out, e)
	}
	return out
}

// CleanupExpired removes every entry older than ttl. Entries with a zero
// StoredAt are never expired.
//
//	C: ts_cleanup() analogue
func (m *TSiloModule) CleanupExpired(ttl time.Duration) {
	cutoff := time.Now().Add(-ttl)
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, e := range m.entries {
		if e.StoredAt.IsZero() {
			continue
		}
		if e.StoredAt.Before(cutoff) {
			delete(m.entries, k)
		}
	}
}

// IsStored reports whether an entry is currently stored under ruri.
//
//	C: ts_is_stored() analogue
func (m *TSiloModule) IsStored(ruri string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.entries[ruri]
	return ok
}

// extractCallID returns the trimmed Call-ID value of msg, or the empty
// string when absent.
func extractCallID(msg *parser.SIPMsg) string {
	if msg.CallID == nil {
		return ""
	}
	return msg.CallID.Body.String()
}

// extractFromTag returns the tag parameter of the From header of msg, or
// the empty string when absent.
func extractFromTag(msg *parser.SIPMsg) string {
	if msg.From == nil {
		return ""
	}
	tb, err := parser.ParseToBody(msg.From.Body)
	if err != nil || tb == nil {
		return ""
	}
	return tb.Tag.String()
}

// extractBody returns the message body as a string, or the empty string
// when there is no body.
func extractBody(msg *parser.SIPMsg) string {
	if b, ok := msg.Body.([]byte); ok && len(b) > 0 {
		return string(b)
	}
	return ""
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu    sync.RWMutex
	defaultTSilo *TSiloModule
)

// DefaultTSilo returns the process-wide TSiloModule, creating it on first
// use.
func DefaultTSilo() *TSiloModule {
	defaultMu.RLock()
	m := defaultTSilo
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultTSilo == nil {
		defaultTSilo = New()
	}
	return defaultTSilo
}

// Init (re)initialises the process-wide TSiloModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultTSilo = New()
}
