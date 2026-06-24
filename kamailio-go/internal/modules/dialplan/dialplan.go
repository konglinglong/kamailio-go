// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Dialplan module - number translation via match/replace rules.
 *
 * Port of the kamailio dialplan module (src/modules/dialplan). Rules
 * associate a regular expression (MatchExpr) with a replacement
 * template (ReplaceExpr); Translate applies the first matching enabled
 * rule to an input string. The replacement template uses Go regexp
 * syntax ($1, $2, ${name} for capture groups).
 *
 * The package is safe for concurrent use.
 */
package dialplan

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
)

// DialplanRule is one translation rule (Kamailio dialplan rule). MatchExpr
// is a Go regexp pattern; ReplaceExpr is a regexp replacement template
// using $1, $2, ${name} for capture groups. Enabled rules are considered
// by Translate/Match; disabled rules are skipped.
type DialplanRule struct {
	ID          int
	MatchExpr   string
	ReplaceExpr string
	Description string
	Enabled     bool
}

// DialplanModule implements the dialplan module. It is safe for
// concurrent use: all state is guarded by mu.
type DialplanModule struct {
	mu       sync.RWMutex
	rules    map[int]*DialplanRule
	order    []int // rule IDs in insertion order
	compiled map[int]*regexp.Regexp
	nextID   int
}

// NewDialplanModule creates a new DialplanModule.
func NewDialplanModule() *DialplanModule {
	return &DialplanModule{
		rules:    make(map[int]*DialplanRule),
		compiled: make(map[int]*regexp.Regexp),
	}
}

// AddRule registers rule and returns the assigned ID. The MatchExpr is
// compiled once when the rule is added; an empty MatchExpr defaults to
// "^(.*)$" (matches the whole input). A rule whose MatchExpr fails to
// compile is stored but skipped by Translate/Match. Returns -1 if rule
// is nil.
func (m *DialplanModule) AddRule(rule *DialplanRule) int {
	if rule == nil {
		return -1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	id := m.nextID
	rule.ID = id
	if strings.TrimSpace(rule.MatchExpr) == "" {
		rule.MatchExpr = "^(.*)$"
	}
	if re, err := regexp.Compile(rule.MatchExpr); err == nil {
		m.compiled[id] = re
	}
	m.rules[id] = rule
	m.order = append(m.order, id)
	return id
}

// RemoveRule deletes the rule with the given ID. Returns true when a rule
// was removed.
func (m *DialplanModule) RemoveRule(id int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rules[id]; !ok {
		return false
	}
	delete(m.rules, id)
	delete(m.compiled, id)
	for i, rid := range m.order {
		if rid == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	return true
}

// Translate applies the first matching enabled rule to input and returns
// the result. Rules are tried in insertion order. Returns the input
// unchanged and an error when no enabled rule matches.
func (m *DialplanModule) Translate(input string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, id := range m.order {
		r := m.rules[id]
		if !r.Enabled {
			continue
		}
		re, ok := m.compiled[id]
		if !ok {
			continue
		}
		if re.MatchString(input) {
			return re.ReplaceAllString(input, r.ReplaceExpr), nil
		}
	}
	return input, errors.New("dialplan: no matching rule")
}

// TranslateWithRule applies the rule identified by ruleID to input
// regardless of insertion order. Returns an error if the rule does not
// exist or is disabled. When the rule's regexp does not match, the input
// is returned unchanged.
func (m *DialplanModule) TranslateWithRule(input string, ruleID int) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.rules[ruleID]
	if !ok {
		return input, errors.New("dialplan: rule not found")
	}
	if !r.Enabled {
		return input, errors.New("dialplan: rule disabled")
	}
	re, ok := m.compiled[ruleID]
	if !ok {
		return input, errors.New("dialplan: rule regexp invalid")
	}
	return re.ReplaceAllString(input, r.ReplaceExpr), nil
}

// Match returns a copy of the first enabled rule whose regexp matches
// input, or nil if none match. Rules are tried in insertion order.
func (m *DialplanModule) Match(input string) *DialplanRule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, id := range m.order {
		r := m.rules[id]
		if !r.Enabled {
			continue
		}
		re, ok := m.compiled[id]
		if !ok {
			continue
		}
		if re.MatchString(input) {
			cp := *r
			return &cp
		}
	}
	return nil
}

// ListRules returns copies of all registered rules in insertion order.
func (m *DialplanModule) ListRules() []*DialplanRule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*DialplanRule, 0, len(m.order))
	for _, id := range m.order {
		cp := *m.rules[id]
		out = append(out, &cp)
	}
	return out
}

// Count returns the number of registered rules.
func (m *DialplanModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.rules)
}

// EnableRule enables the rule with the given ID. Returns true when the
// rule existed and was enabled (or already enabled).
func (m *DialplanModule) EnableRule(id int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rules[id]
	if !ok {
		return false
	}
	r.Enabled = true
	return true
}

// DisableRule disables the rule with the given ID. Returns true when the
// rule existed and was disabled (or already disabled).
func (m *DialplanModule) DisableRule(id int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rules[id]
	if !ok {
		return false
	}
	r.Enabled = false
	return true
}

// LoadFromCSV loads rules from a CSV file. The expected header row is:
//
//	match_expr,replace_expr,description,enabled
//
// where enabled is "1"/"true" for enabled and anything else for
// disabled; a missing enabled column defaults to enabled. Existing
// rules are preserved; loaded rules are appended.
func (m *DialplanModule) LoadFromCSV(path string) error {
	if path == "" {
		return errors.New("dialplan: empty csv path")
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("dialplan: open csv: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return fmt.Errorf("dialplan: read csv: %w", err)
	}
	if len(records) == 0 {
		return errors.New("dialplan: empty csv")
	}

	start := 0
	if first := records[0]; len(first) > 0 && strings.EqualFold(first[0], "match_expr") {
		start = 1
	}
	for i := start; i < len(records); i++ {
		row := records[i]
		matchExpr := strings.TrimSpace(cell(row, 0))
		replaceExpr := strings.TrimSpace(cell(row, 1))
		description := strings.TrimSpace(cell(row, 2))
		enabled := parseBool(cell(row, 4), true) // col 3 reserved/optional
		if matchExpr == "" {
			continue
		}
		m.AddRule(&DialplanRule{
			MatchExpr:   matchExpr,
			ReplaceExpr:  replaceExpr,
			Description: description,
			Enabled:     enabled,
		})
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

// parseBool parses a boolean from "1"/"true"/"yes" (case-insensitive);
// missing/empty yields dflt.
func parseBool(s string, dflt bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes":
		return true
	case "0", "false", "no":
		return false
	default:
		return dflt
	}
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultDP *DialplanModule
)

// DefaultDialplan returns the process-wide DialplanModule, creating one
// on first use.
func DefaultDialplan() *DialplanModule {
	defaultMu.RLock()
	d := defaultDP
	defaultMu.RUnlock()
	if d != nil {
		return d
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultDP == nil {
		defaultDP = NewDialplanModule()
	}
	return defaultDP
}

// Init (re)initialises the process-wide DialplanModule to a fresh state,
// mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultDP = NewDialplanModule()
}
