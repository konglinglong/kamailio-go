// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the IMS QoS NPN module.
 */

package ims_qos_npn

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
	"From: Alice <sip:alice@example.com>;tag=qtag111\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: call-npn-1@10.0.0.1\r\n" +
	"CSeq: 1 INVITE\r\n" +
	"Contact: <sip:alice@10.0.0.1>\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

func TestHandleRequest(t *testing.T) {
	m := NewIMSQoSNpnModule()
	msg := mustParseMsg(t, inviteBytes)
	code, err := m.HandleRequest(msg)
	if err != nil {
		t.Fatalf("HandleRequest failed: %v", err)
	}
	if code != 1 {
		t.Errorf("code = %d, want 1 for new session", code)
	}
	s := m.GetSession("call-npn-1@10.0.0.1")
	if s == nil {
		t.Fatal("expected session to be created")
	}
	if s.CallID != "call-npn-1@10.0.0.1" {
		t.Errorf("CallID = %q", s.CallID)
	}
	if s.UserID != "alice" {
		t.Errorf("UserID = %q, want alice", s.UserID)
	}
	if s.Status != StatusActive {
		t.Errorf("Status = %q, want active", s.Status)
	}
	if len(s.MediaComponents) == 0 {
		t.Fatal("expected at least one media component")
	}
	if s.NpnType != NpnTypeWifi {
		t.Errorf("NpnType = %q, want wifi", s.NpnType)
	}
}

func TestHandleRequestIdempotent(t *testing.T) {
	m := NewIMSQoSNpnModule()
	msg := mustParseMsg(t, inviteBytes)
	c1, _ := m.HandleRequest(msg)
	c2, _ := m.HandleRequest(msg)
	if c1 != 1 || c2 != 2 {
		t.Errorf("codes = %d,%d, want 1,2", c1, c2)
	}
	if m.Count() != 1 {
		t.Errorf("Count = %d, want 1", m.Count())
	}
}

func TestHandleRequestErrors(t *testing.T) {
	m := NewIMSQoSNpnModule()
	if _, err := m.HandleRequest(nil); err == nil {
		t.Error("HandleRequest(nil) should error")
	}
	raw := []byte("INVITE sip:1001@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776asdhds\r\n" +
		"From: Alice <sip:alice@example.com>;tag=qtag111\r\n" +
		"To: Bob <sip:bob@example.com>\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
	msg := mustParseMsg(t, raw)
	if _, err := m.HandleRequest(msg); err == nil {
		t.Error("HandleRequest without Call-ID should error")
	}
}

func TestAuthorizeSession(t *testing.T) {
	m := NewIMSQoSNpnModule()
	msg := mustParseMsg(t, inviteBytes)
	m.HandleRequest(msg)
	if err := m.AuthorizeSession("call-npn-1@10.0.0.1", "alice"); err != nil {
		t.Fatalf("AuthorizeSession failed: %v", err)
	}
	s := m.GetSession("call-npn-1@10.0.0.1")
	if s.Status != StatusAuthorized {
		t.Errorf("Status = %q, want authorized", s.Status)
	}
	if s.UserID != "alice" {
		t.Errorf("UserID = %q", s.UserID)
	}
	if err := m.AuthorizeSession("missing", "x"); err == nil {
		t.Error("AuthorizeSession for missing session should error")
	}
}

func TestAddAndRemoveMediaComponent(t *testing.T) {
	m := NewIMSQoSNpnModule()
	msg := mustParseMsg(t, inviteBytes)
	m.HandleRequest(msg)
	comp := &MediaComponent{CompID: 2, MediaDesc: "video", Bandwidth: 256, Priority: 1}
	if err := m.AddMediaComponent("call-npn-1@10.0.0.1", comp); err != nil {
		t.Fatalf("AddMediaComponent failed: %v", err)
	}
	s := m.GetSession("call-npn-1@10.0.0.1")
	if got := s.MediaComponents[2]; got == nil || got.Bandwidth != 256 {
		t.Errorf("component not added: %v", got)
	}
	if err := m.AddMediaComponent("call-npn-1@10.0.0.1", nil); err == nil {
		t.Error("AddMediaComponent(nil) should error")
	}
	if err := m.AddMediaComponent("missing", comp); err == nil {
		t.Error("AddMediaComponent on missing session should error")
	}
	if err := m.RemoveMediaComponent("call-npn-1@10.0.0.1", 2); err != nil {
		t.Fatalf("RemoveMediaComponent failed: %v", err)
	}
	if s.MediaComponents[2] != nil {
		t.Error("component should be removed")
	}
	if err := m.RemoveMediaComponent("call-npn-1@10.0.0.1", 99); err == nil {
		t.Error("RemoveMediaComponent for missing component should error")
	}
	if err := m.RemoveMediaComponent("missing", 1); err == nil {
		t.Error("RemoveMediaComponent on missing session should error")
	}
}

func TestAddMediaComponentDefaults(t *testing.T) {
	m := NewIMSQoSNpnModuleWithConfig(Config{DefaultBandwidth: 128, NpnType: NpnTypeWifi})
	msg := mustParseMsg(t, inviteBytes)
	m.HandleRequest(msg)
	comp := &MediaComponent{CompID: 3, MediaDesc: "audio"}
	m.AddMediaComponent("call-npn-1@10.0.0.1", comp)
	s := m.GetSession("call-npn-1@10.0.0.1")
	got := s.MediaComponents[3]
	if got.Bandwidth != 128 {
		t.Errorf("Bandwidth = %d, want default 128", got.Bandwidth)
	}
	if got.FlowStatus != FlowEnabled {
		t.Errorf("FlowStatus = %q, want enabled", got.FlowStatus)
	}
}

func TestUpdateBandwidth(t *testing.T) {
	m := NewIMSQoSNpnModuleWithConfig(Config{DefaultBandwidth: 64, MaxBandwidth: 500, NpnType: NpnTypeWifi})
	msg := mustParseMsg(t, inviteBytes)
	m.HandleRequest(msg)
	if err := m.UpdateBandwidth("call-npn-1@10.0.0.1", 1, 200); err != nil {
		t.Fatalf("UpdateBandwidth failed: %v", err)
	}
	s := m.GetSession("call-npn-1@10.0.0.1")
	if got := s.MediaComponents[1]; got.Bandwidth != 200 {
		t.Errorf("Bandwidth = %d, want 200", got.Bandwidth)
	}
	// Clamp to max.
	m.UpdateBandwidth("call-npn-1@10.0.0.1", 1, 9999)
	if got := s.MediaComponents[1]; got.Bandwidth != 500 {
		t.Errorf("Bandwidth = %d, want clamped 500", got.Bandwidth)
	}
	if err := m.UpdateBandwidth("call-npn-1@10.0.0.1", 99, 100); err == nil {
		t.Error("UpdateBandwidth for missing component should error")
	}
	if err := m.UpdateBandwidth("missing", 1, 100); err == nil {
		t.Error("UpdateBandwidth on missing session should error")
	}
}

func TestRemoveAndList(t *testing.T) {
	m := NewIMSQoSNpnModule()
	msg := mustParseMsg(t, inviteBytes)
	m.HandleRequest(msg)
	if got := len(m.ListSessions()); got != 1 {
		t.Errorf("ListSessions len = %d, want 1", got)
	}
	m.RemoveSession("call-npn-1@10.0.0.1")
	if m.GetSession("call-npn-1@10.0.0.1") != nil {
		t.Error("session should be removed")
	}
	if m.Count() != 0 {
		t.Errorf("Count = %d, want 0", m.Count())
	}
}

func TestCleanupExpired(t *testing.T) {
	m := NewIMSQoSNpnModule()
	msg := mustParseMsg(t, inviteBytes)
	m.HandleRequest(msg)
	s := m.GetSession("call-npn-1@10.0.0.1")
	s.updatedAt = time.Now().Add(-3 * time.Hour)
	m.CleanupExpiredTTL(time.Hour)
	if m.GetSession("call-npn-1@10.0.0.1") != nil {
		t.Error("expired session should have been cleaned up")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := NewIMSQoSNpnModule()
	msg := mustParseMsg(t, inviteBytes)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.HandleRequest(msg)
			m.GetSession("call-npn-1@10.0.0.1")
			m.AddMediaComponent("call-npn-1@10.0.0.1", &MediaComponent{CompID: 2, Bandwidth: 64})
			m.UpdateBandwidth("call-npn-1@10.0.0.1", 2, 128)
			m.Count()
			m.ListSessions()
		}()
	}
	wg.Wait()
	if m.Count() != 1 {
		t.Errorf("Count = %d, want 1", m.Count())
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	n := DefaultQoSNpn()
	if n == nil {
		t.Fatal("expected non-nil default QoS NPN module")
	}
	if n.Count() != 0 {
		t.Errorf("Count = %d, want 0 after Init", n.Count())
	}
	if n2 := DefaultQoSNpn(); n != n2 {
		t.Error("DefaultQoSNpn should return the same instance")
	}
}
