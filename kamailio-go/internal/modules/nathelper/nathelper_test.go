// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the nathelper module.
 */

package nathelper

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
	case parser.HdrContact:
		if msg.Contact == nil {
			msg.Contact = h
		}
	case parser.HdrVia:
		if msg.HdrVia1 == nil {
			msg.HdrVia1 = h
		}
		if msg.Via1 == nil {
			msg.Via1 = &parser.ViaBody{Host: str.Mk(viaHost(value))}
		}
	}
	return h
}

// viaHost extracts the host portion of a "SIP/2.0/UDP host:port" Via value.
func viaHost(value string) string {
	parts := strings.SplitN(value, " ", 2)
	if len(parts) < 2 {
		return ""
	}
	rest := parts[1]
	for _, sep := range []string{";", ":"} {
		if idx := strings.Index(rest, sep); idx >= 0 {
			rest = rest[:idx]
		}
	}
	return rest
}

// reqMsg builds a request SIPMsg with the given request-URI.
func reqMsg(uri string) *parser.SIPMsg {
	return &parser.SIPMsg{
		FirstLine: &parser.MsgStart{
			Type: parser.MsgRequest,
			Req: &parser.RequestLine{
				Method:      str.Mk("INVITE"),
				URI:         str.Mk(uri),
				MethodValue: parser.MethodInvite,
			},
		},
	}
}

func TestIsRFC1918(t *testing.T) {
	m := New()
	cases := []struct {
		ip   string
		want bool
	}{
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"172.15.0.1", false}, // just below 172.16/12
		{"172.32.0.1", false}, // just above 172.16/12
		{"192.168.0.1", true},
		{"192.168.99.99", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"100.64.0.1", false}, // not in the three RFC1918 ranges
	}
	for _, c := range cases {
		if got := m.IsRFC1918(c.ip); got != c.want {
			t.Errorf("IsRFC1918(%q) = %v, want %v", c.ip, got, c.want)
		}
	}
	// Invalid IP is not RFC1918.
	if m.IsRFC1918("not-an-ip") {
		t.Errorf("IsRFC1918(invalid) = true, want false")
	}
}

func TestFixNatedSDP(t *testing.T) {
	m := New()
	body := "v=0\r\n" +
		"o=- 1 1 IN IP4 192.168.1.10\r\n" +
		"s=-\r\n" +
		"c=IN IP4 192.168.1.10\r\n" +
		"t=0 0\r\n" +
		"m=audio 5004 RTP/AVP 0\r\n" +
		"c=IN IP4 192.168.1.10\r\n"
	msg := &parser.SIPMsg{Body: []byte(body)}

	if err := m.FixNatedSDP(msg, "203.0.113.5", 0); err != nil {
		t.Fatalf("FixNatedSDP() error = %v", err)
	}
	out := string(msg.Body.([]byte))
	// Only c= lines are rewritten; the o= origin line keeps its address.
	if strings.Contains(out, "c=IN IP4 192.168.1.10") {
		t.Errorf("SDP c= line still contains old IP: %q", out)
	}
	if !strings.Contains(out, "c=IN IP4 203.0.113.5") {
		t.Errorf("SDP missing new c= line: %q", out)
	}
	// Both c= lines (session + media) should be rewritten.
	if strings.Count(out, "c=IN IP4 203.0.113.5") < 2 {
		t.Errorf("expected at least 2 occurrences of new c= IP, got %q", out)
	}

	// No body -> error.
	if err := m.FixNatedSDP(&parser.SIPMsg{}, "1.2.3.4", 0); err == nil {
		t.Errorf("FixNatedSDP() with no body should error")
	}
}

func TestFixNatedSDPPort(t *testing.T) {
	m := New()
	body := "v=0\r\n" +
		"c=IN IP4 192.168.1.10\r\n" +
		"m=audio 5004 RTP/AVP 0\r\n"
	msg := &parser.SIPMsg{Body: []byte(body)}

	if err := m.FixNatedSDP(msg, "203.0.113.5", 6000); err != nil {
		t.Fatalf("FixNatedSDP() error = %v", err)
	}
	out := string(msg.Body.([]byte))
	if !strings.Contains(out, "m=audio 6000 RTP/AVP 0") {
		t.Errorf("media port not rewritten: %q", out)
	}
}

func TestAddContactAlias(t *testing.T) {
	m := New()
	msg := reqMsg("sip:alice@192.168.1.1:5060")
	addHeader(msg, "Contact", "<sip:alice@192.168.1.1:5060>")

	if err := m.AddContactAlias(msg, "203.0.113.7", 5070); err != nil {
		t.Fatalf("AddContactAlias() error = %v", err)
	}
	body := msg.Contact.Body.String()
	if !strings.Contains(body, "alias=") {
		t.Errorf("Contact body missing alias param: %q", body)
	}
	if !strings.Contains(body, "203.0.113.7") {
		t.Errorf("Contact body missing alias IP: %q", body)
	}
	if !strings.Contains(body, "5070") {
		t.Errorf("Contact body missing alias port: %q", body)
	}
	// alias format: alias=ip~port~proto
	if !strings.Contains(body, "203.0.113.7~5070~") {
		t.Errorf("Contact body alias not in ip~port~proto form: %q", body)
	}

	// No Contact header -> error.
	if err := m.AddContactAlias(reqMsg("sip:x@y"), "1.2.3.4", 5060); err == nil {
		t.Errorf("AddContactAlias() with no Contact should error")
	}
}

func TestHandleRURIAlias(t *testing.T) {
	m := New()
	// RURI with alias param: alias=ip~port~proto (proto 1 = UDP)
	msg := reqMsg("sip:alice@192.168.1.1:5060;alias=203.0.113.7~5070~1")

	ip, port, proto, err := m.HandleRURIAlias(msg)
	if err != nil {
		t.Fatalf("HandleRURIAlias() error = %v", err)
	}
	if ip != "203.0.113.7" {
		t.Errorf("ip = %q, want 203.0.113.7", ip)
	}
	if port != 5070 {
		t.Errorf("port = %d, want 5070", port)
	}
	if proto != "udp" {
		t.Errorf("proto = %q, want udp", proto)
	}
	// The alias param must be removed from the RURI.
	uri := msg.FirstLine.Req.URI.String()
	if strings.Contains(uri, "alias=") {
		t.Errorf("RURI still contains alias param: %q", uri)
	}
	if !strings.Contains(uri, "sip:alice@192.168.1.1:5060") {
		t.Errorf("RURI mangled: %q", uri)
	}

	// No alias param -> error.
	msg2 := reqMsg("sip:bob@example.com")
	if _, _, _, err := m.HandleRURIAlias(msg2); err == nil {
		t.Errorf("HandleRURIAlias() with no alias should error")
	}
}

func TestHandleRURIAliasTCP(t *testing.T) {
	m := New()
	msg := reqMsg("sip:alice@10.0.0.1;alias=198.51.100.4~5060~2")
	ip, port, proto, err := m.HandleRURIAlias(msg)
	if err != nil {
		t.Fatalf("HandleRURIAlias() error = %v", err)
	}
	if ip != "198.51.100.4" {
		t.Errorf("ip = %q, want 198.51.100.4", ip)
	}
	if port != 5060 {
		t.Errorf("port = %d, want 5060", port)
	}
	if proto != "tcp" {
		t.Errorf("proto = %q, want tcp", proto)
	}
}

func TestNatUacTestContact1918(t *testing.T) {
	m := New()
	msg := reqMsg("sip:alice@10.0.0.1:5060")
	addHeader(msg, "Contact", "<sip:alice@10.0.0.1:5060>")
	// tests & 1 = Contact IP is RFC1918
	if !m.NatUacTest(msg, 1) {
		t.Errorf("NatUacTest(1) for RFC1918 contact = false, want true")
	}
	// Public contact -> not detected by test 1.
	msg2 := reqMsg("sip:alice@8.8.8.8:5060")
	addHeader(msg2, "Contact", "<sip:alice@8.8.8.8:5060>")
	if m.NatUacTest(msg2, 1) {
		t.Errorf("NatUacTest(1) for public contact = true, want false")
	}
}

func TestNatUacTestViaRport(t *testing.T) {
	m := New()
	msg := reqMsg("sip:alice@example.com")
	addHeader(msg, "Via", "SIP/2.0/UDP 192.0.2.1:5060;branch=z9;rport=5060")
	msg.Via1.RPort = &parser.ViaParam{Value: str.Mk("5060")}
	// tests & 2 = Via rport present
	if !m.NatUacTest(msg, 2) {
		t.Errorf("NatUacTest(2) with rport = false, want true")
	}
}

func TestNatUacTestViaReceived(t *testing.T) {
	m := New()
	msg := reqMsg("sip:alice@example.com")
	addHeader(msg, "Via", "SIP/2.0/UDP 192.0.2.1:5060;branch=z9;received=198.51.100.1")
	msg.Via1.Received = &parser.ViaParam{Value: str.Mk("198.51.100.1")}
	// tests & 4 = Via received present
	if !m.NatUacTest(msg, 4) {
		t.Errorf("NatUacTest(4) with received = false, want true")
	}
}

func TestNatUacTestSDP1918(t *testing.T) {
	m := New()
	body := "v=0\r\nc=IN IP4 192.168.1.1\r\nm=audio 5004 RTP/AVP 0\r\n"
	msg := reqMsg("sip:alice@example.com")
	msg.Body = []byte(body)
	// tests & 8 = SDP IP is RFC1918
	if !m.NatUacTest(msg, 8) {
		t.Errorf("NatUacTest(8) with RFC1918 SDP = false, want true")
	}
}

func TestNatUacTestDomainCompare(t *testing.T) {
	m := New()
	// Contact host is a domain (not an IP) -> test 16 true.
	msg := reqMsg("sip:alice@example.com")
	addHeader(msg, "Contact", "<sip:alice@example.com>")
	if !m.NatUacTest(msg, 16) {
		t.Errorf("NatUacTest(16) with domain contact = false, want true")
	}
	// Contact host is an IP -> test 16 false.
	msg2 := reqMsg("sip:alice@example.com")
	addHeader(msg2, "Contact", "<sip:alice@8.8.8.8>")
	if m.NatUacTest(msg2, 16) {
		t.Errorf("NatUacTest(16) with IP contact = true, want false")
	}
}

func TestNatUacTestCombined(t *testing.T) {
	m := New()
	msg := reqMsg("sip:alice@example.com")
	addHeader(msg, "Contact", "<sip:alice@10.0.0.1:5060>")
	addHeader(msg, "Via", "SIP/2.0/UDP 192.0.2.1:5060;rport=5060;received=198.51.100.1")
	msg.Via1.RPort = &parser.ViaParam{Value: str.Mk("5060")}
	msg.Via1.Received = &parser.ViaParam{Value: str.Mk("198.51.100.1")}
	// Combined mask 1|2|4 should be true.
	if !m.NatUacTest(msg, 1|2|4) {
		t.Errorf("NatUacTest(1|2|4) = false, want true")
	}
	// Mask with no matching bits (e.g. 8 with no SDP) should be false.
	if m.NatUacTest(msg, 8) {
		t.Errorf("NatUacTest(8) without SDP = true, want false")
	}
}

func TestFixNatedContact(t *testing.T) {
	m := New()
	msg := reqMsg("sip:alice@example.com")
	addHeader(msg, "Contact", "<sip:alice@10.0.0.1:5060>")
	if err := m.FixNatedContact(msg, "203.0.113.9", 5090); err != nil {
		t.Fatalf("FixNatedContact() error = %v", err)
	}
	body := msg.Contact.Body.String()
	if !strings.Contains(body, "203.0.113.9") {
		t.Errorf("Contact not rewritten: %q", body)
	}
}

func TestAddRportAlias(t *testing.T) {
	m := New()
	msg := reqMsg("sip:alice@example.com")
	addHeader(msg, "Via", "SIP/2.0/UDP 192.0.2.1:5060;branch=z9")
	if err := m.AddRportAlias(msg, "198.51.100.1", 5060); err != nil {
		t.Fatalf("AddRportAlias() error = %v", err)
	}
	if msg.Via1.Received == nil || msg.Via1.Received.Value.String() != "198.51.100.1" {
		t.Errorf("Via received not set: %+v", msg.Via1.Received)
	}
	if msg.Via1.RPort == nil || msg.Via1.RPort.Value.String() != "5060" {
		t.Errorf("Via rport not set: %+v", msg.Via1.RPort)
	}
}

func TestNatPing(t *testing.T) {
	m := New()
	if err := m.NatPing("sip:alice@10.0.0.1:5060", "udp:192.0.2.1:5060"); err != nil {
		t.Fatalf("NatPing() error = %v", err)
	}
	tgt := m.GetTarget("sip:alice@10.0.0.1:5060")
	if tgt == nil {
		t.Fatalf("GetTarget() returned nil after NatPing")
	}
	if !tgt.Active {
		t.Errorf("target not Active after ping")
	}
	if tgt.Failures != 0 {
		t.Errorf("target Failures = %d, want 0", tgt.Failures)
	}
	if tgt.LastPing.IsZero() {
		t.Errorf("target LastPing not set")
	}
}

func TestRemoveTarget(t *testing.T) {
	m := New()
	m.NatPing("sip:bob@10.0.0.2:5060", "udp:1.2.3.4:5060")
	if !m.RemoveTarget("sip:bob@10.0.0.2:5060") {
		t.Errorf("RemoveTarget() returned false, want true")
	}
	if m.GetTarget("sip:bob@10.0.0.2:5060") != nil {
		t.Errorf("GetTarget() after remove should return nil")
	}
	if m.RemoveTarget("sip:nope@10.0.0.2:5060") {
		t.Errorf("RemoveTarget(unknown) should return false")
	}
}

func TestListTargets(t *testing.T) {
	m := New()
	m.NatPing("sip:a@10.0.0.1:5060", "udp:1.2.3.4:5060")
	m.NatPing("sip:b@10.0.0.2:5060", "udp:1.2.3.4:5060")
	if got := len(m.ListTargets()); got != 2 {
		t.Errorf("ListTargets() len = %d, want 2", got)
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultNathelper()
	if d == nil {
		t.Fatalf("DefaultNathelper() returned nil")
	}
	if DefaultNathelper() != d {
		t.Errorf("DefaultNathelper() returned different instance")
	}
	// Init resets state.
	d.NatPing("sip:reset@10.0.0.1:5060", "udp:1.2.3.4:5060")
	Init()
	if got := len(DefaultNathelper().ListTargets()); got != 0 {
		t.Errorf("after Init, ListTargets() len = %d, want 0", got)
	}
}

func TestConcurrentSafety(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			uri := "sip:user" + string(rune('a'+n%26)) + "@10.0.0." + itoa(n%255+1) + ":5060"
			_ = m.NatPing(uri, "udp:1.2.3.4:5060")
			_ = m.GetTarget(uri)
			_ = m.ListTargets()
			_ = m.IsRFC1918("10.0.0.1")
		}(i)
	}
	wg.Wait()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
