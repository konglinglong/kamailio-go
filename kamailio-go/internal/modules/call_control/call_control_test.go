// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - call_control module tests.
 */

package call_control

import (
	"sync"
	"testing"
)

func TestStartEndAndLimit(t *testing.T) {
	m := New()
	if m.GetActiveCalls("alice") != 0 {
		t.Fatal("expected 0 active calls initially")
	}
	m.StartCall("alice", "call-1")
	m.StartCall("alice", "call-2")
	if got := m.GetActiveCalls("alice"); got != 2 {
		t.Fatalf("active = %d, want 2", got)
	}
	if !m.CheckLimit("alice", 3) {
		t.Error("CheckLimit(alice,3) = false, want true")
	}
	if m.CheckLimit("alice", 2) {
		t.Error("CheckLimit(alice,2) = true, want false")
	}
	if !m.EndCall("call-1") {
		t.Error("EndCall(call-1) = false, want true")
	}
	if got := m.GetActiveCalls("alice"); got != 1 {
		t.Fatalf("active = %d, want 1", got)
	}
}

func TestEndCallUnknown(t *testing.T) {
	m := New()
	m.StartCall("bob", "call-x")
	if m.EndCall("nonexistent") {
		t.Error("EndCall(unknown) = true, want false")
	}
	if m.EndCall("") {
		t.Error("EndCall(empty) = true, want false")
	}
	// Duplicate StartCall for same callID is a no-op.
	m.StartCall("bob", "call-x")
	if got := m.GetActiveCalls("bob"); got != 1 {
		t.Errorf("active = %d, want 1 after dup start", got)
	}
}

func TestConcurrentCalls(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	// 50 goroutines all start the same callID "c": only one should register.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.StartCall("user", "c")
		}()
	}
	// 50 goroutines start unique callIDs.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.StartCall("user", "call-"+itoa(i))
		}(i)
	}
	wg.Wait()
	// 1 (shared "c") + 50 (unique) = 51 active calls.
	if got := m.GetActiveCalls("user"); got != 51 {
		t.Errorf("active = %d, want 51", got)
	}
	// End half of the unique calls concurrently.
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.EndCall("call-" + itoa(i))
		}(i)
	}
	wg.Wait()
	if got := m.GetActiveCalls("user"); got != 26 {
		t.Errorf("active after end = %d, want 26", got)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 8)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}
