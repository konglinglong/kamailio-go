// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - nat_traversal module tests.
 */

package nat_traversal

import (
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

const natInvite = "INVITE sip:bob@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP 203.0.113.5;branch=z9hG4bK1\r\n" +
	"From: Alice <sip:alice@203.0.113.5>;tag=1\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Contact: <sip:alice@192.168.1.10:5060>\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n"

const noNatInvite = "INVITE sip:bob@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP 203.0.113.5;branch=z9hG4bK1\r\n" +
	"From: Alice <sip:alice@203.0.113.5>;tag=1\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Contact: <sip:alice@203.0.113.5:5060>\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n"

func parseMsg(t *testing.T, raw string) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg([]byte(raw))
	if err != nil {
		t.Fatalf("ParseMsg: %v", err)
	}
	return msg
}

func TestCheckNAT(t *testing.T) {
	m := New()
	if !m.CheckNAT(parseMsg(t, natInvite)) {
		t.Error("CheckNAT(natInvite) = false, want true")
	}
	if m.CheckNAT(parseMsg(t, noNatInvite)) {
		t.Error("CheckNAT(noNatInvite) = true, want false")
	}
	if m.CheckNAT(nil) {
		t.Error("CheckNAT(nil) = true, want false")
	}
}

func TestKeepAlive(t *testing.T) {
	m := New()
	if err := m.KeepAlive(""); err == nil {
		t.Error("KeepAlive(empty) expected error")
	}
	if err := m.KeepAlive("sip:alice@192.168.1.10"); err != nil {
		t.Fatalf("KeepAlive: %v", err)
	}
	if m.LastKeepAlive("sip:alice@192.168.1.10").IsZero() {
		t.Error("LastKeepAlive should not be zero")
	}
	m.ProcessKeepAlive("sip:alice@192.168.1.10")
	if m.LastKeepAlive("sip:alice@192.168.1.10").IsZero() {
		t.Error("LastKeepAlive should not be zero after ProcessKeepAlive")
	}
}

func TestIsKeepAliveNeeded(t *testing.T) {
	m := New()
	msg := parseMsg(t, natInvite)
	if !m.IsKeepAliveNeeded(msg) {
		t.Error("IsKeepAliveNeeded should be true before any keep-alive")
	}
	m.ProcessKeepAlive(msg.Contact.Body.String())
	if m.IsKeepAliveNeeded(msg) {
		t.Error("IsKeepAliveNeeded should be false right after keep-alive")
	}
	if m.IsKeepAliveNeeded(parseMsg(t, noNatInvite)) {
		t.Error("IsKeepAliveNeeded(noNAT) should be false")
	}
}
