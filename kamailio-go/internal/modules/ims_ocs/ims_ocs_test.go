// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the IMS OCS module.
 */

package ims_ocs

import (
	"sync"
	"testing"
)

func TestRequestUnits(t *testing.T) {
	m := NewOCSModule()
	s, err := m.RequestUnits("sip:alice@example.com", "voice", 1000)
	if err != nil {
		t.Fatalf("RequestUnits failed: %v", err)
	}
	if s.Subscriber != "sip:alice@example.com" {
		t.Errorf("Subscriber = %q", s.Subscriber)
	}
	if s.ServiceID != "voice" {
		t.Errorf("ServiceID = %q", s.ServiceID)
	}
	if s.GrantedUnits != 1000 {
		t.Errorf("GrantedUnits = %d, want 1000", s.GrantedUnits)
	}
	if s.UsedUnits != 0 {
		t.Errorf("UsedUnits = %d, want 0", s.UsedUnits)
	}
	if s.Status != StatusActive {
		t.Errorf("Status = %q, want active", s.Status)
	}
	if s.SessionID == "" {
		t.Errorf("SessionID should be populated")
	}
	if m.Count() != 1 {
		t.Errorf("Count = %d, want 1", m.Count())
	}
}

func TestRequestUnitsErrors(t *testing.T) {
	m := NewOCSModule()
	if _, err := m.RequestUnits("", "voice", 100); err == nil {
		t.Errorf("RequestUnits with empty subscriber should error")
	}
	if _, err := m.RequestUnits("sip:alice@example.com", "", 100); err == nil {
		t.Errorf("RequestUnits with empty service should error")
	}
}

func TestRequestUnitsTopUp(t *testing.T) {
	m := NewOCSModule()
	s1, _ := m.RequestUnits("sip:alice@example.com", "voice", 100)
	s2, _ := m.RequestUnits("sip:alice@example.com", "voice", 200)
	if s1 != s2 {
		t.Errorf("RequestUnits twice should return the same session")
	}
	if s2.GrantedUnits != 300 {
		t.Errorf("GrantedUnits after top-up = %d, want 300", s2.GrantedUnits)
	}
	if m.Count() != 1 {
		t.Errorf("Count = %d, want 1", m.Count())
	}
}

func TestUpdateUsage(t *testing.T) {
	m := NewOCSModule()
	s, _ := m.RequestUnits("sip:alice@example.com", "voice", 1000)

	if err := m.UpdateUsage(s.SessionID, 400); err != nil {
		t.Fatalf("UpdateUsage failed: %v", err)
	}
	if s.UsedUnits != 400 {
		t.Errorf("UsedUnits = %d, want 400", s.UsedUnits)
	}
	if s.Status != StatusActive {
		t.Errorf("Status = %q, want active", s.Status)
	}
	// Exhaust the quota.
	if err := m.UpdateUsage(s.SessionID, 600); err != nil {
		t.Fatalf("UpdateUsage failed: %v", err)
	}
	if s.UsedUnits != 1000 {
		t.Errorf("UsedUnits = %d, want 1000", s.UsedUnits)
	}
	if s.Status != StatusExhausted {
		t.Errorf("Status = %q, want exhausted", s.Status)
	}
	// Top-up reactivates an exhausted session.
	m.RequestUnits("sip:alice@example.com", "voice", 500)
	if s.Status != StatusActive {
		t.Errorf("Status after top-up = %q, want active", s.Status)
	}
	if err := m.UpdateUsage("nope", 1); err == nil {
		t.Errorf("UpdateUsage on unknown session should error")
	}
}

func TestTerminate(t *testing.T) {
	m := NewOCSModule()
	s, _ := m.RequestUnits("sip:alice@example.com", "voice", 1000)
	if err := m.Terminate(s.SessionID); err != nil {
		t.Fatalf("Terminate failed: %v", err)
	}
	if s.Status != StatusTerminated {
		t.Errorf("Status = %q, want terminated", s.Status)
	}
	// No further usage accepted on a terminated session.
	if err := m.UpdateUsage(s.SessionID, 1); err == nil {
		t.Errorf("UpdateUsage on terminated session should error")
	}
	// No further top-up accepted on a terminated session.
	if _, err := m.RequestUnits("sip:alice@example.com", "voice", 100); err == nil {
		t.Errorf("RequestUnits on terminated session should error")
	}
	if err := m.Terminate("nope"); err == nil {
		t.Errorf("Terminate on unknown session should error")
	}
}

func TestGetSessionAndList(t *testing.T) {
	m := NewOCSModule()
	if s := m.GetSession("nope"); s != nil {
		t.Errorf("GetSession on empty module should return nil")
	}
	s, _ := m.RequestUnits("sip:alice@example.com", "voice", 1000)
	if got := m.GetSession(s.SessionID); got == nil {
		t.Errorf("GetSession returned nil for existing session")
	}
	if got := len(m.List()); got != 1 {
		t.Errorf("List len = %d, want 1", got)
	}
	// A second subscriber/service pair creates a new session.
	m.RequestUnits("sip:bob@example.com", "video", 500)
	if got := m.Count(); got != 2 {
		t.Errorf("Count = %d, want 2", got)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := NewOCSModule()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, _ := m.RequestUnits("sip:alice@example.com", "voice", 10000)
			if s != nil {
				m.UpdateUsage(s.SessionID, 1)
			}
			m.GetSession(s.SessionID)
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
	o := DefaultOCS()
	if o == nil {
		t.Fatal("expected non-nil default OCS module")
	}
	if o.Count() != 0 {
		t.Errorf("Count = %d, want 0 after Init", o.Count())
	}
}
