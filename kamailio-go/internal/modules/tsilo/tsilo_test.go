// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the Transaction silo (tsilo) module.
 */

package tsilo

import (
	"sync"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// buildMsg constructs a SIP message with Call-ID, From (with tag) and a body.
func buildMsg(callID, fromTag, body string) *parser.SIPMsg {
	msg := &parser.SIPMsg{}
	from := msg.AddHeader("From", "<sip:alice@example.com>;tag="+fromTag)
	msg.From = from
	cid := msg.AddHeader("Call-ID", callID)
	msg.CallID = cid
	if body != "" {
		msg.Body = []byte(body)
	}
	return msg
}

func TestStore(t *testing.T) {
	m := New()

	msg := buildMsg("call-1@example.com", "ftag-1", "body-bytes")
	if ret := m.Store("sip:bob@example.com", msg); ret != 0 {
		t.Fatalf("Store() = %d, want 0", ret)
	}
	if m.Count() != 1 {
		t.Errorf("Count() = %d, want 1", m.Count())
	}
	if !m.IsStored("sip:bob@example.com") {
		t.Errorf("IsStored() = false, want true")
	}

	// nil msg -> -1.
	if ret := m.Store("sip:x", nil); ret != -1 {
		t.Errorf("Store(nil msg) = %d, want -1", ret)
	}
	// empty ruri -> -1.
	if ret := m.Store("", msg); ret != -1 {
		t.Errorf("Store(empty ruri) = %d, want -1", ret)
	}
}

func TestRetrieve(t *testing.T) {
	m := New()

	msg := buildMsg("call-2@example.com", "ftag-2", "hello")
	m.Store("sip:carol@example.com", msg)

	e, err := m.Retrieve("sip:carol@example.com")
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if e == nil {
		t.Fatalf("Retrieve() returned nil entry")
	}
	if e.RURI != "sip:carol@example.com" {
		t.Errorf("RURI = %q", e.RURI)
	}
	if e.CallID != "call-2@example.com" {
		t.Errorf("CallID = %q, want call-2@example.com", e.CallID)
	}
	if e.FromTag != "ftag-2" {
		t.Errorf("FromTag = %q, want ftag-2", e.FromTag)
	}
	if e.Body != "hello" {
		t.Errorf("Body = %q, want hello", e.Body)
	}
	if e.StoredAt.IsZero() {
		t.Errorf("StoredAt should be set")
	}

	// Unknown ruri -> error.
	if _, err := m.Retrieve("sip:nope@example.com"); err == nil {
		t.Errorf("Retrieve(unknown) should error")
	}
}

func TestDelete(t *testing.T) {
	m := New()
	m.Store("sip:dave@example.com", buildMsg("c", "t", "b"))

	if !m.Delete("sip:dave@example.com") {
		t.Fatalf("Delete() = false, want true")
	}
	if m.IsStored("sip:dave@example.com") {
		t.Errorf("IsStored() after delete = true, want false")
	}
	if m.Count() != 0 {
		t.Errorf("Count() after delete = %d, want 0", m.Count())
	}
	// Delete again -> false.
	if m.Delete("sip:dave@example.com") {
		t.Errorf("Delete() twice = true, want false")
	}
	// Delete unknown -> false.
	if m.Delete("sip:nope@example.com") {
		t.Errorf("Delete(unknown) = true, want false")
	}
}

func TestListAndCount(t *testing.T) {
	m := New()
	m.Store("sip:a@x", buildMsg("c1", "t1", "b1"))
	m.Store("sip:b@x", buildMsg("c2", "t2", "b2"))
	m.Store("sip:c@x", buildMsg("c3", "t3", "b3"))

	if got := m.Count(); got != 3 {
		t.Errorf("Count() = %d, want 3", got)
	}
	list := m.List()
	if len(list) != 3 {
		t.Fatalf("List() = %d, want 3", len(list))
	}
	ruris := make(map[string]bool)
	for _, e := range list {
		ruris[e.RURI] = true
	}
	for _, want := range []string{"sip:a@x", "sip:b@x", "sip:c@x"} {
		if !ruris[want] {
			t.Errorf("List() missing %q", want)
		}
	}
}

func TestCleanupExpired(t *testing.T) {
	m := New()

	// Store a fresh entry.
	m.Store("sip:fresh@x", buildMsg("c1", "t1", "b1"))
	// Manually insert an expired entry.
	m.mu.Lock()
	m.entries["sip:old@x"] = &TSiloEntry{
		RURI:     "sip:old@x",
		CallID:   "c2",
		FromTag:  "t2",
		Body:     "b2",
		StoredAt: time.Now().Add(-2 * time.Hour),
	}
	m.mu.Unlock()

	if m.Count() != 2 {
		t.Fatalf("Count() = %d, want 2", m.Count())
	}

	// Cleanup with a 1-hour TTL removes only the old entry.
	m.CleanupExpired(1 * time.Hour)
	if m.Count() != 1 {
		t.Fatalf("Count() after cleanup = %d, want 1", m.Count())
	}
	if !m.IsStored("sip:fresh@x") {
		t.Errorf("fresh entry should survive cleanup")
	}
	if m.IsStored("sip:old@x") {
		t.Errorf("old entry should be removed by cleanup")
	}

	// Cleanup with a negative TTL removes every entry (cutoff is in the
	// future, so every StoredAt is considered expired).
	m.CleanupExpired(-1 * time.Second)
	if m.Count() != 0 {
		t.Errorf("Count() after purge cleanup = %d, want 0", m.Count())
	}
}

func TestStoreOverwrite(t *testing.T) {
	m := New()

	m.Store("sip:eve@x", buildMsg("c1", "t1", "b1"))
	m.Store("sip:eve@x", buildMsg("c2", "t2", "b2"))

	if m.Count() != 1 {
		t.Fatalf("Count() = %d, want 1 (overwrite)", m.Count())
	}
	e, err := m.Retrieve("sip:eve@x")
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if e.CallID != "c2" {
		t.Errorf("CallID = %q, want c2 (latest)", e.CallID)
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultTSilo()
	if d == nil {
		t.Fatalf("DefaultTSilo() returned nil")
	}
	if d != DefaultTSilo() {
		t.Fatalf("DefaultTSilo() returned different instances after Init()")
	}

	// Re-init resets state.
	d.Store("sip:x@x", buildMsg("c", "t", "b"))
	if got := d.Count(); got != 1 {
		t.Fatalf("Count() = %d, want 1", got)
	}
	Init()
	if got := DefaultTSilo().Count(); got != 0 {
		t.Errorf("Count() after re-Init = %d, want 0", got)
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultTSilo()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			ruri := "sip:c" + itoa(i) + "@x"
			msg := buildMsg("c"+itoa(i), "t"+itoa(i), "b"+itoa(i))
			shared.Store(ruri, msg)
			shared.IsStored(ruri)
			shared.Retrieve(ruri)
			shared.Count()
			shared.List()
			shared.Delete(ruri)
		}(i)
	}
	wg.Wait()
	if got := shared.Count(); got != 0 {
		t.Errorf("Count() after concurrent = %d, want 0", got)
	}
}

// itoa is a tiny local int->string helper to avoid pulling strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
