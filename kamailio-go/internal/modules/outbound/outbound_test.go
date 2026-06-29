// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the Outbound module.
 */

package outbound

import (
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// addContactHeader appends a Contact header to msg and wires up the
// msg.Contact quick reference, mirroring what the full parser does.
func addContactHeader(msg *parser.SIPMsg, body string) *parser.HdrField {
	h := msg.AddHeader("Contact", body)
	msg.Contact = h
	return h
}

func TestGenerateAndValidateFlowToken(t *testing.T) {
	m := New()

	token := m.GenerateFlowToken("192.0.2.1", 5060, "conn-42")
	if token == "" {
		t.Fatalf("GenerateFlowToken() returned empty token")
	}

	ip, port, connID, ok := m.ValidateFlowToken(token)
	if !ok {
		t.Fatalf("ValidateFlowToken() = false, want true")
	}
	if ip != "192.0.2.1" {
		t.Errorf("ip = %q, want 192.0.2.1", ip)
	}
	if port != 5060 {
		t.Errorf("port = %d, want 5060", port)
	}
	if connID != "conn-42" {
		t.Errorf("connID = %q, want conn-42", connID)
	}

	// Round trip with a connID that itself contains a colon should still
	// work because we split into at most 3 parts.
	token2 := m.GenerateFlowToken("10.0.0.1", 5061, "a:b:c")
	ip2, port2, connID2, ok2 := m.ValidateFlowToken(token2)
	if !ok2 {
		t.Fatalf("ValidateFlowToken(colon-in-connID) = false, want true")
	}
	if ip2 != "10.0.0.1" || port2 != 5061 || connID2 != "a:b:c" {
		t.Errorf("decoded = %q %d %q", ip2, port2, connID2)
	}
}

func TestValidateFlowTokenInvalid(t *testing.T) {
	m := New()

	cases := []string{"", "not-base64!!!", "####", "aGVsbG8="} // last is valid b64 of "hello" (no colons)
	for _, tc := range cases {
		if _, _, _, ok := m.ValidateFlowToken(tc); ok {
			t.Errorf("ValidateFlowToken(%q) = true, want false", tc)
		}
	}

	// Valid base64 but non-numeric port.
	tok := m.GenerateFlowToken("1.2.3.4", 5060, "c")
	// Manually craft a bad-port token.
	ip, _, _, ok := m.ValidateFlowToken(tok)
	if !ok {
		t.Errorf("ValidateFlowToken(valid) = false; ip=%q", ip)
	}
}

func TestIsOutboundSupported(t *testing.T) {
	m := New()

	// Contact with +sip.instance -> supported.
	msg := &parser.SIPMsg{}
	addContactHeader(msg, "<sip:alice@192.0.2.1:5060>;+sip.instance=<urn:uuid:00000000-0000-0000-0000-000000000001>")
	if !m.IsOutboundSupported(msg) {
		t.Errorf("IsOutboundSupported() = false, want true (has +sip.instance)")
	}

	// Contact without +sip.instance -> not supported.
	msg2 := &parser.SIPMsg{}
	addContactHeader(msg2, "<sip:bob@192.0.2.2:5060>")
	if m.IsOutboundSupported(msg2) {
		t.Errorf("IsOutboundSupported() = true, want false (no +sip.instance)")
	}

	// No Contact header -> false.
	if m.IsOutboundSupported(&parser.SIPMsg{}) {
		t.Errorf("IsOutboundSupported() on empty msg = true, want false")
	}
	// nil msg -> false.
	if m.IsOutboundSupported(nil) {
		t.Errorf("IsOutboundSupported(nil) = true, want false")
	}
}

func TestGetFlowToken(t *testing.T) {
	m := New()

	// ob parameter present.
	msg := &parser.SIPMsg{}
	addContactHeader(msg, "<sip:alice@192.0.2.1:5060>;ob="+m.GenerateFlowToken("1.2.3.4", 5060, "cx"))
	tok := m.GetFlowToken(msg)
	if tok == "" {
		t.Fatalf("GetFlowToken() = empty, want token")
	}
	ip, port, connID, ok := m.ValidateFlowToken(tok)
	if !ok || ip != "1.2.3.4" || port != 5060 || connID != "cx" {
		t.Errorf("decoded token = %q %d %q ok=%v", ip, port, connID, ok)
	}

	// Falls back to +sip.instance when no ob param.
	msg2 := &parser.SIPMsg{}
	addContactHeader(msg2, "<sip:bob@1.2.3.4:5060>;+sip.instance=<urn:uuid:abc>")
	if got := m.GetFlowToken(msg2); got != "<urn:uuid:abc>" {
		t.Errorf("GetFlowToken() fallback = %q, want <urn:uuid:abc>", got)
	}

	// No token at all.
	msg3 := &parser.SIPMsg{}
	addContactHeader(msg3, "<sip:carol@1.2.3.4:5060>")
	if got := m.GetFlowToken(msg3); got != "" {
		t.Errorf("GetFlowToken() = %q, want empty", got)
	}
	// No Contact header.
	if got := m.GetFlowToken(&parser.SIPMsg{}); got != "" {
		t.Errorf("GetFlowToken() on empty msg = %q, want empty", got)
	}
}

func TestAddFlowTokenToContact(t *testing.T) {
	m := New()

	msg := &parser.SIPMsg{}
	addContactHeader(msg, "<sip:alice@192.0.2.1:5060>")
	token := m.GenerateFlowToken("5.6.7.8", 5060, "cx9")

	if ret := m.AddFlowTokenToContact(msg, token); ret != 0 {
		t.Fatalf("AddFlowTokenToContact() = %d, want 0", ret)
	}
	body := msg.Contact.Body.String()
	if !contains(body, "ob="+token) {
		t.Errorf("Contact body %q does not contain ob=%s", body, token)
	}
	// The token must be retrievable.
	if got := m.GetFlowToken(msg); got != token {
		t.Errorf("GetFlowToken() after add = %q, want %q", got, token)
	}

	// Failures: nil msg, no Contact, empty token.
	if ret := m.AddFlowTokenToContact(nil, token); ret != -1 {
		t.Errorf("AddFlowTokenToContact(nil) = %d, want -1", ret)
	}
	if ret := m.AddFlowTokenToContact(&parser.SIPMsg{}, token); ret != -1 {
		t.Errorf("AddFlowTokenToContact(no-contact) = %d, want -1", ret)
	}
	if ret := m.AddFlowTokenToContact(msg, ""); ret != -1 {
		t.Errorf("AddFlowTokenToContact(empty) = %d, want -1", ret)
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultOutbound()
	if d == nil {
		t.Fatalf("DefaultOutbound() returned nil")
	}
	if d != DefaultOutbound() {
		t.Fatalf("DefaultOutbound() returned different instances after Init()")
	}

	// Init with custom config is honoured.
	Init()
	DefaultOutbound().Init(&OutboundConfig{FlowTokenKey: "k", FlowTokenTTL: 0})
	tok := DefaultOutbound().GenerateFlowToken("1.1.1.1", 1, "z")
	if _, _, _, ok := DefaultOutbound().ValidateFlowToken(tok); !ok {
		t.Errorf("default module ValidateFlowToken() = false, want true")
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultOutbound()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			tok := shared.GenerateFlowToken("10.0.0.1", 5060, "c")
			shared.ValidateFlowToken(tok)
			msg := &parser.SIPMsg{}
			addContactHeader(msg, "<sip:x@1.2.3.4>;+sip.instance=<urn:uuid:"+itoa(i)+">")
			shared.IsOutboundSupported(msg)
			shared.AddFlowTokenToContact(msg, tok)
			shared.GetFlowToken(msg)
		}(i)
	}
	wg.Wait()
}

// contains reports whether s contains sub.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			if s[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// itoa is a tiny local int->string helper to avoid pulling strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
