// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the counter package.
 */

package counter

import (
	"strconv"
	"sync"
	"testing"
)

func TestRegister(t *testing.T) {
	r := NewCounterRegistry()
	c := r.Register("foo", "grp", "a test counter")
	if c == nil {
		t.Fatal("Register returned nil")
	}
	if c.Name != "foo" {
		t.Errorf("Name = %q, want %q", c.Name, "foo")
	}
	if c.Group != "grp" {
		t.Errorf("Group = %q, want %q", c.Group, "grp")
	}
	if c.Desc != "a test counter" {
		t.Errorf("Desc = %q, want %q", c.Desc, "a test counter")
	}
	// idempotent: registering the same name again returns the same counter
	if c2 := r.Register("foo", "grp", "a test counter"); c2 != c {
		t.Error("Register should return the existing counter for a duplicate name")
	}
	// lookup via Get
	if r.Get("foo") != c {
		t.Error("Get did not return the registered counter")
	}
	if r.Get("missing") != nil {
		t.Error("Get for a missing counter should return nil")
	}
}

func TestInc(t *testing.T) {
	r := NewCounterRegistry()
	r.Register("req", "core", "requests")
	if v := r.Value("req"); v != 0 {
		t.Fatalf("initial value = %d, want 0", v)
	}
	r.Inc("req")
	r.Inc("req")
	r.Inc("req")
	if v := r.Value("req"); v != 3 {
		t.Errorf("value = %d, want 3", v)
	}
	// Inc on an unknown counter is a no-op
	r.Inc("unknown")
	if v := r.Value("unknown"); v != 0 {
		t.Errorf("unknown value = %d, want 0", v)
	}
}

func TestIncBy(t *testing.T) {
	r := NewCounterRegistry()
	r.Register("bytes", "net", "bytes sent")
	r.IncBy("bytes", 100)
	r.IncBy("bytes", 250)
	if v := r.Value("bytes"); v != 350 {
		t.Errorf("value = %d, want 350", v)
	}
	// negative increments are allowed (equivalent to a decrement)
	r.IncBy("bytes", -50)
	if v := r.Value("bytes"); v != 300 {
		t.Errorf("value = %d, want 300", v)
	}
}

func TestDec(t *testing.T) {
	r := NewCounterRegistry()
	r.Register("errors", "core", "error count")
	r.IncBy("errors", 10)
	r.Dec("errors")
	r.Dec("errors")
	if v := r.Value("errors"); v != 8 {
		t.Errorf("value = %d, want 8", v)
	}
	// DecBy mirrors Dec
	r.DecBy("errors", 3)
	if v := r.Value("errors"); v != 5 {
		t.Errorf("value = %d, want 5", v)
	}
}

func TestReset(t *testing.T) {
	r := NewCounterRegistry()
	r.Register("conns", "net", "active connections")
	r.IncBy("conns", 42)
	if v := r.Value("conns"); v != 42 {
		t.Fatalf("value = %d, want 42", v)
	}
	r.Reset("conns")
	if v := r.Value("conns"); v != 0 {
		t.Errorf("value = %d, want 0 after reset", v)
	}
	// reset then increment again works
	r.Inc("conns")
	if v := r.Value("conns"); v != 1 {
		t.Errorf("value = %d, want 1 after reset+inc", v)
	}
	// Reset on an unknown counter is a no-op
	r.Reset("unknown")
}

func TestValue(t *testing.T) {
	r := NewCounterRegistry()
	r.Register("cnt", "g", "d")
	if v := r.Value("cnt"); v != 0 {
		t.Errorf("value = %d, want 0", v)
	}
	if v := r.Value("missing"); v != 0 {
		t.Errorf("missing value = %d, want 0", v)
	}
	r.IncBy("cnt", 7)
	if v := r.Value("cnt"); v != 7 {
		t.Errorf("value = %d, want 7", v)
	}
}

func TestGetGroup(t *testing.T) {
	r := NewCounterRegistry()
	r.Register("a", "grp1", "a")
	r.Register("b", "grp1", "b")
	r.Register("c", "grp2", "c")
	g1 := r.GetGroup("grp1")
	if g1 == nil {
		t.Fatal("GetGroup(grp1) returned nil")
	}
	if g1.Name != "grp1" {
		t.Errorf("group Name = %q, want grp1", g1.Name)
	}
	if len(g1.Counters) != 2 {
		t.Errorf("group has %d counters, want 2", len(g1.Counters))
	}
	if g1.Counters["a"] == nil || g1.Counters["b"] == nil {
		t.Error("group Counters map missing expected entries")
	}
	if r.GetGroup("missing") != nil {
		t.Error("GetGroup for a missing group should return nil")
	}
	// ListGroup returns a snapshot of the group's counters
	list := r.ListGroup("grp1")
	if len(list) != 2 {
		t.Errorf("ListGroup returned %d counters, want 2", len(list))
	}
	if r.ListGroup("missing") != nil {
		t.Error("ListGroup for a missing group should return nil")
	}
}

func TestList(t *testing.T) {
	r := NewCounterRegistry()
	if list := r.List(); len(list) != 0 {
		t.Fatalf("empty registry List returned %d counters, want 0", len(list))
	}
	r.Register("a", "g", "a")
	r.Register("b", "g", "b")
	r.Register("c", "g2", "c")
	list := r.List()
	if len(list) != 3 {
		t.Fatalf("List returned %d counters, want 3", len(list))
	}
	names := make(map[string]bool)
	for _, c := range list {
		names[c.Name] = true
	}
	for _, want := range []string{"a", "b", "c"} {
		if !names[want] {
			t.Errorf("counter %q missing from List", want)
		}
	}
}

func TestStats(t *testing.T) {
	r := NewCounterRegistry()
	r.Register("a", "g", "a")
	r.Register("b", "g", "b")
	r.IncBy("a", 5)
	r.IncBy("b", 15)
	stats := r.Stats()
	if len(stats) != 2 {
		t.Fatalf("Stats has %d entries, want 2", len(stats))
	}
	if stats["a"] != 5 {
		t.Errorf("stats[a] = %d, want 5", stats["a"])
	}
	if stats["b"] != 15 {
		t.Errorf("stats[b] = %d, want 15", stats["b"])
	}
	// mutating after the snapshot must not change it
	r.Inc("a")
	if stats["a"] != 5 {
		t.Errorf("snapshot changed after Inc: stats[a] = %d, want 5", stats["a"])
	}
}

func TestConcurrentInc(t *testing.T) {
	r := NewCounterRegistry()
	r.Register("hot", "g", "hot counter")
	const goroutines = 100
	const incs = 1000
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < incs; j++ {
				r.Inc("hot")
			}
		}()
	}
	wg.Wait()
	want := int64(goroutines * incs)
	if got := r.Value("hot"); got != want {
		t.Errorf("value = %d, want %d", got, want)
	}
}

func TestConcurrentRegister(t *testing.T) {
	r := NewCounterRegistry()
	const n = 100
	var wg sync.WaitGroup

	// All goroutines register the same name; they must observe the
	// same *Counter and the registry must end up with a single entry.
	wg.Add(n)
	ptrs := make([]*Counter, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			ptrs[i] = r.Register("shared", "grp", "shared counter")
		}(i)
	}
	wg.Wait()
	if ptrs[0] == nil {
		t.Fatal("Register returned nil")
	}
	for i := 1; i < n; i++ {
		if ptrs[i] != ptrs[0] {
			t.Fatalf("goroutine %d got %p, want %p", i, ptrs[i], ptrs[0])
		}
	}
	if got := len(r.List()); got != 1 {
		t.Errorf("registry has %d counters, want 1", got)
	}

	// Stress the insert path with distinct names.
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			r.Register(strconv.Itoa(i), "grp", "distinct counter")
		}(i)
	}
	wg.Wait()
	if got := len(r.List()); got != 1+n {
		t.Errorf("registry has %d counters, want %d", got, 1+n)
	}
}
