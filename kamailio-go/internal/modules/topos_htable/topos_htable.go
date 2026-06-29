// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * ToposHTable module - in-memory hash-table backend for topos.
 * Port of the kamailio topos_htable module (src/modules/topos_htable).
 *
 * The topos_htable module stores the topology-hiding state of SIP
 * dialogs (identified by Call-ID and From-tag) in an in-memory hash
 * table. It is the default storage backend used by the topos module
 * when no database is configured.
 *
 * It is safe for concurrent use.
 */

package topos_htable

import (
	"errors"
	"fmt"
	"sync"
)

// ToposHTableModule is the in-memory topos storage backend.
// C: struct module topos_htable
type ToposHTableModule struct {
	mu      sync.RWMutex
	records map[string][]byte
}

// New creates an empty ToposHTableModule.
func New() *ToposHTableModule {
	return &ToposHTableModule{records: make(map[string][]byte)}
}

// Store saves data for the dialog identified by (callID, fromTag). An empty
// callID is rejected.
//
//	C: th_store()
func (m *ToposHTableModule) Store(callID, fromTag string, data []byte) error {
	if m == nil {
		return errors.New("topos_htable: nil module")
	}
	if callID == "" {
		return errors.New("topos_htable: empty call-id")
	}
	key := recordKey(callID, fromTag)
	m.mu.Lock()
	if m.records == nil {
		m.records = make(map[string][]byte)
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	m.records[key] = cp
	m.mu.Unlock()
	return nil
}

// Retrieve returns the data stored for (callID, fromTag). Returns an error
// when no record exists.
//
//	C: th_load()
func (m *ToposHTableModule) Retrieve(callID, fromTag string) ([]byte, error) {
	if m == nil {
		return nil, errors.New("topos_htable: nil module")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := recordKey(callID, fromTag)
	data, ok := m.records[key]
	if !ok {
		return nil, fmt.Errorf("topos_htable: no record for call-id %q tag %q", callID, fromTag)
	}
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}

// Delete removes every record whose Call-ID matches callID. Returns true
// when at least one record was removed.
//
//	C: th_clean()
func (m *ToposHTableModule) Delete(callID string) bool {
	if m == nil || callID == "" {
		return false
	}
	prefix := callID + "|"
	m.mu.Lock()
	defer m.mu.Unlock()
	removed := false
	for key := range m.records {
		if key == callID || (len(key) > len(prefix) && key[:len(prefix)] == prefix) {
			delete(m.records, key)
			removed = true
		}
	}
	return removed
}

// Count returns the number of stored records.
func (m *ToposHTableModule) Count() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.records)
}

// recordKey produces a stable key from a Call-ID and From-tag.
func recordKey(callID, fromTag string) string {
	return callID + "|" + fromTag
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu          sync.RWMutex
	defaultToposHTable *ToposHTableModule
)

// DefaultToposHTable returns the process-wide ToposHTableModule, creating it
// on first use.
func DefaultToposHTable() *ToposHTableModule {
	defaultMu.RLock()
	m := defaultToposHTable
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultToposHTable == nil {
		defaultToposHTable = New()
	}
	return defaultToposHTable
}

// Init (re)initialises the process-wide ToposHTableModule to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultToposHTable = New()
}
