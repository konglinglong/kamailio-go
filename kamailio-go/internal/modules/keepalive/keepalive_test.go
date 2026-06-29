// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the Keepalive module.
 */

package keepalive

import (
	"sync"
	"testing"
	"time"
)

func TestAddTarget(t *testing.T) {
	m := New()

	tgt := m.AddTarget("sip:alice@example.com", "udp:192.0.2.1:5060")
	if tgt == nil {
		t.Fatalf("AddTarget() returned nil")
	}
	if tgt.Contact != "sip:alice@example.com" {
		t.Errorf("Contact = %q", tgt.Contact)
	}
	if tgt.Socket != "udp:192.0.2.1:5060" {
		t.Errorf("Socket = %q", tgt.Socket)
	}
	if !tgt.Active {
		t.Errorf("new target should be Active")
	}
	if tgt.Failures != 0 {
		t.Errorf("new target Failures = %d, want 0", tgt.Failures)
	}

	// Adding the same contact twice returns the existing target (upsert).
	tgt2 := m.AddTarget("sip:alice@example.com", "udp:192.0.2.2:5060")
	if tgt2 != tgt {
		t.Errorf("AddTarget() twice should return the same target")
	}
	// The socket is updated on re-add.
	if tgt.Socket != "udp:192.0.2.2:5060" {
		t.Errorf("Socket after re-add = %q, want updated", tgt.Socket)
	}
}

func TestRemoveTarget(t *testing.T) {
	m := New()

	m.AddTarget("sip:bob@example.com", "udp:192.0.2.1:5060")
	if !m.RemoveTarget("sip:bob@example.com") {
		t.Fatalf("RemoveTarget() returned false, want true")
	}
	if m.GetTarget("sip:bob@example.com") != nil {
		t.Errorf("GetTarget() after remove should return nil")
	}
	if m.Count() != 0 {
		t.Errorf("Count() after remove = %d, want 0", m.Count())
	}

	// Removing again -> false.
	if m.RemoveTarget("sip:bob@example.com") {
		t.Errorf("RemoveTarget() twice should return false")
	}
	// Removing unknown -> false.
	if m.RemoveTarget("sip:nope@example.com") {
		t.Errorf("RemoveTarget(unknown) should return false")
	}
}

func TestSendKeepalive(t *testing.T) {
	m := New()

	tgt := m.AddTarget("sip:carol@example.com", "udp:192.0.2.1:5060")
	before := tgt.LastSent
	time.Sleep(2 * time.Millisecond)
	if err := m.SendKeepalive(tgt); err != nil {
		t.Fatalf("SendKeepalive() error = %v", err)
	}
	if !tgt.LastSent.After(before) {
		t.Errorf("LastSent should advance after SendKeepalive")
	}

	// nil target -> error.
	if err := m.SendKeepalive(nil); err == nil {
		t.Errorf("SendKeepalive(nil) should error")
	}
}

func TestProcessResponse(t *testing.T) {
	m := New()

	tgt := m.AddTarget("sip:dave@example.com", "udp:192.0.2.1:5060")

	// A successful response clears failures and marks the target active.
	tgt.Failures = 2
	m.ProcessResponse("sip:dave@example.com", true)
	if tgt.Failures != 0 {
		t.Errorf("Failures after ok = %d, want 0", tgt.Failures)
	}
	if !tgt.Active {
		t.Errorf("target should be Active after ok")
	}
	if tgt.LastResponse.IsZero() {
		t.Errorf("LastResponse should be set after ProcessResponse")
	}

	// A failed response increments failures; after enough failures the
	// target is marked inactive.
	for i := 0; i < MaxFailures; i++ {
		m.ProcessResponse("sip:dave@example.com", false)
	}
	if tgt.Failures != MaxFailures {
		t.Errorf("Failures = %d, want %d", tgt.Failures, MaxFailures)
	}
	if tgt.Active {
		t.Errorf("target should be inactive after %d failures", MaxFailures)
	}

	// Unknown contact is a no-op (does not panic).
	m.ProcessResponse("sip:nope@example.com", true)
}

func TestGetAndListAndCount(t *testing.T) {
	m := New()

	m.AddTarget("sip:a@x", "udp:1.2.3.4:5060")
	m.AddTarget("sip:b@x", "udp:1.2.3.5:5060")
	m.AddTarget("sip:c@x", "udp:1.2.3.6:5060")

	if got := m.Count(); got != 3 {
		t.Errorf("Count() = %d, want 3", got)
	}
	if m.GetTarget("sip:b@x") == nil {
		t.Errorf("GetTarget(sip:b@x) returned nil")
	}
	if m.GetTarget("sip:nope@x") != nil {
		t.Errorf("GetTarget(unknown) should return nil")
	}
	list := m.ListTargets()
	if len(list) != 3 {
		t.Errorf("ListTargets() = %d, want 3", len(list))
	}

	// ActiveCount reflects the active flag.
	m.GetTarget("sip:a@x").Active = false
	if got := m.ActiveCount(); got != 2 {
		t.Errorf("ActiveCount() = %d, want 2", got)
	}
}

func TestStartStop(t *testing.T) {
	m := New()

	contact := "sip:eve@example.com"
	tgt := m.AddTarget(contact, "udp:192.0.2.1:5060")
	before := tgt.LastSent

	// Start a fast keepalive loop.
	m.Start(5 * time.Millisecond)
	defer m.Stop()

	// Wait long enough for at least one tick to fire. Reads go through
	// the lock-protected accessor to stay race-free with the loop.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if m.LastSentTime(contact).After(before) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !m.LastSentTime(contact).After(before) {
		t.Fatalf("LastSent was not updated by the keepalive loop")
	}

	m.Stop()
	// After Stop the loop must not keep updating LastSent.
	// Give a brief grace period for the stop to take effect.
	time.Sleep(50 * time.Millisecond)
	stoppedAt := m.LastSentTime(contact)
	time.Sleep(100 * time.Millisecond)
	if m.LastSentTime(contact).After(stoppedAt) {
		t.Errorf("LastSent advanced after Stop(): the loop did not stop")
	}

	// Stop is idempotent.
	m.Stop()
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultKeepalive()
	if d == nil {
		t.Fatalf("DefaultKeepalive() returned nil")
	}
	if d != DefaultKeepalive() {
		t.Fatalf("DefaultKeepalive() returned different instances after Init()")
	}

	// Package-level wrappers delegate to the default module.
	Init()
	tgt := AddTarget("sip:default@x", "udp:1.2.3.4:5060")
	if tgt == nil {
		t.Fatalf("package AddTarget() returned nil")
	}
	if got := Count(); got != 1 {
		t.Errorf("package Count() = %d, want 1", got)
	}
	if got := GetTarget("sip:default@x"); got == nil {
		t.Errorf("package GetTarget() returned nil")
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultKeepalive()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			c := "sip:c" + itoa(i) + "@x"
			shared.AddTarget(c, "udp:1.2.3.4:5060")
			shared.GetTarget(c)
			shared.Count()
			shared.ActiveCount()
			shared.ListTargets()
		}(i)
	}
	wg.Wait()
	if got := shared.Count(); got != goroutines {
		t.Errorf("Count() after concurrent adds = %d, want %d", got, goroutines)
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
