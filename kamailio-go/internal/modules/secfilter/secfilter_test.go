// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - SecFilter module tests.
 */

package secfilter

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

var testInvite = []byte("INVITE sip:evil@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.example.com;branch=z9hG4bK776asdhds\r\n" +
	"Max-Forwards: 70\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1928301774\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: a84b4c76e66710@pc33.example.com\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"Contact: <sip:alice@pc33.example.com>\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

func mustParse(t *testing.T, b []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(b)
	if err != nil {
		t.Fatalf("ParseMsg failed: %v", err)
	}
	return msg
}

// TestAddRemoveRule verifies rule insertion, ID assignment and removal.
func TestAddRemoveRule(t *testing.T) {
	m := New()
	id1 := m.AddRule(&FilterRule{Type: RuleDeny, Pattern: "evil.com"})
	id2 := m.AddRule(&FilterRule{Type: RuleAllow, Pattern: "good.com"})
	if id1 != 1 || id2 != 2 {
		t.Fatalf("expected ids 1,2 got %d,%d", id1, id2)
	}
	if m.Count() != 2 {
		t.Fatalf("expected count 2, got %d", m.Count())
	}
	if m.AddRule(nil) != -1 {
		t.Fatal("expected -1 for nil rule")
	}
	if !m.RemoveRule(id1) {
		t.Fatal("expected RemoveRule true")
	}
	if m.Count() != 1 {
		t.Fatalf("expected count 1 after remove, got %d", m.Count())
	}
	if m.RemoveRule(999) {
		t.Fatal("expected RemoveRule false for unknown id")
	}
}

// TestCheckDenyAllow verifies that deny blocks and allow permits, with
// the first match winning.
func TestCheckDenyAllow(t *testing.T) {
	m := New()
	m.AddRule(&FilterRule{Type: RuleDeny, Pattern: "evil*"})
	m.AddRule(&FilterRule{Type: RuleAllow, Pattern: "*"})

	msg := mustParse(t, testInvite) // R-URI sip:evil@example.com
	allowed, reason := m.Check(msg)
	if allowed {
		t.Fatalf("expected deny, got allowed: %s", reason)
	}
	if !strings.Contains(reason, "deny") {
		t.Fatalf("unexpected reason: %s", reason)
	}

	// An allow rule placed first should win.
	m2 := New()
	m2.AddRule(&FilterRule{Type: RuleAllow, Pattern: "sip:evil*"})
	m2.AddRule(&FilterRule{Type: RuleDeny, Pattern: "evil*"})
	allowed, _ = m2.Check(msg)
	if !allowed {
		t.Fatal("expected allow to win when first")
	}
}

// TestCheckDefaultAllow verifies that an unmatched message is allowed.
func TestCheckDefaultAllow(t *testing.T) {
	m := New()
	m.AddRule(&FilterRule{Type: RuleDeny, Pattern: "nomatch"})
	msg := mustParse(t, testInvite)
	allowed, reason := m.Check(msg)
	if !allowed {
		t.Fatalf("expected default allow, got denied: %s", reason)
	}
	if !strings.Contains(reason, "no matching rule") {
		t.Fatalf("unexpected reason: %s", reason)
	}
}

// TestCheckURI verifies URI matching with prefix/suffix patterns.
func TestCheckURI(t *testing.T) {
	m := New()
	m.AddRule(&FilterRule{Type: RuleDeny, Pattern: "sip:bad*"})
	m.AddRule(&FilterRule{Type: RuleDeny, Pattern: "*@spammer.com"})

	if allowed, _ := m.CheckURI("sip:baduser@example.com"); allowed {
		t.Fatal("expected deny for prefix match")
	}
	if allowed, _ := m.CheckURI("sip:user@spammer.com"); allowed {
		t.Fatal("expected deny for suffix match")
	}
	if allowed, _ := m.CheckURI("sip:good@example.com"); !allowed {
		t.Fatal("expected allow for unmatched URI")
	}
}

// TestCheckHeader verifies header-targeted rules.
func TestCheckHeader(t *testing.T) {
	m := New()
	m.AddRule(&FilterRule{Type: RuleDeny, Pattern: "spammer", Header: "From"})

	msg := mustParse(t, testInvite)
	// The From header is alice@example.com -> allowed.
	allowed, _ := m.CheckHeader(msg, "From")
	if !allowed {
		t.Fatal("expected allow for non-matching From")
	}

	// Replace the From header with one that matches the deny pattern.
	msg2 := mustParse(t, testInvite)
	msg2.RemoveHeadersByType(parser.HdrFrom)
	msg2.AddHeader("From", "Spammer <sip:spammer@evil.com>")
	allowed, reason := m.CheckHeader(msg2, "From")
	if allowed {
		t.Fatalf("expected deny for matching From, reason: %s", reason)
	}
	// Absent header -> allowed.
	allowed, _ = m.CheckHeader(msg, "X-Nonexistent")
	if !allowed {
		t.Fatal("expected allow for absent header")
	}
}

// TestEnableDisableRule verifies rule enable/disable toggles.
func TestEnableDisableRule(t *testing.T) {
	m := New()
	id := m.AddRule(&FilterRule{Type: RuleDeny, Pattern: "evil*"})
	if !m.DisableRule(id) {
		t.Fatal("expected DisableRule true")
	}
	msg := mustParse(t, testInvite)
	allowed, _ := m.Check(msg)
	if !allowed {
		t.Fatal("expected allow when deny rule disabled")
	}
	if !m.EnableRule(id) {
		t.Fatal("expected EnableRule true")
	}
	allowed, _ = m.Check(msg)
	if allowed {
		t.Fatal("expected deny when rule re-enabled")
	}
	if m.EnableRule(999) {
		t.Fatal("expected EnableRule false for unknown id")
	}
	if m.DisableRule(999) {
		t.Fatal("expected DisableRule false for unknown id")
	}
}

// TestListRules verifies ListRules returns copies.
func TestListRules(t *testing.T) {
	m := New()
	m.AddRule(&FilterRule{Type: RuleDeny, Pattern: "a"})
	m.AddRule(&FilterRule{Type: RuleAllow, Pattern: "b"})
	rules := m.ListRules()
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	// Mutating a returned rule must not affect the module.
	rules[0].Pattern = "mutated"
	got := m.ListRules()
	if got[0].Pattern != "a" {
		t.Fatalf("expected isolation from ListRules copy, got %q", got[0].Pattern)
	}
}

// TestLoadFromCSV verifies CSV loading.
func TestLoadFromCSV(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rules.csv")
	content := "type,pattern,header,direction,enabled\n" +
		"deny,evil.com,From,inbound,true\n" +
		"allow,good.com,,any,true\n" +
		"deny,bad*,To,outbound,false\n"
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m := New()
	if err := m.LoadFromCSV(p); err != nil {
		t.Fatalf("LoadFromCSV: %v", err)
	}
	if m.Count() != 3 {
		t.Fatalf("expected 3 rules, got %d", m.Count())
	}
	rules := m.ListRules()
	if rules[0].Type != RuleDeny || rules[0].Pattern != "evil.com" || rules[0].Header != "From" {
		t.Fatalf("unexpected rule 0: %+v", rules[0])
	}
	if rules[1].Type != RuleAllow || rules[1].Header != "" {
		t.Fatalf("unexpected rule 1: %+v", rules[1])
	}
	if rules[2].Enabled {
		t.Fatalf("expected rule 2 disabled, got %+v", rules[2])
	}
}

// TestLoadFromCSV_Errors verifies error handling.
func TestLoadFromCSV_Errors(t *testing.T) {
	m := New()
	if err := m.LoadFromCSV(""); err == nil {
		t.Fatal("expected error for empty path")
	}
	if err := m.LoadFromCSV("/nonexistent/path/file.csv"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestGlobalFunctions exercises the package-level API.
func TestGlobalFunctions(t *testing.T) {
	Init()
	m := DefaultSecFilter()
	if m == nil {
		t.Fatal("expected non-nil default module")
	}
	AddRule(&FilterRule{Type: RuleDeny, Pattern: "globalblock*"})
	allowed, _ := CheckURI("sip:globalblock@example.com")
	if allowed {
		t.Fatal("expected deny via global API")
	}
	allowed, _ = CheckURI("sip:ok@example.com")
	if !allowed {
		t.Fatal("expected allow via global API")
	}
}

// TestConcurrentAccess exercises the module under the race detector.
func TestConcurrentAccess(t *testing.T) {
	m := New()
	m.AddRule(&FilterRule{Type: RuleDeny, Pattern: "block*"})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.AddRule(&FilterRule{Type: RuleAllow, Pattern: "ok"})
			m.CheckURI("block" + string(rune('a'+i%5)))
			m.ListRules()
			_ = m.Count()
		}(i)
	}
	wg.Wait()
}
