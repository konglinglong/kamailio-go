// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SCA module tests - Shared Call Appearance.
 */
package sca

import (
	"sync"
	"testing"
)

func TestHoldRetrieveAndIsOnHold(t *testing.T) {
	m := NewSCAModule()
	if m.IsOnHold("call-1") {
		t.Fatal("expected call-1 not on hold initially")
	}
	m.Hold("call-1")
	if !m.IsOnHold("call-1") {
		t.Error("expected call-1 on hold after Hold")
	}
	m.Retrieve("call-1")
	if m.IsOnHold("call-1") {
		t.Error("expected call-1 not on hold after Retrieve")
	}
	// Retrieve on unknown call is a no-op (no panic).
	m.Retrieve("does-not-exist")
}

func TestGetActiveCallsAndCount(t *testing.T) {
	m := NewSCAModule()
	m.Hold("call-a")
	m.Hold("call-b")
	m.Hold("call-c")
	if got := m.Count(); got != 3 {
		t.Errorf("Count = %d, want 3", got)
	}
	// All three were registered on the default line.
	active := m.GetActiveCalls("default")
	if len(active) != 3 {
		t.Errorf("len(GetActiveCalls(default)) = %d, want 3", len(active))
	}
	if got := m.GetActiveCalls("unknown-line"); got != nil {
		t.Errorf("GetActiveCalls(unknown) = %v, want nil", got)
	}
}

func TestConcurrentSCA(t *testing.T) {
	m := NewSCAModule()
	var wg sync.WaitGroup
	const goroutines = 20
	const perG = 10
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(g int) {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				id := "call-" + itoa(g*perG+j)
				m.Hold(id)
				_ = m.IsOnHold(id)
				m.Retrieve(id)
			}
		}(i)
	}
	wg.Wait()
	want := goroutines * perG
	if got := m.Count(); got != want {
		t.Errorf("Count = %d, want %d", got, want)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
