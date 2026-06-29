// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * PipeLimit module - rule-based rate limiting driven by SIP messages.
 *
 * Port of the kamailio pipelimit module (src/modules/pipelimit). A
 * PipeLimitModule holds a set of named PipeRules; each rule pairs a
 * match expression with a limit and an algorithm. When a SIP message
 * is checked against a rule, the message's Call-ID is used as the
 * rate-limiting key and the rule's algorithm decides whether the
 * message is allowed.
 *
 * Two algorithms are supported, matching the C module's PIPE_ALGO_*
 * enum: "token" (token bucket) and "taildrop" (fixed-window counter).
 * A disabled rule always allows the message through.
 */
package pipelimit

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// PipeRule is a single rate-limiting rule, mirroring Kamailio's pipe
// configuration. MatchExpr is the (informational) match expression;
// Limit is the maximum number of requests per interval; Algorithm
// selects "token" or "taildrop".
type PipeRule struct {
	ID        int
	Name      string
	MatchExpr string
	Limit     int
	Algorithm string
	Enabled   bool
}

// PipeStats holds the per-rule allow/reject counters. The fields are
// safe for concurrent access via atomic operations.
type PipeStats struct {
	Allowed  atomic.Int64
	Rejected atomic.Int64
}

// bucketState is the per-rule-per-key state for the token bucket.
type bucketState struct {
	tokens     float64
	lastRefill time.Time
}

// windowState is the per-rule-per-key state for the taildrop algorithm.
type windowState struct {
	count       int
	windowStart time.Time
}

// ruleState is the runtime state attached to an enabled rule.
type ruleState struct {
	stats    PipeStats
	buckets  map[string]*bucketState
	windows  map[string]*windowState
}

// PipeLimitModule implements the pipelimit module. It is safe for
// concurrent use: the rules map and the per-rule state are guarded by
// mu.
type PipeLimitModule struct {
	mu     sync.RWMutex
	rules  map[int]*PipeRule
	byName map[string]*PipeRule
	state  map[int]*ruleState
	nextID int
}

// NewPipeLimitModule creates a new PipeLimitModule.
func NewPipeLimitModule() *PipeLimitModule {
	return &PipeLimitModule{
		rules:  make(map[int]*PipeRule),
		byName: make(map[string]*PipeRule),
		state:  make(map[int]*ruleState),
	}
}

// AddRule adds a rule and returns its assigned id. If rule.Name is
// already in use the existing rule is replaced (its state is reset).
// A zero or negative limit makes the rule reject everything when
// enabled.
func (m *PipeLimitModule) AddRule(rule *PipeRule) int {
	if rule == nil {
		return -1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	id := m.nextID
	rule.ID = id
	if rule.Algorithm == "" {
		rule.Algorithm = "token"
	}
	// If a rule with the same name exists, remove it first so the name
	// index stays consistent.
	if old, ok := m.byName[rule.Name]; ok {
		delete(m.rules, old.ID)
		delete(m.state, old.ID)
	}
	m.rules[id] = rule
	m.byName[rule.Name] = rule
	m.state[id] = &ruleState{
		buckets: make(map[string]*bucketState),
		windows: make(map[string]*windowState),
	}
	return id
}

// RemoveRule removes the rule with the given id. Returns true if a rule
// was removed.
func (m *PipeLimitModule) RemoveRule(id int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rules[id]
	if !ok {
		return false
	}
	delete(m.rules, id)
	delete(m.byName, r.Name)
	delete(m.state, id)
	return true
}

// Check returns true if the message is allowed under the named rule.
// A missing or disabled rule always allows the message. The message's
// Call-ID (or, failing that, its source IP from the Via) is used as the
// rate-limiting key.
func (m *PipeLimitModule) Check(msg *parser.SIPMsg, ruleName string) bool {
	m.mu.RLock()
	rule, ok := m.byName[ruleName]
	m.mu.RUnlock()
	if !ok {
		// No rule means no limit applied.
		return true
	}
	if !rule.Enabled {
		return true
	}
	key := ruleKey(msg)
	allowed := false
	switch rule.Algorithm {
	case "taildrop":
		allowed = m.checkTaildrop(rule.ID, key, rule.Limit, time.Second)
	default:
		allowed = m.checkTokenBucket(rule.ID, key, rule.Limit, time.Second)
	}
	m.mu.RLock()
	st := m.state[rule.ID]
	m.mu.RUnlock()
	if st == nil {
		return allowed
	}
	if allowed {
		st.stats.Allowed.Add(1)
	} else {
		st.stats.Rejected.Add(1)
	}
	return allowed
}

// checkTokenBucket applies the token bucket algorithm for the given rule.
func (m *PipeLimitModule) checkTokenBucket(ruleID int, key string, limit int, interval time.Duration) bool {
	if limit <= 0 {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.state[ruleID]
	if st == nil {
		return false
	}
	now := time.Now()
	b, ok := st.buckets[key]
	if !ok {
		b = &bucketState{tokens: float64(limit), lastRefill: now}
		st.buckets[key] = b
	}
	elapsed := now.Sub(b.lastRefill)
	if elapsed > 0 {
		refill := float64(elapsed) / float64(interval) * float64(limit)
		b.tokens += refill
		if b.tokens > float64(limit) {
			b.tokens = float64(limit)
		}
		b.lastRefill = now
	}
	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}
	return false
}

// checkTaildrop applies the fixed-window counter for the given rule.
func (m *PipeLimitModule) checkTaildrop(ruleID int, key string, limit int, interval time.Duration) bool {
	if limit <= 0 {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.state[ruleID]
	if st == nil {
		return false
	}
	now := time.Now()
	w, ok := st.windows[key]
	if !ok || now.Sub(w.windowStart) >= interval {
		w = &windowState{count: 0, windowStart: now}
		st.windows[key] = w
	}
	if w.count >= limit {
		return false
	}
	w.count++
	return true
}

// GetStats returns the allow/reject counters for the named rule, or nil
// if the rule does not exist.
func (m *PipeLimitModule) GetStats(ruleName string) *PipeStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rule, ok := m.byName[ruleName]
	if !ok {
		return nil
	}
	st := m.state[rule.ID]
	if st == nil {
		return nil
	}
	return &st.stats
}

// ListRules returns all rules. The order is unspecified.
func (m *PipeLimitModule) ListRules() []*PipeRule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*PipeRule, 0, len(m.rules))
	for _, r := range m.rules {
		out = append(out, r)
	}
	return out
}

// EnableRule enables the rule with the given id. Returns true if the
// rule exists.
func (m *PipeLimitModule) EnableRule(id int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rules[id]
	if !ok {
		return false
	}
	r.Enabled = true
	return true
}

// DisableRule disables the rule with the given id. A disabled rule
// always allows messages through. Returns true if the rule exists.
func (m *PipeLimitModule) DisableRule(id int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rules[id]
	if !ok {
		return false
	}
	r.Enabled = false
	return true
}

// Count returns the number of rules.
func (m *PipeLimitModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.rules)
}

// ruleKey extracts the rate-limiting key from a SIP message. The
// Call-ID is used when present; otherwise the Via host is used as a
// fallback so that messages without a Call-ID are still grouped.
func ruleKey(msg *parser.SIPMsg) string {
	if msg == nil {
		return ""
	}
	if msg.CallID != nil {
		if s := msg.CallID.Body.String(); s != "" {
			return s
		}
	}
	if msg.Via1 != nil {
		if h := msg.Via1.Host.String(); h != "" {
			return h
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu       sync.RWMutex
	defaultPipeLimit *PipeLimitModule
)

// DefaultPipeLimit returns the process-wide PipeLimitModule, creating
// one on first use.
func DefaultPipeLimit() *PipeLimitModule {
	defaultMu.RLock()
	m := defaultPipeLimit
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultPipeLimit == nil {
		defaultPipeLimit = NewPipeLimitModule()
	}
	return defaultPipeLimit
}

// Init (re)initialises the process-wide PipeLimitModule to a fresh
// state, mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultPipeLimit = NewPipeLimitModule()
}
