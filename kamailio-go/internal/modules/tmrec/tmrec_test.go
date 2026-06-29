// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the TMRec module.
 */

package tmrec

import (
	"sync"
	"testing"
	"time"
)

func TestCreateDestroyCount(t *testing.T) {
	m := New()
	id1 := m.Create("rec1", 5*time.Millisecond)
	if id1 <= 0 {
		t.Fatalf("Create() = %d, want > 0", id1)
	}
	id2 := m.Create("rec2", 5*time.Millisecond)
	if id2 <= id1 {
		t.Fatalf("Create() = %d, want > %d", id2, id1)
	}
	if m.Count() != 2 {
		t.Errorf("Count() = %d, want 2", m.Count())
	}
	if !m.Destroy(id1) {
		t.Errorf("Destroy(%d) = false, want true", id1)
	}
	if m.Count() != 1 {
		t.Errorf("Count() after destroy = %d, want 1", m.Count())
	}
	if m.Destroy(id1) {
		t.Errorf("Destroy(%d) twice = true, want false", id1)
	}
	if m.Destroy(99999) {
		t.Errorf("Destroy(unknown) = true, want false")
	}
	// Invalid interval -> -1.
	if m.Create("bad", 0) != -1 {
		t.Errorf("Create(0 interval) should return -1")
	}
}

func TestEnableDisable(t *testing.T) {
	m := New()
	id := m.Create("rec", 50*time.Millisecond)
	defer m.Destroy(id)

	// Disable before the first tick fires: no ticks should accumulate.
	m.Disable(id)
	if m.isEnabled(id) {
		t.Errorf("isEnabled = true after Disable")
	}
	time.Sleep(80 * time.Millisecond)
	if got := m.tickCount(id); got != 0 {
		t.Errorf("ticks while disabled = %d, want 0", got)
	}

	// Enable: ticks should now accumulate.
	m.Enable(id)
	if !m.isEnabled(id) {
		t.Errorf("isEnabled = false after Enable")
	}
	time.Sleep(120 * time.Millisecond)
	if got := m.tickCount(id); got < 1 {
		t.Errorf("ticks after enable = %d, want >= 1", got)
	}

	// Enable/Disable on an unknown id must not panic.
	m.Enable(99999)
	m.Disable(99999)
}

func TestConcurrent(t *testing.T) {
	m := New()
	const goroutines = 30
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			id := m.Create("c", 50*time.Millisecond)
			m.Count()
			m.Disable(id)
			m.Enable(id)
			m.Destroy(id)
		}()
	}
	wg.Wait()
	if m.Count() != 0 {
		t.Errorf("Count() after concurrent = %d, want 0", m.Count())
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultTMRec()
	if d == nil {
		t.Fatal("DefaultTMRec() = nil")
	}
	if d != DefaultTMRec() {
		t.Fatal("DefaultTMRec() returned different instances")
	}
	id := d.Create("def", 50*time.Millisecond)
	if id <= 0 {
		t.Fatal("Create on default returned invalid id")
	}
	Init()
	if d.Count() != 0 {
		t.Errorf("Count() after Init = %d, want 0", d.Count())
	}
}
