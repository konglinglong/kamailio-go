// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - dtrie (digit/character trie) tests.
 *
 * Mirrors the C data structure in src/lib/trie/dtrie.{c,h}, providing a
 * generic trie keyed by individual characters with longest-prefix match,
 * prefix deletion and prefix walks.
 */
package dtrie

import (
	"fmt"
	"sync"
	"testing"
)

// TestInsert verifies basic insertion and exact lookup.
func TestInsert(t *testing.T) {
	tr := New()
	tr.Insert("abc", 1)
	tr.Insert("abd", 2)

	if v, ok := tr.Lookup("abc"); !ok || v != 1 {
		t.Errorf("Lookup(abc) = %v,%v; want 1,true", v, ok)
	}
	if v, ok := tr.Lookup("abd"); !ok || v != 2 {
		t.Errorf("Lookup(abd) = %v,%v; want 2,true", v, ok)
	}
	// Overwrite updates the stored value.
	tr.Insert("abc", 11)
	if v, _ := tr.Lookup("abc"); v != 11 {
		t.Errorf("Lookup(abc) after overwrite = %v; want 11", v)
	}
	if tr.Count() != 2 {
		t.Errorf("Count = %d; want 2", tr.Count())
	}
}

// TestLookup verifies exact-match semantics (prefixes without data do not match).
func TestLookup(t *testing.T) {
	tr := New()
	tr.Insert("12", "x")

	if v, ok := tr.Lookup("12"); !ok || v != "x" {
		t.Errorf("Lookup(12) = %v,%v; want x,true", v, ok)
	}
	// Longer key than any inserted entry is not an exact match.
	if _, ok := tr.Lookup("123"); ok {
		t.Error("Lookup(123) should be false (no exact entry)")
	}
	// Shorter key (a prefix of an entry) is not an exact match.
	if _, ok := tr.Lookup("1"); ok {
		t.Error("Lookup(1) should be false (prefix only)")
	}
	// Empty trie lookups miss.
	empty := New()
	if _, ok := empty.Lookup("anything"); ok {
		t.Error("Lookup on empty trie should miss")
	}
}

// TestLongestPrefix verifies longest-prefix matching.
func TestLongestPrefix(t *testing.T) {
	tr := New()
	tr.Insert("12", "a")
	tr.Insert("1234", "b")

	if v, ok := tr.LongestPrefix("123456"); !ok || v != "b" {
		t.Errorf("LongestPrefix(123456) = %v,%v; want b,true", v, ok)
	}
	if v, ok := tr.LongestPrefix("123"); !ok || v != "a" {
		t.Errorf("LongestPrefix(123) = %v,%v; want a,true", v, ok)
	}
	if v, ok := tr.LongestPrefix("12"); !ok || v != "a" {
		t.Errorf("LongestPrefix(12) = %v,%v; want a,true", v, ok)
	}
	if _, ok := tr.LongestPrefix("1"); ok {
		t.Error("LongestPrefix(1) should be false")
	}
	if _, ok := tr.LongestPrefix("999"); ok {
		t.Error("LongestPrefix(999) should be false")
	}
}

// TestDelete verifies single-key deletion and pruning of empty leaves.
func TestDelete(t *testing.T) {
	tr := New()
	tr.Insert("abc", 1)
	tr.Insert("abd", 2)

	if !tr.Delete("abc") {
		t.Error("Delete(abc) should return true")
	}
	if _, ok := tr.Lookup("abc"); ok {
		t.Error("Lookup(abc) after delete should be false")
	}
	// Sibling entry survives.
	if v, ok := tr.Lookup("abd"); !ok || v != 2 {
		t.Errorf("Lookup(abd) after deleting abc = %v,%v; want 2,true", v, ok)
	}
	// Deleting again returns false.
	if tr.Delete("abc") {
		t.Error("Delete(abc) twice should return false")
	}
	// Deleting a non-existent key returns false.
	if tr.Delete("xyz") {
		t.Error("Delete(xyz) should return false")
	}
}

// TestDeletePrefix verifies subtree deletion and the returned count.
func TestDeletePrefix(t *testing.T) {
	tr := New()
	tr.Insert("12", "a")
	tr.Insert("123", "b")
	tr.Insert("1234", "c")
	tr.Insert("999", "d")

	n := tr.DeletePrefix("12")
	if n != 3 {
		t.Errorf("DeletePrefix(12) = %d; want 3", n)
	}
	for _, k := range []string{"12", "123", "1234"} {
		if _, ok := tr.Lookup(k); ok {
			t.Errorf("Lookup(%s) after DeletePrefix should be false", k)
		}
	}
	// Unrelated entry survives.
	if v, ok := tr.Lookup("999"); !ok || v != "d" {
		t.Errorf("Lookup(999) after DeletePrefix(12) = %v,%v; want d,true", v, ok)
	}
	// Deleting a non-existent prefix removes nothing.
	if n := tr.DeletePrefix("777"); n != 0 {
		t.Errorf("DeletePrefix(777) = %d; want 0", n)
	}
}

// TestWalk verifies iteration over entries sharing a prefix.
func TestWalk(t *testing.T) {
	tr := New()
	tr.Insert("12", "a")
	tr.Insert("123", "b")
	tr.Insert("1234", "c")
	tr.Insert("999", "d")

	// Collect into a set so the visit order does not matter.
	got := make(map[string]interface{})
	tr.Walk("12", func(key string, data interface{}) {
		got[key] = data
	})
	want := map[string]interface{}{"12": "a", "123": "b", "1234": "c"}
	if len(got) != len(want) {
		t.Fatalf("Walk(12) returned %d entries; want %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if gv, ok := got[k]; !ok || gv != v {
			t.Errorf("Walk(12) entry %q = %v (ok=%v); want %v", k, gv, ok, v)
		}
	}

	// Walk over a deeper prefix.
	got = make(map[string]interface{})
	tr.Walk("123", func(key string, data interface{}) {
		got[key] = data
	})
	if len(got) != 2 {
		t.Errorf("Walk(123) returned %d entries; want 2: %v", len(got), got)
	}
	if got["123"] != "b" || got["1234"] != "c" {
		t.Errorf("Walk(123) entries = %v", got)
	}

	// Walk over a non-existent prefix yields nothing.
	got = make(map[string]interface{})
	tr.Walk("555", func(key string, data interface{}) {
		got[key] = data
	})
	if len(got) != 0 {
		t.Errorf("Walk(555) should yield nothing; got %v", got)
	}
}

// TestCount verifies Count (entries) and Size (nodes) including pruning.
func TestCount(t *testing.T) {
	tr := New()
	if tr.Count() != 0 {
		t.Errorf("Count on empty = %d; want 0", tr.Count())
	}
	if tr.Size() != 1 {
		t.Errorf("Size on empty = %d; want 1 (root)", tr.Size())
	}

	tr.Insert("abc", 1)
	if tr.Size() != 4 { // root, a, b, c
		t.Errorf("Size after Insert(abc) = %d; want 4", tr.Size())
	}
	tr.Insert("abd", 2)
	if tr.Size() != 5 { // root, a, b, c, d
		t.Errorf("Size after Insert(abd) = %d; want 5", tr.Size())
	}
	if tr.Count() != 2 {
		t.Errorf("Count = %d; want 2", tr.Count())
	}

	tr.Delete("abc")
	if tr.Size() != 4 { // pruned leaf c
		t.Errorf("Size after Delete(abc) = %d; want 4", tr.Size())
	}
	if tr.Count() != 1 {
		t.Errorf("Count after Delete(abc) = %d; want 1", tr.Count())
	}

	tr.Delete("abd")
	if tr.Size() != 1 { // pruned back to root
		t.Errorf("Size after Delete(abd) = %d; want 1", tr.Size())
	}
	if tr.Count() != 0 {
		t.Errorf("Count after Delete(abd) = %d; want 0", tr.Count())
	}

	// Clear resets to an empty (root-only) trie.
	tr.Insert("zzz", 9)
	tr.Insert("zza", 8)
	tr.Clear()
	if tr.Count() != 0 {
		t.Errorf("Count after Clear = %d; want 0", tr.Count())
	}
	if tr.Size() != 1 {
		t.Errorf("Size after Clear = %d; want 1", tr.Size())
	}
}

// TestConcurrentAccess exercises the trie under the race detector.
func TestConcurrentAccess(t *testing.T) {
	tr := New()
	// Seed shared entries.
	for i := 0; i < 50; i++ {
		tr.Insert(fmt.Sprintf("key%02d", i), i)
	}

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				k := fmt.Sprintf("key%02d", (id*7+i)%50)
				tr.Insert(k, i)
				_, _ = tr.Lookup(k)
				_, _ = tr.LongestPrefix(k)
				var n int
				tr.Walk("key", func(string, interface{}) { n++ })
			}
		}(g)
	}
	// Concurrent deleters on a disjoint key range.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			tr.DeletePrefix("zzz")
		}
	}()
	wg.Wait()

	// Trie must remain usable after concurrent stress.
	if tr.Count() < 0 {
		t.Error("Count should be non-negative")
	}
}
