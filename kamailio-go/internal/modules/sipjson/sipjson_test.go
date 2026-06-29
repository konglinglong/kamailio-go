// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SipJSON module tests.
 */
package sipjson

import (
	"strings"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

var inviteBytes = []byte("INVITE sip:bob@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: cid-json@10.0.0.1\r\n" +
	"CSeq: 1 INVITE\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

var replyBytes = []byte("SIP/2.0 200 OK\r\n" +
	"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1\r\n" +
	"To: Bob <sip:bob@example.com>;tag=2\r\n" +
	"Call-ID: cid-json@10.0.0.1\r\n" +
	"CSeq: 1 INVITE\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

func mustParseMsg(t *testing.T, raw []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("failed to parse message: %v", err)
	}
	return msg
}

func TestToJSONRequest(t *testing.T) {
	m := NewSipJSONModule()
	msg := mustParseMsg(t, inviteBytes)
	out, err := m.ToJSON(msg)
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}
	if !strings.Contains(out, `"method":"INVITE"`) {
		t.Errorf("json missing method: %s", out)
	}
	if !strings.Contains(out, `"call_id":"cid-json@10.0.0.1"`) {
		t.Errorf("json missing call_id: %s", out)
	}
	if !strings.Contains(out, `"raw":"INVITE sip:bob`) {
		t.Errorf("json missing raw payload: %s", out)
	}
}

func TestRoundTrip(t *testing.T) {
	m := NewSipJSONModule()
	msg := mustParseMsg(t, inviteBytes)
	out, err := m.ToJSON(msg)
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}
	back, err := m.FromJSON(out)
	if err != nil {
		t.Fatalf("FromJSON failed: %v", err)
	}
	if back == nil {
		t.Fatal("expected non-nil message")
	}
	if !back.IsRequest() {
		t.Error("expected a request after round-trip")
	}
	if back.CallID == nil || back.CallID.Body.String() != "cid-json@10.0.0.1" {
		t.Errorf("CallID after round-trip = %v", back.CallID)
	}
}

func TestFromJSONErrors(t *testing.T) {
	m := NewSipJSONModule()
	if _, err := m.FromJSON(""); err == nil {
		t.Error("expected error for empty json")
	}
	if _, err := m.FromJSON("not json"); err == nil {
		t.Error("expected error for invalid json")
	}
	if _, err := m.ToJSON(nil); err == nil {
		t.Error("expected error for nil message")
	}
	// A JSON document with no raw payload fails.
	if _, err := m.FromJSON(`{"method":"INVITE"}`); err == nil {
		t.Error("expected error for json without raw payload")
	}
}

func TestReplyRoundTrip(t *testing.T) {
	m := NewSipJSONModule()
	msg := mustParseMsg(t, replyBytes)
	out, err := m.ToJSON(msg)
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}
	if !strings.Contains(out, `"status":200`) {
		t.Errorf("json missing status: %s", out)
	}
	back, err := m.FromJSON(out)
	if err != nil {
		t.Fatalf("FromJSON failed: %v", err)
	}
	if !back.IsReply() {
		t.Error("expected a reply after round-trip")
	}
	if back.StatusCode() != 200 {
		t.Errorf("status after round-trip = %d, want 200", back.StatusCode())
	}
}
