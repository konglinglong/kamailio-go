// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - Domain module tests.
 */
package domain

import (
	"sync"
	"testing"
)

// TestAddDomainAndCount verifies insertion, duplicate rejection and counting.
func TestAddDomainAndCount(t *testing.T) {
	m := NewDomainModule()
	id1 := m.AddDomain("example.com", "did1")
	id2 := m.AddDomain("example.org", "did2")
	if id1 == id2 {
		t.Errorf("expected distinct IDs, got %d == %d", id1, id2)
	}
	if got := m.Count(); got != 2 {
		t.Errorf("Count = %d, want 2", got)
	}
	// Duplicate (case-insensitive) is rejected.
	if m.AddDomain("EXAMPLE.com", "did3") != -1 {
		t.Error("expected -1 for duplicate domain")
	}
	if m.AddDomain("", "did4") != -1 {
		t.Error("expected -1 for empty domain")
	}
}

// TestIsDomainKnown verifies case-insensitive domain lookup.
func TestIsDomainKnown(t *testing.T) {
	m := NewDomainModule()
	m.AddDomain("Example.COM", "did1")
	if !m.IsDomainKnown("example.com") {
		t.Error("expected example.com to be known")
	}
	if !m.IsDomainKnown("EXAMPLE.COM") {
		t.Error("expected EXAMPLE.COM to be known")
	}
	if m.IsDomainKnown("unknown.com") {
		t.Error("expected unknown.com to be unknown")
	}
}

// TestGetDomain verifies retrieval returns a copy.
func TestGetDomain(t *testing.T) {
	m := NewDomainModule()
	m.AddDomain("example.com", "did1")
	e := m.GetDomain("example.com")
	if e == nil {
		t.Fatal("expected entry for example.com")
	}
	if e.Did != "did1" {
		t.Errorf("Did = %q, want did1", e.Did)
	}
	if e.LastModified.IsZero() {
		t.Error("expected non-zero LastModified")
	}
	// Mutating the returned copy must not affect the module.
	e.Did = "mutated"
	if m.GetDomain("example.com").Did == "mutated" {
		t.Fatal("expected isolation from GetDomain copy")
	}
	if m.GetDomain("missing.com") != nil {
		t.Error("expected nil for unknown domain")
	}
}

// TestRemoveDomain verifies removal and attribute cleanup.
func TestRemoveDomain(t *testing.T) {
	m := NewDomainModule()
	m.AddDomain("example.com", "did1")
	m.SetAttr("did1", "color", "blue")
	if !m.RemoveDomain("EXAMPLE.com") {
		t.Fatal("expected RemoveDomain true")
	}
	if m.Count() != 0 {
		t.Fatalf("expected count 0, got %d", m.Count())
	}
	// Attributes for the removed domain's did are gone.
	if _, ok := m.GetAttr("did1", "color"); ok {
		t.Error("expected attribute removed with domain")
	}
	if m.RemoveDomain("example.com") {
		t.Error("expected RemoveDomain false for already removed")
	}
}

// TestSetGetAttr verifies attribute set/get and updates.
func TestSetGetAttr(t *testing.T) {
	m := NewDomainModule()
	m.SetAttr("did1", "color", "blue")
	v, ok := m.GetAttr("did1", "color")
	if !ok {
		t.Fatal("expected attribute to exist")
	}
	if v != "blue" {
		t.Errorf("GetAttr = %q, want blue", v)
	}
	// Update existing attribute.
	m.SetAttr("did1", "color", "red")
	v, _ = m.GetAttr("did1", "color")
	if v != "red" {
		t.Errorf("GetAttr = %q, want red after update", v)
	}
	if _, ok := m.GetAttr("did1", "missing"); ok {
		t.Error("expected false for missing attribute")
	}
	if m.SetAttr("", "name", "v") != -1 {
		t.Error("expected -1 for empty did")
	}
	if m.SetAttr("did", "", "v") != -1 {
		t.Error("expected -1 for empty name")
	}
}

// TestListDomains verifies listing returns sorted copies.
func TestListDomains(t *testing.T) {
	m := NewDomainModule()
	m.AddDomain("zeta.com", "d1")
	m.AddDomain("alpha.com", "d2")
	m.AddDomain("mid.com", "d3")
	list := m.ListDomains()
	if len(list) != 3 {
		t.Fatalf("expected 3 domains, got %d", len(list))
	}
	if list[0].Domain != "alpha.com" {
		t.Errorf("first = %q, want alpha.com", list[0].Domain)
	}
	if list[2].Domain != "zeta.com" {
		t.Errorf("last = %q, want zeta.com", list[2].Domain)
	}
	// Mutating a returned copy must not affect the module.
	list[0].Did = "mutated"
	if m.GetDomain("alpha.com").Did == "mutated" {
		t.Fatal("expected isolation from ListDomains copy")
	}
}

// TestReload verifies Reload clears state.
func TestReload(t *testing.T) {
	m := NewDomainModule()
	m.AddDomain("example.com", "did1")
	m.SetAttr("did1", "color", "blue")
	if m.Count() != 1 {
		t.Fatalf("expected count 1 before reload, got %d", m.Count())
	}
	m.Reload()
	if m.Count() != 0 {
		t.Fatalf("expected count 0 after reload, got %d", m.Count())
	}
	if _, ok := m.GetAttr("did1", "color"); ok {
		t.Error("expected attributes cleared after reload")
	}
}

// TestGlobalFunctions exercises the package-level API.
func TestGlobalFunctions(t *testing.T) {
	Init()
	d := DefaultDomain()
	if d == nil {
		t.Fatal("expected non-nil default domain")
	}
	d.AddDomain("global.com", "did1")
	if !d.IsDomainKnown("global.com") {
		t.Error("expected global.com known via default module")
	}
}

// TestConcurrentAccess exercises the module under the race detector.
func TestConcurrentAccess(t *testing.T) {
	m := NewDomainModule()
	m.AddDomain("example.com", "did1")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			dom := "d" + string(rune('0'+n%5)) + ".com"
			m.AddDomain(dom, "did")
			m.IsDomainKnown("example.com")
			m.GetDomain("example.com")
			m.SetAttr("did1", "k", "v")
			m.GetAttr("did1", "k")
			_ = m.ListDomains()
			_ = m.Count()
		}(i)
	}
	wg.Wait()
}
