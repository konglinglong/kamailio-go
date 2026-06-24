// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - RTimer module tests.
 */

package rtimer

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestStartStopIsRunning(t *testing.T) {
	m := New()
	var count int32
	m.Start("t1", 5*time.Millisecond, func() { atomic.AddInt32(&count, 1) })
	if !m.IsRunning("t1") {
		t.Fatal("expected t1 running")
	}
	if got := m.List(); len(got) != 1 || got[0] != "t1" {
		t.Fatalf("List = %v", got)
	}
	// Wait for at least one tick.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&count) > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if atomic.LoadInt32(&count) == 0 {
		t.Fatal("expected handler to be invoked at least once")
	}
	m.Stop("t1")
	if m.IsRunning("t1") {
		t.Fatal("expected t1 not running after Stop")
	}
	if len(m.List()) != 0 {
		t.Fatalf("List after Stop = %v", m.List())
	}
}

func TestStartReplaces(t *testing.T) {
	m := New()
	var c1, c2 int32
	m.Start("dup", 5*time.Millisecond, func() { atomic.AddInt32(&c1, 1) })
	m.Start("dup", 5*time.Millisecond, func() { atomic.AddInt32(&c2, 1) })
	if len(m.List()) != 1 {
		t.Fatalf("expected single timer, got %v", m.List())
	}
	// Wait briefly so the second handler can fire.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&c2) > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if atomic.LoadInt32(&c2) == 0 {
		t.Fatal("expected replaced handler to fire")
	}
	m.Stop("dup")
}

func TestStartIgnoresInvalid(t *testing.T) {
	m := New()
	m.Start("", 5*time.Millisecond, func() {})
	m.Start("t", 0, func() {})
	m.Start("t", 5*time.Millisecond, nil)
	if len(m.List()) != 0 {
		t.Fatalf("expected no timers, got %v", m.List())
	}
}

func TestStopUnknown(t *testing.T) {
	m := New()
	m.Stop("nope") // should not panic
	if len(m.List()) != 0 {
		t.Fatal("expected no timers")
	}
}

func TestStopAll(t *testing.T) {
	m := New()
	var c int32
	m.Start("a", 5*time.Millisecond, func() { atomic.AddInt32(&c, 1) })
	m.Start("b", 5*time.Millisecond, func() { atomic.AddInt32(&c, 1) })
	m.StopAll()
	if len(m.List()) != 0 {
		t.Fatalf("expected no timers after StopAll, got %v", m.List())
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	var c int32
	Start("g", 5*time.Millisecond, func() { atomic.AddInt32(&c, 1) })
	if !IsRunning("g") {
		t.Fatal("expected global timer running")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&c) > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if atomic.LoadInt32(&c) == 0 {
		t.Fatal("expected global handler to fire")
	}
	Stop("g")
	if IsRunning("g") {
		t.Fatal("expected global timer stopped")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	var c int32
	m.Start("c", 5*time.Millisecond, func() { atomic.AddInt32(&c, 1) })
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 20; i++ {
			_ = m.IsRunning("c")
			_ = m.List()
		}
	}()
	time.Sleep(20 * time.Millisecond)
	m.Stop("c")
	<-done
}
