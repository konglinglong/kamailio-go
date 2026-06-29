// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the UIDDomain module.
 */

package uid_domain

import (
	"sync"
	"testing"
)

func TestAddIsKnownRemove(t *testing.T) {
	m := New()
	m.AddDomain("example.com")
	m.AddDomain("test.org")
	if !m.IsKnown("example.com") {
		t.Errorf("IsKnown(example.com) = false, want true")
	}
	if m.IsKnown("unknown.com") {
		t.Errorf("IsKnown(unknown.com) = true, want false")
	}
	if m.IsKnown("") {
		t.Errorf("IsKnown(empty) = true, want false")
	}
	if !m.RemoveDomain("example.com") {
		t.Errorf("RemoveDomain(example.com) = false, want true")
	}
	if m.IsKnown("example.com") {
		t.Errorf("IsKnown(example.com) after remove = true, want false")
	}
	if m.RemoveDomain("example.com") {
		t.Errorf("RemoveDomain(example.com) twice = true, want false")
	}
	if m.RemoveDomain("unknown.com") {
		t.Errorf("RemoveDomain(unknown) = true, want false")
	}
}

func TestList(t *testing.T) {
	m := New()
	if got := m.List(); len(got) != 0 {
		t.Fatalf("List() = %v, want empty", got)
	}
	m.AddDomain("charlie.com")
	m.AddDomain("alpha.com")
	m.AddDomain("bravo.com")
	// Adding a duplicate is a no-op.
	m.AddDomain("alpha.com")
	got := m.List()
	want := []string{"alpha.com", "bravo.com", "charlie.com"}
	if len(got) != len(want) {
		t.Fatalf("List() = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("List()[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			name := "d" + itoa(i) + ".com"
			m.AddDomain(name)
			m.IsKnown(name)
			m.List()
			m.RemoveDomain(name)
		}(i)
	}
	wg.Wait()
	if len(m.List()) != 0 {
		t.Errorf("List() after concurrent = %v, want empty", m.List())
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultUIDDomain()
	if d == nil {
		t.Fatal("DefaultUIDDomain() = nil")
	}
	if d != DefaultUIDDomain() {
		t.Fatal("DefaultUIDDomain() returned different instances")
	}
	d.AddDomain("default.com")
	if !d.IsKnown("default.com") {
		t.Fatal("default add/known failed")
	}
	Init()
	if DefaultUIDDomain().IsKnown("default.com") {
		t.Errorf("IsKnown after re-Init should be false")
	}
}

// itoa is a tiny local int->string helper.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
