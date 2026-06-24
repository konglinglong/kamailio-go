// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the XPrint module.
 */

package xprint

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
	}
	return h
}

// newRequest builds a SIP request with the given first line and headers.
func newRequest(method, uri, callID, from, to string) *parser.SIPMsg {
	msg := &parser.SIPMsg{
		FirstLine: &parser.MsgStart{Req: &parser.RequestLine{
			Method:  str.Mk(method),
			URI:     str.Mk(uri),
			Version: str.Mk("SIP/2.0"),
		}},
	}
	addHeader(msg, "Call-ID", callID)
	addHeader(msg, "From", from)
	addHeader(msg, "To", to)
	return msg
}

func TestPrint(t *testing.T) {
	m := New()
	msg := newRequest(
		"INVITE",
		"sip:bob@example.com",
		"call-1@example.com",
		"<sip:alice@example.com>;tag=ftag",
		"<sip:bob@example.com>",
	)
	out := m.Print(msg, "ci=%ci from=%fu to=%tu uri=%ru method=%rm")
	want := "ci=call-1@example.com from=<sip:alice@example.com>;tag=ftag to=<sip:bob@example.com> uri=sip:bob@example.com method=INVITE"
	if out != want {
		t.Errorf("Print() =\n  %q\nwant\n  %q", out, want)
	}
	// nil msg -> format unchanged.
	if got := m.Print(nil, "ci=%ci"); got != "ci=%ci" {
		t.Errorf("Print(nil) = %q, want format unchanged", got)
	}
	// Reply status code token.
	reply := &parser.SIPMsg{
		FirstLine: &parser.MsgStart{Reply: &parser.ReplyLine{
			Version:    str.Mk("SIP/2.0"),
			Status:     str.Mk("200"),
			Reason:     str.Mk("OK"),
			StatusCode: 200,
		}},
	}
	if got := m.Print(reply, "status=%rs"); got != "status=200" {
		t.Errorf("Print(reply) = %q, want status=200", got)
	}
}

func TestPrintHeadersAndBody(t *testing.T) {
	m := New()
	msg := newRequest("INVITE", "sip:bob@example.com", "call-1@example.com",
		"<sip:alice@example.com>;tag=ftag", "<sip:bob@example.com>")
	msg.Body = []byte("v=0\r\no=- 1 1 IN IP4 1.2.3.4\r\n")

	hdrs := m.PrintHeaders(msg)
	if !strings.Contains(hdrs, "Call-ID: call-1@example.com") {
		t.Errorf("PrintHeaders() missing Call-ID:\n%s", hdrs)
	}
	if !strings.Contains(hdrs, "From: <sip:alice@example.com>;tag=ftag") {
		t.Errorf("PrintHeaders() missing From:\n%s", hdrs)
	}
	// Every header line ends with a newline and has a colon.
	for _, line := range strings.Split(strings.TrimRight(hdrs, "\n"), "\n") {
		if !strings.Contains(line, ": ") {
			t.Errorf("header line %q missing ': '", line)
		}
	}
	body := m.PrintBody(msg)
	if body != "v=0\r\no=- 1 1 IN IP4 1.2.3.4\r\n" {
		t.Errorf("PrintBody() = %q", body)
	}
	// No body -> empty.
	noBody := newRequest("OPTIONS", "sip:x@y", "c", "<sip:a@b>;tag=t", "<sip:c@d>")
	if m.PrintBody(noBody) != "" {
		t.Errorf("PrintBody() with no body should be empty")
	}
}

func TestPrintFirstLine(t *testing.T) {
	m := New()
	msg := newRequest("INVITE", "sip:bob@example.com", "c", "<sip:a@b>;tag=t", "<sip:c@d>")
	if got := m.PrintFirstLine(msg); got != "INVITE sip:bob@example.com SIP/2.0" {
		t.Errorf("PrintFirstLine(request) = %q", got)
	}
	reply := &parser.SIPMsg{
		FirstLine: &parser.MsgStart{Reply: &parser.ReplyLine{
			Version: str.Mk("SIP/2.0"),
			Status:  str.Mk("200"),
			Reason:  str.Mk("OK"),
		}},
	}
	if got := m.PrintFirstLine(reply); got != "SIP/2.0 200 OK" {
		t.Errorf("PrintFirstLine(reply) = %q", got)
	}
	// nil / empty -> empty.
	if got := m.PrintFirstLine(nil); got != "" {
		t.Errorf("PrintFirstLine(nil) = %q, want empty", got)
	}
	if got := m.PrintFirstLine(&parser.SIPMsg{}); got != "" {
		t.Errorf("PrintFirstLine(empty) = %q, want empty", got)
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	msg := newRequest("INVITE", "sip:bob@example.com", "call-1@example.com",
		"<sip:alice@example.com>;tag=ftag", "<sip:bob@example.com>")
	msg.Body = []byte("body")
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			m.Print(msg, "ci=%ci method=%rm")
			m.PrintHeaders(msg)
			m.PrintBody(msg)
			m.PrintFirstLine(msg)
		}()
	}
	wg.Wait()
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultXPrint()
	if d == nil {
		t.Fatal("DefaultXPrint() = nil")
	}
	if d != DefaultXPrint() {
		t.Fatal("DefaultXPrint() returned different instances")
	}
	msg := newRequest("OPTIONS", "sip:x@y", "c", "<sip:a@b>;tag=t", "<sip:c@d>")
	if got := d.PrintFirstLine(msg); !strings.HasPrefix(got, "OPTIONS") {
		t.Errorf("default PrintFirstLine() = %q", got)
	}
}
