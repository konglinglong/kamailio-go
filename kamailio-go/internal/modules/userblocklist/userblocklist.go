// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * UserBlocklist module - per-user destination blocklist with TTL expiry.
 * Port of the kamailio userblocklist module (src/modules/userblocklist).
 *
 * userblocklist maintains a thread-safe registry of blocked
 * (username, domain) pairs, optionally narrowed by a telephone-number
 * prefix. Entries auto-expire after a configurable TTL. A request's
 * request-URI user part is checked against the blocklist so that calls
 * to blocked destinations are rejected.
 *
 * The package is safe for concurrent use.
 */

package userblocklist

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// BlockEntry is one blocked (username, domain) record, optionally
// narrowed by a telephone-number prefix. ExpiresAt is the zero time
// when the entry never expires (permanent).
type BlockEntry struct {
	Username  string
	Domain    string
	Prefix    string
	Reason    string
	ExpiresAt time.Time
}

// blockKey is the composite map key for (username, domain).
type blockKey struct {
	username string
	domain   string
}

// UserBlocklistModule is a thread-safe per-user blocklist with TTL
// expiry.
type UserBlocklistModule struct {
	mu      sync.RWMutex
	entries map[blockKey][]*BlockEntry
}

// New creates an empty UserBlocklistModule.
func New() *UserBlocklistModule {
	return &UserBlocklistModule{entries: make(map[blockKey][]*BlockEntry)}
}

// Add inserts a block entry for (username, domain) with the given prefix,
// reason and TTL. A ttl <= 0 marks the entry as permanent (never expires).
func (m *UserBlocklistModule) Add(username, domain, prefix, reason string, ttl time.Duration) {
	if m == nil {
		return
	}
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := blockKey{username, domain}
	m.entries[key] = append(m.entries[key], &BlockEntry{
		Username:  username,
		Domain:    domain,
		Prefix:    prefix,
		Reason:    reason,
		ExpiresAt: exp,
	})
}

// Remove deletes the entry for (username, domain) whose prefix matches.
// An empty prefix argument matches entries with an empty prefix. Returns
// true when an entry was removed.
func (m *UserBlocklistModule) Remove(username, domain, prefix string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := blockKey{username, domain}
	list, ok := m.entries[key]
	if !ok {
		return false
	}
	for i, e := range list {
		if e.Prefix == prefix {
			m.entries[key] = append(list[:i], list[i+1:]...)
			if len(m.entries[key]) == 0 {
				delete(m.entries, key)
			}
			return true
		}
	}
	return false
}

// IsBlocked reports whether (username, domain) is currently blocked for
// the given number prefix. An entry matches when its Prefix is empty
// (wildcard) or is a prefix of the supplied number. Expired entries are
// not considered.
func (m *UserBlocklistModule) IsBlocked(username, domain, prefix string) bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	list, ok := m.entries[blockKey{username, domain}]
	if !ok {
		return false
	}
	now := time.Now()
	for _, e := range list {
		if !e.ExpiresAt.IsZero() && now.After(e.ExpiresAt) {
			continue
		}
		if e.Prefix == "" || strings.HasPrefix(prefix, e.Prefix) {
			return true
		}
	}
	return false
}

// Check evaluates the request-URI of msg against the blocklist. It
// returns (blocked, reason): when the R-URI user is blocked the call is
// rejected with the matching entry's reason.
func (m *UserBlocklistModule) Check(msg *parser.SIPMsg) (bool, string) {
	if m == nil || msg == nil {
		return false, "nil message"
	}
	if msg.FirstLine == nil || msg.FirstLine.Req == nil {
		return false, "no request URI"
	}
	uriStr := msg.FirstLine.Req.URI.String()
	if uriStr == "" {
		return false, "empty request URI"
	}
	u, err := parser.ParseURI(uriStr)
	if err != nil || u.Type == parser.ErrorURIT {
		return false, "invalid request URI"
	}
	user := u.User.String()
	host := u.Host.String()
	if user == "" {
		return false, "no user in request URI"
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	list, ok := m.entries[blockKey{user, host}]
	if !ok {
		return false, "not blocked"
	}
	now := time.Now()
	for _, e := range list {
		if !e.ExpiresAt.IsZero() && now.After(e.ExpiresAt) {
			continue
		}
		if e.Prefix == "" || strings.HasPrefix(user, e.Prefix) {
			return true, e.Reason
		}
	}
	return false, "not blocked"
}

// Get returns copies of all entries for (username, domain), including
// expired ones (callers may filter via ExpiresAt). Returns nil when no
// entries exist.
func (m *UserBlocklistModule) Get(username, domain string) []*BlockEntry {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	list, ok := m.entries[blockKey{username, domain}]
	if !ok {
		return nil
	}
	out := make([]*BlockEntry, 0, len(list))
	for _, e := range list {
		cp := *e
		out = append(out, &cp)
	}
	return out
}

// Count returns the total number of entries across all (username,
// domain) keys, including not-yet-swept expired entries.
func (m *UserBlocklistModule) Count() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, list := range m.entries {
		n += len(list)
	}
	return n
}

// List returns a snapshot of all non-expired entries as copies, sorted
// by username, domain then prefix for deterministic ordering.
func (m *UserBlocklistModule) List() []*BlockEntry {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := time.Now()
	out := make([]*BlockEntry, 0)
	for _, list := range m.entries {
		for _, e := range list {
			if !e.ExpiresAt.IsZero() && now.After(e.ExpiresAt) {
				continue
			}
			cp := *e
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Username != out[j].Username {
			return out[i].Username < out[j].Username
		}
		if out[i].Domain != out[j].Domain {
			return out[i].Domain < out[j].Domain
		}
		return out[i].Prefix < out[j].Prefix
	})
	return out
}

// Cleanup removes all expired entries. Permanent entries (ttl <= 0) are
// never removed.
func (m *UserBlocklistModule) Cleanup() {
	if m == nil {
		return
	}
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, list := range m.entries {
		kept := list[:0]
		for _, e := range list {
			if e.ExpiresAt.IsZero() || !now.After(e.ExpiresAt) {
				kept = append(kept, e)
			}
		}
		if len(kept) == 0 {
			delete(m.entries, key)
		} else {
			m.entries[key] = kept
		}
	}
}

// Clear removes every entry, including permanent ones.
func (m *UserBlocklistModule) Clear() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = make(map[blockKey][]*BlockEntry)
}

// LoadFromCSV loads entries from a CSV file. The expected header row is:
//
//	username,domain,prefix,reason,ttl
//
// where ttl is the lifetime in seconds (0 or empty means permanent).
// Missing trailing columns default to empty/0. Existing entries are
// preserved; loaded entries are appended.
func (m *UserBlocklistModule) LoadFromCSV(path string) error {
	if path == "" {
		return errors.New("userblocklist: empty csv path")
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("userblocklist: open csv: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return fmt.Errorf("userblocklist: read csv: %w", err)
	}
	if len(records) == 0 {
		return errors.New("userblocklist: empty csv")
	}

	start := 0
	if first := records[0]; len(first) > 0 && strings.EqualFold(first[0], "username") {
		start = 1
	}
	for i := start; i < len(records); i++ {
		row := records[i]
		username := strings.TrimSpace(cell(row, 0))
		domain := strings.TrimSpace(cell(row, 1))
		prefix := strings.TrimSpace(cell(row, 2))
		reason := strings.TrimSpace(cell(row, 3))
		ttl := parseTTL(cell(row, 4))
		if username == "" {
			continue
		}
		m.Add(username, domain, prefix, reason, ttl)
	}
	return nil
}

// cell returns row[i] or "" when i is out of range.
func cell(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return row[i]
}

// parseTTL parses a TTL in seconds; empty/invalid yields 0 (permanent).
func parseTTL(s string) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *UserBlocklistModule
)

// DefaultUserBlocklist returns the process-wide UserBlocklistModule,
// creating it on first use.
func DefaultUserBlocklist() *UserBlocklistModule {
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

// Init (re)initialises the process-wide UserBlocklistModule to a fresh
// state, mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// Add is the package-level wrapper.
func Add(username, domain, prefix, reason string, ttl time.Duration) {
	DefaultUserBlocklist().Add(username, domain, prefix, reason, ttl)
}

// IsBlocked is the package-level wrapper.
func IsBlocked(username, domain, prefix string) bool {
	return DefaultUserBlocklist().IsBlocked(username, domain, prefix)
}

// Check is the package-level wrapper.
func Check(msg *parser.SIPMsg) (bool, string) {
	return DefaultUserBlocklist().Check(msg)
}
