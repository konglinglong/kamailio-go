// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - KEX module tests.
 */

package kex

import (
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

var (
	testInvite = []byte("INVITE sip:bob@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP pc33.example.com;branch=z9hG4bK776\r\n" +
		"From: Alice <sip:alice@example.com>;tag=1928301774\r\n" +
		"To: Bob <sip:bob@example.com>\r\n" +
		"Call-ID: a84b4c76e66710@pc33.example.com\r\n" +
		"CSeq: 314159 INVITE\r\n" +
		"Content-Length: 0\r\n\r\n")

	testReply = []byte("SIP/2.0 200 OK\r\n" +
		"Via: SIP/2.0/UDP pc33.example.com;branch=z9hG4bK776\r\n" +
		"From: Alice <sip:alice@example.com>;tag=1928301774\r\n" +
		"To: Bob <sip:bob@example.com>;tag=9876\r\n" +
		"Call-ID: a84b4c76e66710@pc33.example.com\r\n" +
		"CSeq: 314159 INVITE\r\n" +
		"Content-Length: 0\r\n\r\n")
)

func mustParse(t *testing.T, b []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(b)
	if err != nil {
		t.Fatalf("ParseMsg failed: %v", err)
	}
	return msg
}

func TestGetMsgTypeMethodStatus(t *testing.T) {
	m := New()
	req := mustParse(t, testInvite)
	if got := m.GetMsgType(req); got != "request" {
		t.Fatalf("GetMsgType(request) = %q, want request", got)
	}
	if got := m.GetMsgMethod(req); got != "INVITE" {
		t.Fatalf("GetMsgMethod = %q, want INVITE", got)
	}
	if got := m.GetMsgStatus(req); got != 0 {
		t.Fatalf("GetMsgStatus(request) = %d, want 0", got)
	}

	rep := mustParse(t, testReply)
	if got := m.GetMsgType(rep); got != "reply" {
		t.Fatalf("GetMsgType(reply) = %q, want reply", got)
	}
	if got := m.GetMsgMethod(rep); got != "" {
		t.Fatalf("GetMsgMethod(reply) = %q, want empty", got)
	}
	if got := m.GetMsgStatus(rep); got != 200 {
		t.Fatalf("GetMsgStatus(reply) = %d, want 200", got)
	}
}

func TestIsMyURI(t *testing.T) {
	m := New()
	m.AddMyURI("example.com")
	m.AddMyURI("sip:proxy@kamailio.org")
	if !m.IsMyURI("example.com") {
		t.Fatal("expected example.com to be my URI")
	}
	if !m.IsMyURI("sip:alice@example.com") {
		t.Fatal("expected host-part match for alice@example.com")
	}
	if !m.IsMyURI("sip:proxy@kamailio.org") {
		t.Fatal("expected exact match for proxy URI")
	}
	if m.IsMyURI("evil.com") {
		t.Fatal("expected evil.com to not be my URI")
	}
	if m.IsMyURI("") {
		t.Fatal("expected empty URI to not match")
	}
}

func TestNilMessage(t *testing.T) {
	m := New()
	if got := m.GetMsgType(nil); got != "unknown" {
		t.Fatalf("GetMsgType(nil) = %q, want unknown", got)
	}
	if got := m.GetMsgMethod(nil); got != "" {
		t.Fatalf("GetMsgMethod(nil) = %q, want empty", got)
	}
	if got := m.GetMsgStatus(nil); got != 0 {
		t.Fatalf("GetMsgStatus(nil) = %d, want 0", got)
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	DefaultKEX().AddMyURI("global.example.com")
	if !IsMyURI("global.example.com") {
		t.Fatal("expected global IsMyURI to match")
	}
	req := mustParse(t, testInvite)
	if GetMsgType(req) != "request" {
		t.Fatal("expected global GetMsgType request")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	m.AddMyURI("example.com")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.AddMyURI("host" + string(rune('a'+i%5)))
			_ = m.IsMyURI("example.com")
		}(i)
	}
	wg.Wait()
}
