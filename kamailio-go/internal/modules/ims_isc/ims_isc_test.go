// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the IMS ISC module.
 */

package ims_isc

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
	"From: Alice <sip:alice@example.com>;tag=itag111\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: call-isc-1@10.0.0.1\r\n" +
	"CSeq: 1 INVITE\r\n" +
	"Contact: <sip:alice@10.0.0.1>\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

func TestAddRouteAndCount(t *testing.T) {
	m := NewISCModule()
	id1 := m.AddRoute(&ISCRoute{
		TriggerPoint: "method:INVITE",
		ASAddress:    "sip:as1@example.com",
		ASName:       "as1",
		Priority:     1,
		SessionCase:  SessionCaseOriginating,
	})
	id2 := m.AddRoute(&ISCRoute{
		TriggerPoint: "method:REGISTER",
		ASAddress:    "sip:as2@example.com",
		ASName:       "as2",
		Priority:     2,
		SessionCase:  SessionCaseOriginating,
	})
	if id1 == id2 || id1 < 0 || id2 < 0 {
		t.Errorf("expected distinct positive ids, got %d %d", id1, id2)
	}
	if got := m.Count(); got != 2 {
		t.Errorf("Count = %d, want 2", got)
	}
	if m.AddRoute(nil) != -1 {
		t.Errorf("AddRoute(nil) should return -1")
	}
}

func TestRemoveRoute(t *testing.T) {
	m := NewISCModule()
	id := m.AddRoute(&ISCRoute{TriggerPoint: "method:INVITE", ASAddress: "sip:as1@example.com", Priority: 1, SessionCase: SessionCaseOriginating})
	if !m.RemoveRoute(id) {
		t.Errorf("RemoveRoute returned false for existing route")
	}
	if m.Count() != 0 {
		t.Errorf("Count after remove = %d, want 0", m.Count())
	}
	if m.RemoveRoute(id) {
		t.Errorf("RemoveRoute returned true for non-existent route")
	}
}

func TestEvaluateEmptyTrigger(t *testing.T) {
	m := NewISCModule()
	m.AddRoute(&ISCRoute{
		TriggerPoint:    "",
		ASAddress:       "sip:as1@example.com",
		ASName:          "as1",
		DefaultHandling: DefaultHandlingContinue,
		Priority:        1,
		SessionCase:     SessionCaseOriginating,
	})
	msg := mustParseMsg(t, inviteBytes)
	r, err := m.Evaluate(msg, SessionCaseOriginating)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if r.ASAddress != "sip:as1@example.com" {
		t.Errorf("ASAddress = %q", r.ASAddress)
	}
}

func TestEvaluateMethodMatch(t *testing.T) {
	m := NewISCModule()
	// Lower priority value evaluated first; the REGISTER route should
	// not match an INVITE.
	m.AddRoute(&ISCRoute{TriggerPoint: "method:REGISTER", ASAddress: "sip:as-reg@example.com", Priority: 1, SessionCase: SessionCaseOriginating})
	m.AddRoute(&ISCRoute{TriggerPoint: "method:INVITE", ASAddress: "sip:as-inv@example.com", Priority: 2, SessionCase: SessionCaseOriginating})
	msg := mustParseMsg(t, inviteBytes)
	r, err := m.Evaluate(msg, SessionCaseOriginating)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if r.ASAddress != "sip:as-inv@example.com" {
		t.Errorf("ASAddress = %q, want sip:as-inv@example.com", r.ASAddress)
	}
}

func TestEvaluatePriorityOrder(t *testing.T) {
	m := NewISCModule()
	// Two routes that both match; the one with the lower priority value
	// wins even though it was added later.
	m.AddRoute(&ISCRoute{TriggerPoint: "", ASAddress: "sip:as-low-prio@example.com", Priority: 10, SessionCase: SessionCaseOriginating})
	m.AddRoute(&ISCRoute{TriggerPoint: "", ASAddress: "sip:as-high-prio@example.com", Priority: 1, SessionCase: SessionCaseOriginating})
	msg := mustParseMsg(t, inviteBytes)
	r, err := m.Evaluate(msg, SessionCaseOriginating)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if r.ASAddress != "sip:as-high-prio@example.com" {
		t.Errorf("ASAddress = %q, want sip:as-high-prio@example.com", r.ASAddress)
	}
}

func TestEvaluateSessionCaseFilter(t *testing.T) {
	m := NewISCModule()
	m.AddRoute(&ISCRoute{TriggerPoint: "", ASAddress: "sip:as-orig@example.com", Priority: 1, SessionCase: SessionCaseOriginating})
	msg := mustParseMsg(t, inviteBytes)
	// No route registered for the terminating session case.
	if _, err := m.Evaluate(msg, SessionCaseTerminating); err == nil {
		t.Errorf("Evaluate with no matching session case should error")
	}
}

func TestEvaluateNilMsg(t *testing.T) {
	m := NewISCModule()
	m.AddRoute(&ISCRoute{TriggerPoint: "", ASAddress: "sip:as1@example.com", Priority: 1, SessionCase: SessionCaseOriginating})
	if _, err := m.Evaluate(nil, SessionCaseOriginating); err == nil {
		t.Errorf("Evaluate(nil) should error")
	}
}

func TestIsInitialFilterTriggered(t *testing.T) {
	m := NewISCModule()
	msg := mustParseMsg(t, inviteBytes)

	// method match
	if !m.IsInitialFilterTriggered(msg, &ISCRoute{TriggerPoint: "method:INVITE"}) {
		t.Errorf("method:INVITE should trigger")
	}
	// uri match
	if !m.IsInitialFilterTriggered(msg, &ISCRoute{TriggerPoint: "uri:1001"}) {
		t.Errorf("uri:1001 should trigger")
	}
	// call-id match
	if !m.IsInitialFilterTriggered(msg, &ISCRoute{TriggerPoint: "call-isc-1"}) {
		t.Errorf("call-isc-1 should trigger")
	}
	// no match
	if m.IsInitialFilterTriggered(msg, &ISCRoute{TriggerPoint: "method:BYE"}) {
		t.Errorf("method:BYE should not trigger an INVITE")
	}
	// empty trigger always matches
	if !m.IsInitialFilterTriggered(msg, &ISCRoute{TriggerPoint: ""}) {
		t.Errorf("empty trigger should always match")
	}
}

func TestListRoutes(t *testing.T) {
	m := NewISCModule()
	m.AddRoute(&ISCRoute{TriggerPoint: "", ASAddress: "sip:as1@example.com", Priority: 3, SessionCase: SessionCaseOriginating})
	m.AddRoute(&ISCRoute{TriggerPoint: "", ASAddress: "sip:as2@example.com", Priority: 1, SessionCase: SessionCaseOriginating})
	list := m.ListRoutes()
	if len(list) != 2 {
		t.Fatalf("ListRoutes len = %d, want 2", len(list))
	}
	if list[0].Priority != 1 {
		t.Errorf("first route priority = %d, want 1 (sorted ascending)", list[0].Priority)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := NewISCModule()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := m.AddRoute(&ISCRoute{TriggerPoint: "", ASAddress: "sip:as1@example.com", Priority: 1, SessionCase: SessionCaseOriginating})
			m.RemoveRoute(id)
			m.Count()
			m.ListRoutes()
		}()
	}
	wg.Wait()
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	im := DefaultISC()
	if im == nil {
		t.Fatal("expected non-nil default ISC module")
	}
	if im.Count() != 0 {
		t.Errorf("Count = %d, want 0 after Init", im.Count())
	}
}
