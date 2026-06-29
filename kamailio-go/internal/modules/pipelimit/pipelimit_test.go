// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * PipeLimit module tests - rule-based rate limiting.
 */
package pipelimit

import (
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

var inviteBytes = []byte("INVITE sip:bob@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776asdhds\r\n" +
	"Max-Forwards: 70\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1928301774\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: call-1234@10.0.0.1\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"Contact: <sip:alice@10.0.0.1>\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

func mustParseMsg(t *testing.T, raw []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("failed to parse message: %v", err)
	}
	return msg
}

func TestAddRuleAndCount(t *testing.T) {
	m := NewPipeLimitModule()
	id1 := m.AddRule(&PipeRule{Name: "r1", Limit: 5, Algorithm: "token", Enabled: true})
	id2 := m.AddRule(&PipeRule{Name: "r2", Limit: 5, Algorithm: "taildrop", Enabled: true})
	if id1 == id2 {
		t.Errorf("expected distinct ids, got %d == %d", id1, id2)
	}
	if m.Count() != 2 {
		t.Errorf("Count = %d, want 2", m.Count())
	}
	rules := m.ListRules()
	if len(rules) != 2 {
		t.Errorf("len(ListRules) = %d, want 2", len(rules))
	}
}

func TestAddRuleNil(t *testing.T) {
	m := NewPipeLimitModule()
	if id := m.AddRule(nil); id != -1 {
		t.Errorf("AddRule(nil) = %d, want -1", id)
	}
}

func TestAddRuleReplacesByName(t *testing.T) {
	m := NewPipeLimitModule()
	id1 := m.AddRule(&PipeRule{Name: "dup", Limit: 1, Enabled: true})
	id2 := m.AddRule(&PipeRule{Name: "dup", Limit: 2, Enabled: true})
	if id1 == id2 {
		t.Errorf("expected distinct ids for same-name replacement")
	}
	if m.Count() != 1 {
		t.Errorf("Count = %d, want 1 after replace", m.Count())
	}
	if m.RemoveRule(id1) {
		t.Error("expected old rule to be gone after replace")
	}
	if !m.RemoveRule(id2) {
		t.Error("expected new rule to be removable")
	}
}

func TestRemoveRule(t *testing.T) {
	m := NewPipeLimitModule()
	id := m.AddRule(&PipeRule{Name: "r", Limit: 1, Enabled: true})
	if !m.RemoveRule(id) {
		t.Error("RemoveRule returned false for existing rule")
	}
	if m.RemoveRule(id) {
		t.Error("RemoveRule returned true for already-removed rule")
	}
	if m.RemoveRule(99999) {
		t.Error("RemoveRule returned true for unknown id")
	}
}

func TestCheckTokenBucket(t *testing.T) {
	m := NewPipeLimitModule()
	m.AddRule(&PipeRule{Name: "tb", Limit: 3, Algorithm: "token", Enabled: true})
	msg := mustParseMsg(t, inviteBytes)

	for i := 0; i < 3; i++ {
		if !m.Check(msg, "tb") {
			t.Fatalf("check %d rejected, expected allowed", i)
		}
	}
	if m.Check(msg, "tb") {
		t.Error("check after limit allowed, expected rejected")
	}
	stats := m.GetStats("tb")
	if stats == nil {
		t.Fatal("expected non-nil stats")
	}
	if stats.Allowed.Load() != 3 {
		t.Errorf("Allowed = %d, want 3", stats.Allowed.Load())
	}
	if stats.Rejected.Load() != 1 {
		t.Errorf("Rejected = %d, want 1", stats.Rejected.Load())
	}
}

func TestCheckTaildrop(t *testing.T) {
	m := NewPipeLimitModule()
	m.AddRule(&PipeRule{Name: "td", Limit: 2, Algorithm: "taildrop", Enabled: true})
	msg := mustParseMsg(t, inviteBytes)

	if !m.Check(msg, "td") {
		t.Error("first check rejected")
	}
	if !m.Check(msg, "td") {
		t.Error("second check rejected")
	}
	if m.Check(msg, "td") {
		t.Error("third check allowed, expected rejected")
	}
}

func TestCheckDisabledRule(t *testing.T) {
	m := NewPipeLimitModule()
	m.AddRule(&PipeRule{Name: "off", Limit: 1, Algorithm: "token", Enabled: false})
	msg := mustParseMsg(t, inviteBytes)
	// A disabled rule always allows.
	for i := 0; i < 10; i++ {
		if !m.Check(msg, "off") {
			t.Fatalf("check %d rejected for disabled rule", i)
		}
	}
	stats := m.GetStats("off")
	if stats == nil {
		t.Fatal("expected non-nil stats")
	}
	if stats.Rejected.Load() != 0 {
		t.Errorf("Rejected = %d, want 0 for disabled rule", stats.Rejected.Load())
	}
}

func TestCheckUnknownRule(t *testing.T) {
	m := NewPipeLimitModule()
	msg := mustParseMsg(t, inviteBytes)
	// An unknown rule always allows.
	if !m.Check(msg, "nonexistent") {
		t.Error("check for unknown rule rejected")
	}
}

func TestEnableDisableRule(t *testing.T) {
	m := NewPipeLimitModule()
	id := m.AddRule(&PipeRule{Name: "r", Limit: 1, Algorithm: "token", Enabled: true})
	msg := mustParseMsg(t, inviteBytes)

	if !m.Check(msg, "r") {
		t.Error("first check rejected")
	}
	if m.Check(msg, "r") {
		t.Error("second check allowed, expected rejected")
	}
	if !m.DisableRule(id) {
		t.Error("DisableRule returned false")
	}
	// Now disabled, should always allow.
	if !m.Check(msg, "r") {
		t.Error("check after disable rejected")
	}
	if !m.EnableRule(id) {
		t.Error("EnableRule returned false")
	}
	// Re-enabling resets nothing; the bucket is still drained.
	if m.Check(msg, "r") {
		t.Error("check after re-enable allowed, expected rejected (bucket drained)")
	}
	if m.DisableRule(99999) {
		t.Error("DisableRule returned true for unknown id")
	}
	if m.EnableRule(99999) {
		t.Error("EnableRule returned true for unknown id")
	}
}

func TestGetStats(t *testing.T) {
	m := NewPipeLimitModule()
	m.AddRule(&PipeRule{Name: "r", Limit: 1, Algorithm: "token", Enabled: true})
	msg := mustParseMsg(t, inviteBytes)
	m.Check(msg, "r")
	m.Check(msg, "r") // rejected
	stats := m.GetStats("r")
	if stats == nil {
		t.Fatal("expected non-nil stats")
	}
	if stats.Allowed.Load() != 1 || stats.Rejected.Load() != 1 {
		t.Errorf("Allowed=%d Rejected=%d, want 1/1", stats.Allowed.Load(), stats.Rejected.Load())
	}
	if m.GetStats("nonexistent") != nil {
		t.Error("expected nil stats for unknown rule")
	}
}

func TestConcurrentCheck(t *testing.T) {
	m := NewPipeLimitModule()
	m.AddRule(&PipeRule{Name: "c", Limit: 100, Algorithm: "token", Enabled: true})
	msg := mustParseMsg(t, inviteBytes)
	const goroutines = 50
	const perG = 4 // 200 total, only 100 allowed
	var wg sync.WaitGroup
	wg.Add(goroutines)
	var allowed int64
	var mu sync.Mutex
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				if m.Check(msg, "c") {
					mu.Lock()
					allowed++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	if allowed > 100 {
		t.Errorf("allowed = %d, want <= 100", allowed)
	}
	stats := m.GetStats("c")
	if stats.Allowed.Load()+stats.Rejected.Load() != int64(goroutines*perG) {
		t.Errorf("Allowed+Rejected = %d, want %d", stats.Allowed.Load()+stats.Rejected.Load(), goroutines*perG)
	}
}

func TestDefaultPipeLimitAndInit(t *testing.T) {
	Init()
	d1 := DefaultPipeLimit()
	d2 := DefaultPipeLimit()
	if d1 != d2 {
		t.Error("DefaultPipeLimit returned different instances")
	}
	d1.AddRule(&PipeRule{Name: "r", Limit: 1, Enabled: true})
	if d2.Count() != 1 {
		t.Errorf("Count after add via default = %d, want 1", d2.Count())
	}
	Init()
	if DefaultPipeLimit().Count() != 0 {
		t.Error("expected reset after Init()")
	}
}
