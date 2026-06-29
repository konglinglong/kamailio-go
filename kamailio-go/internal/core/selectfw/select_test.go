// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Tests for the select framework.
 */

package selectfw

import (
	"strings"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

func parseMsg(t *testing.T, raw string) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg([]byte(raw))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return msg
}

const inviteMsg = "INVITE sip:bob%40example.com@ims.example.com:5060;transport=tcp SIP/2.0\r\n" +
	"Via: SIP/2.0/TCP 192.0.2.1:5060;branch=z9hG4bKabc123\r\n" +
	"Via: SIP/2.0/UDP 192.0.2.2:5061;branch=z9hG4bKdef456\r\n" +
	"From: \"Alice\" <sip:alice@example.com>;tag=aliceTag001\r\n" +
	"To: <sip:bob@example.com>\r\n" +
	"Call-ID: callid-12345@example.com\r\n" +
	"CSeq: 1 INVITE\r\n" +
	"Contact: <sip:alice@192.168.1.1:5060>\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n"

func TestParseSelect_SimplePath(t *testing.T) {
	p, err := ParseSelectFromExpr("@via.1.host")
	if err != nil {
		t.Fatalf("ParseSelectFromExpr: %v", err)
	}
	if len(p.Segments) != 3 {
		t.Fatalf("expected 3 segments, got %d: %v", len(p.Segments), p.Segments)
	}
	if p.Segments[0] != "via" || p.Segments[1] != "1" || p.Segments[2] != "host" {
		t.Errorf("unexpected segments: %v", p.Segments)
	}
}

func TestParseSelect_QuotedSegment(t *testing.T) {
	p, err := ParseSelectFromExpr(`@msg.header."P-Asserted-Identity"`)
	if err != nil {
		t.Fatalf("ParseSelectFromExpr: %v", err)
	}
	if len(p.Segments) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(p.Segments))
	}
	if p.Segments[2] != "P-Asserted-Identity" {
		t.Errorf("expected 'P-Asserted-Identity', got %q", p.Segments[2])
	}
}

func TestParseSelect_MissingAt(t *testing.T) {
	_, err := ParseSelectFromExpr("via.1.host")
	if err == nil {
		t.Fatal("expected error for missing @")
	}
}

func TestParseSelect_Empty(t *testing.T) {
	_, err := ParseSelectFromExpr("@")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestSelect_RURI(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@ruri", msg)
	if err != nil {
		t.Fatalf("Evaluate @ruri: %v", err)
	}
	if !strings.Contains(v.Str, "ims.example.com") {
		t.Errorf("expected RURI to contain ims.example.com, got %q", v.Str)
	}
}

func TestSelect_RURIUser(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@ruri.user", msg)
	if err != nil {
		t.Fatalf("Evaluate @ruri.user: %v", err)
	}
	if !strings.Contains(v.Str, "bob") {
		t.Errorf("expected user to contain 'bob', got %q", v.Str)
	}
}

func TestSelect_RURIHost(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@ruri.host", msg)
	if err != nil {
		t.Fatalf("Evaluate @ruri.host: %v", err)
	}
	if v.Str != "ims.example.com" {
		t.Errorf("expected host 'ims.example.com', got %q", v.Str)
	}
}

func TestSelect_RURITransport(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@ruri.transport", msg)
	if err != nil {
		t.Fatalf("Evaluate @ruri.transport: %v", err)
	}
	if v.Str != "tcp" {
		t.Errorf("expected transport 'tcp', got %q", v.Str)
	}
}

func TestSelect_Via1Host(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@via.1.host", msg)
	if err != nil {
		t.Fatalf("Evaluate @via.1.host: %v", err)
	}
	if v.Str != "192.0.2.1" {
		t.Errorf("expected '192.0.2.1', got %q", v.Str)
	}
}

func TestSelect_Via2Host(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@via.2.host", msg)
	if err != nil {
		t.Fatalf("Evaluate @via.2.host: %v", err)
	}
	if v.Str != "192.0.2.2" {
		t.Errorf("expected '192.0.2.2', got %q", v.Str)
	}
}

func TestSelect_Via1Transport(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@via.1.transport", msg)
	if err != nil {
		t.Fatalf("Evaluate @via.1.transport: %v", err)
	}
	if v.Str != "TCP" {
		t.Errorf("expected 'TCP', got %q", v.Str)
	}
}

func TestSelect_Via1Branch(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@via.1.branch", msg)
	if err != nil {
		t.Fatalf("Evaluate @via.1.branch: %v", err)
	}
	if v.Str != "z9hG4bKabc123" {
		t.Errorf("expected 'z9hG4bKabc123', got %q", v.Str)
	}
}

func TestSelect_From(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@from", msg)
	if err != nil {
		t.Fatalf("Evaluate @from: %v", err)
	}
	if !strings.Contains(v.Str, "alice@example.com") {
		t.Errorf("expected from to contain alice@example.com, got %q", v.Str)
	}
}

func TestSelect_FromTag(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@from.tag", msg)
	if err != nil {
		t.Fatalf("Evaluate @from.tag: %v", err)
	}
	if v.Str != "aliceTag001" {
		t.Errorf("expected 'aliceTag001', got %q", v.Str)
	}
}

func TestSelect_FromUser(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@from.user", msg)
	if err != nil {
		t.Fatalf("Evaluate @from.user: %v", err)
	}
	if v.Str != "alice" {
		t.Errorf("expected 'alice', got %q", v.Str)
	}
}

func TestSelect_To(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@to", msg)
	if err != nil {
		t.Fatalf("Evaluate @to: %v", err)
	}
	if !strings.Contains(v.Str, "bob@example.com") {
		t.Errorf("expected to contain bob@example.com, got %q", v.Str)
	}
}

func TestSelect_ToUser(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@to.user", msg)
	if err != nil {
		t.Fatalf("Evaluate @to.user: %v", err)
	}
	if v.Str != "bob" {
		t.Errorf("expected 'bob', got %q", v.Str)
	}
}

func TestSelect_CallID(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@callid", msg)
	if err != nil {
		t.Fatalf("Evaluate @callid: %v", err)
	}
	if v.Str != "callid-12345@example.com" {
		t.Errorf("expected 'callid-12345@example.com', got %q", v.Str)
	}
}

func TestSelect_CSeq(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@cseq", msg)
	if err != nil {
		t.Fatalf("Evaluate @cseq: %v", err)
	}
	if v.Str != "1 INVITE" {
		t.Errorf("expected '1 INVITE', got %q", v.Str)
	}
}

func TestSelect_CSeqNum(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@cseq.num", msg)
	if err != nil {
		t.Fatalf("Evaluate @cseq.num: %v", err)
	}
	if v.Str != "1" {
		t.Errorf("expected '1', got %q", v.Str)
	}
}

func TestSelect_CSeqMethod(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@cseq.method", msg)
	if err != nil {
		t.Fatalf("Evaluate @cseq.method: %v", err)
	}
	if v.Str != "INVITE" {
		t.Errorf("expected 'INVITE', got %q", v.Str)
	}
}

func TestSelect_Method(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@method", msg)
	if err != nil {
		t.Fatalf("Evaluate @method: %v", err)
	}
	if v.Str != "INVITE" {
		t.Errorf("expected 'INVITE', got %q", v.Str)
	}
}

func TestSelect_Contact(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@contact", msg)
	if err != nil {
		t.Fatalf("Evaluate @contact: %v", err)
	}
	if !strings.Contains(v.Str, "alice@192.168.1.1") {
		t.Errorf("expected alice@192.168.1.1, got %q", v.Str)
	}
}

func TestSelect_ContactHost(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@contact.host", msg)
	if err != nil {
		t.Fatalf("Evaluate @contact.host: %v", err)
	}
	if v.Str != "192.168.1.1" {
		t.Errorf("expected '192.168.1.1', got %q", v.Str)
	}
}

func TestSelect_MsgHeader(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate(`@msg.header."Call-ID"`, msg)
	if err != nil {
		t.Fatalf("Evaluate @msg.header.Call-ID: %v", err)
	}
	if v.Str != "callid-12345@example.com" {
		t.Errorf("expected callid, got %q", v.Str)
	}
}

func TestSelect_MsgHeaderMissing(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	_, err := Evaluate(`@msg.header."X-Nonexistent"`, msg)
	if err == nil {
		t.Fatal("expected error for missing header")
	}
}

func TestSelect_MsgBody(t *testing.T) {
	raw := "INVITE sip:bob@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 127.0.0.1\r\n" +
		"Content-Length: 12\r\n" +
		"\r\n" +
		"v=0\r\no=root"
	msg := parseMsg(t, raw)
	v, err := Evaluate("@msg.body", msg)
	if err != nil {
		t.Fatalf("Evaluate @msg.body: %v", err)
	}
	if !strings.Contains(v.Str, "v=0") {
		t.Errorf("expected body to contain v=0, got %q", v.Str)
	}
}

func TestSelect_MsgFirstLine(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@msg.first_line", msg)
	if err != nil {
		t.Fatalf("Evaluate @msg.first_line: %v", err)
	}
	if !strings.Contains(v.Str, "INVITE") {
		t.Errorf("expected first line to contain INVITE, got %q", v.Str)
	}
}

func TestSelect_User(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@user", msg)
	if err != nil {
		t.Fatalf("Evaluate @user: %v", err)
	}
	if !strings.Contains(v.Str, "bob") {
		t.Errorf("expected user to contain bob, got %q", v.Str)
	}
}

func TestSelect_Domain(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@domain", msg)
	if err != nil {
		t.Fatalf("Evaluate @domain: %v", err)
	}
	if v.Str != "ims.example.com" {
		t.Errorf("expected 'ims.example.com', got %q", v.Str)
	}
}

func TestSelect_Version(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	v, err := Evaluate("@v", msg)
	if err != nil {
		t.Fatalf("Evaluate @v: %v", err)
	}
	if v.Str != "SIP/2.0" {
		t.Errorf("expected 'SIP/2.0', got %q", v.Str)
	}
}

func TestSelect_UnknownComponent(t *testing.T) {
	msg := parseMsg(t, inviteMsg)
	_, err := Evaluate("@nonexistent", msg)
	if err == nil {
		t.Fatal("expected error for unknown component")
	}
}

func TestSelect_ReplyMessage(t *testing.T) {
	raw := "SIP/2.0 200 OK\r\n" +
		"Via: SIP/2.0/UDP 127.0.0.1\r\n" +
		"From: <sip:alice@example.com>;tag=abc\r\n" +
		"To: <sip:bob@example.com>;tag=xyz\r\n" +
		"Call-ID: call-1\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n"
	msg := parseMsg(t, raw)
	v, err := Evaluate("@v", msg)
	if err != nil {
		t.Fatalf("Evaluate @v on reply: %v", err)
	}
	if v.Str != "SIP/2.0" {
		t.Errorf("expected 'SIP/2.0', got %q", v.Str)
	}
}

func TestSelect_NilMessage(t *testing.T) {
	_, err := Evaluate("@ruri", nil)
	if err == nil {
		t.Fatal("expected error for nil message")
	}
}

func TestSelectRegistry_CustomHandler(t *testing.T) {
	r := NewSelectRegistry()
	r.Register("custom", func(msg *parser.SIPMsg, segs []string) (SelectValue, error) {
		return strVal("custom-value"), nil
	})
	msg := parseMsg(t, inviteMsg)
	v, err := r.Evaluate(msg, SelectPath{Segments: []string{"custom"}})
	if err != nil {
		t.Fatalf("custom handler: %v", err)
	}
	if v.Str != "custom-value" {
		t.Errorf("expected 'custom-value', got %q", v.Str)
	}
}

func TestSelectValue_Str2(t *testing.T) {
	v := SelectValue{Str: "hello", OK: true}
	s := v.Str2()
	if s.String() != "hello" {
		t.Errorf("expected 'hello', got %q", s.String())
	}
	empty := SelectValue{OK: false}
	if empty.Str2().Len != 0 {
		t.Error("unset value should produce empty str.Str")
	}
}
