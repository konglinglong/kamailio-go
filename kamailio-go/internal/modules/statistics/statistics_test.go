// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Statistics module tests - named counters.
 */
package statistics

import (
	"sync"
	"testing"
)

func TestRegisterAndGet(t *testing.T) {
	m := NewStatisticsModule()
	s := m.Register("invites", "number of invites")
	if s == nil {
		t.Fatal("expected non-nil stat")
	}
	if s.Name != "invites" || s.Description != "number of invites" {
		t.Errorf("stat = {%q %q}, want {invites number of invites}", s.Name, s.Description)
	}
	if got := m.Get("invites"); got != s {
		t.Errorf("Get returned %p, want %p", got, s)
	}
	if got := m.Get("nonexistent"); got != nil {
		t.Errorf("Get(nonexistent) = %v, want nil", got)
	}
	// Registering the same name twice returns the same stat.
	if got := m.Register("invites", "other"); got != s {
		t.Errorf("re-register returned %p, want %p", got, s)
	}
	if s.Description != "number of invites" {
		t.Errorf("description was overwritten: %q", s.Description)
	}
}

func TestIncDec(t *testing.T) {
	m := NewStatisticsModule()
	m.Inc("counter")
	m.Inc("counter")
	m.IncBy("counter", 10)
	if got := m.Value("counter"); got != 12 {
		t.Errorf("Value = %d, want 12", got)
	}
	m.Dec("counter")
	m.DecBy("counter", 5)
	if got := m.Value("counter"); got != 6 {
		t.Errorf("Value = %d, want 6", got)
	}
}

func TestReset(t *testing.T) {
	m := NewStatisticsModule()
	m.IncBy("counter", 100)
	m.Reset("counter")
	if got := m.Value("counter"); got != 0 {
		t.Errorf("Value after Reset = %d, want 0", got)
	}
}

func TestLazyRegistration(t *testing.T) {
	m := NewStatisticsModule()
	// Inc on an unregistered stat creates it on the fly.
	m.Inc("lazy")
	if got := m.Value("lazy"); got != 1 {
		t.Errorf("Value(lazy) = %d, want 1", got)
	}
	s := m.Get("lazy")
	if s == nil {
		t.Fatal("expected lazy stat to be registered after Inc")
	}
	if s.Description != "" {
		t.Errorf("lazy stat Description = %q, want empty", s.Description)
	}
	// Value of an unknown stat is 0.
	if got := m.Value("unknown"); got != 0 {
		t.Errorf("Value(unknown) = %d, want 0", got)
	}
}

func TestListAndStats(t *testing.T) {
	m := NewStatisticsModule()
	m.Register("a", "stat a")
	m.Register("b", "stat b")
	m.Inc("a")
	m.IncBy("b", 5)

	if m.Count() != 2 {
		t.Errorf("Count = %d, want 2", m.Count())
	}
	if got := len(m.List()); got != 2 {
		t.Errorf("len(List) = %d, want 2", got)
	}
	stats := m.Stats()
	if stats["a"] != 1 {
		t.Errorf("Stats[a] = %d, want 1", stats["a"])
	}
	if stats["b"] != 5 {
		t.Errorf("Stats[b] = %d, want 5", stats["b"])
	}
}

func TestStatValueMethod(t *testing.T) {
	m := NewStatisticsModule()
	s := m.Register("x", "")
	m.IncBy("x", 42)
	if got := s.Value(); got != 42 {
		t.Errorf("Stat.Value() = %d, want 42", got)
	}
}

func TestConcurrentInc(t *testing.T) {
	m := NewStatisticsModule()
	m.Register("c", "")
	const goroutines = 50
	const perG = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				m.Inc("c")
			}
		}()
	}
	wg.Wait()
	want := int64(goroutines * perG)
	if got := m.Value("c"); got != want {
		t.Errorf("Value(c) = %d, want %d", got, want)
	}
}

func TestDefaultStatisticsAndInit(t *testing.T) {
	Init()
	d1 := DefaultStatistics()
	d2 := DefaultStatistics()
	if d1 != d2 {
		t.Error("DefaultStatistics returned different instances")
	}
	d1.Register("x", "")
	d1.Inc("x")
	if d2.Value("x") != 1 {
		t.Errorf("Value after inc via default = %d, want 1", d2.Value("x"))
	}
	Init()
	if DefaultStatistics().Count() != 0 {
		t.Error("expected reset after Init()")
	}
}
