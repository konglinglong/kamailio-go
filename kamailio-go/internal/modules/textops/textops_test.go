// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the textops module.
 */

package textops

import (
	"bytes"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// inviteMsg is a sample INVITE with two Via headers and a small body used by
// the tests below.
const inviteMsg = "INVITE sip:bob@biloxi.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.atlanta.com;branch=z9hG4bK776asdhds\r\n" +
	"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776asdhds2\r\n" +
	"Max-Forwards: 70\r\n" +
	"To: Bob <sip:bob@biloxi.com>\r\n" +
	"From: Alice <sip:alice@atlanta.com>;tag=1928301774\r\n" +
	"Call-ID: a84b4c76e66710@pc33.atlanta.com\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"Contact: <sip:alice@pc33.atlanta.com>\r\n" +
	"Content-Type: application/sdp\r\n" +
	"Content-Length: 4\r\n" +
	"\r\n" +
	"test"

func parseMsg(t *testing.T) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg([]byte(inviteMsg))
	if err != nil {
		t.Fatalf("ParseMsg failed: %v", err)
	}
	return msg
}

func TestSearch(t *testing.T) {
	to := NewTextOpsModule()
	msg := parseMsg(t)

	if !to.Search(msg, "INVITE") {
		t.Error("Search(INVITE) = false, want true")
	}
	if to.Search(msg, "zzzznotfound") {
		t.Error("Search(zzzznotfound) = true, want false")
	}
	// Case-insensitive search via (?i) prefix.
	if !to.Search(msg, "(?i)invite") {
		t.Error("Search((?i)invite) = false, want true")
	}
	// nil message must not panic.
	if to.Search(nil, "x") {
		t.Error("Search(nil) = true, want false")
	}
}

func TestSearchBody(t *testing.T) {
	to := NewTextOpsModule()
	msg := parseMsg(t)

	if !to.SearchBody(msg, "test") {
		t.Error("SearchBody(test) = false, want true")
	}
	// "INVITE" lives in the request line / headers, not the body.
	if to.SearchBody(msg, "INVITE") {
		t.Error("SearchBody(INVITE) = true, want false")
	}
	if to.SearchBody(msg, "absent") {
		t.Error("SearchBody(absent) = true, want false")
	}
}

func TestSubst(t *testing.T) {
	to := NewTextOpsModule()
	msg := parseMsg(t)

	// "biloxi" appears in the R-URI and the To header (2 occurrences).
	n := to.Subst(msg, "biloxi", "chicago")
	if n != 2 {
		t.Errorf("Subst(biloxi->chicago) count = %d, want 2", n)
	}
	if !bytes.Contains(msg.Buf, []byte("chicago")) {
		t.Error("buffer does not contain 'chicago' after Subst")
	}
	if bytes.Contains(msg.Buf, []byte("biloxi")) {
		t.Error("buffer still contains 'biloxi' after Subst")
	}

	// No match -> 0 substitutions and buffer unchanged.
	n2 := to.Subst(msg, "zzzznotfound", "x")
	if n2 != 0 {
		t.Errorf("Subst(no-match) count = %d, want 0", n2)
	}
}

func TestRemoveHeader(t *testing.T) {
	to := NewTextOpsModule()
	msg := parseMsg(t)

	n := to.RemoveHeader(msg, "Contact")
	if n != 1 {
		t.Errorf("RemoveHeader(Contact) = %d, want 1", n)
	}
	if to.IsPresentHeader(msg, "Contact") {
		t.Error("Contact still present after RemoveHeader")
	}

	// Remove all Via headers (there are two).
	n2 := to.RemoveHeader(msg, "Via")
	if n2 != 2 {
		t.Errorf("RemoveHeader(Via) = %d, want 2", n2)
	}
	if to.CountHeader(msg, "Via") != 0 {
		t.Error("Via headers remain after RemoveHeader")
	}

	// Removing an absent header returns 0.
	n3 := to.RemoveHeader(msg, "X-Absent")
	if n3 != 0 {
		t.Errorf("RemoveHeader(X-Absent) = %d, want 0", n3)
	}
}

func TestIsPresentHeader(t *testing.T) {
	to := NewTextOpsModule()
	msg := parseMsg(t)

	if !to.IsPresentHeader(msg, "To") {
		t.Error("IsPresentHeader(To) = false, want true")
	}
	if !to.IsPresentHeader(msg, "Call-ID") {
		t.Error("IsPresentHeader(Call-ID) = false, want true")
	}
	if to.IsPresentHeader(msg, "X-Missing") {
		t.Error("IsPresentHeader(X-Missing) = true, want false")
	}
	// Header name matching is case-insensitive.
	if !to.IsPresentHeader(msg, "via") {
		t.Error("IsPresentHeader(via) = false, want true (case-insensitive)")
	}
}

func TestCountHeader(t *testing.T) {
	to := NewTextOpsModule()
	msg := parseMsg(t)

	if got := to.CountHeader(msg, "Via"); got != 2 {
		t.Errorf("CountHeader(Via) = %d, want 2", got)
	}
	if got := to.CountHeader(msg, "To"); got != 1 {
		t.Errorf("CountHeader(To) = %d, want 1", got)
	}
	if got := to.CountHeader(msg, "X-Missing"); got != 0 {
		t.Errorf("CountHeader(X-Missing) = %d, want 0", got)
	}
}

func TestAppendToReply(t *testing.T) {
	to := NewTextOpsModule()
	msg := parseMsg(t)

	ClearReplyHeaders(msg)
	to.AppendToReply(msg, "P-Header: value1")
	to.AppendToReply(msg, "P-Other: value2")

	hdrs := GetReplyHeaders(msg)
	if len(hdrs) != 2 {
		t.Fatalf("GetReplyHeaders len = %d, want 2", len(hdrs))
	}
	if hdrs[0] != "P-Header: value1" {
		t.Errorf("hdrs[0] = %q, want %q", hdrs[0], "P-Header: value1")
	}
	if hdrs[1] != "P-Other: value2" {
		t.Errorf("hdrs[1] = %q, want %q", hdrs[1], "P-Other: value2")
	}

	// A fresh message should have no accumulated reply headers.
	other := parseMsg(t)
	if got := GetReplyHeaders(other); got != nil {
		t.Errorf("GetReplyHeaders(fresh) = %v, want nil", got)
	}
}
