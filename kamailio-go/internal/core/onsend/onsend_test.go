// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for onsend package - matching C onsend.c
 */

package onsend

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// makeMsg builds a minimal SIPMsg suitable for hook execution.
func makeMsg() *parser.SIPMsg {
	return &parser.SIPMsg{}
}

// makeAddr returns a destination address suitable for hook execution.
func makeAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5060}
}

// TestRegister verifies that registering a hook returns a unique,
// positive ID and that Count reflects the registration.
func TestRegister(t *testing.T) {
	m := NewOnSendManager()

	id1 := m.Register(func(msg *parser.SIPMsg, dst net.Addr, isRequest bool) int {
		return 1
	})
	if id1 <= 0 {
		t.Fatalf("first Register returned non-positive id %d", id1)
	}

	id2 := m.Register(func(msg *parser.SIPMsg, dst net.Addr, isRequest bool) int {
		return 1
	})
	if id2 <= 0 {
		t.Fatalf("second Register returned non-positive id %d", id2)
	}
	if id1 == id2 {
		t.Fatalf("Register returned duplicate ids: %d", id1)
	}

	if got := m.Count(); got != 2 {
		t.Fatalf("Count after 2 registers = %d, want 2", got)
	}

	// Registering a nil hook must fail and return -1 without
	// changing the count.
	badID := m.Register(nil)
	if badID != -1 {
		t.Fatalf("Register(nil) returned %d, want -1", badID)
	}
	if got := m.Count(); got != 2 {
		t.Fatalf("Count after failed register = %d, want 2", got)
	}
}

// TestUnregister verifies that Unregister removes a hook by ID and
// that unregistering an unknown ID is a safe no-op.
func TestUnregister(t *testing.T) {
	m := NewOnSendManager()

	var hits int64
	id1 := m.Register(func(msg *parser.SIPMsg, dst net.Addr, isRequest bool) int {
		atomic.AddInt64(&hits, 1)
		return 1
	})
	id2 := m.Register(func(msg *parser.SIPMsg, dst net.Addr, isRequest bool) int {
		atomic.AddInt64(&hits, 1)
		return 1
	})

	if got := m.Count(); got != 2 {
		t.Fatalf("Count = %d, want 2", got)
	}

	m.Unregister(id1)
	if got := m.Count(); got != 1 {
		t.Fatalf("Count after unregister id1 = %d, want 1", got)
	}

	// Remaining hook must still execute and allow.
	ok := m.Execute(makeMsg(), makeAddr(), true)
	if !ok {
		t.Fatalf("Execute after unregister id1 = false, want true")
	}
	if hits != 1 {
		t.Fatalf("remaining hook hit %d times, want 1", hits)
	}

	m.Unregister(id2)
	if got := m.Count(); got != 0 {
		t.Fatalf("Count after unregister id2 = %d, want 0", got)
	}

	// Unregistering an already-removed or unknown id is a no-op.
	m.Unregister(id1)
	m.Unregister(99999)
}

// TestExecuteAllow verifies that Execute returns true when every
// registered hook allows sending (returns 1).
func TestExecuteAllow(t *testing.T) {
	m := NewOnSendManager()

	m.Register(func(msg *parser.SIPMsg, dst net.Addr, isRequest bool) int {
		return 1
	})
	m.Register(func(msg *parser.SIPMsg, dst net.Addr, isRequest bool) int {
		return 1
	})
	m.Register(func(msg *parser.SIPMsg, dst net.Addr, isRequest bool) int {
		return 1
	})

	if ok := m.Execute(makeMsg(), makeAddr(), true); !ok {
		t.Fatalf("Execute = false, want true (all hooks allow)")
	}
}

// TestExecuteBlock verifies that a single hook returning 0 blocks
// sending and short-circuits the remaining hooks.
func TestExecuteBlock(t *testing.T) {
	m := NewOnSendManager()

	var ranAfterBlock int64
	m.Register(func(msg *parser.SIPMsg, dst net.Addr, isRequest bool) int {
		return 1
	})
	m.Register(func(msg *parser.SIPMsg, dst net.Addr, isRequest bool) int {
		return 0 // block
	})
	m.Register(func(msg *parser.SIPMsg, dst net.Addr, isRequest bool) int {
		atomic.AddInt64(&ranAfterBlock, 1)
		return 1
	})

	if ok := m.Execute(makeMsg(), makeAddr(), true); ok {
		t.Fatalf("Execute = true, want false (a hook blocked)")
	}
	if ranAfterBlock != 0 {
		t.Fatalf("hook after blocking hook ran %d times, want 0 (short-circuit)", ranAfterBlock)
	}
}

// TestCount verifies Count across registrations and unregistrations.
func TestCount(t *testing.T) {
	m := NewOnSendManager()

	if got := m.Count(); got != 0 {
		t.Fatalf("initial Count = %d, want 0", got)
	}

	allow := func(msg *parser.SIPMsg, dst net.Addr, isRequest bool) int { return 1 }
	id1 := m.Register(allow)
	id2 := m.Register(allow)
	id3 := m.Register(allow)

	if got := m.Count(); got != 3 {
		t.Fatalf("Count = %d, want 3", got)
	}

	m.Unregister(id2)
	if got := m.Count(); got != 2 {
		t.Fatalf("Count after unregister = %d, want 2", got)
	}

	m.Unregister(id1)
	m.Unregister(id3)
	if got := m.Count(); got != 0 {
		t.Fatalf("Count after unregister all = %d, want 0", got)
	}
}

// TestClear verifies that Clear removes all registered hooks.
func TestClear(t *testing.T) {
	m := NewOnSendManager()

	m.Register(func(msg *parser.SIPMsg, dst net.Addr, isRequest bool) int { return 1 })
	m.Register(func(msg *parser.SIPMsg, dst net.Addr, isRequest bool) int { return 1 })
	m.Register(func(msg *parser.SIPMsg, dst net.Addr, isRequest bool) int { return 1 })

	if got := m.Count(); got != 3 {
		t.Fatalf("Count before Clear = %d, want 3", got)
	}

	m.Clear()

	if got := m.Count(); got != 0 {
		t.Fatalf("Count after Clear = %d, want 0", got)
	}

	// After Clear, Execute must allow (no hooks to block).
	if ok := m.Execute(makeMsg(), makeAddr(), true); !ok {
		t.Fatalf("Execute after Clear = false, want true")
	}

	// Clearing an already-empty manager is a no-op.
	m.Clear()
}

// TestConcurrentExecute exercises the manager under concurrent
// registration, execution and unregistration to validate the race-free
// locking (run with -race).
func TestConcurrentExecute(t *testing.T) {
	m := NewOnSendManager()

	var executed int64

	allow := func(msg *parser.SIPMsg, dst net.Addr, isRequest bool) int {
		atomic.AddInt64(&executed, 1)
		return 1
	}

	// Pre-populate so Execute always has something to run.
	for i := 0; i < 10; i++ {
		m.Register(allow)
	}

	var wg sync.WaitGroup
	const goroutines = 50

	// Writers: register and unregister.
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			id := m.Register(allow)
			m.Execute(makeMsg(), makeAddr(), true)
			m.Unregister(id)
		}()
	}

	// Readers: execute and count concurrently.
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			m.Execute(makeMsg(), makeAddr(), false)
			m.Count()
		}()
	}

	wg.Wait()

	// The manager must remain usable after the storm.
	m.Clear()
	if got := m.Count(); got != 0 {
		t.Errorf("Count after Clear = %d, want 0", got)
	}
	if executed <= 0 {
		t.Error("no hooks executed during concurrent run")
	}
}
