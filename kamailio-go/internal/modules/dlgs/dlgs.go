// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Dlgs module - dialog store.
 * Port of the kamailio dlgs module (src/modules/dlgs).
 *
 * The dlgs module maintains an in-memory store of SIP dialog records,
 * keyed by a generated dialog ID and indexed by (Call-ID, From-tag).
 * It provides CRUD operations plus state-based queries and TTL-based
 * expiry.
 *
 * It is safe for concurrent use.
 */

package dlgs

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultState is the state assigned to a newly created dialog.
const DefaultState = "early"

// DialogRecord captures the state of a single SIP dialog.
type DialogRecord struct {
	ID        string
	CallID    string
	FromTag   string
	ToTag     string
	FromURI   string
	ToURI     string
	RURI      string
	State     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DlgsModule maintains a collection of DialogRecords.
type DlgsModule struct {
	mu        sync.RWMutex
	records   map[string]*DialogRecord
	byCallTag map[string]string // callID|fromTag -> ID
	counter   atomic.Uint64
}

// New creates a DlgsModule with empty stores.
func New() *DlgsModule {
	return &DlgsModule{
		records:   make(map[string]*DialogRecord),
		byCallTag: make(map[string]string),
	}
}

// Create stores a new dialog record for the given dialog identifiers and
// returns it. The record starts in the "early" state.
//
//	C: dlgs_create()
func (m *DlgsModule) Create(callID, fromTag, fromURI, toURI, ruri string) *DialogRecord {
	now := time.Now()
	rec := &DialogRecord{
		ID:        m.newID(),
		CallID:    callID,
		FromTag:   fromTag,
		FromURI:   fromURI,
		ToURI:     toURI,
		RURI:      ruri,
		State:     DefaultState,
		CreatedAt: now,
		UpdatedAt: now,
	}
	key := callTagKey(callID, fromTag)
	m.mu.Lock()
	if m.records == nil {
		m.records = make(map[string]*DialogRecord)
	}
	if m.byCallTag == nil {
		m.byCallTag = make(map[string]string)
	}
	m.records[rec.ID] = rec
	m.byCallTag[key] = rec.ID
	m.mu.Unlock()
	return rec
}

// Get returns the dialog record for the given Call-ID and From-tag, or
// nil when no such record exists.
//
//	C: dlgs_get()
func (m *DlgsModule) Get(callID, fromTag string) *DialogRecord {
	m.mu.RLock()
	id := m.byCallTag[callTagKey(callID, fromTag)]
	m.mu.RUnlock()
	if id == "" {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.records[id]
}

// GetByID returns the dialog record with the given ID, or nil.
//
//	C: dlgs_get_by_id()
func (m *DlgsModule) GetByID(id string) *DialogRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.records[id]
}

// Update sets the state of the dialog identified by id and refreshes its
// UpdatedAt timestamp. Returns true when the record was found.
//
//	C: dlgs_update()
func (m *DlgsModule) Update(id, state string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[id]
	if !ok {
		return false
	}
	rec.State = state
	rec.UpdatedAt = time.Now()
	return true
}

// Delete removes the dialog identified by id. Returns true when the
// record was found and removed.
//
//	C: dlgs_delete()
func (m *DlgsModule) Delete(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[id]
	if !ok {
		return false
	}
	delete(m.records, id)
	delete(m.byCallTag, callTagKey(rec.CallID, rec.FromTag))
	return true
}

// List returns a slice of all stored dialog records.
func (m *DlgsModule) List() []*DialogRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*DialogRecord, 0, len(m.records))
	for _, rec := range m.records {
		out = append(out, rec)
	}
	return out
}

// Count returns the number of stored dialog records.
func (m *DlgsModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.records)
}

// CountByState returns the number of stored dialog records whose state
// matches the given value.
func (m *DlgsModule) CountByState(state string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, rec := range m.records {
		if rec.State == state {
			count++
		}
	}
	return count
}

// CleanupExpired removes every record whose UpdatedAt is older than ttl
// from the current time.
//
//	C: dlgs_cleanup_expired()
func (m *DlgsModule) CleanupExpired(ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for id, rec := range m.records {
		if now.Sub(rec.UpdatedAt) > ttl {
			delete(m.records, id)
			delete(m.byCallTag, callTagKey(rec.CallID, rec.FromTag))
		}
	}
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

// newID returns a unique dialog ID.
func (m *DlgsModule) newID() string {
	n := m.counter.Add(1)
	return fmt.Sprintf("dlg-%d-%d", n, time.Now().UnixNano())
}

// callTagKey produces a stable key from a Call-ID and From-tag.
func callTagKey(callID, fromTag string) string {
	return callID + "|" + fromTag
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultDM *DlgsModule
)

// DefaultDlgs returns the process-wide DlgsModule, creating it on first use.
func DefaultDlgs() *DlgsModule {
	defaultMu.RLock()
	m := defaultDM
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultDM == nil {
		defaultDM = New()
	}
	return defaultDM
}

// Init (re)initialises the process-wide DlgsModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultDM = New()
}

// Create is the package-level wrapper around DefaultDlgs().Create.
func Create(callID, fromTag, fromURI, toURI, ruri string) *DialogRecord {
	return DefaultDlgs().Create(callID, fromTag, fromURI, toURI, ruri)
}

// Get is the package-level wrapper around DefaultDlgs().Get.
func Get(callID, fromTag string) *DialogRecord {
	return DefaultDlgs().Get(callID, fromTag)
}

// GetByID is the package-level wrapper around DefaultDlgs().GetByID.
func GetByID(id string) *DialogRecord {
	return DefaultDlgs().GetByID(id)
}

// Update is the package-level wrapper around DefaultDlgs().Update.
func Update(id, state string) bool {
	return DefaultDlgs().Update(id, state)
}

// Delete is the package-level wrapper around DefaultDlgs().Delete.
func Delete(id string) bool {
	return DefaultDlgs().Delete(id)
}

// List is the package-level wrapper around DefaultDlgs().List.
func List() []*DialogRecord {
	return DefaultDlgs().List()
}

// Count is the package-level wrapper around DefaultDlgs().Count.
func Count() int {
	return DefaultDlgs().Count()
}

// CountByState is the package-level wrapper around DefaultDlgs().CountByState.
func CountByState(state string) int {
	return DefaultDlgs().CountByState(state)
}

// CleanupExpired is the package-level wrapper around DefaultDlgs().CleanupExpired.
func CleanupExpired(ttl time.Duration) {
	DefaultDlgs().CleanupExpired(ttl)
}
