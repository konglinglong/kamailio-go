// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SpeedDial module tests.
 */
package speeddial

import (
	"sync"
	"testing"
)

func TestAddLookupRemove(t *testing.T) {
	m := NewSpeedDialModule()
	if _, ok := m.Lookup("**1"); ok {
		t.Fatal("expected **1 not found initially")
	}
	m.Add("**1", "sip:bob@example.com")
	target, ok := m.Lookup("**1")
	if !ok {
		t.Fatal("expected **1 found after Add")
	}
	if target != "sip:bob@example.com" {
		t.Errorf("target = %q, want sip:bob@example.com", target)
	}
	if !m.Remove("**1") {
		t.Error("Remove returned false for existing shortcode")
	}
	if _, ok := m.Lookup("**1"); ok {
		t.Error("expected **1 not found after Remove")
	}
	if m.Remove("**1") {
		t.Error("Remove returned true for already-removed shortcode")
	}
}

func TestOverwriteAndList(t *testing.T) {
	m := NewSpeedDialModule()
	m.Add("**2", "sip:old@example.com")
	m.Add("**2", "sip:new@example.com") // overwrite
	target, ok := m.Lookup("**2")
	if !ok || target != "sip:new@example.com" {
		t.Errorf("Lookup(**2) = %q,%v, want sip:new@example.com,true", target, ok)
	}
	m.Add("**3", "sip:carol@example.com")
	listed := m.List()
	if len(listed) != 2 {
		t.Fatalf("len(List) = %d, want 2", len(listed))
	}
	if listed["**2"] != "sip:new@example.com" || listed["**3"] != "sip:carol@example.com" {
		t.Errorf("List = %v", listed)
	}
	// Mutating the returned map must not affect the module.
	listed["**2"] = "tampered"
	if t2, _ := m.Lookup("**2"); t2 != "sip:new@example.com" {
		t.Errorf("internal map tampered: Lookup(**2) = %q", t2)
	}
}

func TestAddEmptyShortcode(t *testing.T) {
	m := NewSpeedDialModule()
	m.Add("", "sip:ignored@example.com")
	if len(m.List()) != 0 {
		t.Errorf("expected empty list, got %v", m.List())
	}
}

func TestConcurrentSpeedDial(t *testing.T) {
	m := NewSpeedDialModule()
	var wg sync.WaitGroup
	const goroutines = 20
	const perG = 10
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(g int) {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				code := "**" + itoa(g*perG+j)
				m.Add(code, "sip:t@example.com")
				_, _ = m.Lookup(code)
				m.Remove(code)
			}
		}(i)
	}
	wg.Wait()
	if len(m.List()) != 0 {
		t.Errorf("len(List) after concurrent add/remove = %d, want 0", len(m.List()))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
