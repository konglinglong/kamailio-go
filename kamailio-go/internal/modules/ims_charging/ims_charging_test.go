// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the IMS Charging module.
 */

package ims_charging

import (
	"sync"
	"testing"
	"time"

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
	"From: Alice <sip:alice@example.com>;tag=ftag111\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: call-chg-1@10.0.0.1\r\n" +
	"CSeq: 1 INVITE\r\n" +
	"Contact: <sip:alice@10.0.0.1>\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

func TestStartSession(t *testing.T) {
	m := NewChargingModule()
	msg := mustParseMsg(t, inviteBytes)
	s := m.StartSession(msg, "sip:alice@example.com", DirectionMO)
	if s == nil {
		t.Fatal("expected non-nil session")
	}
	if s.CallID != "call-chg-1@10.0.0.1" {
		t.Errorf("CallID = %q", s.CallID)
	}
	if s.FromTag != "ftag111" {
		t.Errorf("FromTag = %q, want ftag111", s.FromTag)
	}
	if s.Subscriber != "sip:alice@example.com" {
		t.Errorf("Subscriber = %q", s.Subscriber)
	}
	if s.Direction != DirectionMO {
		t.Errorf("Direction = %q, want MO", s.Direction)
	}
	if s.Status != StatusActive {
		t.Errorf("Status = %q, want active", s.Status)
	}
	if s.ID == "" || s.ChargingID == "" {
		t.Errorf("ID and ChargingID should be populated, got %q %q", s.ID, s.ChargingID)
	}
	if m.Count() != 1 {
		t.Errorf("Count = %d, want 1", m.Count())
	}
}

func TestStartSessionIdempotent(t *testing.T) {
	m := NewChargingModule()
	msg := mustParseMsg(t, inviteBytes)
	s1 := m.StartSession(msg, "sip:alice@example.com", DirectionMO)
	s2 := m.StartSession(msg, "sip:alice@example.com", DirectionMO)
	if s1 != s2 {
		t.Errorf("StartSession twice should return the same session")
	}
	if m.Count() != 1 {
		t.Errorf("Count = %d, want 1", m.Count())
	}
}

func TestStartSessionNilMsg(t *testing.T) {
	m := NewChargingModule()
	if s := m.StartSession(nil, "sub", DirectionMO); s != nil {
		t.Errorf("StartSession(nil) should return nil, got %v", s)
	}
}

func TestEndSession(t *testing.T) {
	m := NewChargingModule()
	msg := mustParseMsg(t, inviteBytes)
	m.StartSession(msg, "sip:alice@example.com", DirectionMO)

	if err := m.EndSession("call-chg-1@10.0.0.1", "ftag111"); err != nil {
		t.Fatalf("EndSession failed: %v", err)
	}
	s := m.GetSession("call-chg-1@10.0.0.1", "ftag111")
	if s == nil {
		t.Fatal("session should still exist after EndSession")
	}
	if s.Status != StatusTerminated {
		t.Errorf("Status = %q, want terminated", s.Status)
	}
	if s.EndedAt.IsZero() {
		t.Errorf("EndedAt should be set after EndSession")
	}

	if err := m.EndSession("nope", "nope"); err == nil {
		t.Errorf("EndSession for unknown session should error")
	}
}

func TestUpdateSession(t *testing.T) {
	m := NewChargingModule()
	msg := mustParseMsg(t, inviteBytes)
	m.StartSession(msg, "sip:alice@example.com", DirectionMO)

	if !m.UpdateSession("call-chg-1@10.0.0.1", "ftag111", StatusPending) {
		t.Errorf("UpdateSession returned false for existing session")
	}
	s := m.GetSession("call-chg-1@10.0.0.1", "ftag111")
	if s.Status != StatusPending {
		t.Errorf("Status = %q, want pending", s.Status)
	}
	if m.UpdateSession("nope", "nope", "x") {
		t.Errorf("UpdateSession returned true for unknown session")
	}
}

func TestCountByDirectionAndList(t *testing.T) {
	m := NewChargingModule()
	msg := mustParseMsg(t, inviteBytes)
	m.StartSession(msg, "sip:alice@example.com", DirectionMO)

	// Build a second message with a different Call-ID for an MT session.
	raw2 := []byte("INVITE sip:1002@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776asdhds\r\n" +
		"From: Carol <sip:carol@example.com>;tag=ftag222\r\n" +
		"To: Dave <sip:dave@example.com>\r\n" +
		"Call-ID: call-chg-2@10.0.0.1\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Contact: <sip:carol@10.0.0.1>\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
	msg2 := mustParseMsg(t, raw2)
	m.StartSession(msg2, "sip:carol@example.com", DirectionMT)

	if got := m.Count(); got != 2 {
		t.Errorf("Count = %d, want 2", got)
	}
	if got := m.CountByDirection(DirectionMO); got != 1 {
		t.Errorf("CountByDirection(MO) = %d, want 1", got)
	}
	if got := m.CountByDirection(DirectionMT); got != 1 {
		t.Errorf("CountByDirection(MT) = %d, want 1", got)
	}
	if got := m.CountByDirection("XX"); got != 0 {
		t.Errorf("CountByDirection(XX) = %d, want 0", got)
	}
	if got := len(m.List()); got != 2 {
		t.Errorf("List len = %d, want 2", got)
	}
}

func TestCleanupExpired(t *testing.T) {
	m := NewChargingModule()
	msg := mustParseMsg(t, inviteBytes)
	s := m.StartSession(msg, "sip:alice@example.com", DirectionMO)
	// Force the session to look old.
	s.updatedAt = time.Now().Add(-1 * time.Hour)

	m.CleanupExpired(30 * time.Minute)
	if m.Count() != 0 {
		t.Errorf("Count after cleanup = %d, want 0", m.Count())
	}
	// A session updated recently survives.
	m.StartSession(msg, "sip:alice@example.com", DirectionMO)
	m.CleanupExpired(30 * time.Minute)
	if m.Count() != 1 {
		t.Errorf("Count after cleanup of fresh = %d, want 1", m.Count())
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := NewChargingModule()
	msg := mustParseMsg(t, inviteBytes)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.StartSession(msg, "sip:alice@example.com", DirectionMO)
			m.GetSession("call-chg-1@10.0.0.1", "ftag111")
			m.Count()
			m.CountByDirection(DirectionMO)
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
	c := DefaultCharging()
	if c == nil {
		t.Fatal("expected non-nil default charging module")
	}
	if c.Count() != 0 {
		t.Errorf("Count = %d, want 0 after Init", c.Count())
	}
}
