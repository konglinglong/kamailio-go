// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - mangler module tests.
 */

package mangler

import (
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

const inviteSDP = "INVITE sip:bob@biloxi.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.atlanta.com;branch=z9hG4bK776\r\n" +
	"From: Alice <sip:alice@192.168.1.5>;tag=1928\r\n" +
	"To: Bob <sip:bob@biloxi.com>\r\n" +
	"Contact: <sip:alice@192.168.1.5:5060>\r\n" +
	"Content-Type: application/sdp\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n" +
	"v=0\r\no=- 1 1 IN IP4 192.168.1.5\r\n" +
	"c=IN IP4 192.168.1.5\r\nm=audio 5004 RTP/AVP 0\r\n"

func parseMsg(t *testing.T) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg([]byte(inviteSDP))
	if err != nil {
		t.Fatalf("ParseMsg: %v", err)
	}
	return msg
}

func TestMangleContact(t *testing.T) {
	m := New()
	msg := parseMsg(t)
	n := m.MangleContact(msg)
	if n != 1 {
		t.Fatalf("MangleContact = %d, want 1", n)
	}
	c := msg.Contact
	if c == nil {
		t.Fatal("no Contact header")
	}
	if !contains(c.Body.String(), "@"+MangleIP) {
		t.Errorf("Contact body = %q, want to contain %q", c.Body.String(), "@"+MangleIP)
	}
}

func TestMangleSDP(t *testing.T) {
	m := New()
	msg := parseMsg(t)
	n := m.MangleSDP(msg)
	if n != 1 {
		t.Fatalf("MangleSDP = %d, want 1", n)
	}
	b := bodyString(msg)
	if !contains(b, "c=IN IP4 "+MangleIP) {
		t.Errorf("body does not contain mangled c= line: %q", b)
	}
}

func TestUnmangleContact(t *testing.T) {
	m := New()
	msg := parseMsg(t)
	m.MangleContact(msg)
	n := m.UnmangleContact(msg)
	if n != 1 {
		t.Fatalf("UnmangleContact = %d, want 1", n)
	}
	c := msg.Contact
	if contains(c.Body.String(), "@"+MangleIP) {
		t.Errorf("Contact still mangled: %q", c.Body.String())
	}
}

func TestNilSafety(t *testing.T) {
	m := New()
	if m.MangleContact(nil) != 0 {
		t.Error("MangleContact(nil) != 0")
	}
	if m.MangleSDP(nil) != 0 {
		t.Error("MangleSDP(nil) != 0")
	}
	if m.UnmangleContact(nil) != 0 {
		t.Error("UnmangleContact(nil) != 0")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
