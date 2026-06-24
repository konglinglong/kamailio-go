// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the unified timer framework.
 */

package timer

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegisterTimer(t *testing.T) {
	tm := NewTimerManager()
	defer tm.StopAll()

	var count atomic.Int32
	tm.RegisterTimer("periodic", 10*time.Millisecond,
		func(p interface{}) { count.Add(1) }, nil)

	time.Sleep(100 * time.Millisecond)

	if c := count.Load(); c < 3 {
		t.Errorf("expected at least 3 ticks, got %d", c)
	}
}

func TestRegisterOnce(t *testing.T) {
	tm := NewTimerManager()
	defer tm.StopAll()

	var count atomic.Int32
	tm.RegisterOnce("once", 20*time.Millisecond,
		func(p interface{}) { count.Add(1) }, nil)

	time.Sleep(100 * time.Millisecond)
	if c := count.Load(); c != 1 {
		t.Errorf("expected 1 call after fire, got %d", c)
	}

	// Wait again: a one-shot timer must not fire a second time.
	time.Sleep(100 * time.Millisecond)
	if c := count.Load(); c != 1 {
		t.Errorf("expected still 1 call (one-shot), got %d", c)
	}
}

func TestStopTimer(t *testing.T) {
	tm := NewTimerManager()
	defer tm.StopAll()

	var count atomic.Int32
	tmr := tm.RegisterTimer("stoppable", 10*time.Millisecond,
		func(p interface{}) { count.Add(1) }, nil)

	time.Sleep(50 * time.Millisecond)
	tm.Stop(tmr)

	before := count.Load()
	if before < 2 {
		t.Errorf("expected at least 2 ticks before stop, got %d", before)
	}

	// Stop blocks until the goroutine exits, so no further ticks.
	time.Sleep(50 * time.Millisecond)
	if c := count.Load(); c != before {
		t.Errorf("expected no ticks after stop, got %d (was %d)", c, before)
	}
}

func TestStopAll(t *testing.T) {
	tm := NewTimerManager()

	var c1, c2 atomic.Int32
	tm.RegisterTimer("t1", 10*time.Millisecond,
		func(p interface{}) { c1.Add(1) }, nil)
	tm.RegisterTimer("t2", 10*time.Millisecond,
		func(p interface{}) { c2.Add(1) }, nil)

	time.Sleep(50 * time.Millisecond)
	tm.StopAll()

	if tm.Count() != 0 {
		t.Errorf("expected 0 timers after StopAll, got %d", tm.Count())
	}

	b1, b2 := c1.Load(), c2.Load()
	time.Sleep(50 * time.Millisecond)
	if c1.Load() != b1 || c2.Load() != b2 {
		t.Error("timers should not fire after StopAll")
	}
}

func TestGetTimer(t *testing.T) {
	tm := NewTimerManager()
	defer tm.StopAll()

	tm.RegisterTimer("foo", 100*time.Millisecond,
		func(p interface{}) {}, nil)
	tm.RegisterTimer("bar", 100*time.Millisecond,
		func(p interface{}) {}, nil)

	if tm.GetTimer("foo") == nil {
		t.Error("expected to find timer 'foo'")
	}
	if tm.GetTimer("bar") == nil {
		t.Error("expected to find timer 'bar'")
	}
	if tm.GetTimer("missing") != nil {
		t.Error("expected nil for missing timer")
	}
}

func TestListTimers(t *testing.T) {
	tm := NewTimerManager()
	defer tm.StopAll()

	tm.RegisterTimer("a", 100*time.Millisecond, func(p interface{}) {}, nil)
	tm.RegisterTimer("b", 100*time.Millisecond, func(p interface{}) {}, nil)
	tm.RegisterTimer("c", 100*time.Millisecond, func(p interface{}) {}, nil)

	list := tm.ListTimers()
	if len(list) != 3 {
		t.Errorf("expected 3 timers, got %d", len(list))
	}

	names := make(map[string]bool)
	for _, tmr := range list {
		names[tmr.Name] = true
	}
	for _, n := range []string{"a", "b", "c"} {
		if !names[n] {
			t.Errorf("expected timer %q in list", n)
		}
	}
}

func TestConcurrentAccess(t *testing.T) {
	tm := NewTimerManager()
	defer tm.StopAll()

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("t-%d", i)
			tm.RegisterTimer(name, 50*time.Millisecond,
				func(p interface{}) {}, nil)
			tm.GetTimer(name)
			tm.ListTimers()
			tm.Count()
		}(i)
	}
	wg.Wait()

	if tm.Count() != n {
		t.Errorf("expected %d timers, got %d", n, tm.Count())
	}
}
