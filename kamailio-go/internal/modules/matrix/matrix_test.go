// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - matrix module tests.
 */

package matrix

import (
	"sync"
	"testing"
)

func TestSetAndLookup(t *testing.T) {
	m := New()
	m.Set("src1", "dst1", "routeA")
	m.Set("src1", "dst2", "routeB")
	m.Set("src2", "dst1", "routeC")

	if v, err := m.Lookup("src1", "dst1"); err != nil || v != "routeA" {
		t.Errorf("Lookup(src1,dst1) = %q, %v, want routeA", v, err)
	}
	if v, err := m.Lookup("src2", "dst1"); err != nil || v != "routeC" {
		t.Errorf("Lookup(src2,dst1) = %q, %v, want routeC", v, err)
	}
}

func TestLookupMissing(t *testing.T) {
	m := New()
	if _, err := m.Lookup("nope", "nope"); err == nil {
		t.Error("Lookup(missing) expected error")
	}
	m.Set("src1", "dst1", "x")
	if _, err := m.Lookup("src1", "missing"); err == nil {
		t.Error("Lookup(missing col) expected error")
	}
}

func TestRemoveAndList(t *testing.T) {
	m := New()
	m.Set("r", "c1", "v1")
	m.Set("r", "c2", "v2")
	if !m.Remove("r", "c1") {
		t.Error("Remove(r,c1) = false, want true")
	}
	if m.Remove("r", "c1") {
		t.Error("Remove(r,c1) twice = true, want false")
	}
	lst := m.List()
	if len(lst) != 1 || len(lst["r"]) != 1 || lst["r"]["c2"] != "v2" {
		t.Errorf("List = %v, want r->c2->v2", lst)
	}
	// Removing last column prunes the row.
	m.Remove("r", "c2")
	if len(m.List()) != 0 {
		t.Error("expected empty matrix after removing last cell")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.Set("r", "c", "v")
			_, _ = m.Lookup("r", "c")
			_ = m.List()
		}(i)
	}
	wg.Wait()
}
