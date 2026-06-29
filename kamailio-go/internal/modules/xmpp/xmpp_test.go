// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - xmpp module tests.
 *
 * These tests do NOT require a running XMPP server. They exercise the
 * gateway against an in-memory mock XMPPConn so message / presence flows
 * and SIP<->XMPP conversion are verified, including concurrent access.
 */

package xmpp

import (
	"strings"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// A SIP MESSAGE with a text body.
var sipMessage = []byte("MESSAGE sip:bob@sip.example.org SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP kamailio;branch=z9hG4bK-test\r\n" +
	"Max-Forwards: 70\r\n" +
	"From: <sip:alice@sip.example.org>;tag=fromtag\r\n" +
	"To: <sip:bob@sip.example.org>\r\n" +
	"Call-ID: test@kamailio\r\n" +
	"CSeq: 1 MESSAGE\r\n" +
	"Content-Type: text/plain\r\n" +
	"Content-Length: 5\r\n" +
	"\r\n" +
	"hello")

func mustParseSIP(t *testing.T, b []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(b)
	if err != nil {
		t.Fatalf("ParseMsg: %v", err)
	}
	return msg
}

func TestConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Server != "127.0.0.1" {
		t.Errorf("server = %q", cfg.Server)
	}
	if cfg.Port != 5222 {
		t.Errorf("port = %d, want 5222", cfg.Port)
	}
	if cfg.Domain != "xmpp.example.org" {
		t.Errorf("domain = %q", cfg.Domain)
	}
}

func TestConfigValidate(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Errorf("default config: %v", err)
	}
	if err := (&Config{}).Validate(); err == nil {
		t.Error("empty config expected error")
	}
	if err := (&Config{Server: "h", Port: 0, Domain: "d"}).Validate(); err == nil {
		t.Error("port 0 expected error")
	}
}

func TestSetters(t *testing.T) {
	m := New()
	m.SetServer("xmpp.test", 5269)
	m.SetCredentials("gw@xmpp.test", "secret")
	m.SetDomain("xmpp.test")
	cfg := m.Config()
	if cfg.Server != "xmpp.test" || cfg.Port != 5269 {
		t.Errorf("server cfg = %v", cfg)
	}
	if cfg.JID != "gw@xmpp.test" || cfg.Password != "secret" {
		t.Errorf("creds cfg = %v", cfg)
	}
	if cfg.Domain != "xmpp.test" {
		t.Errorf("domain = %q", cfg.Domain)
	}
}

func TestConnectAndDisconnect(t *testing.T) {
	m := New()
	if m.IsConnected() {
		t.Error("IsConnected true before Connect")
	}
	if err := m.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !m.IsConnected() {
		t.Error("IsConnected false after Connect")
	}
	// Second Connect is a no-op.
	if err := m.Connect(); err != nil {
		t.Fatalf("second Connect: %v", err)
	}
	if err := m.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if m.IsConnected() {
		t.Error("IsConnected true after Disconnect")
	}
}

func TestSendXMPPMessage(t *testing.T) {
	m := New()
	if err := m.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := m.SendXMPPMessage("bob@xmpp.example.org", "hi"); err != nil {
		t.Fatalf("SendXMPPMessage: %v", err)
	}
	if got := m.SentCount(); got != 1 {
		t.Errorf("SentCount = %d, want 1", got)
	}
}

func TestSendXMPPMessageNotConnected(t *testing.T) {
	m := New()
	if err := m.SendXMPPMessage("bob@xmpp.example.org", "hi"); err == nil {
		t.Error("SendXMPPMessage before Connect expected error")
	}
}

func TestSendXMPPMessageEmptyRecipient(t *testing.T) {
	m := New()
	if err := m.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := m.SendXMPPMessage("", "hi"); err == nil {
		t.Error("SendXMPPMessage('') expected error")
	}
}

func TestSendXMPPPresence(t *testing.T) {
	m := New()
	if err := m.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := m.SendXMPPPresence("available"); err != nil {
		t.Fatalf("SendXMPPPresence: %v", err)
	}
	if err := m.SendXMPPPresence("away"); err != nil {
		t.Fatalf("SendXMPPPresence: %v", err)
	}
	if got := m.SentCount(); got != 2 {
		t.Errorf("SentCount = %d, want 2", got)
	}
}

func TestSIPToXMPP(t *testing.T) {
	m := New()
	msg := mustParseSIP(t, sipMessage)
	out, err := m.SIPToXMPP(msg)
	if err != nil {
		t.Fatalf("SIPToXMPP: %v", err)
	}
	if !strings.Contains(out, "<message") {
		t.Errorf("output missing <message>: %s", out)
	}
	if !strings.Contains(out, "alice@") {
		t.Errorf("output missing from alice: %s", out)
	}
	if !strings.Contains(out, "bob@") {
		t.Errorf("output missing to bob: %s", out)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("output missing body: %s", out)
	}
}

func TestSIPToXMPPNil(t *testing.T) {
	m := New()
	if _, err := m.SIPToXMPP(nil); err == nil {
		t.Error("SIPToXMPP(nil) expected error")
	}
}

func TestSIPToXMPPNotRequest(t *testing.T) {
	m := New()
	reply := []byte("SIP/2.0 200 OK\r\n" +
		"Via: SIP/2.0/UDP kamailio;branch=z9hG4bK-test\r\n" +
		"From: <sip:alice@sip.example.org>;tag=fromtag\r\n" +
		"To: <sip:bob@sip.example.org>\r\n" +
		"Call-ID: test@kamailio\r\n" +
		"CSeq: 1 MESSAGE\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
	msg := mustParseSIP(t, reply)
	if _, err := m.SIPToXMPP(msg); err == nil {
		t.Error("SIPToXMPP(reply) expected error")
	}
}

func TestXMPPToSIP(t *testing.T) {
	m := New()
	xmppStanza := `<message from="alice@xmpp.example.org" to="bob@xmpp.example.org" type="chat"><body>hi there</body></message>`
	msg, err := m.XMPPToSIP(xmppStanza)
	if err != nil {
		t.Fatalf("XMPPToSIP: %v", err)
	}
	if msg == nil {
		t.Fatal("XMPPToSIP returned nil")
	}
	if !msg.IsRequest() {
		t.Error("not a request")
	}
	if msg.Method() != parser.MethodMessage {
		t.Errorf("method = %v, want MESSAGE", msg.Method())
	}
}

func TestXMPPToSIPEmpty(t *testing.T) {
	m := New()
	if _, err := m.XMPPToSIP(""); err == nil {
		t.Error("XMPPToSIP('') expected error")
	}
}

func TestXMPPToSIPBadXML(t *testing.T) {
	m := New()
	if _, err := m.XMPPToSIP("<not xml"); err == nil {
		t.Error("XMPPToSIP(bad xml) expected error")
	}
}

func TestInitResetsConnection(t *testing.T) {
	m := New()
	if err := m.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	cfg := *DefaultConfig()
	if err := m.Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.IsConnected() {
		t.Error("IsConnected true after Init")
	}
}

func TestDefaultAndInit(t *testing.T) {
	cfg := *DefaultConfig()
	if err := Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	d := DefaultXMPP()
	if d == nil {
		t.Fatal("DefaultXMPP nil")
	}
	if err := Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := SendXMPPMessage("x@xmpp.example.org", "y"); err != nil {
		t.Fatalf("SendXMPPMessage: %v", err)
	}
	if err := Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	if err := m.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			to := "user" + itoa(i%5) + "@xmpp.example.org"
			if err := m.SendXMPPMessage(to, "msg"); err != nil {
				t.Errorf("SendXMPPMessage %d: %v", i, err)
			}
			if err := m.SendXMPPPresence("available"); err != nil {
				t.Errorf("SendXMPPPresence %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	if got := m.SentCount(); got != 100 {
		t.Errorf("SentCount = %d, want 100", got)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
