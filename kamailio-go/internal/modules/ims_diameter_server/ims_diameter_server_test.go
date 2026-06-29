// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the IMS Diameter Server module.
 */

package ims_diameter_server

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRegisterAndHandleMessage(t *testing.T) {
	m := NewDiameterServerModule()
	called := false
	handler := func(req *DiameterMessage) (*DiameterMessage, error) {
		called = true
		return m.BuildAnswer(req, ResultCodeSuccess), nil
	}
	if err := m.RegisterHandler(318, handler); err != nil {
		t.Fatalf("RegisterHandler failed: %v", err)
	}

	req := &DiameterMessage{
		Version:       DiameterVersion,
		CommandCode:   318,
		ApplicationID: 16777216,
		HopByHopID:    1,
		EndToEndID:    2,
		AVPs: []DiameterAVP{
			{Code: AVPCodeSessionID, Data: "sid-1"},
		},
	}
	ans, err := m.HandleMessage(req)
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}
	if !called {
		t.Error("handler was not invoked")
	}
	if ans.CommandCode != 318 {
		t.Errorf("CommandCode = %d, want 318", ans.CommandCode)
	}
	if ans.HopByHopID != 1 || ans.EndToEndID != 2 {
		t.Errorf("ids not preserved: hbh=%d e2e=%d", ans.HopByHopID, ans.EndToEndID)
	}
	rc := FindAVP(ans, AVPCodeResultCode)
	if rc == nil {
		t.Fatal("answer missing Result-Code AVP")
	}
	if rc.Data != ResultCodeSuccess {
		t.Errorf("ResultCode = %v, want %d", rc.Data, ResultCodeSuccess)
	}
	sid := FindAVP(ans, AVPCodeSessionID)
	if sid == nil || sid.Data != "sid-1" {
		t.Errorf("Session-Id not copied back: %v", sid)
	}
}

func TestHandleMessageNoHandler(t *testing.T) {
	m := NewDiameterServerModule()
	req := &DiameterMessage{CommandCode: 999, HopByHopID: 7, EndToEndID: 8}
	ans, err := m.HandleMessage(req)
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}
	rc := FindAVP(ans, AVPCodeResultCode)
	if rc == nil || rc.Data != ResultCodeCommandUnsupported {
		t.Errorf("expected CommandUnsupported, got %v", rc)
	}
}

func TestHandleMessageErrors(t *testing.T) {
	m := NewDiameterServerModule()
	if _, err := m.HandleMessage(nil); err == nil {
		t.Error("HandleMessage(nil) should error")
	}
}

func TestRegisterHandlerErrors(t *testing.T) {
	m := NewDiameterServerModule()
	if err := m.RegisterHandler(0, func(req *DiameterMessage) (*DiameterMessage, error) {
		return nil, nil
	}); err == nil {
		t.Error("RegisterHandler with code 0 should error")
	}
	// nil handler removes registration without error.
	if err := m.RegisterHandler(318, nil); err != nil {
		t.Errorf("RegisterHandler nil should not error: %v", err)
	}
}

func TestHandlerReturnsError(t *testing.T) {
	m := NewDiameterServerModule()
	m.RegisterHandler(318, func(req *DiameterMessage) (*DiameterMessage, error) {
		return nil, errors.New("boom")
	})
	if _, err := m.HandleMessage(&DiameterMessage{CommandCode: 318}); err == nil {
		t.Error("expected error from handler")
	}
}

func TestSessionLifecycle(t *testing.T) {
	m := NewDiameterServerModule()
	s := m.CreateSession("sid-1", 318)
	if s == nil {
		t.Fatal("CreateSession returned nil")
	}
	if s.SessionID != "sid-1" {
		t.Errorf("SessionID = %q", s.SessionID)
	}
	if s.CommandCode != 318 {
		t.Errorf("CommandCode = %d", s.CommandCode)
	}
	if s.OriginHost == "" {
		t.Error("OriginHost should be populated from config")
	}
	if m.SessionCount() != 1 {
		t.Errorf("SessionCount = %d, want 1", m.SessionCount())
	}
	// Idempotent: same session id returns the same session.
	s2 := m.CreateSession("sid-1", 318)
	if s != s2 {
		t.Error("CreateSession twice should return the same session")
	}
	if m.SessionCount() != 1 {
		t.Errorf("SessionCount = %d, want 1", m.SessionCount())
	}
	// Generated session id when empty.
	s3 := m.CreateSession("", 318)
	if s3.SessionID == "" {
		t.Error("CreateSession with empty id should generate one")
	}
	if got := m.GetSession("sid-1"); got != s {
		t.Error("GetSession returned wrong session")
	}
	if got := m.GetSession("missing"); got != nil {
		t.Error("GetSession for missing should return nil")
	}
	m.RemoveSession("sid-1")
	if m.GetSession("sid-1") != nil {
		t.Error("GetSession after Remove should return nil")
	}
}

func TestListSessions(t *testing.T) {
	m := NewDiameterServerModule()
	m.CreateSession("a", 1)
	m.CreateSession("b", 2)
	if got := len(m.ListSessions()); got != 2 {
		t.Errorf("ListSessions len = %d, want 2", got)
	}
}

func TestStartStop(t *testing.T) {
	m := NewDiameterServerModuleWithConfig(Config{
		ListenAddr:     "127.0.0.1:0",
		OriginHost:     "h",
		OriginRealm:    "r",
		MaxConnections: 10,
	})
	if err := m.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if !m.IsStarted() {
		t.Error("expected started after Start")
	}
	// Second Start is a no-op.
	if err := m.Start(); err != nil {
		t.Fatalf("second Start failed: %v", err)
	}
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if m.IsStarted() {
		t.Error("expected stopped after Stop")
	}
}

func TestBuildAnswerDefaults(t *testing.T) {
	m := NewDiameterServerModule()
	m.SetConfig(Config{OriginHost: "oh", OriginRealm: "or"})
	req := &DiameterMessage{CommandCode: 300, HopByHopID: 5, EndToEndID: 6}
	ans := m.BuildAnswer(req, ResultCodeLimitedSuccess)
	if oh := FindAVP(ans, AVPCodeOriginHost); oh == nil || oh.Data != "oh" {
		t.Errorf("OriginHost not set: %v", oh)
	}
	if or := FindAVP(ans, AVPCodeOriginRealm); or == nil || or.Data != "or" {
		t.Errorf("OriginRealm not set: %v", or)
	}
}

func TestCleanupExpired(t *testing.T) {
	m := NewDiameterServerModule()
	s := m.CreateSession("old", 318)
	s.updatedAt = time.Now().Add(-2 * time.Hour)
	m.CreateSession("new", 318)
	m.CleanupExpired(time.Hour)
	if m.GetSession("old") != nil {
		t.Error("old session should have been cleaned up")
	}
	if m.GetSession("new") == nil {
		t.Error("new session should remain")
	}
}

func TestAddAndFindAVP(t *testing.T) {
	msg := &DiameterMessage{}
	AddAVP(msg, DiameterAVP{Code: 263, Data: "sid"})
	if got := FindAVP(msg, 263); got == nil || got.Data != "sid" {
		t.Errorf("FindAVP failed: %v", got)
	}
	if got := FindAVP(msg, 999); got != nil {
		t.Error("FindAVP for missing code should return nil")
	}
	if AddAVP(nil, DiameterAVP{}) != nil {
		t.Error("AddAVP(nil) should return nil")
	}
	if FindAVP(nil, 1) != nil {
		t.Error("FindAVP(nil) should return nil")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := NewDiameterServerModule()
	m.RegisterHandler(318, func(req *DiameterMessage) (*DiameterMessage, error) {
		return m.BuildAnswer(req, ResultCodeSuccess), nil
	})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			m.CreateSession("sid", 318)
			m.GetSession("sid")
			m.SessionCount()
			m.ListSessions()
			m.HandleMessage(&DiameterMessage{CommandCode: 318, HopByHopID: uint32(n)})
		}(i)
	}
	wg.Wait()
	if m.SessionCount() != 1 {
		t.Errorf("SessionCount = %d, want 1", m.SessionCount())
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	d := DefaultDiameterServer()
	if d == nil {
		t.Fatal("expected non-nil default Diameter server")
	}
	if d.SessionCount() != 0 {
		t.Errorf("SessionCount = %d, want 0 after Init", d.SessionCount())
	}
	if d2 := DefaultDiameterServer(); d != d2 {
		t.Error("DefaultDiameterServer should return the same instance")
	}
}
