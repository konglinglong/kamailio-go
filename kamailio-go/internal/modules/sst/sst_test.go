// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the sst (Session Timer, RFC 4028) module.
 */

package sst

import (
	"sync"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// inviteWithSE builds an INVITE carrying the given Session-Expires / Min-SE
// headers (either may be empty to omit it).
func inviteWithSE(callID, se, minSE string) *parser.SIPMsg {
	raw := "INVITE sip:bob@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 192.168.1.1:5060;branch=z9hG4bK12345\r\n" +
		"From: <sip:alice@example.com>;tag=abc\r\n" +
		"To: <sip:bob@example.com>\r\n" +
		"Call-ID: " + callID + "\r\n" +
		"CSeq: 1 INVITE\r\n"
	if se != "" {
		raw += "Session-Expires: " + se + "\r\n"
	}
	if minSE != "" {
		raw += "Min-SE: " + minSE + "\r\n"
	}
	raw += "Content-Length: 0\r\n\r\n"
	msg, err := parser.ParseMsg([]byte(raw))
	if err != nil {
		panic(err)
	}
	return msg
}

// okWithSE builds a 200 OK response carrying a Session-Expires header.
func okWithSE(callID, se string) *parser.SIPMsg {
	raw := "SIP/2.0 200 OK\r\n" +
		"Via: SIP/2.0/UDP 192.168.1.1:5060;branch=z9hG4bK12345\r\n" +
		"From: <sip:alice@example.com>;tag=abc\r\n" +
		"To: <sip:bob@example.com>;tag=xyz\r\n" +
		"Call-ID: " + callID + "\r\n" +
		"CSeq: 1 INVITE\r\n"
	if se != "" {
		raw += "Session-Expires: " + se + "\r\n"
	}
	raw += "Content-Length: 0\r\n\r\n"
	msg, err := parser.ParseMsg([]byte(raw))
	if err != nil {
		panic(err)
	}
	return msg
}

func TestCheckSessionExpires(t *testing.T) {
	m := New() // default minSE = 90
	if !m.CheckSessionExpires(90) {
		t.Errorf("CheckSessionExpires(90) = false, want true (== minSE)")
	}
	if !m.CheckSessionExpires(1800) {
		t.Errorf("CheckSessionExpires(1800) = false, want true")
	}
	if m.CheckSessionExpires(89) {
		t.Errorf("CheckSessionExpires(89) = true, want false (< minSE)")
	}
}

func TestHandleRequest(t *testing.T) {
	m := New()
	msg := inviteWithSE("sst-req-1", "1800;refresher=uas", "90")

	expires, err := m.HandleRequest(msg)
	if err != nil {
		t.Fatalf("HandleRequest() error = %v", err)
	}
	if expires != 1800 {
		t.Errorf("expires = %d, want 1800", expires)
	}
	// Session should be registered.
	s := m.GetSession("sst-req-1")
	if s == nil {
		t.Fatalf("GetSession() returned nil after HandleRequest")
	}
	if s.Expires != 1800 {
		t.Errorf("session Expires = %d, want 1800", s.Expires)
	}
	if s.Refresher != "uas" {
		t.Errorf("session Refresher = %q, want uas", s.Refresher)
	}
}

func TestHandleRequestTooSmall(t *testing.T) {
	m := New() // minSE = 90
	msg := inviteWithSE("sst-req-small", "30;refresher=uac", "90")
	if _, err := m.HandleRequest(msg); err == nil {
		t.Errorf("HandleRequest() with SE=30 < minSE=90 should error")
	}
}

func TestHandleRequestNoSE(t *testing.T) {
	m := New()
	msg := inviteWithSE("sst-req-nose", "", "")
	if _, err := m.HandleRequest(msg); err == nil {
		t.Errorf("HandleRequest() with no Session-Expires should error")
	}
}

func TestHandleResponse(t *testing.T) {
	m := New()
	// Pre-register a session so the response can update it.
	m.AddSession(&Session{
		CallID:    "sst-resp-1",
		Expires:   1800,
		Refresher: "uas",
		Method:    "INVITE",
	})
	msg := okWithSE("sst-resp-1", "1200;refresher=uac")

	expires, err := m.HandleResponse(msg)
	if err != nil {
		t.Fatalf("HandleResponse() error = %v", err)
	}
	if expires != 1200 {
		t.Errorf("expires = %d, want 1200", expires)
	}
	s := m.GetSession("sst-resp-1")
	if s == nil {
		t.Fatalf("GetSession() returned nil after HandleResponse")
	}
	if s.Expires != 1200 {
		t.Errorf("session Expires = %d, want 1200", s.Expires)
	}
	if s.Refresher != "uac" {
		t.Errorf("session Refresher = %q, want uac", s.Refresher)
	}
}

func TestHandleResponseNoSE(t *testing.T) {
	m := New()
	msg := okWithSE("sst-resp-nose", "")
	if _, err := m.HandleResponse(msg); err == nil {
		t.Errorf("HandleResponse() with no Session-Expires should error")
	}
}

func TestGenerateRefresh(t *testing.T) {
	m := New()
	m.AddSession(&Session{
		CallID:    "sst-refresh-1",
		Expires:   1800,
		Refresher: "uas",
		Method:    "INVITE",
	})
	refresh, err := m.GenerateRefresh("sst-refresh-1")
	if err != nil {
		t.Fatalf("GenerateRefresh() error = %v", err)
	}
	if refresh == nil {
		t.Fatalf("GenerateRefresh() returned nil")
	}
	if !refresh.IsRequest() {
		t.Errorf("refresh is not a request")
	}
	if refresh.Method() != parser.MethodInvite {
		t.Errorf("refresh method = %v, want INVITE", refresh.Method())
	}
	// Must carry the Session-Expires header.
	if refresh.SessionExpires == nil {
		t.Errorf("refresh missing Session-Expires header")
	}
	// Call-ID must match.
	if refresh.CallID == nil {
		t.Errorf("refresh missing Call-ID header")
	} else if refresh.CallID.Body.String() != "sst-refresh-1" {
		t.Errorf("refresh Call-ID = %q, want sst-refresh-1", refresh.CallID.Body.String())
	}
}

func TestGenerateRefreshUpdate(t *testing.T) {
	m := New()
	m.SetMethod("UPDATE")
	m.AddSession(&Session{
		CallID:    "sst-refresh-2",
		Expires:   600,
		Refresher: "uac",
		Method:    "UPDATE",
	})
	refresh, err := m.GenerateRefresh("sst-refresh-2")
	if err != nil {
		t.Fatalf("GenerateRefresh() error = %v", err)
	}
	if refresh.Method() != parser.MethodUpdate {
		t.Errorf("refresh method = %v, want UPDATE", refresh.Method())
	}
}

func TestGenerateRefreshUnknown(t *testing.T) {
	m := New()
	if _, err := m.GenerateRefresh("unknown-call-id"); err == nil {
		t.Errorf("GenerateRefresh(unknown) should error")
	}
}

func TestAddRemoveGetSession(t *testing.T) {
	m := New()
	m.AddSession(&Session{CallID: "c1", Expires: 90, Refresher: "uac", Method: "INVITE"})
	if m.GetSession("c1") == nil {
		t.Fatalf("GetSession(c1) returned nil")
	}
	m.RemoveSession("c1")
	if m.GetSession("c1") != nil {
		t.Errorf("GetSession(c1) after remove should return nil")
	}
	// Removing again is a no-op.
	m.RemoveSession("c1")
}

func TestCleanupExpired(t *testing.T) {
	m := New()
	// Fresh session (not expired).
	m.AddSession(&Session{
		CallID:      "fresh",
		Expires:     1800,
		Refresher:   "uac",
		Method:      "INVITE",
		LastRefresh: time.Now(),
	})
	// Stale session (last refreshed long ago).
	m.AddSession(&Session{
		CallID:      "stale",
		Expires:     90,
		Refresher:   "uas",
		Method:      "INVITE",
		LastRefresh: time.Now().Add(-2 * time.Hour),
	})
	m.CleanupExpired()
	if m.GetSession("fresh") == nil {
		t.Errorf("fresh session was removed by CleanupExpired")
	}
	if m.GetSession("stale") != nil {
		t.Errorf("stale session was not removed by CleanupExpired")
	}
}

func TestSetMinSE(t *testing.T) {
	m := New()
	m.SetMinSE(300)
	if !m.CheckSessionExpires(300) {
		t.Errorf("CheckSessionExpires(300) = false, want true after SetMinSE(300)")
	}
	if m.CheckSessionExpires(200) {
		t.Errorf("CheckSessionExpires(200) = true, want false after SetMinSE(300)")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultSST()
	if d == nil {
		t.Fatalf("DefaultSST() returned nil")
	}
	if DefaultSST() != d {
		t.Errorf("DefaultSST() returned different instance")
	}
	d.AddSession(&Session{CallID: "reset", Expires: 90, Refresher: "uac", Method: "INVITE"})
	Init()
	if DefaultSST().GetSession("reset") != nil {
		t.Errorf("after Init, session should be gone")
	}
}

func TestConcurrentSafety(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := "c" + itoa(n)
			m.AddSession(&Session{CallID: id, Expires: 90, Refresher: "uac", Method: "INVITE"})
			_ = m.GetSession(id)
			_ = m.CheckSessionExpires(90)
			m.RemoveSession(id)
		}(i)
	}
	wg.Wait()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
