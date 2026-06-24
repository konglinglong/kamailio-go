// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - mtree module tests.
 */

package mtree

import (
	"sync"
	"testing"
)

func TestInsertAndLookup(t *testing.T) {
	m := New()
	m.Insert("123", "us-east")
	m.Insert("1234", "us-east-ny")
	m.Insert("99", "eu")

	if v, ok := m.Lookup("1234567"); !ok || v != "us-east-ny" {
		t.Errorf("Lookup(1234567) = %q,%v, want us-east-ny,true", v, ok)
	}
	if v, ok := m.Lookup("123"); !ok || v != "us-east" {
		t.Errorf("Lookup(123) = %q,%v, want us-east,true", v, ok)
	}
	if v, ok := m.Lookup("9999"); !ok || v != "eu" {
		t.Errorf("Lookup(9999) = %q,%v, want eu,true", v, ok)
	}
	if _, ok := m.Lookup("555"); ok {
		t.Error("Lookup(555) should not match")
	}
}

func TestDeleteAndSize(t *testing.T) {
	m := New()
	m.Insert("a", "1")
	m.Insert("b", "2")
	if m.Size() != 2 {
		t.Errorf("Size = %d, want 2", m.Size())
	}
	if !m.Delete("a") {
		t.Error("Delete(a) = false, want true")
	}
	if m.Delete("a") {
		t.Error("Delete(a) twice = true, want false")
	}
	if m.Size() != 1 {
		t.Errorf("Size after delete = %d, want 1", m.Size())
	}
}

func TestList(t *testing.T) {
	m := New()
	m.Insert("x", "1")
	m.Insert("y", "2")
	lst := m.List()
	if len(lst) != 2 {
		t.Fatalf("List len = %d, want 2", len(lst))
	}
	if lst["x"] != "1" || lst["y"] != "2" {
		t.Errorf("List = %v", lst)
	}
	// Mutating the copy must not affect the module.
	lst["x"] = "mutated"
	if v, _ := m.Lookup("x"); v != "1" {
		t.Errorf("internal value affected by copy mutation: %q", v)
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.Insert("k", "v")
			_, _ = m.Lookup("k")
			_ = m.List()
		}(i)
	}
	wg.Wait()
}
