// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the domainpolicy module.
 */

package domainpolicy

import (
	"sync"
	"testing"
)

func TestAddCheckRemove(t *testing.T) {
	m := New()

	m.AddRule("example.com", "allow")
	m.AddRule("blocked.com", "deny")

	p, ok := m.Check("example.com")
	if !ok || p != "allow" {
		t.Errorf("Check(example.com) = %q,%v, want %q,true", p, ok, "allow")
	}

	if _, ok := m.Check("unknown.com"); ok {
		t.Errorf("Check(unknown) should return false")
	}

	if !m.RemoveRule("example.com") {
		t.Errorf("RemoveRule(example.com) returned false")
	}
	if m.RemoveRule("example.com") {
		t.Errorf("RemoveRule twice should return false")
	}
}

func TestAddUpdate(t *testing.T) {
	m := New()
	m.AddRule("d.com", "allow")
	if got := m.Count(); got != 1 {
		t.Errorf("Count() = %d, want 1", got)
	}
	m.AddRule("d.com", "deny")
	if got := m.Count(); got != 1 {
		t.Errorf("Count() after update = %d, want 1", got)
	}
	p, _ := m.Check("d.com")
	if p != "deny" {
		t.Errorf("Check(d.com) = %q, want %q", p, "deny")
	}
}

func TestList(t *testing.T) {
	m := New()
	m.AddRule("a.com", "1")
	m.AddRule("b.com", "2")

	list := m.List()
	if len(list) != 2 {
		t.Fatalf("List() = %d entries, want 2", len(list))
	}
	if list["a.com"] != "1" || list["b.com"] != "2" {
		t.Errorf("List() = %v", list)
	}
	list["a.com"] = "mutated"
	p, _ := m.Check("a.com")
	if p != "1" {
		t.Errorf("Check(a.com) after mutating list = %q, want %q", p, "1")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultDomainPolicy()
	if d == nil {
		t.Fatalf("DefaultDomainPolicy() returned nil")
	}
	if d != DefaultDomainPolicy() {
		t.Fatalf("DefaultDomainPolicy() returned different instances")
	}
	AddRule("pkg.com", "allow")
	p, ok := Check("pkg.com")
	if !ok || p != "allow" {
		t.Errorf("package Check(pkg.com) = %q,%v", p, ok)
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultDomainPolicy()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			dom := "d" + itoa(i) + ".com"
			shared.AddRule(dom, "p")
			shared.Check(dom)
			shared.List()
			shared.RemoveRule(dom)
		}(i)
	}
	wg.Wait()
	if got := shared.Count(); got != 0 {
		t.Errorf("Count() after concurrent = %d, want 0", got)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
