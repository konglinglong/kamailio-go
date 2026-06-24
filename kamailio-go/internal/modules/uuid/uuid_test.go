// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the UUID module.
 */

package uuid

import (
	"sync"
	"testing"
)

func TestGenerate(t *testing.T) {
	m := New()
	u := m.Generate()
	if !m.Validate(u) {
		t.Errorf("Generate() produced invalid uuid %q", u)
	}
	if m.Version(u) != 4 {
		t.Errorf("Version(%q) = %d, want 4", u, m.Version(u))
	}
	// Two generated UUIDs should (almost certainly) differ.
	u2 := m.Generate()
	if u == u2 {
		t.Errorf("Generate() returned duplicate %q", u)
	}
}

func TestValidate(t *testing.T) {
	m := New()
	valid := []string{
		"550e8400-e29b-41d4-a716-446655440000", // v4
		"12345678-1234-5234-6234-1234567890ab", // v5-ish (well-formed)
		"00000000-0000-1000-8000-000000000000", // v1
	}
	for _, u := range valid {
		if !m.Validate(u) {
			t.Errorf("Validate(%q) = false, want true", u)
		}
	}
	invalid := []string{
		"",
		"not-a-uuid",
		"550e8400-e29b-41d4-a716",              // too short
		"550e8400-e29b-41d4-a716-44665544000g",  // bad hex
		"550e8400e29b41d4a716446655440000",      // no dashes
	}
	for _, u := range invalid {
		if m.Validate(u) {
			t.Errorf("Validate(%q) = true, want false", u)
		}
	}
}

func TestVersion(t *testing.T) {
	m := New()
	cases := []struct {
		uuid string
		ver  int
	}{
		{"00000000-0000-1000-8000-000000000000", 1},
		{"00000000-0000-2000-8000-000000000000", 2},
		{"00000000-0000-3000-8000-000000000000", 3},
		{"00000000-0000-4000-8000-000000000000", 4},
		{"00000000-0000-5000-8000-000000000000", 5},
	}
	for _, c := range cases {
		if got := m.Version(c.uuid); got != c.ver {
			t.Errorf("Version(%q) = %d, want %d", c.uuid, got, c.ver)
		}
	}
	// Invalid uuid -> 0.
	if m.Version("nope") != 0 {
		t.Errorf("Version(invalid) = %d, want 0", m.Version("nope"))
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	seen := make(chan string, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			u := m.Generate()
			seen <- u
		}()
	}
	wg.Wait()
	close(seen)
	uniq := make(map[string]bool, goroutines)
	for u := range seen {
		if !m.Validate(u) {
			t.Errorf("concurrent Generate() produced invalid uuid %q", u)
		}
		if m.Version(u) != 4 {
			t.Errorf("concurrent Generate() version = %d, want 4", m.Version(u))
		}
		if uniq[u] {
			t.Errorf("duplicate uuid from concurrent Generate(): %q", u)
		}
		uniq[u] = true
	}
	if len(uniq) != goroutines {
		t.Errorf("got %d unique uuids, want %d", len(uniq), goroutines)
	}
}

func TestDefault(t *testing.T) {
	if New() == nil {
		t.Fatal("New() = nil")
	}
	if DefaultUUID() == nil {
		t.Fatal("DefaultUUID() = nil")
	}
	if Init() == nil {
		t.Fatal("Init() = nil")
	}
}
