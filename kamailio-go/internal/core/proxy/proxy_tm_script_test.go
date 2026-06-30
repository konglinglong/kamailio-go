// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * End-to-end tests for the TM callback → script route dispatch wiring.
 *
 * These tests verify the full chain:
 *   1. Script binds t_on_reply / t_on_failure / t_on_branch in
 *      request_route.
 *   2. ProxyCore.ProcessRequest runs the script, creates the TM
 *      transaction, and stamps the route names + source address onto
 *      the cell.
 *   3. ProxyCore.ProcessReply routes the reply through HandleResponse,
 *      which fires the TMCB callbacks.
 *   4. The callbacks dispatch the named script route block.
 *   5. When the route sets a reply (sl_send_reply), the proxy builds
 *      and sends it back to the original client via the listener.
 */

package proxy

import (
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/script"
	"github.com/kamailio/kamailio-go/internal/core/transport"
	"github.com/kamailio/kamailio-go/internal/modules/tm"
)

// captureListener is a minimal Listener that records every SendTo call
// so tests can assert which replies were sent to which address.
type captureListener struct {
	mu   sync.Mutex
	sent []capturedSend
}

type capturedSend struct {
	dst  net.Addr
	data []byte
}

func (c *captureListener) SendTo(dst net.Addr, data []byte) error {
	c.mu.Lock()
	c.sent = append(c.sent, capturedSend{dst: dst, data: append([]byte(nil), data...)})
	c.mu.Unlock()
	return nil
}

func (c *captureListener) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5060}
}

func (c *captureListener) SendSocketInfo() *transport.SocketInfo {
	return &transport.SocketInfo{Protocol: transport.ProtoUDP, Address: net.ParseIP("127.0.0.1"), Port: 5060}
}

func (c *captureListener) snapshot() []capturedSend {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]capturedSend, len(c.sent))
	copy(out, c.sent)
	return out
}

// buildInviteRaw constructs a raw INVITE with the given Call-ID and Via branch.
func buildInviteRaw(callID, branch string) []byte {
	return []byte("INVITE sip:bob@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1:5060;branch=" + branch + "\r\n" +
		"Max-Forwards: 70\r\n" +
		"From: Alice <sip:alice@example.com>;tag=ftag\r\n" +
		"To: Bob <sip:bob@example.com>\r\n" +
		"Call-ID: " + callID + "\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Contact: <sip:alice@10.0.0.1:5060>\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
}

// buildReplyRaw constructs a raw SIP reply matching the request's Via
// branch and Call-ID with the supplied status code and reason.
func buildReplyRaw(callID, branch string, code int, reason string) []byte {
	return []byte("SIP/2.0 " + itoa(code) + " " + reason + "\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1:5060;branch=" + branch + "\r\n" +
		"From: Alice <sip:alice@example.com>;tag=ftag\r\n" +
		"To: Bob <sip:bob@example.com>;tag=ttag\r\n" +
		"Call-ID: " + callID + "\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	pos := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		b[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

func mustParseMsg(t *testing.T, raw []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("ParseMsg: %v", err)
	}
	return msg
}

// --- Tests ---

// TestTMScriptWiring_RoutesStampedOnCell verifies that after
// ProcessRequest runs a script with t_on_reply / t_on_failure /
// t_on_branch, the created transaction cell has the route names set.
func TestTMScriptWiring_RoutesStampedOnCell(t *testing.T) {
	const scriptText = `
request_route {
    t_on_reply("REPL");
    t_on_failure("FAIL");
    t_on_branch("BR");
}
onreply_route[REPL] { xlog("reply"); }
failure_route[FAIL] { xlog("fail"); }
branch_route[BR] { xlog("branch"); }
`
	sc, err := script.Parse(scriptText)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	pcore := NewProxyCore(&ProxyConfig{Realm: "test"})
	pcore.SetScript(sc)
	tmMgr := tm.NewManager(1024)
	pcore.SetTM(tmMgr)

	invite := mustParseMsg(t, buildInviteRaw("stamp-test@example.com", "z9hG4bKstamp001"))
	src := &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 5060}
	pcore.ProcessRequest(invite, src, nil)

	cell, err := tmMgr.LookupRequest(invite)
	if err != nil {
		t.Fatalf("LookupRequest: %v", err)
	}
	if cell == nil {
		t.Fatal("LookupRequest returned nil cell")
	}
	onReply, onFailure, onBranch := cell.TMRoutes()
	if onReply != "REPL" {
		t.Errorf("OnReplyRoute = %q, want REPL", onReply)
	}
	if onFailure != "FAIL" {
		t.Errorf("OnFailureRoute = %q, want FAIL", onFailure)
	}
	if onBranch != "BR" {
		t.Errorf("OnBranchRoute = %q, want BR", onBranch)
	}
	if cell.SourceAddr == nil {
		t.Error("SourceAddr is nil, want the request source address")
	}
}

// TestTMScriptWiring_FailureRouteDispatched verifies that when a
// non-2xx final reply arrives, the failure_route bound to the
// transaction runs and its sl_send_reply causes a reply to be sent
// back to the original client.
func TestTMScriptWiring_FailureRouteDispatched(t *testing.T) {
	const scriptText = `
request_route {
    t_on_failure("FAIL");
}
failure_route[FAIL] {
    sl_send_reply("500", "Server Internal Error");
}
`
	sc, err := script.Parse(scriptText)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	pcore := NewProxyCore(&ProxyConfig{Realm: "test"})
	pcore.SetScript(sc)
	tmMgr := tm.NewManager(1024)
	pcore.SetTM(tmMgr)
	cl := &captureListener{}
	pcore.AddListener(cl)

	const callID = "fail-test@example.com"
	const branch = "z9hG4bKfail001"
	invite := mustParseMsg(t, buildInviteRaw(callID, branch))
	src := &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 5060}
	pcore.ProcessRequest(invite, src, nil)

	// Send a 404 Not Found reply on the same transaction.
	reply := mustParseMsg(t, buildReplyRaw(callID, branch, 404, "Not Found"))
	pcore.ProcessReply(reply, &net.UDPAddr{IP: net.ParseIP("10.0.0.2"), Port: 5060})

	sent := cl.snapshot()
	if len(sent) == 0 {
		t.Fatal("expected at least one reply sent via listener, got 0")
	}
	// The failure_route's sl_send_reply("500", "Server Internal Error")
	// should have produced a SIP/2.0 500 reply to the original source.
	var found500 bool
	for _, s := range sent {
		if strings.Contains(string(s.data), "SIP/2.0 500") &&
			strings.Contains(string(s.data), "Server Internal Error") {
			found500 = true
			// Verify it was sent to the original request source.
			udp, ok := s.dst.(*net.UDPAddr)
			if !ok {
				t.Errorf("dst is not *net.UDPAddr, got %T", s.dst)
			} else if !udp.IP.Equal(net.ParseIP("10.0.0.1")) || udp.Port != 5060 {
				t.Errorf("reply sent to %s, want 10.0.0.1:5060", s.dst)
			}
			break
		}
	}
	if !found500 {
		t.Errorf("500 reply not found in sent data; got %d sends", len(sent))
		for i, s := range sent {
			t.Logf("send[%d]: %s", i, string(s.data))
		}
	}
}

// TestTMScriptWiring_OnReplyRouteDispatched verifies that when a
// provisional reply arrives, the onreply_route bound to the
// transaction runs. The route uses sl_send_reply to produce an
// observable side effect.
func TestTMScriptWiring_OnReplyRouteDispatched(t *testing.T) {
	const scriptText = `
request_route {
    t_on_reply("REPL");
}
onreply_route[REPL] {
    sl_send_reply("100", "OnReply Fired");
}
`
	sc, err := script.Parse(scriptText)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	pcore := NewProxyCore(&ProxyConfig{Realm: "test"})
	pcore.SetScript(sc)
	tmMgr := tm.NewManager(1024)
	pcore.SetTM(tmMgr)
	cl := &captureListener{}
	pcore.AddListener(cl)

	const callID = "reply-test@example.com"
	const branch = "z9hG4bKreply001"
	invite := mustParseMsg(t, buildInviteRaw(callID, branch))
	src := &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 5060}
	pcore.ProcessRequest(invite, src, nil)

	// Send a 100 Trying provisional reply on the same transaction.
	reply := mustParseMsg(t, buildReplyRaw(callID, branch, 100, "Trying"))
	pcore.ProcessReply(reply, &net.UDPAddr{IP: net.ParseIP("10.0.0.2"), Port: 5060})

	sent := cl.snapshot()
	var foundOnReply bool
	for _, s := range sent {
		if strings.Contains(string(s.data), "SIP/2.0 100") &&
			strings.Contains(string(s.data), "OnReply Fired") {
			foundOnReply = true
			break
		}
	}
	if !foundOnReply {
		t.Errorf("onreply_route reply not found; got %d sends", len(sent))
		for i, s := range sent {
			t.Logf("send[%d]: %s", i, string(s.data))
		}
	}
}

// TestTMScriptWiring_NoRoutesNoReply verifies that when the script
// does not bind any TM routes, processing a reply does not produce
// spurious replies via the listener.
func TestTMScriptWiring_NoRoutesNoReply(t *testing.T) {
	const scriptText = `
request_route {
    xlog("no tm routes");
}
`
	sc, err := script.Parse(scriptText)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	pcore := NewProxyCore(&ProxyConfig{Realm: "test"})
	pcore.SetScript(sc)
	tmMgr := tm.NewManager(1024)
	pcore.SetTM(tmMgr)
	cl := &captureListener{}
	pcore.AddListener(cl)

	const callID = "noroute-test@example.com"
	const branch = "z9hG4bKnoroute001"
	invite := mustParseMsg(t, buildInviteRaw(callID, branch))
	src := &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 5060}
	pcore.ProcessRequest(invite, src, nil)

	reply := mustParseMsg(t, buildReplyRaw(callID, branch, 404, "Not Found"))
	pcore.ProcessReply(reply, &net.UDPAddr{IP: net.ParseIP("10.0.0.2"), Port: 5060})

	if sends := cl.snapshot(); len(sends) != 0 {
		t.Errorf("expected 0 sends when no TM routes bound, got %d", len(sends))
		for i, s := range sends {
			t.Logf("send[%d]: %s", i, string(s.data))
		}
	}
}
