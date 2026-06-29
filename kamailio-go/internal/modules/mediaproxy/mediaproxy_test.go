// SPDX-License-Identifier-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - MediaProxy module tests.
 */

package mediaproxy

import (
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

var testInvite = []byte("INVITE sip:bob@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.example.com;branch=z9hG4bK776\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1928301774\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: mp-call-1@pc33.example.com\r\n" +
	"CSeq: 1 INVITE\r\n" +
	"Content-Length: 0\r\n\r\n")

func mustParse(t *testing.T, b []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(b)
	if err != nil {
		t.Fatalf("ParseMsg failed: %v", err)
	}
	return msg
}

func TestOfferAnswerDelete(t *testing.T) {
	m := New()
	msg := mustParse(t, testInvite)
	id, err := m.Offer(msg)
	if err != nil {
		t.Fatalf("Offer: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty session id")
	}
	if m.Count() != 1 {
		t.Fatalf("Count = %d, want 1", m.Count())
	}
	id2, err := m.Answer(msg)
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if id2 != id {
		t.Fatalf("Answer id %q != Offer id %q", id2, id)
	}
	if err := m.Delete("mp-call-1@pc33.example.com"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if m.Count() != 0 {
		t.Fatalf("Count after delete = %d, want 0", m.Count())
	}
}

func TestOfferErrors(t *testing.T) {
	m := New()
	if _, err := m.Offer(nil); err == nil {
		t.Fatal("expected error for nil message")
	}
	noCallID := mustParse(t, []byte("INVITE sip:bob@example.com SIP/2.0\r\n"+
		"Via: SIP/2.0/UDP pc33;branch=z9hG4bK1\r\nContent-Length: 0\r\n\r\n"))
	if _, err := m.Offer(noCallID); err == nil {
		t.Fatal("expected error for missing Call-ID")
	}
	m.SetUp(false)
	if m.Ping() {
		t.Fatal("expected Ping false when down")
	}
	msg := mustParse(t, testInvite)
	if _, err := m.Offer(msg); err == nil {
		t.Fatal("expected error when proxy is down")
	}
	m.SetUp(true)
	if !m.Ping() {
		t.Fatal("expected Ping true when up")
	}
}

func TestAnswerNoSession(t *testing.T) {
	m := New()
	msg := mustParse(t, testInvite)
	if _, err := m.Answer(msg); err == nil {
		t.Fatal("expected error for Answer without Offer")
	}
	if err := m.Delete("unknown"); err == nil {
		t.Fatal("expected error for Delete unknown session")
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	if DefaultMediaProxy() == nil {
		t.Fatal("expected non-nil default")
	}
	if !DefaultMediaProxy().Ping() {
		t.Fatal("expected default Ping true")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	msg := mustParse(t, testInvite)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.Offer(msg)
			_ = m.Ping()
			_ = m.Count()
		}()
	}
	wg.Wait()
}
