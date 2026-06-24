// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the Timer module.
 */

package timer

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartStop(t *testing.T) {
	m := New()
	var n atomic.Int64
	m.Start("t1", 2*time.Millisecond, func() { n.Add(1) })
	if !m.IsRunning("t1") {
		t.Fatal("IsRunning(t1) = false, want true")
	}
	time.Sleep(30 * time.Millisecond)
	if n.Load() < 1 {
		t.Errorf("timer fired %d times, want >= 1", n.Load())
	}
	if !m.Stop("t1") {
		t.Errorf("Stop(t1) = false, want true")
	}
	if m.IsRunning("t1") {
		t.Errorf("IsRunning(t1) after stop = true, want false")
	}
	// Stop again -> false.
	if m.Stop("t1") {
		t.Errorf("Stop(t1) twice = true, want false")
	}
	// Invalid args are ignored.
	m.Start("", 2*time.Millisecond, func() {})
	m.Start("x", 0, func() {})
	m.Start("y", 2*time.Millisecond, nil)
	if m.IsRunning("x") || m.IsRunning("y") {
		t.Errorf("invalid Start args should not register a timer")
	}
}

func TestListAndStopAll(t *testing.T) {
	m := New()
	m.Start("a", 50*time.Millisecond, func() {})
	m.Start("b", 50*time.Millisecond, func() {})
	m.Start("c", 50*time.Millisecond, func() {})
	got := m.List()
	if len(got) != 3 {
		t.Fatalf("List() = %v, want 3 entries", got)
	}
	want := []string{"a", "b", "c"}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("List()[%d] = %q, want %q", i, got[i], w)
		}
	}
	m.StopAll()
	if len(m.List()) != 0 {
		t.Errorf("List() after StopAll = %v, want empty", m.List())
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	var n atomic.Int64
	const goroutines = 30
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			name := "t" + strconv.Itoa(i)
			m.Start(name, 5*time.Millisecond, func() { n.Add(1) })
			m.IsRunning(name)
			m.List()
			m.Stop(name)
		}(i)
	}
	wg.Wait()
	if len(m.List()) != 0 {
		t.Errorf("List() after concurrent = %v, want empty", m.List())
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultTimer()
	if d == nil {
		t.Fatal("DefaultTimer() = nil")
	}
	if d != DefaultTimer() {
		t.Fatal("DefaultTimer() returned different instances")
	}
	d.Start("def", 50*time.Millisecond, func() {})
	if !d.IsRunning("def") {
		t.Error("default timer not running after Start")
	}
	Init()
	if d.IsRunning("def") {
		t.Error("Init() should reset default timers")
	}
}
