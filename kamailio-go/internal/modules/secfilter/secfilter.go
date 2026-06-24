// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SecFilter module - SIP security filtering (allow/deny rules).
 * Port of the kamailio secfilter module (src/modules/secfilter).
 *
 * secfilter applies allow/deny rules against SIP messages: a rule may
 * target the request-URI or a named header, matching by exact value,
 * prefix or suffix. The first matching enabled rule decides the
 * outcome; when no rule matches the message is allowed.
 *
 * Rules can be managed programmatically or loaded from a CSV file.
 * The module is safe for concurrent use.
 */

package secfilter

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// RuleType selects whether a rule permits or blocks a match.
type RuleType string

const (
	// RuleAllow permits traffic that matches the rule.
	RuleAllow RuleType = "allow"
	// RuleDeny blocks traffic that matches the rule.
	RuleDeny RuleType = "deny"
)

// FilterRule is one allow/deny rule.
type FilterRule struct {
	ID        int
	Type      RuleType
	Pattern   string
	Header    string
	Direction string
	Enabled   bool
}

// SecFilterModule evaluates allow/deny rules against SIP messages.
type SecFilterModule struct {
	mu     sync.RWMutex
	rules  []*FilterRule
	nextID int
}

// New creates an empty SecFilterModule.
func New() *SecFilterModule {
	return &SecFilterModule{}
}

// AddRule inserts a rule and returns the assigned ID, or -1 when the
// rule is nil. The rule is enabled by default unless it is explicitly
// disabled. The supplied ID (if non-zero) is preserved.
func (m *SecFilterModule) AddRule(rule *FilterRule) int {
	if rule == nil {
		return -1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	r := *rule
	if r.ID == 0 {
		r.ID = m.nextID
	} else if r.ID > m.nextID {
		m.nextID = r.ID
	}
	if r.Type == "" {
		r.Type = RuleDeny
	}
	r.Enabled = true
	cp := r
	m.rules = append(m.rules, &cp)
	return cp.ID
}

// RemoveRule deletes the rule with the given ID. It returns true when a
// rule was removed.
func (m *SecFilterModule) RemoveRule(id int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.rules {
		if r.ID == id {
			m.rules = append(m.rules[:i], m.rules[i+1:]...)
			return true
		}
	}
	return false
}

// EnableRule enables the rule with the given ID. Returns true when the
// rule exists (and was enabled or already enabled).
func (m *SecFilterModule) EnableRule(id int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rules {
		if r.ID == id {
			r.Enabled = true
			return true
		}
	}
	return false
}

// DisableRule disables the rule with the given ID. Returns true when the
// rule exists.
func (m *SecFilterModule) DisableRule(id int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rules {
		if r.ID == id {
			r.Enabled = false
			return true
		}
	}
	return false
}

// ListRules returns a snapshot copy of all rules.
func (m *SecFilterModule) ListRules() []*FilterRule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*FilterRule, 0, len(m.rules))
	for _, r := range m.rules {
		cp := *r
		out = append(out, &cp)
	}
	return out
}

// Count returns the number of rules.
func (m *SecFilterModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.rules)
}

// Check evaluates msg against all enabled rules. It returns (allowed,
// reason): the first matching rule decides; when no rule matches the
// message is allowed with reason "no matching rule".
//
// A rule with an empty Header targets the request-URI; otherwise it
// targets the named header's body.
func (m *SecFilterModule) Check(msg *parser.SIPMsg) (bool, string) {
	if msg == nil {
		return false, "nil message"
	}
	ruri := ""
	if msg.FirstLine != nil && msg.FirstLine.Req != nil {
		ruri = msg.FirstLine.Req.URI.String()
	}
	return m.checkValue(msg, ruri, "")
}

// CheckURI evaluates a URI string against rules that target the
// request-URI (Header == ""). Returns (allowed, reason).
func (m *SecFilterModule) CheckURI(uri string) (bool, string) {
	return m.checkValue(nil, uri, "")
}

// CheckHeader evaluates a named header of msg against rules targeting
// that header. Returns (allowed, reason). When the header is absent the
// message is allowed.
func (m *SecFilterModule) CheckHeader(msg *parser.SIPMsg, header string) (bool, string) {
	if msg == nil {
		return false, "nil message"
	}
	if header == "" {
		return true, "no header specified"
	}
	hdr := msg.GetHeaderByType(parser.HdrOther)
	// Fall back to a case-insensitive scan by name.
	if hdr == nil {
		for _, h := range msg.Headers {
			if strings.EqualFold(h.Name.String(), header) {
				hdr = h
				break
			}
		}
	}
	if hdr == nil {
		return true, "header absent"
	}
	return m.checkValue(nil, hdr.Body.String(), header)
}

// checkValue walks the rules in order, applying those whose Header
// matches headerName (empty Header matches any). The first matching
// enabled rule decides. msg is used only to resolve the request-URI when
// value is empty and headerName is empty.
func (m *SecFilterModule) checkValue(msg *parser.SIPMsg, value, headerName string) (bool, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, r := range m.rules {
		if !r.Enabled {
			continue
		}
		if !ruleAppliesTo(r, headerName) {
			continue
		}
		candidate := value
		if headerName == "" && candidate == "" && msg != nil {
			if msg.FirstLine != nil && msg.FirstLine.Req != nil {
				candidate = msg.FirstLine.Req.URI.String()
			}
		}
		if !matchPattern(r.Pattern, candidate) {
			continue
		}
		reason := fmt.Sprintf("%s rule %d matched %q", r.Type, r.ID, r.Pattern)
		if r.Type == RuleDeny {
			return false, reason
		}
		return true, reason
	}
	return true, "no matching rule"
}

// ruleAppliesTo reports whether r should be considered for headerName.
// A rule with an empty Header applies to any target; otherwise it must
// match headerName (case-insensitive).
func ruleAppliesTo(r *FilterRule, headerName string) bool {
	if r.Header == "" {
		return true
	}
	if headerName == "" {
		// A header-specific rule does not apply to the R-URI check.
		return false
	}
	return strings.EqualFold(r.Header, headerName)
}

// matchPattern reports whether value matches pattern. Matching is
// case-insensitive and substring-oriented:
//   - an empty or "*" pattern matches anything;
//   - a pattern ending with "*" matches when value contains the prefix
//     part (e.g. "evil*" matches "sip:evil@x");
//   - a pattern starting with "*" matches when value ends with the suffix
//     part (e.g. "*@x.com" matches "sip:u@x.com");
//   - a pattern with both leading and trailing "*" matches when value
//     contains the middle part;
//   - otherwise value must contain the pattern verbatim.
func matchPattern(pattern, value string) bool {
	pattern = strings.TrimSpace(pattern)
	value = strings.TrimSpace(value)
	if pattern == "" || pattern == "*" {
		return true
	}
	pl := strings.ToLower(pattern)
	vl := strings.ToLower(value)
	hasLead := strings.HasPrefix(pl, "*")
	hasTrail := strings.HasSuffix(pl, "*")
	switch {
	case hasLead && hasTrail:
		return strings.Contains(vl, strings.Trim(pl, "*"))
	case hasTrail:
		return strings.Contains(vl, strings.TrimSuffix(pl, "*"))
	case hasLead:
		return strings.HasSuffix(vl, strings.TrimPrefix(pl, "*"))
	default:
		return strings.Contains(vl, pl)
	}
}

// LoadFromCSV loads rules from a CSV file. The expected header row is:
//
//	type,pattern,header,direction,enabled
//
// Missing trailing columns default to empty/false. The "enabled" column
// is true for "true"/"1"/"yes" (case-insensitive). Existing rules are
// preserved; loaded rules are appended.
func (m *SecFilterModule) LoadFromCSV(path string) error {
	if path == "" {
		return errors.New("secfilter: empty csv path")
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("secfilter: open csv: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1 // allow variable column counts
	records, err := r.ReadAll()
	if err != nil {
		return fmt.Errorf("secfilter: read csv: %w", err)
	}
	if len(records) == 0 {
		return errors.New("secfilter: empty csv")
	}

	start := 0
	// Skip a header row when the first column looks like a label.
	if first := records[0]; len(first) > 0 && strings.EqualFold(first[0], "type") {
		start = 1
	}
	for i := start; i < len(records); i++ {
		row := records[i]
		enabled := parseBool(cell(row, 4))
		rule := &FilterRule{
			Type:      RuleType(strings.ToLower(strings.TrimSpace(cell(row, 0)))),
			Pattern:   strings.TrimSpace(cell(row, 1)),
			Header:    strings.TrimSpace(cell(row, 2)),
			Direction: strings.TrimSpace(cell(row, 3)),
			Enabled:   enabled,
		}
		if rule.Type == "" {
			rule.Type = RuleDeny
		}
		id := m.AddRule(rule)
		// AddRule activates new rules by default; honour an explicit
		// "disabled" flag from the CSV by disabling afterwards.
		if !enabled {
			m.DisableRule(id)
		}
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

// parseBool parses a permissive boolean.
func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *SecFilterModule
)

// DefaultSecFilter returns the process-wide SecFilterModule, creating it
// on first use.
func DefaultSecFilter() *SecFilterModule {
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

// Init (re)initialises the process-wide SecFilterModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// AddRule is the package-level wrapper.
func AddRule(rule *FilterRule) int { return DefaultSecFilter().AddRule(rule) }

// RemoveRule is the package-level wrapper.
func RemoveRule(id int) bool { return DefaultSecFilter().RemoveRule(id) }

// Check is the package-level wrapper.
func Check(msg *parser.SIPMsg) (bool, string) { return DefaultSecFilter().Check(msg) }

// CheckURI is the package-level wrapper.
func CheckURI(uri string) (bool, string) { return DefaultSecFilter().CheckURI(uri) }
