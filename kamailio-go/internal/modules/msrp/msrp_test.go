// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - msrp module tests.
 */

package msrp

import (
	"testing"
)

func TestIsMSRP(t *testing.T) {
	m := New()
	if !m.IsMSRP([]byte("MSRP SEND txn\r\n")) {
		t.Error("IsMSRP(MSRP...) = false, want true")
	}
	if m.IsMSRP([]byte("SIP/2.0 200 OK\r\n")) {
		t.Error("IsMSRP(SIP...) = true, want false")
	}
	if m.IsMSRP([]byte("MS")) {
		t.Error("IsMSRP(short) = true, want false")
	}
}

func TestParseMessage(t *testing.T) {
	m := New()
	raw := []byte("MSRP SEND abc123\r\n" +
		"To-Path: msrp://bob.example.com\r\n" +
		"From-Path: msrp://alice.example.com\r\n" +
		"Message-ID: msg-1\r\n" +
		"\r\n" +
		"hello body\r\n" +
		"-------abc123$\r\n")
	msg, err := m.ParseMessage(raw)
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if msg.Method != "SEND" {
		t.Errorf("Method = %q, want SEND", msg.Method)
	}
	if msg.TxnID != "abc123" {
		t.Errorf("TxnID = %q, want abc123", msg.TxnID)
	}
	if msg.ToPath != "msrp://bob.example.com" {
		t.Errorf("ToPath = %q", msg.ToPath)
	}
	if msg.FromPath != "msrp://alice.example.com" {
		t.Errorf("FromPath = %q", msg.FromPath)
	}
	if msg.MessageID != "msg-1" {
		t.Errorf("MessageID = %q", msg.MessageID)
	}
	if msg.Body != "hello body" {
		t.Errorf("Body = %q, want 'hello body'", msg.Body)
	}
}

func TestBuildAndRoundTrip(t *testing.T) {
	m := New()
	orig := &MSRPMsg{
		Method:    "SEND",
		TxnID:     "tx1",
		ToPath:    "msrp://a.example.com",
		FromPath:  "msrp://b.example.com",
		MessageID: "m1",
		Body:      "payload",
	}
	data := m.BuildMessage(orig)
	if data == nil {
		t.Fatal("BuildMessage returned nil")
	}
	if !m.IsMSRP(data) {
		t.Error("built message is not MSRP")
	}
	parsed, err := m.ParseMessage(data)
	if err != nil {
		t.Fatalf("ParseMessage(built): %v", err)
	}
	if parsed.Method != orig.Method {
		t.Errorf("Method = %q, want %q", parsed.Method, orig.Method)
	}
	if parsed.Body != orig.Body {
		t.Errorf("Body = %q, want %q", parsed.Body, orig.Body)
	}
	if parsed.TxnID != orig.TxnID {
		t.Errorf("TxnID = %q, want %q", parsed.TxnID, orig.TxnID)
	}
}

func TestParseError(t *testing.T) {
	m := New()
	if _, err := m.ParseMessage([]byte("SIP/2.0 200 OK\r\n")); err == nil {
		t.Error("ParseMessage(non-MSRP) expected error")
	}
}
