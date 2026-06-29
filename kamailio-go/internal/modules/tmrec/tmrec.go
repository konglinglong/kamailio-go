// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * TMRec module - timer records with enable/disable.
 * Port of the kamailio tmrec module (src/modules/tmrec).
 *
 * The tmrec module maintains named timer records identified by an
 * integer id. Each record ticks at a fixed interval; the tick counter
 * only advances while the record is enabled. Records can be enabled,
 * disabled and destroyed individually.
 *
 * It is safe for concurrent use.
 */

package tmrec

import (
	"sync"
	"sync/atomic"
	"time"
)

// tmRec is one timer record.
type tmRec struct {
	id       int
	name     string
	interval time.Duration
	enabled  atomic.Bool
	ticks    atomic.Int64
	ticker   *time.Ticker
	stop     chan struct{}
	done     chan struct{}
}

// TMRecModule manages timer records.
// C: struct module tmrec
type TMRecModule struct {
	mu      sync.Mutex
	nextID  int
	records map[int]*tmRec
}

// New creates a TMRecModule with no records.
func New() *TMRecModule {
	return &TMRecModule{records: make(map[int]*tmRec)}
}

// Create starts a new enabled timer record and returns its id. Returns -1
// for a non-positive interval.
//
//	C: tmrec_create()
func (m *TMRecModule) Create(name string, interval time.Duration) int {
	if m == nil || interval <= 0 {
		return -1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	id := m.nextID
	rec := &tmRec{
		id:       id,
		name:     name,
		interval: interval,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	rec.enabled.Store(true)
	rec.ticker = time.NewTicker(interval)
	if m.records == nil {
		m.records = make(map[int]*tmRec)
	}
	m.records[id] = rec
	go m.run(rec)
	return id
}

// run is the per-record loop.
func (m *TMRecModule) run(r *tmRec) {
	defer close(r.done)
	for {
		select {
		case <-r.ticker.C:
			if r.enabled.Load() {
				r.ticks.Add(1)
			}
		case <-r.stop:
			r.ticker.Stop()
			return
		}
	}
}

// Destroy stops and removes the record with the given id. Returns true when
// a record was destroyed.
//
//	C: tmrec_destroy()
func (m *TMRecModule) Destroy(id int) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	rec, ok := m.records[id]
	if ok {
		delete(m.records, id)
	}
	m.mu.Unlock()
	if !ok {
		return false
	}
	close(rec.stop)
	<-rec.done
	return true
}

// Enable resumes tick counting for the record with the given id.
//
//	C: tmrec_resume()
func (m *TMRecModule) Enable(id int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	rec, ok := m.records[id]
	m.mu.Unlock()
	if !ok {
		return
	}
	rec.enabled.Store(true)
}

// Disable pauses tick counting for the record with the given id.
//
//	C: tmrec_pause()
func (m *TMRecModule) Disable(id int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	rec, ok := m.records[id]
	m.mu.Unlock()
	if !ok {
		return
	}
	rec.enabled.Store(false)
}

// Count returns the number of timer records.
func (m *TMRecModule) Count() int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.records)
}

// tickCount returns the number of times the record has fired while enabled.
// Unexported helper used by tests.
func (m *TMRecModule) tickCount(id int) int64 {
	m.mu.Lock()
	rec, ok := m.records[id]
	m.mu.Unlock()
	if !ok {
		return -1
	}
	return rec.ticks.Load()
}

// isEnabled reports whether the record is enabled.
func (m *TMRecModule) isEnabled(id int) bool {
	m.mu.Lock()
	rec, ok := m.records[id]
	m.mu.Unlock()
	if !ok {
		return false
	}
	return rec.enabled.Load()
}

// stopAll stops every record and waits for each loop to exit.
func (m *TMRecModule) stopAll() {
	m.mu.Lock()
	entries := m.records
	m.records = make(map[int]*tmRec)
	m.mu.Unlock()
	for _, r := range entries {
		close(r.stop)
		<-r.done
	}
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu     sync.RWMutex
	defaultTMRec  *TMRecModule
)

// DefaultTMRec returns the process-wide TMRecModule, creating it on first use.
func DefaultTMRec() *TMRecModule {
	defaultMu.RLock()
	m := defaultTMRec
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultTMRec == nil {
		defaultTMRec = New()
	}
	return defaultTMRec
}

// Init (re)initialises the process-wide TMRecModule to a fresh state,
// stopping every previously running record. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultTMRec != nil {
		defaultTMRec.stopAll()
	}
	defaultTMRec = New()
}
