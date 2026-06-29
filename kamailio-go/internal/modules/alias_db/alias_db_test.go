// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the alias_db module.
 */

package alias_db

import (
	"sync"
	"testing"
)

func TestAddLookup(t *testing.T) {
	m := New()

	m.Add("alice", "sip:alice@192.0.2.1")
	c, ok := m.Lookup("alice")
	if !ok {
		t.Fatalf("Lookup(alice) not found")
	}
	if c != "sip:alice@192.0.2.1" {
		t.Errorf("Lookup(alice) = %q, want %q", c, "sip:alice@192.0.2.1")
	}

	// Unknown alias.
	if _, ok := m.Lookup("nobody"); ok {
		t.Errorf("Lookup(nobody) should return false")
	}
}

func TestAddUpdateAndCount(t *testing.T) {
	m := New()

	m.Add("a1", "sip:a@1")
	m.Add("a2", "sip:a@2")
	if got := m.Count(); got != 2 {
		t.Errorf("Count() = %d, want 2", got)
	}

	// Re-adding updates the contact and does not increase the count.
	m.Add("a1", "sip:a@updated")
	if got := m.Count(); got != 2 {
		t.Errorf("Count() after update = %d, want 2", got)
	}
	c, _ := m.Lookup("a1")
	if c != "sip:a@updated" {
		t.Errorf("Lookup(a1) = %q, want %q", c, "sip:a@updated")
	}
}

func TestRemove(t *testing.T) {
	m := New()

	m.Add("x", "sip:x@y")
	if !m.Remove("x") {
		t.Fatalf("Remove(x) returned false, want true")
	}
	if _, ok := m.Lookup("x"); ok {
		t.Errorf("Lookup(x) after remove should return false")
	}
	if m.Remove("x") {
		t.Errorf("Remove(x) twice should return false")
	}
	if m.Remove("never") {
		t.Errorf("Remove(never) should return false")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultAliasDB()
	if d == nil {
		t.Fatalf("DefaultAliasDB() returned nil")
	}
	if d != DefaultAliasDB() {
		t.Fatalf("DefaultAliasDB() returned different instances")
	}
	Add("def", "sip:def@x")
	c, ok := Lookup("def")
	if !ok || c != "sip:def@x" {
		t.Errorf("package Lookup(def) = %q,%v", c, ok)
	}
	if got := Count(); got < 1 {
		t.Errorf("package Count() = %d, want >= 1", got)
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultAliasDB()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			shared.Add(itoa(i), "sip:c@"+itoa(i))
			shared.Lookup(itoa(i))
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
