// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - BLST module tests.
 */

package blst

import (
	"sync"
	"testing"
	"time"
)

// TestAddGetIsBlocked verifies insertion, retrieval and blocking status.
func TestAddGetIsBlocked(t *testing.T) {
	m := New()
	m.Add("10.0.0.1", 5060, "INVITE", "too many 5xx", 5*time.Minute)

	if !m.IsBlocked("10.0.0.1", 5060, "INVITE") {
		t.Fatal("expected blocked")
	}
	if m.IsBlocked("10.0.0.2", 5060, "INVITE") {
		t.Fatal("expected not blocked for unknown ip")
	}
	e := m.Get("10.0.0.1", 5060)
	if e == nil {
		t.Fatal("expected entry")
	}
	if e.Reason != "too many 5xx" {
		t.Fatalf("unexpected reason: %q", e.Reason)
	}
	if e.Method != "INVITE" {
		t.Fatalf("unexpected method: %q", e.Method)
	}
}

// TestRemove verifies entry removal.
func TestRemove(t *testing.T) {
	m := New()
	m.Add("10.0.0.1", 5060, "INVITE", "x", 5*time.Minute)
	if !m.Remove("10.0.0.1", 5060) {
		t.Fatal("expected Remove true")
	}
	if m.IsBlocked("10.0.0.1", 5060, "INVITE") {
		t.Fatal("expected not blocked after remove")
	}
	if m.Remove("10.0.0.1", 5060) {
		t.Fatal("expected Remove false for unknown entry")
	}
}

// TestHit verifies hit counting and return value.
func TestHit(t *testing.T) {
	m := New()
	m.Add("10.0.0.1", 5060, "INVITE", "x", 5*time.Minute)

	if !m.Hit("10.0.0.1", 5060, "INVITE") {
		t.Fatal("expected Hit true for blocked entry")
	}
	if !m.Hit("10.0.0.1", 5060, "INVITE") {
		t.Fatal("expected Hit true again")
	}
	e := m.Get("10.0.0.1", 5060)
	if e.HitCount != 2 {
		t.Fatalf("expected HitCount 2, got %d", e.HitCount)
	}
	if m.Hit("10.0.0.9", 5060, "INVITE") {
		t.Fatal("expected Hit false for unknown entry")
	}
}

// TestExpiryAndCleanup verifies TTL expiry and Cleanup.
func TestExpiryAndCleanup(t *testing.T) {
	m := New()
	m.Add("10.0.0.1", 5060, "INVITE", "x", 50*time.Millisecond)
	m.Add("10.0.0.2", 5060, "INVITE", "perm", 0) // permanent

	if !m.IsBlocked("10.0.0.1", 5060, "INVITE") {
		t.Fatal("expected blocked before expiry")
	}
	time.Sleep(80 * time.Millisecond)
	if m.IsBlocked("10.0.0.1", 5060, "INVITE") {
		t.Fatal("expected not blocked after expiry")
	}
	if !m.IsBlocked("10.0.0.2", 5060, "INVITE") {
		t.Fatal("expected permanent entry still blocked")
	}

	// Expired entry should still be counted until Cleanup/Get sweeps it.
	if m.Count() != 2 {
		t.Fatalf("expected count 2 before cleanup, got %d", m.Count())
	}
	m.Cleanup()
	if m.Count() != 1 {
		t.Fatalf("expected count 1 after cleanup, got %d", m.Count())
	}
	// Get lazily removes expired entries.
	m.Add("10.0.0.3", 5060, "INVITE", "x", 30*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	if m.Get("10.0.0.3", 5060) != nil {
		t.Fatal("expected nil for expired entry via Get")
	}
}

// TestSetIgnoreMethod verifies that ignored methods bypass blocking.
func TestSetIgnoreMethod(t *testing.T) {
	m := New()
	m.Add("10.0.0.1", 5060, "INVITE", "x", 5*time.Minute)
	m.SetIgnoreMethod("REGISTER")

	if m.IsBlocked("10.0.0.1", 5060, "REGISTER") {
		t.Fatal("expected REGISTER ignored (not blocked)")
	}
	if m.Hit("10.0.0.1", 5060, "REGISTER") {
		t.Fatal("expected REGISTER ignored (no hit)")
	}
	// Case-insensitive.
	m.SetIgnoreMethod("invite")
	if m.IsBlocked("10.0.0.1", 5060, "INVITE") {
		t.Fatal("expected INVITE ignored after SetIgnoreMethod")
	}
	// Other methods still blocked.
	if !m.IsBlocked("10.0.0.1", 5060, "OPTIONS") {
		t.Fatal("expected OPTIONS still blocked")
	}
}

// TestListCountClear verifies listing, counting and clearing.
func TestListCountClear(t *testing.T) {
	m := New()
	m.Add("10.0.0.2", 5060, "INVITE", "b", 5*time.Minute)
	m.Add("10.0.0.1", 5060, "INVITE", "a", 5*time.Minute)
	m.Add("10.0.0.1", 5062, "INVITE", "c", 5*time.Minute)

	if m.Count() != 3 {
		t.Fatalf("expected count 3, got %d", m.Count())
	}
	list := m.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(list))
	}
	// Sorted by IP then port.
	if list[0].IP != "10.0.0.1" || list[0].Port != 5060 {
		t.Fatalf("unexpected first entry: %+v", list[0])
	}
	if list[1].IP != "10.0.0.1" || list[1].Port != 5062 {
		t.Fatalf("unexpected second entry: %+v", list[1])
	}
	if list[2].IP != "10.0.0.2" {
		t.Fatalf("unexpected third entry: %+v", list[2])
	}
	// Mutating a returned entry must not affect the module.
	list[0].Reason = "mutated"
	if m.Get("10.0.0.1", 5060).Reason != "a" {
		t.Fatal("expected isolation from List copy")
	}
	m.Clear()
	if m.Count() != 0 {
		t.Fatalf("expected count 0 after Clear, got %d", m.Count())
	}
}

// TestReAddPreservesHitCount verifies that re-adding refreshes metadata
// but keeps the accumulated hit count.
func TestReAddPreservesHitCount(t *testing.T) {
	m := New()
	m.Add("10.0.0.1", 5060, "INVITE", "first", 5*time.Minute)
	m.Hit("10.0.0.1", 5060, "INVITE")
	m.Hit("10.0.0.1", 5060, "INVITE")
	m.Add("10.0.0.1", 5060, "OPTIONS", "second", 5*time.Minute)
	e := m.Get("10.0.0.1", 5060)
	if e.Reason != "second" {
		t.Fatalf("expected refreshed reason, got %q", e.Reason)
	}
	if e.Method != "OPTIONS" {
		t.Fatalf("expected refreshed method, got %q", e.Method)
	}
	if e.HitCount != 2 {
		t.Fatalf("expected preserved HitCount 2, got %d", e.HitCount)
	}
}

// TestGlobalFunctions exercises the package-level API.
func TestGlobalFunctions(t *testing.T) {
	Init()
	m := DefaultBLST()
	if m == nil {
		t.Fatal("expected non-nil default module")
	}
	Add("10.0.0.5", 5060, "INVITE", "global", 5*time.Minute)
	if !IsBlocked("10.0.0.5", 5060, "INVITE") {
		t.Fatal("expected blocked via global API")
	}
	if !Hit("10.0.0.5", 5060, "INVITE") {
		t.Fatal("expected Hit true via global API")
	}
}

// TestConcurrentAccess exercises the module under the race detector.
func TestConcurrentAccess(t *testing.T) {
	m := New()
	m.SetIgnoreMethod("REGISTER")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ip := "10.0.0." + string(rune('0'+i%10))
			m.Add(ip, 5060, "INVITE", "x", 5*time.Minute)
			m.IsBlocked(ip, 5060, "INVITE")
			m.Hit(ip, 5060, "INVITE")
			m.Get(ip, 5060)
			m.List()
			_ = m.Count()
		}(i)
	}
	wg.Wait()
}
