// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the Regex module.
 */

package regex

import (
	"strings"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

func buildMsg() *parser.SIPMsg {
	msg := &parser.SIPMsg{}
	msg.FirstLine = &parser.MsgStart{
		Type:  parser.MsgRequest,
		Flags: parser.FLINEFlagProtoSIP,
		Req: &parser.RequestLine{
			Method:      str.Mk("INVITE"),
			URI:         str.Mk("sip:bob@example.com"),
			MethodValue: parser.MethodInvite,
		},
	}
	msg.AddHeader("Via", "SIP/2.0/UDP 192.0.2.1:5060;branch=z9hG4bKabc")
	msg.AddHeader("From", "<sip:alice@example.com>;tag=1234")
	msg.AddHeader("To", "<sip:bob@example.com>")
	msg.AddHeader("Call-ID", "call-1234@example.com")
	msg.AddHeader("CSeq", "1 INVITE")
	msg.AddHeader("Contact", "<sip:alice@192.0.2.1:5060>")
	msg.Body = []byte("v=0\r\no=- 1 1 IN IP4 192.0.2.1\r\n")
	return msg
}

func TestCompile(t *testing.T) {
	m := New()

	re, err := m.Compile(`^sip:`)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if re == nil {
		t.Fatalf("Compile() returned nil regexp")
	}
	if !re.MatchString("sip:alice@example.com") {
		t.Errorf("compiled regexp did not match")
	}

	// Empty pattern -> error.
	if _, err := m.Compile(""); err == nil {
		t.Errorf("Compile(\"\") should error")
	}
	// Invalid pattern -> error.
	if _, err := m.Compile(`(unclosed`); err == nil {
		t.Errorf("Compile(invalid) should error")
	}
}

func TestMatch(t *testing.T) {
	m := New()

	if !m.Match(`\d+`, "abc123def") {
		t.Errorf("Match(digits) = false, want true")
	}
	if m.Match(`^xyz`, "abc123def") {
		t.Errorf("Match(^xyz) = true, want false")
	}
	// Invalid pattern -> false (no panic).
	if m.Match(`(bad`, "abc") {
		t.Errorf("Match(invalid) = true, want false")
	}
	// Empty pattern -> false.
	if m.Match("", "abc") {
		t.Errorf("Match(empty) = true, want false")
	}
}

func TestMatchMsgAndBody(t *testing.T) {
	m := New()
	msg := buildMsg()

	// Match against the whole message.
	if !m.MatchMsg(msg, `INVITE sip:bob@example.com`) {
		t.Errorf("MatchMsg(request line) = false, want true")
	}
	if !m.MatchMsg(msg, `Call-ID: call-1234@example.com`) {
		t.Errorf("MatchMsg(Call-ID) = false, want true")
	}

	// Match against the body.
	if !m.MatchBody(msg, `o=- 1 1 IN IP4 192.0.2.1`) {
		t.Errorf("MatchBody(SDP o= line) = false, want true")
	}
	if m.MatchBody(msg, `NOTPRESENT`) {
		t.Errorf("MatchBody(NOTPRESENT) = true, want false")
	}

	// nil msg -> false.
	if m.MatchMsg(nil, `x`) {
		t.Errorf("MatchMsg(nil) = true, want false")
	}
	if m.MatchBody(nil, `x`) {
		t.Errorf("MatchBody(nil) = true, want false")
	}
}

func TestMatchHeader(t *testing.T) {
	m := New()
	msg := buildMsg()

	if !m.MatchHeader(msg, "Call-ID", `call-1234@example\.com`) {
		t.Errorf("MatchHeader(Call-ID) = false, want true")
	}
	// Case-insensitive header name lookup.
	if !m.MatchHeader(msg, "call-id", `call-1234`) {
		t.Errorf("MatchHeader(call-id lower) = false, want true")
	}
	// Header present but pattern does not match.
	if m.MatchHeader(msg, "From", `nomatch`) {
		t.Errorf("MatchHeader(From nomatch) = true, want false")
	}
	// Missing header -> false.
	if m.MatchHeader(msg, "X-Missing", `x`) {
		t.Errorf("MatchHeader(X-Missing) = true, want false")
	}
	// nil msg -> false.
	if m.MatchHeader(nil, "Call-ID", `x`) {
		t.Errorf("MatchHeader(nil) = true, want false")
	}
}

func TestReplace(t *testing.T) {
	m := New()

	out, err := m.Replace(`\d+`, "abc123def456", "N")
	if err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	if out != "abcNdefN" {
		t.Errorf("Replace() = %q, want abcNdefN", out)
	}

	// Empty pattern -> error.
	if _, err := m.Replace("", "abc", "x"); err == nil {
		t.Errorf("Replace(empty) should error")
	}
	// Invalid pattern -> error.
	if _, err := m.Replace(`(bad`, "abc", "x"); err == nil {
		t.Errorf("Replace(invalid) should error")
	}
}

func TestReplaceMsg(t *testing.T) {
	m := New()
	msg := buildMsg()

	// Replace the Call-ID value in the rebuilt message.
	count := m.ReplaceMsg(msg, `call-1234@example\.com`, "call-9999@example.com")
	if count < 1 {
		t.Fatalf("ReplaceMsg() count = %d, want >= 1", count)
	}
	if !strings.Contains(string(msg.Buf), "call-9999@example.com") {
		t.Errorf("msg.Buf does not contain the replacement")
	}
	if strings.Contains(string(msg.Buf), "call-1234@example.com") {
		t.Errorf("msg.Buf still contains the original value")
	}

	// No match -> 0 replacements, buffer unchanged length-wise.
	msg2 := buildMsg()
	before := len(msg2.RebuildMessage())
	if got := m.ReplaceMsg(msg2, `ZZZ-NOMATCH-ZZZ`, "x"); got != 0 {
		t.Errorf("ReplaceMsg(nomatch) = %d, want 0", got)
	}
	after := len(msg2.RebuildMessage())
	if before != after {
		t.Errorf("buffer length changed on no-match: %d -> %d", before, after)
	}

	// nil msg -> -1.
	if got := m.ReplaceMsg(nil, `x`, "y"); got != -1 {
		t.Errorf("ReplaceMsg(nil) = %d, want -1", got)
	}
	// Invalid pattern -> -1.
	if got := m.ReplaceMsg(msg, `(bad`, "y"); got != -1 {
		t.Errorf("ReplaceMsg(invalid) = %d, want -1", got)
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultRegex()
	if d == nil {
		t.Fatalf("DefaultRegex() returned nil")
	}
	if d != DefaultRegex() {
		t.Fatalf("DefaultRegex() returned different instances after Init()")
	}
	if !d.Match(`\d`, "a1b") {
		t.Errorf("default Match() = false, want true")
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultRegex()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			shared.Match(`\d+`, "abc123")
			shared.Compile(`^sip:`)
			msg := buildMsg()
			shared.MatchMsg(msg, `INVITE`)
			shared.MatchBody(msg, `o=-`)
			shared.MatchHeader(msg, "Call-ID", `call-`)
			shared.Replace(`\d`, "N", "a1b2")
			shared.ReplaceMsg(msg, `call-1234`, "call-9")
		}()
	}
	wg.Wait()
}
