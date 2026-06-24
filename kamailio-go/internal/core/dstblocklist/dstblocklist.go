// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * dstblocklist - destination blocklist with TTL expiry.
 *
 * This package is the kamailio-go equivalent of Kamailio's core
 * dst_blocklist.c: a thread-safe registry of temporarily-blocked
 * destinations (IP+port) that auto-expires after a configurable TTL.
 *
 * A Blocklist holds the entries; a BlocklistManager groups named
 * blocklists; package-level globals provide a default blocklist with a
 * background cleanup goroutine, mirroring Kamailio's blst_timer.
 *
 * The package is safe for concurrent use.
 */

package dstblocklist

import (
	"sort"
	"sync"
	"time"
)

// DefaultCleanupInterval is the interval used by the background cleanup
// goroutine for the default blocklist when Init/DefaultBlocklist is
// called without an explicit override. It mirrors Kamailio's
// DEFAULT_BLST_TIMER_INTERVAL (60s). Change it before calling Init to
// configure the cleanup cadence.
var DefaultCleanupInterval = 60 * time.Second

// BlockEntry is one blocked destination record. ExpiresAt is the zero
// time when the entry never expires (permanent).
type BlockEntry struct {
	IP        string
	Port      int
	Reason    string
	ExpiresAt time.Time
	HitCount  int64
}

// blockKey is the composite map key for (ip, port).
type blockKey struct {
	ip   string
	port int
}

// Blocklist is a thread-safe set of blocked destinations with optional
// TTL expiry and a background cleanup goroutine.
type Blocklist struct {
	mu              sync.RWMutex
	entries         map[blockKey]*BlockEntry
	cleanupInterval time.Duration
	stopC           chan struct{}
	stopped         chan struct{}
	closeOnce       sync.Once
	started         bool
}

// NewBlocklist creates a new Blocklist. If cleanupInterval > 0, a
// background goroutine periodically calls Cleanup to shed expired
// entries; otherwise expiry is handled lazily by Get/Hit and explicitly
// by Cleanup.
func NewBlocklist(cleanupInterval time.Duration) *Blocklist {
	bl := &Blocklist{
		entries:         make(map[blockKey]*BlockEntry),
		cleanupInterval: cleanupInterval,
		stopC:           make(chan struct{}),
		stopped:         make(chan struct{}),
	}
	if cleanupInterval > 0 {
		bl.started = true
		go bl.cleanupLoop()
	}
	return bl
}

// Close stops the background cleanup goroutine, if any. It is safe to
// call multiple times and on a Blocklist without a goroutine. The
// Blocklist remains usable after Close (only the background sweep stops).
func (bl *Blocklist) Close() {
	if bl == nil {
		return
	}
	bl.closeOnce.Do(func() {
		close(bl.stopC)
	})
	if bl.started {
		<-bl.stopped
	}
}

// Add inserts or updates a blocklist entry for (ip, port) with the given
// TTL. A ttl <= 0 marks the entry as permanent (never expires). Re-adding
// an existing entry refreshes its Reason and ExpiresAt while preserving
// the accumulated HitCount.
func (bl *Blocklist) Add(ip string, port int, reason string, ttl time.Duration) {
	if bl == nil {
		return
	}
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	bl.mu.Lock()
	defer bl.mu.Unlock()
	key := blockKey{ip, port}
	if e, ok := bl.entries[key]; ok {
		e.Reason = reason
		e.ExpiresAt = exp
		return
	}
	bl.entries[key] = &BlockEntry{
		IP:        ip,
		Port:      port,
		Reason:    reason,
		ExpiresAt: exp,
	}
}

// Remove deletes the entry for (ip, port). It returns true if an entry
// was removed and false if no matching entry existed.
func (bl *Blocklist) Remove(ip string, port int) bool {
	if bl == nil {
		return false
	}
	bl.mu.Lock()
	defer bl.mu.Unlock()
	key := blockKey{ip, port}
	if _, ok := bl.entries[key]; ok {
		delete(bl.entries, key)
		return true
	}
	return false
}

// IsBlocked reports whether (ip, port) is currently blocked. Expired
// entries are reported as not blocked.
func (bl *Blocklist) IsBlocked(ip string, port int) bool {
	if bl == nil {
		return false
	}
	bl.mu.RLock()
	defer bl.mu.RUnlock()
	e, ok := bl.entries[blockKey{ip, port}]
	if !ok {
		return false
	}
	if !e.ExpiresAt.IsZero() && time.Now().After(e.ExpiresAt) {
		return false
	}
	return true
}

// Get returns a copy of the entry for (ip, port), or nil if the entry is
// missing or expired. Expired entries are lazily removed.
func (bl *Blocklist) Get(ip string, port int) *BlockEntry {
	if bl == nil {
		return nil
	}
	bl.mu.Lock()
	defer bl.mu.Unlock()
	key := blockKey{ip, port}
	e, ok := bl.entries[key]
	if !ok {
		return nil
	}
	if !e.ExpiresAt.IsZero() && time.Now().After(e.ExpiresAt) {
		delete(bl.entries, key)
		return nil
	}
	cp := *e
	return &cp
}

// Hit increments the hit counter for (ip, port) and returns true if the
// destination is currently blocked. A missing or expired entry yields
// false (and is lazily removed if expired).
func (bl *Blocklist) Hit(ip string, port int) bool {
	if bl == nil {
		return false
	}
	bl.mu.Lock()
	defer bl.mu.Unlock()
	key := blockKey{ip, port}
	e, ok := bl.entries[key]
	if !ok {
		return false
	}
	if !e.ExpiresAt.IsZero() && time.Now().After(e.ExpiresAt) {
		delete(bl.entries, key)
		return false
	}
	e.HitCount++
	return true
}

// Cleanup removes all expired entries. Permanent entries (ttl <= 0) are
// never removed.
func (bl *Blocklist) Cleanup() {
	if bl == nil {
		return
	}
	now := time.Now()
	bl.mu.Lock()
	defer bl.mu.Unlock()
	for k, e := range bl.entries {
		if !e.ExpiresAt.IsZero() && now.After(e.ExpiresAt) {
			delete(bl.entries, k)
		}
	}
}

// Count returns the number of entries currently held, including any
// not-yet-swept expired entries.
func (bl *Blocklist) Count() int {
	if bl == nil {
		return 0
	}
	bl.mu.RLock()
	defer bl.mu.RUnlock()
	return len(bl.entries)
}

// List returns a snapshot of all non-expired entries as copies, sorted by
// IP then port for deterministic ordering.
func (bl *Blocklist) List() []*BlockEntry {
	if bl == nil {
		return nil
	}
	bl.mu.RLock()
	defer bl.mu.RUnlock()
	now := time.Now()
	out := make([]*BlockEntry, 0, len(bl.entries))
	for _, e := range bl.entries {
		if !e.ExpiresAt.IsZero() && now.After(e.ExpiresAt) {
			continue
		}
		cp := *e
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IP != out[j].IP {
			return out[i].IP < out[j].IP
		}
		return out[i].Port < out[j].Port
	})
	return out
}

// Clear removes every entry, including permanent ones.
func (bl *Blocklist) Clear() {
	if bl == nil {
		return
	}
	bl.mu.Lock()
	defer bl.mu.Unlock()
	bl.entries = make(map[blockKey]*BlockEntry)
}

// cleanupLoop is the background janitor; it exits when stopC is closed.
func (bl *Blocklist) cleanupLoop() {
	defer close(bl.stopped)
	ticker := time.NewTicker(bl.cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-bl.stopC:
			return
		case <-ticker.C:
			bl.Cleanup()
		}
	}
}

// BlocklistManager owns named Blocklists, typically a single instance per
// process. Each created Blocklist shares the manager's cleanup interval.
type BlocklistManager struct {
	mu              sync.RWMutex
	blocklists      map[string]*Blocklist
	cleanupInterval time.Duration
}

// NewBlocklistManager returns a new empty manager. Blocklists created by
// it use cleanupInterval for their background sweep (0 disables it).
func NewBlocklistManager(cleanupInterval time.Duration) *BlocklistManager {
	return &BlocklistManager{
		blocklists:      make(map[string]*Blocklist),
		cleanupInterval: cleanupInterval,
	}
}

// GetBlocklist returns the existing blocklist by name, or nil if missing.
func (m *BlocklistManager) GetBlocklist(name string) *Blocklist {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.blocklists[name]
}

// CreateBlocklist returns the existing blocklist by name, creating a new
// one if it does not yet exist.
func (m *BlocklistManager) CreateBlocklist(name string) *Blocklist {
	if m == nil {
		return NewBlocklist(0)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if bl, ok := m.blocklists[name]; ok {
		return bl
	}
	bl := NewBlocklist(m.cleanupInterval)
	m.blocklists[name] = bl
	return bl
}

// RemoveBlocklist deletes the named blocklist and stops its background
// goroutine, if any. It is a no-op if the name is unknown.
func (m *BlocklistManager) RemoveBlocklist(name string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	bl, ok := m.blocklists[name]
	if ok {
		delete(m.blocklists, name)
	}
	m.mu.Unlock()
	if bl != nil {
		bl.Close()
	}
}

// Names returns a sorted snapshot of the current blocklist names.
func (m *BlocklistManager) Names() []string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.blocklists))
	for n := range m.blocklists {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Close stops the background goroutines of all managed blocklists and
// clears the registry.
func (m *BlocklistManager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	lists := m.blocklists
	m.blocklists = make(map[string]*Blocklist)
	m.mu.Unlock()
	for _, bl := range lists {
		bl.Close()
	}
}

// ---------------------------------------------------------------
// package-level default blocklist

var (
	globalMu sync.Mutex
	defMgr   *BlocklistManager
	defBL    *Blocklist
	defReady bool
)

// Init initialises the default blocklist and manager and starts the
// background cleanup goroutine using DefaultCleanupInterval. It is
// idempotent; subsequent calls are no-ops.
func Init() {
	globalMu.Lock()
	defer globalMu.Unlock()
	if defReady {
		return
	}
	defMgr = NewBlocklistManager(DefaultCleanupInterval)
	defBL = defMgr.CreateBlocklist("default")
	defReady = true
}

// Shutdown stops the default blocklist's background goroutine and resets
// the package-level state. It is safe to call Init again afterwards.
func Shutdown() {
	globalMu.Lock()
	mgr := defMgr
	defMgr = nil
	defBL = nil
	defReady = false
	globalMu.Unlock()
	if mgr != nil {
		mgr.Close()
	}
}

// DefaultBlocklist returns the default blocklist, initialising it on
// first use. It is safe for concurrent use.
func DefaultBlocklist() *Blocklist {
	globalMu.Lock()
	defer globalMu.Unlock()
	if !defReady {
		defMgr = NewBlocklistManager(DefaultCleanupInterval)
		defBL = defMgr.CreateBlocklist("default")
		defReady = true
	}
	return defBL
}

// Add adds an entry to the default blocklist. If the default blocklist is
// not initialised it is created on demand.
func Add(ip string, port int, reason string, ttl time.Duration) {
	bl := DefaultBlocklist()
	bl.Add(ip, port, reason, ttl)
}

// IsBlocked reports whether (ip, port) is blocked in the default
// blocklist.
func IsBlocked(ip string, port int) bool {
	bl := DefaultBlocklist()
	return bl.IsBlocked(ip, port)
}

// Hit increments the hit counter for (ip, port) in the default blocklist
// and returns true if the destination is currently blocked.
func Hit(ip string, port int) bool {
	bl := DefaultBlocklist()
	return bl.Hit(ip, port)
}

// Cleanup removes expired entries from the default blocklist.
func Cleanup() {
	bl := DefaultBlocklist()
	bl.Cleanup()
}
