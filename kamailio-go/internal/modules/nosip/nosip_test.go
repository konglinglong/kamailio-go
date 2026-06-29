// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - NoSIP module tests.
 */

package nosip

import (
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

var testInvite = []byte("INVITE sip:bob@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.example.com;branch=z9hG4bK776\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1928301774\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: a84b4c76e66710@pc33.example.com\r\n" +
	"CSeq: 1 INVITE\r\n" +
	"Content-Length: 0\r\n\r\n")

func mustParse(t *testing.T, b []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(b)
	if err != nil {
		t.Fatalf("ParseMsg failed: %v", err)
	}
	return msg
}

// nonSIPMsg builds a MsgStart that is a request but lacks the SIP protocol
// flag, simulating an MSRP-style first line.
func nonSIPMsg(scheme string) *parser.SIPMsg {
	return &parser.SIPMsg{
		FirstLine: &parser.MsgStart{
			Type: parser.MsgRequest,
			Req: &parser.RequestLine{
				Method:      str.Mk("MSRP"),
				URI:         str.Mk(scheme + "alice@example.com;tcp"),
				Version:     str.Mk("MSRP/2.0"),
				MethodValue: parser.MethodOther,
			},
		},
	}
}

func TestIsNoSIP(t *testing.T) {
	m := New()
	sip := mustParse(t, testInvite)
	if m.IsNoSIP(sip) {
		t.Fatal("expected SIP message to not be NoSIP")
	}
	if !m.IsNoSIP(nonSIPMsg("msrp://")) {
		t.Fatal("expected msrp message to be NoSIP")
	}
	if !m.IsNoSIP(nil) {
		t.Fatal("expected nil to be NoSIP")
	}
}

func TestGetProtocol(t *testing.T) {
	m := New()
	sip := mustParse(t, testInvite)
	if got := m.GetProtocol(sip); got != "sip" {
		t.Fatalf("GetProtocol(sip) = %q, want sip", got)
	}
	if got := m.GetProtocol(nonSIPMsg("msrp://")); got != "msrp" {
		t.Fatalf("GetProtocol(msrp) = %q, want msrp", got)
	}
	if got := m.GetProtocol(nil); got != "unknown" {
		t.Fatalf("GetProtocol(nil) = %q, want unknown", got)
	}
}

func TestProcessNoSIP(t *testing.T) {
	m := New()
	sip := mustParse(t, testInvite)
	// SIP message -> not processed.
	if err := m.ProcessNoSIP(sip); err != nil {
		t.Fatalf("ProcessNoSIP(sip): %v", err)
	}
	if got := m.ProcessedCount(); got != 0 {
		t.Fatalf("ProcessedCount = %d, want 0", got)
	}
	if err := m.ProcessNoSIP(nonSIPMsg("msrp://")); err != nil {
		t.Fatalf("ProcessNoSIP(msrp): %v", err)
	}
	if got := m.ProcessedCount(); got != 1 {
		t.Fatalf("ProcessedCount = %d, want 1", got)
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	if DefaultNoSIP() == nil {
		t.Fatal("expected non-nil default")
	}
	sip := mustParse(t, testInvite)
	if IsNoSIP(sip) {
		t.Fatal("expected SIP message to not be NoSIP via global")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	msg := nonSIPMsg("msrp://")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.ProcessNoSIP(msg)
			_ = m.IsNoSIP(msg)
			_ = m.GetProtocol(msg)
			_ = m.ProcessedCount()
		}()
	}
	wg.Wait()
	if m.ProcessedCount() != 20 {
		t.Fatalf("ProcessedCount = %d, want 20", m.ProcessedCount())
	}
}
