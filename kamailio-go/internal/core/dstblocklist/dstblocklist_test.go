// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go — dstblocklist unit tests.
 */

package dstblocklist

import (
	"sync"
	"testing"
	"time"
)

// newTestBlocklist returns a Blocklist with no background goroutine so
// tests can drive Cleanup explicitly and avoid leaking goroutines.
func newTestBlocklist() *Blocklist {
	return NewBlocklist(0)
}

// TestAdd verifies that Add inserts an entry that IsBlocked sees and
// that Get returns the stored details.
func TestAdd(t *testing.T) {
	bl := newTestBlocklist()
	defer bl.Close()

	bl.Add("10.0.0.1", 5060, "send failed", 0)
	if !bl.IsBlocked("10.0.0.1", 5060) {
		t.Fatal("expected 10.0.0.1:5060 to be blocked after Add")
	}
	e := bl.Get("10.0.0.1", 5060)
	if e == nil {
		t.Fatal("expected non-nil entry from Get")
	}
	if e.IP != "10.0.0.1" || e.Port != 5060 {
		t.Fatalf("unexpected ip/port: %s:%d", e.IP, e.Port)
	}
	if e.Reason != "send failed" {
		t.Fatalf("expected reason 'send failed', got %q", e.Reason)
	}
	if !e.ExpiresAt.IsZero() {
		t.Fatalf("expected permanent entry (zero ExpiresAt), got %v", e.ExpiresAt)
	}

	// A different port must not be blocked.
	if bl.IsBlocked("10.0.0.1", 5061) {
		t.Fatal("expected 10.0.0.1:5061 to NOT be blocked")
	}

	// Re-adding refreshes reason while preserving HitCount.
	bl.Hit("10.0.0.1", 5060)
	bl.Add("10.0.0.1", 5060, "timeout", 0)
	e = bl.Get("10.0.0.1", 5060)
	if e == nil {
		t.Fatal("expected entry after re-add")
	}
	if e.Reason != "timeout" {
		t.Fatalf("expected refreshed reason 'timeout', got %q", e.Reason)
	}
	if e.HitCount != 1 {
		t.Fatalf("expected HitCount preserved at 1, got %d", e.HitCount)
	}
}

// TestRemove verifies that Remove deletes an entry and reports whether
// something was removed.
func TestRemove(t *testing.T) {
	bl := newTestBlocklist()
	defer bl.Close()

	bl.Add("10.0.0.2", 5060, "icmp", 0)
	if !bl.IsBlocked("10.0.0.2", 5060) {
		t.Fatal("expected blocked before Remove")
	}
	if !bl.Remove("10.0.0.2", 5060) {
		t.Fatal("expected Remove to return true for existing entry")
	}
	if bl.IsBlocked("10.0.0.2", 5060) {
		t.Fatal("expected NOT blocked after Remove")
	}
	if bl.Remove("10.0.0.2", 5060) {
		t.Fatal("expected Remove to return false for missing entry")
	}
}

// TestIsBlocked verifies blocking semantics including expiry and port
// isolation.
func TestIsBlocked(t *testing.T) {
	bl := newTestBlocklist()
	defer bl.Close()

	bl.Add("10.0.0.3", 5060, "503", 0)
	if !bl.IsBlocked("10.0.0.3", 5060) {
		t.Fatal("expected blocked")
	}
	if bl.IsBlocked("10.0.0.3", 5061) {
		t.Fatal("expected different port not blocked")
	}
	if bl.IsBlocked("10.0.0.4", 5060) {
		t.Fatal("expected different ip not blocked")
	}

	// Expired entry must read as not blocked.
	bl.Add("10.0.0.5", 5060, "temp", 20*time.Millisecond)
	time.Sleep(60 * time.Millisecond)
	if bl.IsBlocked("10.0.0.5", 5060) {
		t.Fatal("expected expired entry to read as not blocked")
	}
}

// TestHit verifies that Hit increments the counter and reports blocked
// status, and that hitting a non-blocked destination returns false.
func TestHit(t *testing.T) {
	bl := newTestBlocklist()
	defer bl.Close()

	bl.Add("10.0.0.6", 5060, "connect failed", 0)
	for i := 1; i <= 3; i++ {
		if !bl.Hit("10.0.0.6", 5060) {
			t.Fatalf("hit %d: expected blocked=true", i)
		}
	}
	e := bl.Get("10.0.0.6", 5060)
	if e == nil {
		t.Fatal("expected entry")
	}
	if e.HitCount != 3 {
		t.Fatalf("expected HitCount=3, got %d", e.HitCount)
	}

	// Hitting an unknown destination returns false and records nothing.
	if bl.Hit("10.0.0.99", 5060) {
		t.Fatal("expected Hit on unknown destination to return false")
	}
	if bl.Get("10.0.0.99", 5060) != nil {
		t.Fatal("expected no entry created by Hit on unknown destination")
	}

	// Hitting an expired entry returns false and lazily removes it.
	bl.Add("10.0.0.7", 5060, "temp", 20*time.Millisecond)
	time.Sleep(60 * time.Millisecond)
	if bl.Hit("10.0.0.7", 5060) {
		t.Fatal("expected Hit on expired entry to return false")
	}
	if bl.Count() != 1 {
		t.Fatalf("expected expired entry lazily removed, count=1, got %d", bl.Count())
	}
}

// TestCleanup verifies that Cleanup removes expired entries while
// keeping permanent and still-valid ones.
func TestCleanup(t *testing.T) {
	bl := newTestBlocklist()
	defer bl.Close()

	bl.Add("10.0.0.8", 5060, "permanent", 0)              // never expires
	bl.Add("10.0.0.9", 5060, "short", 30*time.Millisecond) // expires soon
	bl.Add("10.0.0.10", 5060, "long", time.Hour)           // far future

	if got := bl.Count(); got != 3 {
		t.Fatalf("expected count=3 before cleanup, got %d", got)
	}

	time.Sleep(80 * time.Millisecond)
	bl.Cleanup()

	if got := bl.Count(); got != 2 {
		t.Fatalf("expected count=2 after cleanup, got %d", got)
	}
	if !bl.IsBlocked("10.0.0.8", 5060) {
		t.Fatal("expected permanent entry to survive cleanup")
	}
	if !bl.IsBlocked("10.0.0.10", 5060) {
		t.Fatal("expected long-TTL entry to survive cleanup")
	}
	if bl.IsBlocked("10.0.0.9", 5060) {
		t.Fatal("expected short-TTL entry to be removed by cleanup")
	}
}

// TestCount verifies that Count reflects adds and removes.
func TestCount(t *testing.T) {
	bl := newTestBlocklist()
	defer bl.Close()

	if got := bl.Count(); got != 0 {
		t.Fatalf("expected count=0, got %d", got)
	}
	for i := 0; i < 5; i++ {
		bl.Add("10.1.0.0", 5000+i, "r", 0)
	}
	if got := bl.Count(); got != 5 {
		t.Fatalf("expected count=5, got %d", got)
	}
	bl.Remove("10.1.0.0", 5000)
	if got := bl.Count(); got != 4 {
		t.Fatalf("expected count=4 after remove, got %d", got)
	}
}

// TestList verifies that List returns a sorted snapshot of non-expired
// entries as independent copies.
func TestList(t *testing.T) {
	bl := newTestBlocklist()
	defer bl.Close()

	bl.Add("10.2.0.3", 5060, "c", 0)
	bl.Add("10.2.0.1", 5060, "a", 0)
	bl.Add("10.2.0.2", 5061, "b", 0)
	bl.Add("10.2.0.4", 5060, "exp", 20*time.Millisecond)

	time.Sleep(60 * time.Millisecond)
	out := bl.List()
	if len(out) != 3 {
		t.Fatalf("expected 3 non-expired entries, got %d", len(out))
	}
	// Sorted by IP then port.
	want := []struct {
		ip   string
		port int
	}{
		{"10.2.0.1", 5060},
		{"10.2.0.2", 5061},
		{"10.2.0.3", 5060},
	}
	for i, w := range want {
		if out[i].IP != w.ip || out[i].Port != w.port {
			t.Fatalf("entry %d: expected %s:%d, got %s:%d", i, w.ip, w.port, out[i].IP, out[i].Port)
		}
	}

	// Mutating a returned entry must not affect the blocklist.
	out[0].Reason = "tampered"
	if e := bl.Get(out[0].IP, out[0].Port); e != nil && e.Reason == "tampered" {
		t.Fatal("List must return copies, not live pointers")
	}
}

// TestClear verifies that Clear empties the blocklist.
func TestClear(t *testing.T) {
	bl := newTestBlocklist()
	defer bl.Close()

	for i := 0; i < 4; i++ {
		bl.Add("10.3.0.0", 6000+i, "r", 0)
	}
	if got := bl.Count(); got != 4 {
		t.Fatalf("expected count=4 before clear, got %d", got)
	}
	bl.Clear()
	if got := bl.Count(); got != 0 {
		t.Fatalf("expected count=0 after clear, got %d", got)
	}
	if bl.IsBlocked("10.3.0.0", 6000) {
		t.Fatal("expected not blocked after clear")
	}
}

// TestManager verifies blocklist creation, lookup, naming and removal.
func TestManager(t *testing.T) {
	m := NewBlocklistManager(0)
	defer m.Close()

	if got := m.GetBlocklist("missing"); got != nil {
		t.Fatal("expected nil for missing blocklist")
	}
	foo := m.CreateBlocklist("foo")
	if foo == nil {
		t.Fatal("expected non-nil blocklist from CreateBlocklist")
	}
	// Creating again returns the same instance.
	if m.CreateBlocklist("foo") != foo {
		t.Fatal("expected CreateBlocklist to be idempotent")
	}
	m.CreateBlocklist("bar")

	if got := m.GetBlocklist("foo"); got != foo {
		t.Fatal("expected GetBlocklist to return the created instance")
	}

	names := m.Names()
	if len(names) != 2 || names[0] != "bar" || names[1] != "foo" {
		t.Fatalf("expected names [bar foo], got %v", names)
	}

	// Entries are isolated per blocklist.
	foo.Add("10.4.0.1", 5060, "x", 0)
	if !foo.IsBlocked("10.4.0.1", 5060) {
		t.Fatal("expected foo to contain the entry")
	}
	bar := m.GetBlocklist("bar")
	if bar.IsBlocked("10.4.0.1", 5060) {
		t.Fatal("expected bar to be isolated from foo")
	}

	m.RemoveBlocklist("foo")
	if m.GetBlocklist("foo") != nil {
		t.Fatal("expected foo to be gone after RemoveBlocklist")
	}
	names = m.Names()
	if len(names) != 1 || names[0] != "bar" {
		t.Fatalf("expected names [bar] after removal, got %v", names)
	}
}

// TestConcurrentAccess exercises Add/IsBlocked/Hit/Remove/Cleanup/List
// concurrently to verify thread safety under -race.
func TestConcurrentAccess(t *testing.T) {
	bl := newTestBlocklist()
	defer bl.Close()

	const goroutines = 16
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				ip := "10.5.0." + itoaByte(g%4)
				port := 5000 + (i % 5)
				switch i % 6 {
				case 0:
					bl.Add(ip, port, "r", 0)
				case 1:
					bl.IsBlocked(ip, port)
				case 2:
					bl.Hit(ip, port)
				case 3:
					bl.Remove(ip, port)
				case 4:
					bl.Cleanup()
				case 5:
					_ = bl.List()
				}
			}
		}()
	}
	wg.Wait()

	// After all the churn the blocklist must still be usable and consistent.
	total := bl.Count()
	if total < 0 {
		t.Fatalf("expected non-negative count, got %d", total)
	}
	listed := bl.List()
	if len(listed) > total {
		t.Fatalf("List (%d) must not exceed Count (%d)", len(listed), total)
	}
}

// itoaByte returns the single-digit decimal string for n (0..9).
func itoaByte(n int) string {
	if n < 0 || n > 9 {
		return "0"
	}
	return string(rune('0' + n))
}
