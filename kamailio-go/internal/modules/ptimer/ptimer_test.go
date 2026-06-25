// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the ptimer module - script-programmable timers.
 */
package ptimer

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegisterTimer(t *testing.T) {
	m := New()
	var calls int32
	cb := func() int { atomic.AddInt32(&calls, 1); return 0 }
	id, err := m.RegisterTimer("t1", 10*time.Millisecond, cb, false)
	if err != nil {
		t.Fatalf("RegisterTimer error: %v", err)
	}
	if id <= 0 {
		t.Errorf("id = %d, want > 0", id)
	}
	got := m.GetTimer(id)
	if got == nil {
		t.Fatalf("GetTimer returned nil")
	}
	if got.Name != "t1" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.Active {
		t.Errorf("newly registered timer should not be active")
	}
	if got.Interval != 10*time.Millisecond {
		t.Errorf("Interval = %v", got.Interval)
	}
	if !got.OneShot == false {
		t.Errorf("OneShot = %v, want false", got.OneShot)
	}
}

func TestRegisterTimerErrors(t *testing.T) {
	m := New()
	cb := func() int { return 0 }
	if _, err := m.RegisterTimer("", 10*time.Millisecond, cb, false); err == nil {
		t.Errorf("empty name should error")
	}
	if _, err := m.RegisterTimer("t", 0, cb, false); err == nil {
		t.Errorf("zero interval should error")
	}
	if _, err := m.RegisterTimer("t", 10*time.Millisecond, nil, false); err == nil {
		t.Errorf("nil callback should error")
	}
	// Duplicate name.
	_, _ = m.RegisterTimer("dup", 10*time.Millisecond, cb, false)
	if _, err := m.RegisterTimer("dup", 10*time.Millisecond, cb, false); err == nil {
		t.Errorf("duplicate name should error")
	}
}

func TestStartStopTimer(t *testing.T) {
	m := New()
	var calls int32
	cb := func() int { atomic.AddInt32(&calls, 1); return 0 }
	id, _ := m.RegisterTimer("t", 5*time.Millisecond, cb, false)
	if err := m.StartTimer(id); err != nil {
		t.Fatalf("StartTimer error: %v", err)
	}
	// Starting twice should error.
	if err := m.StartTimer(id); err == nil {
		t.Errorf("StartTimer twice should error")
	}
	time.Sleep(30 * time.Millisecond)
	if err := m.StopTimer(id); err != nil {
		t.Fatalf("StopTimer error: %v", err)
	}
	if atomic.LoadInt32(&calls) < 1 {
		t.Errorf("callback not invoked, calls=%d", calls)
	}
	// After stop, Active must be false.
	if got := m.GetTimer(id); got != nil && got.Active {
		t.Errorf("timer still active after StopTimer")
	}
}

func TestStopTimerErrors(t *testing.T) {
	m := New()
	if err := m.StopTimer(999); err == nil {
		t.Errorf("StopTimer on missing id should error")
	}
	if err := m.UnregisterTimer(999); err == nil {
		t.Errorf("UnregisterTimer on missing id should error")
	}
	if err := m.UnregisterTimerByName("missing"); err == nil {
		t.Errorf("UnregisterTimerByName on missing name should error")
	}
}

func TestOneShotTimer(t *testing.T) {
	m := New()
	var calls int32
	cb := func() int { atomic.AddInt32(&calls, 1); return 0 }
	id, _ := m.RegisterTimer("oneshot", 5*time.Millisecond, cb, true)
	_ = m.StartTimer(id)
	time.Sleep(30 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("one-shot callback calls = %d, want 1", got)
	}
	if got := m.GetTimer(id); got != nil && got.Active {
		t.Errorf("one-shot timer should be inactive after firing")
	}
}

func TestCallbackAbort(t *testing.T) {
	m := New()
	var calls int32
	cb := func() int { atomic.AddInt32(&calls, 1); return -1 }
	id, _ := m.RegisterTimer("abort", 5*time.Millisecond, cb, false)
	_ = m.StartTimer(id)
	time.Sleep(30 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("aborted callback calls = %d, want 1", got)
	}
}

func TestUnregisterTimer(t *testing.T) {
	m := New()
	var calls int32
	cb := func() int { atomic.AddInt32(&calls, 1); return 0 }
	id, _ := m.RegisterTimer("t", 5*time.Millisecond, cb, false)
	_ = m.StartTimer(id)
	time.Sleep(20 * time.Millisecond)
	if err := m.UnregisterTimer(id); err != nil {
		t.Fatalf("UnregisterTimer error: %v", err)
	}
	if m.GetTimer(id) != nil {
		t.Errorf("GetTimer should return nil after unregister")
	}
	// Counter must stop increasing.
	before := atomic.LoadInt32(&calls)
	time.Sleep(30 * time.Millisecond)
	after := atomic.LoadInt32(&calls)
	if after > before {
		t.Errorf("timer still firing after unregister: before=%d after=%d", before, after)
	}
}

func TestUnregisterTimerByName(t *testing.T) {
	m := New()
	cb := func() int { return 0 }
	id, _ := m.RegisterTimer("named", 5*time.Millisecond, cb, false)
	if err := m.UnregisterTimerByName("named"); err != nil {
		t.Fatalf("UnregisterTimerByName error: %v", err)
	}
	if m.GetTimer(id) != nil {
		t.Errorf("timer should be gone after UnregisterTimerByName")
	}
}

func TestStartAllStopAll(t *testing.T) {
	m := New()
	var calls int64
	mkcb := func() TimerCallback {
		return func() int { atomic.AddInt64(&calls, 1); return 0 }
	}
	_, _ = m.RegisterTimer("a", 5*time.Millisecond, mkcb(), false)
	_, _ = m.RegisterTimer("b", 5*time.Millisecond, mkcb(), false)
	_, _ = m.RegisterTimer("c", 5*time.Millisecond, mkcb(), false)
	if err := m.StartAll(); err != nil {
		t.Fatalf("StartAll error: %v", err)
	}
	for _, e := range m.ListTimers() {
		if !e.Active {
			t.Errorf("timer %d not active after StartAll", e.ID)
		}
	}
	time.Sleep(30 * time.Millisecond)
	if err := m.StopAll(); err != nil {
		t.Fatalf("StopAll error: %v", err)
	}
	for _, e := range m.ListTimers() {
		if e.Active {
			t.Errorf("timer %d still active after StopAll", e.ID)
		}
	}
	if atomic.LoadInt64(&calls) < 3 {
		t.Errorf("callbacks not invoked, calls=%d", calls)
	}
}

func TestListAndGetTimers(t *testing.T) {
	m := New()
	cb := func() int { return 0 }
	id1, _ := m.RegisterTimer("a", 10*time.Millisecond, cb, false)
	id2, _ := m.RegisterTimer("b", 20*time.Millisecond, cb, true)
	list := m.ListTimers()
	if len(list) != 2 {
		t.Fatalf("ListTimers len = %d, want 2", len(list))
	}
	if list[0].ID != id1 || list[1].ID != id2 {
		t.Errorf("ListTimers not sorted by ID: %+v", list)
	}
	if m.Count() != 2 {
		t.Errorf("Count = %d, want 2", m.Count())
	}
	if got := m.GetTimerByName("b"); got == nil || got.ID != id2 {
		t.Errorf("GetTimerByName(b) = %+v", got)
	}
	if got := m.GetTimerByName("missing"); got != nil {
		t.Errorf("GetTimerByName(missing) should be nil")
	}
}

func TestDefaultPTimerAndInit(t *testing.T) {
	Init()
	a := DefaultPTimer()
	b := DefaultPTimer()
	if a != b {
		t.Error("DefaultPTimer should return the same instance")
	}
	// Register a timer on the singleton, then re-init to confirm reset.
	_, _ = a.RegisterTimer("tmp", 10*time.Millisecond, func() int { return 0 }, false)
	if a.Count() != 1 {
		t.Errorf("Count = %d, want 1", a.Count())
	}
	Init()
	c := DefaultPTimer()
	if c == a {
		t.Error("Init should reset the default instance")
	}
	if c.Count() != 0 {
		t.Errorf("Count after Init = %d, want 0", c.Count())
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	cb := func() int { return 0 }
	// Concurrently register, start, list, stop and unregister timers.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := "t" + itoa(n)
			id, err := m.RegisterTimer(name, 5*time.Millisecond, cb, false)
			if err != nil {
				return
			}
			_ = m.StartTimer(id)
			_ = m.ListTimers()
			_ = m.GetTimer(id)
			_ = m.GetTimerByName(name)
			time.Sleep(10 * time.Millisecond)
			_ = m.StopTimer(id)
			_ = m.UnregisterTimer(id)
		}(i)
	}
	wg.Wait()
}

// itoa is a tiny dependency-free int->string to keep imports minimal.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
