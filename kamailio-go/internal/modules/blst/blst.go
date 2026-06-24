// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * BLST module - destination blocklist with TTL expiry.
 * Port of the kamailio blst module (src/modules/blst).
 *
 * blst maintains a thread-safe registry of temporarily-blocked
 * destinations (IP+port) that auto-expire after a configurable TTL. A
 * set of "ignore" methods can be configured so that blocking is bypassed
 * for selected request methods (e.g. REGISTER), mirroring Kamailio's
 * blst_set_ignore.
 *
 * The package is safe for concurrent use.
 */

package blst

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// BLSTEntry is one blocked destination record. ExpiresAt is the zero
// time when the entry never expires (permanent).
type BLSTEntry struct {
	IP        string
	Port      int
	Method    string
	Reason    string
	ExpiresAt time.Time
	HitCount  int64
}

// blstKey is the composite map key for (ip, port).
type blstKey struct {
	ip   string
	port int
}

// BLSTModule is a thread-safe destination blocklist with TTL expiry and
// per-method ignore support.
type BLSTModule struct {
	mu            sync.RWMutex
	entries       map[blstKey]*BLSTEntry
	ignoreMethods map[string]struct{}
}

// New creates an empty BLSTModule.
func New() *BLSTModule {
	return &BLSTModule{
		entries:       make(map[blstKey]*BLSTEntry),
		ignoreMethods: make(map[string]struct{}),
	}
}

// Add inserts or updates a blocklist entry for (ip, port) with the given
// TTL, method and reason. A ttl <= 0 marks the entry as permanent (never
// expires). Re-adding an existing entry refreshes its Method, Reason and
// ExpiresAt while preserving the accumulated HitCount.
func (m *BLSTModule) Add(ip string, port int, method string, reason string, ttl time.Duration) {
	if m == nil {
		return
	}
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := blstKey{ip, port}
	if e, ok := m.entries[key]; ok {
		e.Method = method
		e.Reason = reason
		e.ExpiresAt = exp
		return
	}
	m.entries[key] = &BLSTEntry{
		IP:        ip,
		Port:      port,
		Method:    method,
		Reason:    reason,
		ExpiresAt: exp,
	}
}

// Remove deletes the entry for (ip, port). It returns true if an entry
// was removed.
func (m *BLSTModule) Remove(ip string, port int) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := blstKey{ip, port}
	if _, ok := m.entries[key]; ok {
		delete(m.entries, key)
		return true
	}
	return false
}

// IsBlocked reports whether (ip, port) is currently blocked for the
// given method. Expired entries are reported as not blocked. A method
// registered via SetIgnoreMethod bypasses blocking entirely.
func (m *BLSTModule) IsBlocked(ip string, port int, method string) bool {
	if m == nil {
		return false
	}
	if m.isIgnored(method) {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.entries[blstKey{ip, port}]
	if !ok {
		return false
	}
	return !m.expiredLocked(e)
}

// Hit increments the hit counter for (ip, port) and returns true if the
// destination is currently blocked for the given method. A missing,
// expired or ignored-method entry yields false (expired entries are
// lazily removed).
func (m *BLSTModule) Hit(ip string, port int, method string) bool {
	if m == nil {
		return false
	}
	if m.isIgnored(method) {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := blstKey{ip, port}
	e, ok := m.entries[key]
	if !ok {
		return false
	}
	if m.expiredLocked(e) {
		delete(m.entries, key)
		return false
	}
	e.HitCount++
	return true
}

// Get returns a copy of the entry for (ip, port), or nil if the entry is
// missing or expired. Expired entries are lazily removed.
func (m *BLSTModule) Get(ip string, port int) *BLSTEntry {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := blstKey{ip, port}
	e, ok := m.entries[key]
	if !ok {
		return nil
	}
	if m.expiredLocked(e) {
		delete(m.entries, key)
		return nil
	}
	cp := *e
	return &cp
}

// Count returns the number of entries currently held, including any
// not-yet-swept expired entries.
func (m *BLSTModule) Count() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}

// List returns a snapshot of all non-expired entries as copies, sorted
// by IP then port for deterministic ordering.
func (m *BLSTModule) List() []*BLSTEntry {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := time.Now()
	out := make([]*BLSTEntry, 0, len(m.entries))
	for _, e := range m.entries {
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

// Cleanup removes all expired entries. Permanent entries (ttl <= 0) are
// never removed.
func (m *BLSTModule) Cleanup() {
	if m == nil {
		return
	}
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, e := range m.entries {
		if !e.ExpiresAt.IsZero() && now.After(e.ExpiresAt) {
			delete(m.entries, k)
		}
	}
}

// Clear removes every entry, including permanent ones, and resets the
// ignore-method set.
func (m *BLSTModule) Clear() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = make(map[blstKey]*BLSTEntry)
	m.ignoreMethods = make(map[string]struct{})
}

// SetIgnoreMethod registers a method to be ignored by IsBlocked and Hit
// (i.e. blocking is bypassed for that method). The match is
// case-insensitive.
func (m *BLSTModule) SetIgnoreMethod(method string) {
	if m == nil || method == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ignoreMethods[strings.ToUpper(method)] = struct{}{}
}

// isIgnored reports whether method is in the ignore set (RLock held by
// caller is not required; this acquires its own read lock).
func (m *BLSTModule) isIgnored(method string) bool {
	if method == "" {
		return false
	}
	m.mu.RLock()
	_, ok := m.ignoreMethods[strings.ToUpper(method)]
	m.mu.RUnlock()
	return ok
}

// expiredLocked reports whether e has expired. Callers must hold the
// write (or read) lock; this performs no locking itself.
func (m *BLSTModule) expiredLocked(e *BLSTEntry) bool {
	return !e.ExpiresAt.IsZero() && time.Now().After(e.ExpiresAt)
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *BLSTModule
)

// DefaultBLST returns the process-wide BLSTModule, creating it on first
// use.
func DefaultBLST() *BLSTModule {
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

// Init (re)initialises the process-wide BLSTModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// Add is the package-level wrapper.
func Add(ip string, port int, method string, reason string, ttl time.Duration) {
	DefaultBLST().Add(ip, port, method, reason, ttl)
}

// IsBlocked is the package-level wrapper.
func IsBlocked(ip string, port int, method string) bool {
	return DefaultBLST().IsBlocked(ip, port, method)
}

// Hit is the package-level wrapper.
func Hit(ip string, port int, method string) bool {
	return DefaultBLST().Hit(ip, port, method)
}
