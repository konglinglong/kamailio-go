// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - Dialplan module tests.
 */
package dialplan

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestAddRuleAndCount verifies insertion, ID assignment and counting.
func TestAddRuleAndCount(t *testing.T) {
	m := NewDialplanModule()
	id1 := m.AddRule(&DialplanRule{
		MatchExpr: `^(\d+)$`, ReplaceExpr: "sip:$1@example.com", Enabled: true,
	})
	id2 := m.AddRule(&DialplanRule{
		MatchExpr: `^0(\d+)$`, ReplaceExpr: "sip:$1@zero.com", Enabled: true,
	})
	if id1 == id2 {
		t.Errorf("expected distinct IDs, got %d == %d", id1, id2)
	}
	if got := m.Count(); got != 2 {
		t.Errorf("Count = %d, want 2", got)
	}
	if got := len(m.ListRules()); got != 2 {
		t.Errorf("ListRules len = %d, want 2", got)
	}
}

// TestTranslate verifies the first matching enabled rule is applied.
func TestTranslate(t *testing.T) {
	m := NewDialplanModule()
	m.AddRule(&DialplanRule{
		MatchExpr: `^0(\d+)$`, ReplaceExpr: "sip:$1@zero.com", Enabled: true,
	})
	m.AddRule(&DialplanRule{
		MatchExpr: `^(\d+)$`, ReplaceExpr: "sip:$1@example.com", Enabled: true,
	})

	got, err := m.Translate("012345")
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if got != "sip:12345@zero.com" {
		t.Errorf("Translate = %q, want sip:12345@zero.com", got)
	}

	// A pure-digit input matches the second rule.
	got, err = m.Translate("98765")
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if got != "sip:98765@example.com" {
		t.Errorf("Translate = %q, want sip:98765@example.com", got)
	}
}

// TestTranslateNoMatch verifies an error is returned when nothing matches.
func TestTranslateNoMatch(t *testing.T) {
	m := NewDialplanModule()
	m.AddRule(&DialplanRule{
		MatchExpr: `^(\d+)$`, ReplaceExpr: "sip:$1@example.com", Enabled: true,
	})
	got, err := m.Translate("abc")
	if err == nil {
		t.Error("expected error for non-matching input")
	}
	if got != "abc" {
		t.Errorf("expected input returned unchanged, got %q", got)
	}
}

// TestTranslateWithRule verifies translation by explicit rule ID.
func TestTranslateWithRule(t *testing.T) {
	m := NewDialplanModule()
	id := m.AddRule(&DialplanRule{
		MatchExpr: `^(\d+)$`, ReplaceExpr: "sip:$1@example.com", Enabled: true,
	})
	got, err := m.TranslateWithRule("1001", id)
	if err != nil {
		t.Fatalf("TranslateWithRule failed: %v", err)
	}
	if got != "sip:1001@example.com" {
		t.Errorf("TranslateWithRule = %q, want sip:1001@example.com", got)
	}
	// Unknown rule ID errors.
	if _, err := m.TranslateWithRule("1001", 999); err == nil {
		t.Error("expected error for unknown rule ID")
	}
}

// TestMatch verifies finding a matching rule returns a copy.
func TestMatch(t *testing.T) {
	m := NewDialplanModule()
	m.AddRule(&DialplanRule{
		MatchExpr: `^(\d+)$`, ReplaceExpr: "sip:$1@example.com",
		Description: "digits", Enabled: true,
	})
	r := m.Match("1001")
	if r == nil {
		t.Fatal("expected matching rule")
	}
	if r.Description != "digits" {
		t.Errorf("Description = %q, want digits", r.Description)
	}
	// Mutating the returned copy must not affect the module.
	r.Description = "mutated"
	if m.Match("1001").Description == "mutated" {
		t.Fatal("expected isolation from Match copy")
	}
	if m.Match("abc") != nil {
		t.Error("expected nil for non-matching input")
	}
}

// TestRemoveRule verifies rule removal.
func TestRemoveRule(t *testing.T) {
	m := NewDialplanModule()
	id := m.AddRule(&DialplanRule{
		MatchExpr: `^(\d+)$`, ReplaceExpr: "sip:$1@example.com", Enabled: true,
	})
	if !m.RemoveRule(id) {
		t.Fatal("expected RemoveRule true")
	}
	if m.Count() != 0 {
		t.Fatalf("expected count 0, got %d", m.Count())
	}
	if _, err := m.TranslateWithRule("1001", id); err == nil {
		t.Error("expected error for removed rule")
	}
	if m.RemoveRule(id) {
		t.Error("expected RemoveRule false for already removed")
	}
}

// TestEnableDisable verifies enabling/disabling rules.
func TestEnableDisable(t *testing.T) {
	m := NewDialplanModule()
	id := m.AddRule(&DialplanRule{
		MatchExpr: `^(\d+)$`, ReplaceExpr: "sip:$1@example.com", Enabled: true,
	})
	if !m.DisableRule(id) {
		t.Fatal("expected DisableRule true")
	}
	if _, err := m.Translate("1001"); err == nil {
		t.Error("expected error when all rules disabled")
	}
	if !m.EnableRule(id) {
		t.Fatal("expected EnableRule true")
	}
	got, err := m.Translate("1001")
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if got != "sip:1001@example.com" {
		t.Errorf("Translate = %q, want sip:1001@example.com", got)
	}
	if m.EnableRule(999) {
		t.Error("expected EnableRule false for unknown rule")
	}
}

// TestLoadFromCSV verifies CSV loading.
func TestLoadFromCSV(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rules.csv")
	content := "match_expr,replace_expr,description,enabled\n" +
		`^0(\d+)$,sip:$1@zero.com,leading-zero,1` + "\n" +
		`^(\d+)$,sip:$1@example.com,digits,1` + "\n"
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m := NewDialplanModule()
	if err := m.LoadFromCSV(p); err != nil {
		t.Fatalf("LoadFromCSV: %v", err)
	}
	if m.Count() != 2 {
		t.Fatalf("expected 2 rules, got %d", m.Count())
	}
	got, err := m.Translate("012345")
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if got != "sip:12345@zero.com" {
		t.Errorf("Translate = %q, want sip:12345@zero.com", got)
	}
}

// TestLoadFromCSV_Errors verifies error handling.
func TestLoadFromCSV_Errors(t *testing.T) {
	m := NewDialplanModule()
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
	d := DefaultDialplan()
	if d == nil {
		t.Fatal("expected non-nil default dialplan")
	}
	d.AddRule(&DialplanRule{
		MatchExpr: `^(\d+)$`, ReplaceExpr: "sip:$1@example.com", Enabled: true,
	})
	if d.Count() != 1 {
		t.Errorf("Count = %d, want 1", d.Count())
	}
}

// TestConcurrentAccess exercises the module under the race detector.
func TestConcurrentAccess(t *testing.T) {
	m := NewDialplanModule()
	id := m.AddRule(&DialplanRule{
		MatchExpr: `^(\d+)$`, ReplaceExpr: "sip:$1@example.com", Enabled: true,
	})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.Translate("1001")
			_, _ = m.TranslateWithRule("1001", id)
			_ = m.Match("1001")
			_ = m.ListRules()
			_ = m.Count()
			m.DisableRule(id)
			m.EnableRule(id)
		}()
	}
	wg.Wait()
}
