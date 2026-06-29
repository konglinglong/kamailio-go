// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * pua module - Presence User Agent.
 *
 * Port of the kamailio pua module (src/modules/pua). A PUAModule acts as
 * a presence user agent: it sends PUBLISH requests to publish presence
 * state for a presentity and SUBSCRIBE requests to watch another
 * presentity's state. Each outstanding publication or subscription is
 * tracked as a PUARecord keyed by the presentity URI, with an ETag and
 * an expiry time used by CleanupExpired.
 *
 * The module is safe for concurrent use.
 */
package pua

import (
	"fmt"
	"sync"
	"time"
)

// PUARecord tracks a single outstanding publication or subscription.
// ETag is assigned by the server on PUBLISH and used to refresh or
// remove the publication. Expires is the absolute time at which the
// record lapses.
type PUARecord struct {
	PresURI    string
	WatcherURI string
	ETag       string
	Expires    time.Time
	Body       string
	Event      string
	State      string
}

// PUAModule is the presence user agent. It stores PUARecords keyed by
// the presentity URI and exposes publish/subscribe helpers.
type PUAModule struct {
	mu      sync.RWMutex
	records map[string]*PUARecord
	etagSeq uint64
}

// NewPUAModule creates a PUAModule with empty record storage.
func NewPUAModule() *PUAModule {
	return &PUAModule{records: make(map[string]*PUARecord)}
}

// SendPublish publishes body for presURI with the given expires (in
// seconds). A PUARecord is created or refreshed and an ETag is
// assigned. expires must be positive.
func (m *PUAModule) SendPublish(presURI string, body string, expires int) error {
	if presURI == "" {
		return fmt.Errorf("pua: empty presURI")
	}
	if expires <= 0 {
		return fmt.Errorf("pua: invalid expires %d", expires)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rec := m.records[presURI]
	if rec == nil {
		rec = &PUARecord{PresURI: presURI}
		m.records[presURI] = rec
	}
	rec.Body = body
	rec.Event = "presence"
	rec.State = "published"
	rec.Expires = time.Now().Add(time.Duration(expires) * time.Second)
	if rec.ETag == "" {
		m.etagSeq++
		rec.ETag = fmt.Sprintf("pua-%d-%d", m.etagSeq, time.Now().UnixNano())
	}
	return nil
}

// SendSubscribe subscribes watcherURI to presURI for the given event
// with expires (in seconds). A PUARecord is created or refreshed.
func (m *PUAModule) SendSubscribe(watcherURI, presURI string, event string, expires int) error {
	if watcherURI == "" || presURI == "" {
		return fmt.Errorf("pua: empty watcher or presURI")
	}
	if expires <= 0 {
		return fmt.Errorf("pua: invalid expires %d", expires)
	}
	if event == "" {
		event = "presence"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rec := m.records[presURI]
	if rec == nil {
		rec = &PUARecord{PresURI: presURI}
		m.records[presURI] = rec
	}
	rec.WatcherURI = watcherURI
	rec.Event = event
	rec.State = "active"
	rec.Expires = time.Now().Add(time.Duration(expires) * time.Second)
	if rec.ETag == "" {
		m.etagSeq++
		rec.ETag = fmt.Sprintf("pua-%d-%d", m.etagSeq, time.Now().UnixNano())
	}
	return nil
}

// SendUnsubscribe terminates the subscription from watcherURI to
// presURI, removing the matching record. It is not an error if no
// such record exists.
func (m *PUAModule) SendUnsubscribe(watcherURI, presURI string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[presURI]
	if !ok || rec == nil {
		return nil
	}
	// Remove when the watcher matches, or when the record has no
	// watcher (a pure publication being torn down).
	if rec.WatcherURI == "" || rec.WatcherURI == watcherURI {
		delete(m.records, presURI)
	}
	return nil
}

// UpdateRecord inserts or replaces a PUARecord keyed by its PresURI.
// Returns true when the record was stored.
func (m *PUAModule) UpdateRecord(record *PUARecord) bool {
	if record == nil || record.PresURI == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[record.PresURI] = record
	return true
}

// GetRecord returns the PUARecord for presURI, or nil if none exists.
func (m *PUAModule) GetRecord(presURI string) *PUARecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.records[presURI]
}

// DeleteRecord removes the record for presURI. Returns true if a record
// was removed.
func (m *PUAModule) DeleteRecord(presURI string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.records[presURI]; !ok {
		return false
	}
	delete(m.records, presURI)
	return true
}

// Count returns the number of tracked records.
func (m *PUAModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.records)
}

// List returns a snapshot of all tracked records. The order is
// unspecified.
func (m *PUAModule) List() []*PUARecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*PUARecord, 0, len(m.records))
	for _, r := range m.records {
		out = append(out, r)
	}
	return out
}

// CleanupExpired removes every record whose Expires time is in the
// past.
func (m *PUAModule) CleanupExpired() {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	for uri, rec := range m.records {
		if rec == nil || now.After(rec.Expires) {
			delete(m.records, uri)
		}
	}
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultPU *PUAModule
)

// DefaultPUA returns the process-wide PUAModule, creating one on first
// use.
func DefaultPUA() *PUAModule {
	defaultMu.RLock()
	p := defaultPU
	defaultMu.RUnlock()
	if p != nil {
		return p
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultPU == nil {
		defaultPU = NewPUAModule()
	}
	return defaultPU
}

// Init (re)initialises the process-wide PUAModule to a fresh state,
// mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultPU = NewPUAModule()
}
