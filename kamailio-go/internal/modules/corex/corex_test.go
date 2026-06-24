// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the corex (core extensions) module.
 */

package corex

import (
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// inviteToBob is an INVITE whose R-URI, From and To all point at biloxi.com.
const inviteToBob = "INVITE sip:bob@biloxi.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.atlanta.com;branch=z9hG4bK776\r\n" +
	"To: Bob <sip:bob@biloxi.com>\r\n" +
	"From: Alice <sip:alice@atlanta.com>;tag=1928301774\r\n" +
	"Call-ID: a84b4c76e66710@pc33.atlanta.com\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"Contact: <sip:alice@pc33.atlanta.com>\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n"

// inviteWithBody carries an SDP body and a Content-Length matching it.
const inviteWithBody = "INVITE sip:bob@biloxi.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.atlanta.com;branch=z9hG4bK776\r\n" +
	"To: Bob <sip:bob@biloxi.com>\r\n" +
	"From: Alice <sip:alice@127.0.0.1>;tag=1928301774\r\n" +
	"Call-ID: a84b4c76e66710@pc33.atlanta.com\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"Content-Type: application/sdp\r\n" +
	"Content-Length: 20\r\n" +
	"Expires: 3600\r\n" +
	"\r\n" +
	"v=0\r\no=alice 0 0\r\n"

// inviteNoCL is an INVITE without a Content-Length header.
const inviteNoCL = "INVITE sip:bob@biloxi.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.atlanta.com;branch=z9hG4bK776\r\n" +
	"To: Bob <sip:bob@biloxi.com>\r\n" +
	"From: Alice <sip:alice@127.0.0.1>;tag=1928301774\r\n" +
	"Call-ID: a84b4c76e66710@pc33.atlanta.com\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"\r\n"

func mustParse(t *testing.T, raw string) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg([]byte(raw))
	if err != nil {
		t.Fatalf("ParseMsg failed: %v", err)
	}
	return msg
}

func TestAppendBranch(t *testing.T) {
	c := NewCoreXModule()
	msg := mustParse(t, inviteToBob)

	if got := c.AppendBranch(msg, "sip:bob@biloxi.com", 100); got != 1 {
		t.Errorf("AppendBranch = %d, want 1", got)
	}
	if got := c.AppendBranch(msg, "sip:carol@biloxi.com", 200); got != 1 {
		t.Errorf("AppendBranch(2) = %d, want 1", got)
	}

	branches := c.Branches()
	if len(branches) != 2 {
		t.Fatalf("len(Branches) = %d, want 2", len(branches))
	}
	if branches[0].URI != "sip:bob@biloxi.com" {
		t.Errorf("branches[0].URI = %q, want sip:bob@biloxi.com", branches[0].URI)
	}
	if branches[0].Q != 100 {
		t.Errorf("branches[0].Q = %d, want 100", branches[0].Q)
	}
	if branches[1].URI != "sip:carol@biloxi.com" {
		t.Errorf("branches[1].URI = %q, want sip:carol@biloxi.com", branches[1].URI)
	}

	// Empty / nil message rejected.
	if got := c.AppendBranch(nil, "sip:x@y", 0); got != -1 {
		t.Errorf("AppendBranch(nil msg) = %d, want -1", got)
	}
	if got := c.AppendBranch(msg, "  ", 0); got != -1 {
		t.Errorf("AppendBranch(empty uri) = %d, want -1", got)
	}

	c.ResetBranches()
	if len(c.Branches()) != 0 {
		t.Errorf("len(Branches) after reset = %d, want 0", len(c.Branches()))
	}
}

func TestForceAndSetSocket(t *testing.T) {
	c := NewCoreXModule()
	msg := mustParse(t, inviteToBob)

	if got := c.ForceSendSocket(msg, "udp:127.0.0.1:5060"); got != 1 {
		t.Errorf("ForceSendSocket = %d, want 1", got)
	}
	if msg.ForceSendSocket != "udp:127.0.0.1:5060" {
		t.Errorf("ForceSendSocket not recorded, got %v", msg.ForceSendSocket)
	}

	if got := c.SetSendSocket(msg, "127.0.0.1:5060"); got != 1 {
		t.Errorf("SetSendSocket = %d, want 1", got)
	}
	if msg.ForceSendSocket != "127.0.0.1:5060" {
		t.Errorf("SetSendSocket not recorded, got %v", msg.ForceSendSocket)
	}

	if got := c.SetRecvSocket(msg, "127.0.0.1:5061"); got != 1 {
		t.Errorf("SetRecvSocket = %d, want 1", got)
	}

	// Empty socket rejected.
	if got := c.ForceSendSocket(msg, ""); got != -1 {
		t.Errorf("ForceSendSocket(empty) = %d, want -1", got)
	}
	if got := c.SetSendSocket(nil, "x"); got != -1 {
		t.Errorf("SetSendSocket(nil) = %d, want -1", got)
	}
}

func TestIsMyself(t *testing.T) {
	c := NewCoreXModule()
	// Loopback defaults are pre-registered.
	if !c.IsMyself("sip:alice@127.0.0.1") {
		t.Error("IsMyself(127.0.0.1) = false, want true")
	}
	if !c.IsMyself("sip:alice@localhost") {
		t.Error("IsMyself(localhost) = false, want true")
	}
	if c.IsMyself("sip:alice@biloxi.com") {
		t.Error("IsMyself(biloxi.com) = true, want false")
	}

	c.AddMyHost("biloxi.com")
	if !c.IsMyself("sip:alice@biloxi.com") {
		t.Error("IsMyself(biloxi.com after add) = false, want true")
	}
	// Case-insensitive host.
	if !c.IsMyself("sip:alice@BILOXI.COM") {
		t.Error("IsMyself(BILOXI.COM) = false, want true")
	}

	// Subdomain alias matching.
	c.AddAliasSubdomains("example.com")
	if !c.IsMyself("sip:alice@sub.example.com") {
		t.Error("IsMyself(sub.example.com) = false, want true")
	}
	if c.IsMyself("sip:alice@notexample.com") {
		t.Error("IsMyself(notexample.com) = true, want false")
	}

	// Unparseable URI.
	if c.IsMyself("not-a-uri") {
		t.Error("IsMyself(not-a-uri) = true, want false")
	}
}

func TestIsMyselfRURIFromTo(t *testing.T) {
	c := NewCoreXModule()
	c.AddMyHost("biloxi.com")
	c.AddMyHost("127.0.0.1")

	msg := mustParse(t, inviteToBob)
	// R-URI host is biloxi.com.
	if !c.IsMyselfRURI(msg) {
		t.Error("IsMyselfRURI = false, want true (biloxi.com)")
	}
	// From host is atlanta.com (not ours).
	if c.IsMyselfFrom(msg) {
		t.Error("IsMyselfFrom = true, want false (atlanta.com)")
	}
	// To host is biloxi.com (ours).
	if !c.IsMyselfTo(msg) {
		t.Error("IsMyselfTo = false, want true (biloxi.com)")
	}

	// After rewriting the R-URI to a non-local host, IsMyselfRURI flips.
	msg.SetRURI("sip:carol@nowhere.com")
	if c.IsMyselfRURI(msg) {
		t.Error("IsMyselfRURI after rewrite = true, want false (nowhere.com)")
	}

	// nil message is safe.
	if c.IsMyselfRURI(nil) {
		t.Error("IsMyselfRURI(nil) = true, want false")
	}
}

func TestHasExpiresAndGT(t *testing.T) {
	c := NewCoreXModule()

	withExp := mustParse(t, inviteWithBody)
	withoutExp := mustParse(t, inviteNoCL)

	if !c.HasExpires(withExp) {
		t.Error("HasExpires(with Expires) = false, want true")
	}
	if c.HasExpires(withoutExp) {
		t.Error("HasExpires(no Expires) = true, want false")
	}
	if c.HasExpires(nil) {
		t.Error("HasExpires(nil) = true, want false")
	}

	// Expires=3600 > 100, but not > 3600.
	if !c.HasExpiresGT(withExp, 100) {
		t.Error("HasExpiresGT(100) = false, want true")
	}
	if c.HasExpiresGT(withExp, 3600) {
		t.Error("HasExpiresGT(3600) = true, want false (not strictly greater)")
	}
	if c.HasExpiresGT(withExp, 5000) {
		t.Error("HasExpiresGT(5000) = true, want false")
	}
	if c.HasExpiresGT(withoutExp, 0) {
		t.Error("HasExpiresGT(no Expires) = true, want false")
	}
}

func TestHasBodyAndContentLength(t *testing.T) {
	c := NewCoreXModule()

	withBody := mustParse(t, inviteWithBody)
	noBody := mustParse(t, inviteNoCL)

	if !c.HasBody(withBody) {
		t.Error("HasBody(with body) = false, want true")
	}
	if c.HasBody(noBody) {
		t.Error("HasBody(no body) = true, want false")
	}
	if c.HasBody(nil) {
		t.Error("HasBody(nil) = true, want false")
	}

	if !c.HasContentLength(withBody) {
		t.Error("HasContentLength(with CL) = false, want true")
	}
	if c.HasContentLength(noBody) {
		t.Error("HasContentLength(no CL) = true, want false")
	}
	if c.HasContentLength(nil) {
		t.Error("HasContentLength(nil) = true, want false")
	}

	// ContentLength convenience helper.
	if got := c.ContentLength(withBody); got != 20 {
		t.Errorf("ContentLength(with body) = %d, want 20", got)
	}
	if got := c.ContentLength(noBody); got != -1 {
		t.Errorf("ContentLength(no CL) = %d, want -1", got)
	}
}

func TestDefaultAndInit(t *testing.T) {
	d := DefaultCoreX()
	if d == nil {
		t.Fatal("DefaultCoreX() = nil")
	}
	if !d.IsMyself("sip:alice@127.0.0.1") {
		t.Error("DefaultCoreX does not know 127.0.0.1")
	}

	// Register a host on the default, then Init resets it.
	d.AddMyHost("temporary.example")
	if !d.IsMyself("sip:alice@temporary.example") {
		t.Error("host not registered on default")
	}
	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if d2 := DefaultCoreX(); d2.IsMyself("sip:alice@temporary.example") {
		t.Error("Init() did not reset the default instance")
	}
}

func TestConcurrentAccess(t *testing.T) {
	c := NewCoreXModule()
	c.AddMyHost("biloxi.com")
	msg := mustParse(t, inviteToBob)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c.AppendBranch(msg, "sip:b@biloxi.com", i)
			_ = c.IsMyself("sip:alice@biloxi.com")
			_ = c.HasExpires(msg)
			_ = c.Branches()
		}(i)
	}
	wg.Wait()
}
