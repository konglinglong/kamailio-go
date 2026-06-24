// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the IMS Dialog module.
 */

package ims_dialog

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
	"From: Alice <sip:alice@example.com>;tag=dtag111\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: call-dlg-1@10.0.0.1\r\n" +
	"CSeq: 1 INVITE\r\n" +
	"Route: <sip:route1@example.com;lr>\r\n" +
	"Route: <sip:route2@example.com;lr>\r\n" +
	"Contact: <sip:alice@10.0.0.1>\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

func TestCreate(t *testing.T) {
	m := NewIMSDialogModule()
	msg := mustParseMsg(t, inviteBytes)
	d := m.Create(msg, DirOriginating)
	if d == nil {
		t.Fatal("expected non-nil dialog")
	}
	if d.CallID != "call-dlg-1@10.0.0.1" {
		t.Errorf("CallID = %q", d.CallID)
	}
	if d.FromTag != "dtag111" {
		t.Errorf("FromTag = %q, want dtag111", d.FromTag)
	}
	if d.Direction != DirOriginating {
		t.Errorf("Direction = %q, want originating", d.Direction)
	}
	if d.State != StateEarly {
		t.Errorf("State = %q, want early", d.State)
	}
	if d.Contact == "" {
		t.Errorf("Contact should be populated")
	}
	if len(d.RouteSet) != 2 {
		t.Errorf("RouteSet len = %d, want 2", len(d.RouteSet))
	}
	if d.RURI == "" {
		t.Errorf("RURI should be populated")
	}
	if m.Count() != 1 {
		t.Errorf("Count = %d, want 1", m.Count())
	}
}

func TestCreateIdempotent(t *testing.T) {
	m := NewIMSDialogModule()
	msg := mustParseMsg(t, inviteBytes)
	d1 := m.Create(msg, DirOriginating)
	d2 := m.Create(msg, DirOriginating)
	if d1 != d2 {
		t.Errorf("Create twice should return the same dialog")
	}
	if m.Count() != 1 {
		t.Errorf("Count = %d, want 1", m.Count())
	}
}

func TestCreateNilMsg(t *testing.T) {
	m := NewIMSDialogModule()
	if d := m.Create(nil, DirOriginating); d != nil {
		t.Errorf("Create(nil) should return nil, got %v", d)
	}
}

func TestUpdate(t *testing.T) {
	m := NewIMSDialogModule()
	msg := mustParseMsg(t, inviteBytes)
	m.Create(msg, DirOriginating)

	if !m.Update("call-dlg-1@10.0.0.1", "dtag111", StateConfirmed) {
		t.Errorf("Update returned false for existing dialog")
	}
	d := m.Get("call-dlg-1@10.0.0.1", "dtag111")
	if d.State != StateConfirmed {
		t.Errorf("State = %q, want confirmed", d.State)
	}
	if m.Update("nope", "nope", "x") {
		t.Errorf("Update returned true for unknown dialog")
	}
}

func TestDelete(t *testing.T) {
	m := NewIMSDialogModule()
	msg := mustParseMsg(t, inviteBytes)
	m.Create(msg, DirOriginating)

	if !m.Delete("call-dlg-1@10.0.0.1", "dtag111") {
		t.Errorf("Delete returned false for existing dialog")
	}
	if m.Count() != 0 {
		t.Errorf("Count after delete = %d, want 0", m.Count())
	}
	if m.Delete("call-dlg-1@10.0.0.1", "dtag111") {
		t.Errorf("Delete returned true for non-existent dialog")
	}
}

func TestCountByStateAndList(t *testing.T) {
	m := NewIMSDialogModule()
	msg := mustParseMsg(t, inviteBytes)
	m.Create(msg, DirOriginating)

	raw2 := []byte("INVITE sip:1002@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776asdhds\r\n" +
		"From: Carol <sip:carol@example.com>;tag=dtag222\r\n" +
		"To: Dave <sip:dave@example.com>\r\n" +
		"Call-ID: call-dlg-2@10.0.0.1\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Contact: <sip:carol@10.0.0.1>\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
	m.Create(mustParseMsg(t, raw2), DirTerminating)
	m.Update("call-dlg-2@10.0.0.1", "dtag222", StateConfirmed)

	if got := m.Count(); got != 2 {
		t.Errorf("Count = %d, want 2", got)
	}
	if got := m.CountByState(StateEarly); got != 1 {
		t.Errorf("CountByState(early) = %d, want 1", got)
	}
	if got := m.CountByState(StateConfirmed); got != 1 {
		t.Errorf("CountByState(confirmed) = %d, want 1", got)
	}
	if got := m.CountByState(StateTerminated); got != 0 {
		t.Errorf("CountByState(terminated) = %d, want 0", got)
	}
	if got := len(m.List()); got != 2 {
		t.Errorf("List len = %d, want 2", got)
	}
}

func TestCleanupExpired(t *testing.T) {
	m := NewIMSDialogModule()
	msg := mustParseMsg(t, inviteBytes)
	d := m.Create(msg, DirOriginating)
	d.UpdatedAt = time.Now().Add(-1 * time.Hour)

	m.CleanupExpired(30 * time.Minute)
	if m.Count() != 0 {
		t.Errorf("Count after cleanup = %d, want 0", m.Count())
	}
	// A recently updated dialog survives.
	m.Create(msg, DirOriginating)
	m.CleanupExpired(30 * time.Minute)
	if m.Count() != 1 {
		t.Errorf("Count after cleanup of fresh = %d, want 1", m.Count())
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := NewIMSDialogModule()
	msg := mustParseMsg(t, inviteBytes)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Create(msg, DirOriginating)
			m.Get("call-dlg-1@10.0.0.1", "dtag111")
			m.Update("call-dlg-1@10.0.0.1", "dtag111", StateConfirmed)
			m.Count()
			m.CountByState(StateEarly)
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
	d := DefaultIMSDialog()
	if d == nil {
		t.Fatal("expected non-nil default IMS dialog module")
	}
	if d.Count() != 0 {
		t.Errorf("Count = %d, want 0 after Init", d.Count())
	}
}
