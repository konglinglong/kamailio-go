// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the IMS QoS module.
 */

package ims_qos

import (
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

func mustParseMsg(t *testing.T, raw []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("failed to parse message: %v", err)
	}
	return msg
}

var inviteBytes = []byte("INVITE sip:1001@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776asdhds\r\n" +
	"Max-Forwards: 70\r\n" +
	"From: Alice <sip:alice@example.com>;tag=qtag111\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: call-qos-1@10.0.0.1\r\n" +
	"CSeq: 1 INVITE\r\n" +
	"Contact: <sip:alice@10.0.0.1>\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

func TestAuthorize(t *testing.T) {
	m := NewQoSModule()
	msg := mustParseMsg(t, inviteBytes)
	s, err := m.Authorize(msg)
	if err != nil {
		t.Fatalf("Authorize failed: %v", err)
	}
	if s.CallID != "call-qos-1@10.0.0.1" {
		t.Errorf("CallID = %q", s.CallID)
	}
	if s.FromTag != "qtag111" {
		t.Errorf("FromTag = %q, want qtag111", s.FromTag)
	}
	if s.Status != StatusAuthorized {
		t.Errorf("Status = %q, want authorized", s.Status)
	}
	if len(s.MediaComponents) == 0 {
		t.Fatal("expected at least one media component")
	}
	mc := s.MediaComponents[0]
	if mc.MediaType != "audio" {
		t.Errorf("MediaType = %q, want audio", mc.MediaType)
	}
	if mc.MaxRequestedBWUL <= 0 || mc.MaxRequestedBWDL <= 0 {
		t.Errorf("bandwidths should be positive, got UL=%d DL=%d", mc.MaxRequestedBWUL, mc.MaxRequestedBWDL)
	}
	if m.Count() != 1 {
		t.Errorf("Count = %d, want 1", m.Count())
	}
}

func TestAuthorizeErrors(t *testing.T) {
	m := NewQoSModule()
	if _, err := m.Authorize(nil); err == nil {
		t.Errorf("Authorize(nil) should error")
	}
	// Message without Call-ID.
	raw := []byte("INVITE sip:1001@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776asdhds\r\n" +
		"From: Alice <sip:alice@example.com>;tag=qtag111\r\n" +
		"To: Bob <sip:bob@example.com>\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
	msg := mustParseMsg(t, raw)
	if _, err := m.Authorize(msg); err == nil {
		t.Errorf("Authorize without Call-ID should error")
	}
}

func TestAuthorizeIdempotent(t *testing.T) {
	m := NewQoSModule()
	msg := mustParseMsg(t, inviteBytes)
	s1, _ := m.Authorize(msg)
	s2, _ := m.Authorize(msg)
	if s1 != s2 {
		t.Errorf("Authorize twice should return the same session")
	}
	if m.Count() != 1 {
		t.Errorf("Count = %d, want 1", m.Count())
	}
}

func TestUpdateSession(t *testing.T) {
	m := NewQoSModule()
	msg := mustParseMsg(t, inviteBytes)
	m.Authorize(msg)

	if !m.UpdateSession("call-qos-1@10.0.0.1", "qtag111", StatusPending) {
		t.Errorf("UpdateSession returned false for existing session")
	}
	s := m.GetSession("call-qos-1@10.0.0.1", "qtag111")
	if s.Status != StatusPending {
		t.Errorf("Status = %q, want pending", s.Status)
	}
	if m.UpdateSession("nope", "nope", "x") {
		t.Errorf("UpdateSession returned true for unknown session")
	}
}

func TestRevokeSession(t *testing.T) {
	m := NewQoSModule()
	msg := mustParseMsg(t, inviteBytes)
	m.Authorize(msg)

	if err := m.RevokeSession("call-qos-1@10.0.0.1", "qtag111"); err != nil {
		t.Fatalf("RevokeSession failed: %v", err)
	}
	s := m.GetSession("call-qos-1@10.0.0.1", "qtag111")
	if s.Status != StatusRevoked {
		t.Errorf("Status = %q, want revoked", s.Status)
	}
	for _, mc := range s.MediaComponents {
		if mc.Status != StatusRevoked {
			t.Errorf("media component %d status = %q, want revoked", mc.MediaNumber, mc.Status)
		}
	}
	if err := m.RevokeSession("nope", "nope"); err == nil {
		t.Errorf("RevokeSession for unknown session should error")
	}
}

func TestGetSessionAndList(t *testing.T) {
	m := NewQoSModule()
	if s := m.GetSession("x", "y"); s != nil {
		t.Errorf("GetSession on empty module should return nil")
	}
	msg := mustParseMsg(t, inviteBytes)
	m.Authorize(msg)
	if s := m.GetSession("call-qos-1@10.0.0.1", "qtag111"); s == nil {
		t.Errorf("GetSession returned nil for existing session")
	}
	if got := len(m.List()); got != 1 {
		t.Errorf("List len = %d, want 1", got)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := NewQoSModule()
	msg := mustParseMsg(t, inviteBytes)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Authorize(msg)
			m.GetSession("call-qos-1@10.0.0.1", "qtag111")
			m.UpdateSession("call-qos-1@10.0.0.1", "qtag111", StatusAuthorized)
			m.Count()
			m.List()
		}()
	}
	wg.Wait()
	if m.Count() != 1 {
		t.Errorf("Count after concurrent access = %d, want 1", m.Count())
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	q := DefaultQoS()
	if q == nil {
		t.Fatal("expected non-nil default QoS module")
	}
	if q.Count() != 0 {
		t.Errorf("Count = %d, want 0 after Init", q.Count())
	}
}
