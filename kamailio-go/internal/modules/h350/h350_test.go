// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the h350 module.
 */

package h350

import (
	"sync"
	"testing"
)

func TestAddLookup(t *testing.T) {
	m := New()

	if err := m.Add(&H350Entry{DN: "cn=alice,dc=x", UID: "alice", Phone: "1001"}); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	e, err := m.Lookup("alice")
	if err != nil {
		t.Fatalf("Lookup error: %v", err)
	}
	if e.DN != "cn=alice,dc=x" {
		t.Errorf("Lookup().DN = %q, want %q", e.DN, "cn=alice,dc=x")
	}
	if e.Phone != "1001" {
		t.Errorf("Lookup().Phone = %q, want %q", e.Phone, "1001")
	}

	if _, err := m.Lookup("nobody"); err == nil {
		t.Errorf("Lookup(nobody) should error")
	}
}

func TestAddErrors(t *testing.T) {
	m := New()
	if err := m.Add(nil); err == nil {
		t.Errorf("Add(nil) should error")
	}
	if err := m.Add(&H350Entry{DN: "x", UID: "", Phone: "1"}); err == nil {
		t.Errorf("Add(empty UID) should error")
	}
}

func TestLookupReturnsCopy(t *testing.T) {
	m := New()
	m.Add(&H350Entry{DN: "dn", UID: "u", Phone: "p"})

	e, _ := m.Lookup("u")
	e.Phone = "mutated"
	again, _ := m.Lookup("u")
	if again.Phone != "p" {
		t.Errorf("stored entry mutated via returned pointer: Phone = %q", again.Phone)
	}
}

func TestRemove(t *testing.T) {
	m := New()
	m.Add(&H350Entry{UID: "u", DN: "d", Phone: "p"})
	if !m.Remove("u") {
		t.Errorf("Remove(u) returned false")
	}
	if m.Remove("u") {
		t.Errorf("Remove(u) twice should return false")
	}
	if got := m.Count(); got != 0 {
		t.Errorf("Count() after remove = %d, want 0", got)
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultH350()
	if d == nil {
		t.Fatalf("DefaultH350() returned nil")
	}
	if err := Add(&H350Entry{UID: "pkg", DN: "d", Phone: "p"}); err != nil {
		t.Fatalf("package Add error: %v", err)
	}
	e, err := Lookup("pkg")
	if err != nil || e == nil {
		t.Errorf("package Lookup(pkg) = %v,%v", e, err)
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultH350()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			u := itoa(i)
			shared.Add(&H350Entry{UID: u, DN: "d", Phone: "p"})
			shared.Lookup(u)
			shared.Count()
		}(i)
	}
	wg.Wait()
	if got := shared.Count(); got != n {
		t.Errorf("Count() after concurrent = %d, want %d", got, n)
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
