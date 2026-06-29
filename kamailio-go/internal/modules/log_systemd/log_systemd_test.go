// SPDX-License-Identifier-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - LogSystemd module tests.
 */

package log_systemd

import (
	"sync"
	"testing"
)

func TestLogAndCount(t *testing.T) {
	m := New()
	m.SetLevel("debug")
	m.Log("info", "hello")
	m.Log("error", "boom")
	if got := m.Count(); got != 2 {
		t.Fatalf("Count = %d, want 2", got)
	}
	entries := m.Entries()
	if entries[0].Message != "hello" || entries[1].Message != "boom" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
	if entries[1].Level != LevelError {
		t.Fatalf("entry 1 level = %d, want %d", entries[1].Level, LevelError)
	}
}

func TestSetLevelFilters(t *testing.T) {
	m := New()
	m.SetLevel("warning")
	m.Log("debug", "dropped")
	m.Log("info", "dropped")
	m.Log("warning", "kept")
	m.Log("error", "kept")
	if got := m.Count(); got != 2 {
		t.Fatalf("Count = %d, want 2 (only warning+)", got)
	}
	for _, e := range m.Entries() {
		if e.Level < LevelWarning {
			t.Fatalf("unexpected below-threshold entry: %+v", e)
		}
	}
}

func TestClose(t *testing.T) {
	m := New()
	m.SetLevel("debug")
	m.Log("info", "before")
	m.Close()
	if !m.IsClosed() {
		t.Fatal("expected closed")
	}
	m.Log("info", "after")
	if got := m.Count(); got != 1 {
		t.Fatalf("Count = %d, want 1 (closed drops logs)", got)
	}
}

func TestInitResets(t *testing.T) {
	m := New()
	m.SetLevel("debug")
	m.Log("info", "old")
	m.Close()
	m.Init()
	if m.Count() != 0 {
		t.Fatalf("Count after Init = %d, want 0", m.Count())
	}
	if m.IsClosed() {
		t.Fatal("expected not closed after Init")
	}
	m.Log("info", "fresh")
	if m.Count() != 1 {
		t.Fatalf("Count = %d, want 1 after fresh log", m.Count())
	}
}

func TestUnknownLevelDefaultsInfo(t *testing.T) {
	m := New()
	m.SetLevel("debug")
	m.Log("bogus", "x")
	entries := m.Entries()
	if len(entries) != 1 || entries[0].Level != LevelInfo {
		t.Fatalf("expected unknown level to default to info, got %+v", entries)
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	SetLevel("debug")
	Log("info", "global")
	if got := DefaultLogSystemd().Count(); got < 1 {
		t.Fatalf("global Count = %d, want >=1", got)
	}
	Close()
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	m.SetLevel("debug")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.Log("info", "msg")
			_ = m.Entries()
			_ = m.Count()
		}(i)
	}
	wg.Wait()
	if m.Count() != 20 {
		t.Fatalf("Count = %d, want 20", m.Count())
	}
}
