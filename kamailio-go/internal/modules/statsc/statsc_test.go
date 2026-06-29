// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * StatsC module tests.
 */
package statsc

import (
	"strings"
	"sync"
	"testing"
)

func TestCollectAndGet(t *testing.T) {
	m := NewStatsCModule()
	if got := m.Get("calls"); got != 0 {
		t.Errorf("Get(calls) = %d, want 0", got)
	}
	m.Collect("calls", 5)
	m.Collect("calls", 3)
	if got := m.Get("calls"); got != 8 {
		t.Errorf("Get(calls) = %d, want 8", got)
	}
	// Collect with a negative value subtracts.
	m.Collect("calls", -2)
	if got := m.Get("calls"); got != 6 {
		t.Errorf("Get(calls) = %d, want 6", got)
	}
	// Unknown stat returns 0.
	if got := m.Get("nope"); got != 0 {
		t.Errorf("Get(nope) = %d, want 0", got)
	}
	// Empty name is ignored.
	m.Collect("", 10)
	if got := m.Get(""); got != 0 {
		t.Errorf("Get(empty) = %d, want 0", got)
	}
}

func TestListAndClear(t *testing.T) {
	m := NewStatsCModule()
	m.Collect("a", 1)
	m.Collect("b", 2)
	m.Collect("c", 3)
	listed := m.List()
	if len(listed) != 3 {
		t.Fatalf("len(List) = %d, want 3", len(listed))
	}
	if listed["a"] != 1 || listed["b"] != 2 || listed["c"] != 3 {
		t.Errorf("List = %v", listed)
	}
	// Mutating the returned map must not affect the module.
	listed["a"] = 999
	if got := m.Get("a"); got != 1 {
		t.Errorf("Get(a) after tamper = %d, want 1", got)
	}
	m.Clear()
	if got := m.Get("a"); got != 0 {
		t.Errorf("Get(a) after Clear = %d, want 0", got)
	}
	if len(m.List()) != 0 {
		t.Errorf("len(List) after Clear = %d, want 0", len(m.List()))
	}
}

func TestExport(t *testing.T) {
	m := NewStatsCModule()
	m.Collect("zebra", 10)
	m.Collect("alpha", 20)
	m.Collect("mid", 30)
	out := m.Export()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("Export lines = %d, want 3", len(lines))
	}
	// Export is sorted by name.
	want := []string{"alpha=20", "mid=30", "zebra=10"}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("Export line %d = %q, want %q", i, lines[i], w)
		}
	}
}

func TestConcurrentStatsC(t *testing.T) {
	m := NewStatsCModule()
	var wg sync.WaitGroup
	const goroutines = 20
	const perG = 50
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				m.Collect("hits", 1)
			}
		}()
	}
	wg.Wait()
	want := int64(goroutines * perG)
	if got := m.Get("hits"); got != want {
		t.Errorf("Get(hits) = %d, want %d", got, want)
	}
}
