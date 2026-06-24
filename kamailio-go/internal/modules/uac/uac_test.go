// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the UAC module.
 */

package uac

import (
	"strings"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

// addHeader appends a header to msg and wires up the matching quick-access
// pointer, mirroring what the full parser does.
func addHeader(msg *parser.SIPMsg, name, value string) *parser.HdrField {
	h := msg.AddHeader(name, value)
	switch h.Type {
	case parser.HdrFrom:
		msg.From = h
	case parser.HdrTo:
		msg.To = h
	case parser.HdrCallID:
		msg.CallID = h
	case parser.HdrCSeq:
		msg.CSeq = h
	case parser.HdrRoute:
		if msg.Route == nil {
			msg.Route = h
		}
	case parser.HdrVia:
		if msg.HdrVia1 == nil {
			msg.HdrVia1 = h
		}
	}
	return h
}

func TestSendRequest(t *testing.T) {
	m := New()

	raw, err := m.SendRequest(
		"INVITE",
		"sip:bob@example.com",
		"sip:alice@example.com",
		"sip:bob@example.com",
		[]string{"Subject: Test Call", "Allow: INVITE,ACK"},
		"v=0\r\n",
	)
	if err != nil {
		t.Fatalf("SendRequest() error = %v", err)
	}
	s := string(raw)

	// Request line must come first.
	if !strings.HasPrefix(s, "INVITE sip:bob@example.com SIP/2.0\r\n") {
		t.Errorf("request line missing/wrong: %q", s[:len("INVITE sip:bob@example.com SIP/2.0\r\n")])
	}
	// Mandatory headers must be present.
	for _, want := range []string{"From: ", "To: ", "Call-ID: ", "CSeq: ", "Max-Forwards: "} {
		if !strings.Contains(s, want) {
			t.Errorf("SendRequest output missing %q", want)
		}
	}
	// Caller-supplied headers are appended verbatim.
	if !strings.Contains(s, "Subject: Test Call") {
		t.Errorf("SendRequest output missing caller header 'Subject'")
	}
	if !strings.Contains(s, "Allow: INVITE,ACK") {
		t.Errorf("SendRequest output missing caller header 'Allow'")
	}
	// Content-Length reflects the body length ("v=0\r\n" = 5 bytes).
	if !strings.Contains(s, "Content-Length: 5") {
		t.Errorf("SendRequest output missing correct Content-Length, got:\n%s", s)
	}
	// Body is appended after the blank line separator.
	if !strings.HasSuffix(s, "\r\n\r\nv=0\r\n") {
		t.Errorf("SendRequest body not appended correctly, tail = %q", s[len(s)-20:])
	}

	// Empty method -> error.
	if _, err := m.SendRequest("", "sip:bob@example.com", "sip:a@b", "sip:c@d", nil, ""); err == nil {
		t.Errorf("SendRequest(empty method) should error")
	}
	// Empty RURI -> error.
	if _, err := m.SendRequest("INVITE", "", "sip:a@b", "sip:c@d", nil, ""); err == nil {
		t.Errorf("SendRequest(empty ruri) should error")
	}
}

func TestReplaceFrom(t *testing.T) {
	m := New()

	msg := &parser.SIPMsg{}
	addHeader(msg, "From", "<sip:alice@example.com>;tag=abc123")

	if ret := m.ReplaceFrom(msg, "Bob", "sip:newfrom@example.com"); ret != 0 {
		t.Fatalf("ReplaceFrom() = %d, want 0", ret)
	}
	body := msg.From.Body.String()
	if !strings.Contains(body, "sip:newfrom@example.com") {
		t.Errorf("From body after replace = %q, want new URI", body)
	}
	// The original tag must be preserved.
	if !strings.Contains(body, "tag=abc123") {
		t.Errorf("From body after replace = %q, want tag preserved", body)
	}
	// Display name is carried through.
	if !strings.Contains(body, "Bob") {
		t.Errorf("From body after replace = %q, want display name 'Bob'", body)
	}

	// No From header -> -1.
	if ret := m.ReplaceFrom(&parser.SIPMsg{}, "X", "sip:x@y"); ret != -1 {
		t.Errorf("ReplaceFrom() without From = %d, want -1", ret)
	}
	// nil message -> -1.
	if ret := m.ReplaceFrom(nil, "X", "sip:x@y"); ret != -1 {
		t.Errorf("ReplaceFrom(nil) = %d, want -1", ret)
	}
}

func TestRestoreFrom(t *testing.T) {
	m := New()

	orig := "<sip:alice@example.com>;tag=abc123"
	msg := &parser.SIPMsg{}
	addHeader(msg, "From", orig)

	m.ReplaceFrom(msg, "Bob", "sip:newfrom@example.com")
	if msg.From.Body.String() == orig {
		t.Fatalf("ReplaceFrom did not change the From header")
	}
	if ret := m.RestoreFrom(msg); ret != 0 {
		t.Fatalf("RestoreFrom() = %d, want 0", ret)
	}
	if got := msg.From.Body.String(); got != orig {
		t.Errorf("From body after restore = %q, want %q", got, orig)
	}

	// Restore without a prior Replace -> -1 (nothing to restore).
	msg2 := &parser.SIPMsg{}
	addHeader(msg2, "From", "<sip:x@y>;tag=t")
	if ret := m.RestoreFrom(msg2); ret != -1 {
		t.Errorf("RestoreFrom() without backup = %d, want -1", ret)
	}
	// nil message -> -1.
	if ret := m.RestoreFrom(nil); ret != -1 {
		t.Errorf("RestoreFrom(nil) = %d, want -1", ret)
	}
}

func TestReplaceToAndRestoreTo(t *testing.T) {
	m := New()

	orig := "<sip:bob@example.com>;tag=xyz"
	msg := &parser.SIPMsg{}
	addHeader(msg, "To", orig)

	if ret := m.ReplaceTo(msg, "Carol", "sip:newto@example.com"); ret != 0 {
		t.Fatalf("ReplaceTo() = %d, want 0", ret)
	}
	body := msg.To.Body.String()
	if !strings.Contains(body, "sip:newto@example.com") {
		t.Errorf("To body after replace = %q, want new URI", body)
	}
	if !strings.Contains(body, "tag=xyz") {
		t.Errorf("To body after replace = %q, want tag preserved", body)
	}

	if ret := m.RestoreTo(msg); ret != 0 {
		t.Fatalf("RestoreTo() = %d, want 0", ret)
	}
	if got := msg.To.Body.String(); got != orig {
		t.Errorf("To body after restore = %q, want %q", got, orig)
	}

	// No To header -> -1.
	if ret := m.ReplaceTo(&parser.SIPMsg{}, "X", "sip:x@y"); ret != -1 {
		t.Errorf("ReplaceTo() without To = %d, want -1", ret)
	}
	// Restore without backup -> -1.
	if ret := m.RestoreTo(&parser.SIPMsg{}); ret != -1 {
		t.Errorf("RestoreTo() without backup = %d, want -1", ret)
	}
}

func TestAuth(t *testing.T) {
	m := New()

	msg := &parser.SIPMsg{}
	// A request needs a method and RURI for the digest HA2.
	if msg.FirstLine == nil {
		msg.FirstLine = &parser.MsgStart{}
	}
	msg.FirstLine.Req = &parser.RequestLine{
		Method:      str.Mk("INVITE"),
		MethodValue: parser.MethodInvite,
		URI:         str.Mk("sip:bob@example.com"),
	}

	if ret := m.Auth(msg, "example.com", "alice", "secret"); ret != 0 {
		t.Fatalf("Auth() = %d, want 0", ret)
	}
	auths := msg.GetAllHeadersByType(parser.HdrAuthorization)
	if len(auths) != 1 {
		t.Fatalf("expected 1 Authorization header, got %d", len(auths))
	}
	body := auths[0].Body.String()
	// The digest response must carry the realm, username and a response.
	for _, want := range []string{"realm=\"example.com\"", "username=\"alice\"", "response="} {
		if !strings.Contains(body, want) {
			t.Errorf("Authorization body %q missing %q", body, want)
		}
	}

	// nil message -> -1.
	if ret := m.Auth(nil, "r", "u", "p"); ret != -1 {
		t.Errorf("Auth(nil) = %d, want -1", ret)
	}
}

func TestReqInDialog(t *testing.T) {
	m := New()

	// In-dialog request: both From and To tags present.
	inDialog := &parser.SIPMsg{}
	addHeader(inDialog, "From", "<sip:alice@example.com>;tag=abc")
	addHeader(inDialog, "To", "<sip:bob@example.com>;tag=xyz")
	if !m.ReqInDialog(inDialog) {
		t.Errorf("ReqInDialog() = false, want true (both tags present)")
	}

	// Initial request: From tag present, To tag absent.
	initial := &parser.SIPMsg{}
	addHeader(initial, "From", "<sip:alice@example.com>;tag=abc")
	addHeader(initial, "To", "<sip:bob@example.com>")
	if m.ReqInDialog(initial) {
		t.Errorf("ReqInDialog() = true, want false (no To tag)")
	}

	// No From tag -> not in-dialog.
	noFromTag := &parser.SIPMsg{}
	addHeader(noFromTag, "From", "<sip:alice@example.com>")
	addHeader(noFromTag, "To", "<sip:bob@example.com>;tag=xyz")
	if m.ReqInDialog(noFromTag) {
		t.Errorf("ReqInDialog() = true, want false (no From tag)")
	}

	// nil message -> false.
	if m.ReqInDialog(nil) {
		t.Errorf("ReqInDialog(nil) = true, want false")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultUAC()
	if d == nil {
		t.Fatalf("DefaultUAC() returned nil")
	}
	if d != DefaultUAC() {
		t.Fatalf("DefaultUAC() returned different instances after Init()")
	}

	// Package-level wrappers delegate to the default module.
	Init()
	raw, err := SendRequest("OPTIONS", "sip:target@example.com", "sip:a@b", "sip:c@d", nil, "")
	if err != nil {
		t.Fatalf("package SendRequest() error = %v", err)
	}
	if !strings.HasPrefix(string(raw), "OPTIONS sip:target@example.com SIP/2.0") {
		t.Errorf("package SendRequest() request line wrong: %q", string(raw)[:40])
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultUAC()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			msg := &parser.SIPMsg{}
			addHeader(msg, "From", "<sip:a@b>;tag=t")
			addHeader(msg, "To", "<sip:c@d>;tag=u")
			shared.ReplaceFrom(msg, "N", "sip:from@example.com")
			shared.RestoreFrom(msg)
			shared.ReqInDialog(msg)
		}(i)
	}
	wg.Wait()
}
