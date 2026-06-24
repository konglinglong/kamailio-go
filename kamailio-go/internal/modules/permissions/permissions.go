// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * permissions - source/destination allow-deny rules.
 *
 * Holds a list of (srcIP, dstIP, allow) rules and answers whether a given
 * source IP or URI is permitted. Rules may be loaded from a CSV file with
 * columns src,dst,allow. Mirrors the kamailio permissions module.
 */

package permissions

import (
	"bufio"
	"errors"
	"os"
	"strings"
	"sync"
)

// Rule is one permission entry.
type Rule struct {
	SrcIP string
	DstIP string
	Allow bool
}

// PermissionsModule evaluates allow/deny rules.
type PermissionsModule struct {
	mu    sync.RWMutex
	rules []Rule
}

// New returns a new PermissionsModule.
func New() *PermissionsModule {
	return &PermissionsModule{}
}

// CheckURI reports whether any allow rule matches the URI's host (or no
// deny rule matches). With no rules loaded, everything is allowed.
func (m *PermissionsModule) CheckURI(uri string) bool {
	if m == nil {
		return true
	}
	host := extractHost(uri)
	if host == "" {
		return true
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, r := range m.rules {
		if r.SrcIP == host || r.DstIP == host {
			return r.Allow
		}
	}
	return true
}

// CheckSource reports whether a source IP is allowed. With no rules, true.
func (m *PermissionsModule) CheckSource(ip string) bool {
	if m == nil {
		return true
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, r := range m.rules {
		if r.SrcIP == ip {
			return r.Allow
		}
	}
	return true
}

// AddRule appends a (srcIP, dstIP, allow) rule.
func (m *PermissionsModule) AddRule(srcIP, dstIP string, allow bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules = append(m.rules, Rule{SrcIP: srcIP, DstIP: dstIP, Allow: allow})
}

// RemoveRule removes the first rule matching (srcIP, dstIP). Returns true
// if a rule was removed.
func (m *PermissionsModule) RemoveRule(srcIP, dstIP string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.rules {
		if r.SrcIP == srcIP && r.DstIP == dstIP {
			m.rules = append(m.rules[:i], m.rules[i+1:]...)
			return true
		}
	}
	return false
}

// LoadFromCSV loads rules from a CSV file with columns src,dst,allow where
// allow is "1"/"true" to permit and anything else to deny.
func (m *PermissionsModule) LoadFromCSV(path string) error {
	if m == nil {
		return errors.New("permissions: nil module")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var loaded []Rule
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}
		allow := strings.TrimSpace(parts[2]) == "1" ||
			strings.EqualFold(strings.TrimSpace(parts[2]), "true")
		loaded = append(loaded, Rule{
			SrcIP: strings.TrimSpace(parts[0]),
			DstIP: strings.TrimSpace(parts[1]),
			Allow: allow,
		})
	}
	if err := sc.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	m.rules = append(m.rules, loaded...)
	m.mu.Unlock()
	return nil
}

// RuleCount returns the number of rules currently loaded.
func (m *PermissionsModule) RuleCount() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.rules)
}

// extractHost returns the host portion of a SIP URI, or the input if it is
// not a URI.
func extractHost(uri string) string {
	s := uri
	if i := strings.Index(s, "sip:"); i == 0 {
		s = s[4:]
	}
	if i := strings.Index(s, "sips:"); i == 0 {
		s = s[5:]
	}
	if i := strings.IndexAny(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.IndexAny(s, ";:>?"); i >= 0 {
		s = s[:i]
	}
	return s
}
