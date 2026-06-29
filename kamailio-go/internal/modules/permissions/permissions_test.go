// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - permissions module tests.
 */

package permissions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAddAndCheckSource(t *testing.T) {
	m := New()
	m.AddRule("10.0.0.1", "any", false)
	m.AddRule("10.0.0.2", "any", true)
	if m.CheckSource("10.0.0.1") {
		t.Error("CheckSource(10.0.0.1) = true, want false (deny)")
	}
	if !m.CheckSource("10.0.0.2") {
		t.Error("CheckSource(10.0.0.2) = false, want true (allow)")
	}
	if !m.CheckSource("10.0.0.99") {
		t.Error("CheckSource(unlisted) = false, want true (default allow)")
	}
}

func TestCheckURIAndRemove(t *testing.T) {
	m := New()
	m.AddRule("1.2.3.4", "5.6.7.8", false)
	if m.CheckURI("sip:user@5.6.7.8") {
		t.Error("CheckURI(denied dst) = true, want false")
	}
	if !m.RemoveRule("1.2.3.4", "5.6.7.8") {
		t.Error("RemoveRule = false, want true")
	}
	if !m.CheckURI("sip:user@5.6.7.8") {
		t.Error("CheckURI after remove should be true (default allow)")
	}
	if m.RemoveRule("1.2.3.4", "5.6.7.8") {
		t.Error("RemoveRule twice = true, want false")
	}
}

func TestLoadFromCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.csv")
	content := "# header\n" +
		"10.0.0.1,any,0\n" +
		"10.0.0.2,any,1\n" +
		"10.0.0.3,any,true\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m := New()
	if err := m.LoadFromCSV(path); err != nil {
		t.Fatalf("LoadFromCSV: %v", err)
	}
	if m.RuleCount() != 3 {
		t.Errorf("RuleCount = %d, want 3", m.RuleCount())
	}
	if m.CheckSource("10.0.0.1") {
		t.Error("10.0.0.1 should be denied")
	}
	if !m.CheckSource("10.0.0.2") {
		t.Error("10.0.0.2 should be allowed")
	}
	if !m.CheckSource("10.0.0.3") {
		t.Error("10.0.0.3 should be allowed (true)")
	}
}

func TestLoadFromCSLMissingFile(t *testing.T) {
	m := New()
	if err := m.LoadFromCSV("/nonexistent/path/file.csv"); err == nil {
		t.Error("LoadFromCSV(missing) expected error")
	}
}
