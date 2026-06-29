// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the cfgt module.
 */

package cfgt

import (
	"sync"
	"testing"
)

func TestTrackGetDiff(t *testing.T) {
	m := New()

	m.Track("debug", "0")
	m.Track("loglevel", "3")

	v, ok := m.Get("debug")
	if !ok || v != "0" {
		t.Errorf("Get(debug) = %q,%v, want %q,true", v, ok, "0")
	}

	// No changes yet.
	if diff := m.Diff(); len(diff) != 0 {
		t.Errorf("Diff() = %v, want empty", diff)
	}

	// Change one value.
	m.Update("debug", "1")
	diff := m.Diff()
	if len(diff) != 1 {
		t.Fatalf("Diff() = %v, want 1 entry", diff)
	}
	if diff["debug"] != "1" {
		t.Errorf("Diff()[debug] = %q, want %q", diff["debug"], "1")
	}
}

func TestClear(t *testing.T) {
	m := New()
	m.Track("a", "1")
	m.Track("b", "2")
	if got := m.Count(); got != 2 {
		t.Fatalf("Count() = %d, want 2", got)
	}
	m.Clear()
	if got := m.Count(); got != 0 {
		t.Errorf("Count() after Clear = %d, want 0", got)
	}
	if diff := m.Diff(); len(diff) != 0 {
		t.Errorf("Diff() after Clear = %v, want empty", diff)
	}
}

func TestReTrackResetsBaseline(t *testing.T) {
	m := New()
	m.Track("k", "v1")
	m.Update("k", "v2")
	if len(m.Diff()) != 1 {
		t.Fatalf("Diff() before re-track should have 1 entry")
	}
	m.Track("k", "v2")
	if len(m.Diff()) != 0 {
		t.Errorf("Diff() after re-track should be empty")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultCfgT()
	if d == nil {
		t.Fatalf("DefaultCfgT() returned nil")
	}
	if d != DefaultCfgT() {
		t.Fatalf("DefaultCfgT() returned different instances")
	}
	Track("pkg", "0")
	Update("pkg", "1")
	diff := Diff()
	if diff["pkg"] != "1" {
		t.Errorf("package Diff()[pkg] = %q, want %q", diff["pkg"], "1")
	}
	Clear()
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultCfgT()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			k := itoa(i)
			shared.Track(k, "0")
			shared.Update(k, "1")
			shared.Get(k)
			shared.Diff()
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
