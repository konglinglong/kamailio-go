// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * RateLimit module tests - per-key rate limiting.
 */
package ratelimit

import (
	"sync"
	"testing"
	"time"
)

func TestTokenBucketAllowsBurst(t *testing.T) {
	m := NewRateLimitModule()
	const limit = 5
	// First `limit` requests should be allowed (bucket starts full).
	for i := 0; i < limit; i++ {
		if !m.CheckTokenBucket("k1", limit, time.Second) {
			t.Fatalf("request %d rejected, expected allowed", i)
		}
	}
	// The next request should be rejected (bucket empty).
	if m.CheckTokenBucket("k1", limit, time.Second) {
		t.Error("request after burst allowed, expected rejected")
	}
}

func TestTokenBucketRefills(t *testing.T) {
	m := NewRateLimitModule()
	const limit = 2
	// Drain the bucket.
	m.CheckTokenBucket("k2", limit, time.Second)
	m.CheckTokenBucket("k2", limit, time.Second)
	if m.CheckTokenBucket("k2", limit, time.Second) {
		t.Fatal("expected rejection after draining")
	}
	// Wait long enough for one token to refill (limit/interval per second,
	// so for limit=2 interval=1s a token refills every 500ms).
	time.Sleep(600 * time.Millisecond)
	if !m.CheckTokenBucket("k2", limit, time.Second) {
		t.Error("expected allowance after refill")
	}
}

func TestTaildropRejectsAfterLimit(t *testing.T) {
	m := NewRateLimitModule()
	const limit = 3
	for i := 0; i < limit; i++ {
		if !m.CheckTaildrop("k3", limit, time.Second) {
			t.Fatalf("request %d rejected, expected allowed", i)
		}
	}
	if m.CheckTaildrop("k3", limit, time.Second) {
		t.Error("request after limit allowed, expected rejected")
	}
}

func TestTaildropWindowRollsOver(t *testing.T) {
	m := NewRateLimitModule()
	const limit = 2
	m.CheckTaildrop("k4", limit, 200*time.Millisecond)
	m.CheckTaildrop("k4", limit, 200*time.Millisecond)
	if m.CheckTaildrop("k4", limit, 200*time.Millisecond) {
		t.Fatal("expected rejection within window")
	}
	// After the window rolls over, requests are allowed again.
	time.Sleep(250 * time.Millisecond)
	if !m.CheckTaildrop("k4", limit, 200*time.Millisecond) {
		t.Error("expected allowance after window rollover")
	}
}

func TestCheckDispatchesByConfig(t *testing.T) {
	// Default config uses token bucket.
	m := NewRateLimitModule()
	if !m.Check("k5", 1, time.Second) {
		t.Error("first token-bucket check should be allowed")
	}
	if m.Check("k5", 1, time.Second) {
		t.Error("second token-bucket check should be rejected")
	}
	// Switch to taildrop.
	m.SetConfig(RateLimitConfig{Algorithm: "taildrop", Limit: 1, Interval: time.Second})
	if !m.Check("k6", 1, time.Second) {
		t.Error("first taildrop check should be allowed")
	}
	if m.Check("k6", 1, time.Second) {
		t.Error("second taildrop check should be rejected")
	}
}

func TestResetAndCount(t *testing.T) {
	m := NewRateLimitModule()
	m.CheckTaildrop("k7", 5, time.Second)
	m.CheckTaildrop("k7", 5, time.Second)
	if got := m.Count("k7"); got != 2 {
		t.Errorf("Count = %d, want 2", got)
	}
	m.Reset("k7")
	if got := m.Count("k7"); got != 0 {
		t.Errorf("Count after Reset = %d, want 0", got)
	}
}

func TestListAndClear(t *testing.T) {
	m := NewRateLimitModule()
	m.CheckTaildrop("a", 5, time.Second)
	m.CheckTaildrop("b", 5, time.Second)
	m.CheckTaildrop("b", 5, time.Second)
	list := m.List()
	if list["a"] != 1 || list["b"] != 2 {
		t.Errorf("List = %v, want a=1 b=2", list)
	}
	m.Clear()
	if got := m.List(); len(got) != 0 {
		t.Errorf("len(List) after Clear = %d, want 0", len(got))
	}
}

func TestInvalidLimit(t *testing.T) {
	m := NewRateLimitModule()
	if m.CheckTokenBucket("k", 0, time.Second) {
		t.Error("expected rejection for limit=0")
	}
	if m.CheckTokenBucket("k", -1, time.Second) {
		t.Error("expected rejection for limit<0")
	}
	if m.CheckTaildrop("k", 0, time.Second) {
		t.Error("expected rejection for limit=0")
	}
}

func TestConcurrentCheck(t *testing.T) {
	m := NewRateLimitModule()
	const limit = 100
	const goroutines = 50
	const perG = 4 // 200 total attempts, only 100 allowed
	var wg sync.WaitGroup
	wg.Add(goroutines)
	var allowed int64
	var mu sync.Mutex
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				if m.CheckTokenBucket("conc", limit, time.Second) {
					mu.Lock()
					allowed++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	if allowed > int64(limit) {
		t.Errorf("allowed = %d, want <= %d", allowed, limit)
	}
}

func TestDefaultRateLimitAndInit(t *testing.T) {
	Init()
	d1 := DefaultRateLimit()
	d2 := DefaultRateLimit()
	if d1 != d2 {
		t.Error("DefaultRateLimit returned different instances")
	}
	d1.CheckTokenBucket("k", 1, time.Second)
	if d2.Count("k") == 0 {
		t.Error("expected shared state via default")
	}
	Init()
	if got := DefaultRateLimit().List(); len(got) != 0 {
		t.Error("expected reset after Init()")
	}
}
