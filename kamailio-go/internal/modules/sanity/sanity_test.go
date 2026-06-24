// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - Sanity module tests.
 */

package sanity

import (
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// A well-formed INVITE that should pass every sanity check.
var testInvite = []byte("INVITE sip:user@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.example.com;branch=z9hG4bK776asdhds\r\n" +
	"Max-Forwards: 70\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1928301774\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: a84b4c76e66710@pc33.example.com\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"Contact: <sip:alice@pc33.example.com>\r\n" +
	"Content-Type: application/sdp\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

// An INVITE whose CSeq method does not match the request method.
var testBadCSeq = []byte("INVITE sip:user@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.example.com;branch=z9hG4bK776asdhds\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1928301774\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: a84b4c76e66710@pc33.example.com\r\n" +
	"CSeq: 314159 BYE\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

// An INVITE with a Content-Length/body mismatch.
var testBadCL = []byte("INVITE sip:user@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.example.com;branch=z9hG4bK776asdhds\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1928301774\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: a84b4c76e66710@pc33.example.com\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"Content-Length: 5\r\n" +
	"\r\n")

func mustParse(t *testing.T, b []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(b)
	if err != nil {
		t.Fatalf("ParseMsg failed: %v", err)
	}
	return msg
}

// TestCheck verifies the top-level Check against a well-formed message.
func TestCheck(t *testing.T) {
	s := New()
	msg := mustParse(t, testInvite)
	r := s.Check(msg, int(CheckAll))
	if !r.Passed {
		t.Fatalf("expected all checks to pass, failed: %v", r.FailedChecks)
	}
}

// TestCheckURI validates SIP URI acceptance/rejection.
func TestCheckURI(t *testing.T) {
	s := New()
	cases := []struct {
		uri string
		ok  bool
	}{
		{"sip:user@example.com", true},
		{"sips:user@example.com:5061", true},
		{"tel:+1234567890", true},
		{"", false},
		{"not-a-uri", false},
		{"http://example.com", false},
	}
	for _, c := range cases {
		if got := s.CheckURI(c.uri); got != c.ok {
			t.Errorf("CheckURI(%q) = %v, want %v", c.uri, got, c.ok)
		}
	}
}

// TestCheckVia verifies the top Via header validation.
func TestCheckVia(t *testing.T) {
	s := New()
	msg := mustParse(t, testInvite)
	if !s.CheckVia(msg) {
		t.Fatal("expected CheckVia to pass for valid message")
	}
}

// TestCheckVia_Missing verifies a missing Via fails.
func TestCheckVia_Missing(t *testing.T) {
	s := New()
	msg := &parser.SIPMsg{}
	if s.CheckVia(msg) {
		t.Fatal("expected CheckVia to fail with no Via")
	}
}

// TestCheckCSeq verifies CSeq validation against a matching method.
func TestCheckCSeq(t *testing.T) {
	s := New()
	msg := mustParse(t, testInvite)
	if !s.CheckCSeq(msg) {
		t.Fatal("expected CheckCSeq to pass for valid message")
	}
}

// TestCheckCSeq_MethodMismatch verifies a CSeq method mismatch is detected.
func TestCheckCSeq_MethodMismatch(t *testing.T) {
	s := New()
	msg := mustParse(t, testBadCSeq)
	if s.CheckCSeq(msg) {
		t.Fatal("expected CheckCSeq to fail for method mismatch")
	}
}

// TestCheckRURI verifies the request URI validation.
func TestCheckRURI(t *testing.T) {
	s := New()
	msg := mustParse(t, testInvite)
	if !s.CheckRURI(msg) {
		t.Fatal("expected CheckRURI to pass for valid message")
	}
}

// TestCheckContentLength verifies the Content-Length/body match.
func TestCheckContentLength(t *testing.T) {
	s := New()
	msg := mustParse(t, testInvite)
	if !s.CheckContentLength(msg) {
		t.Fatal("expected CheckContentLength to pass for valid message")
	}
}

// TestCheckContentLength_Mismatch verifies a mismatch is detected.
func TestCheckContentLength_Mismatch(t *testing.T) {
	s := New()
	msg := mustParse(t, testBadCL)
	if s.CheckContentLength(msg) {
		t.Fatal("expected CheckContentLength to fail for mismatch")
	}
}

// TestAllChecksPass verifies a fully valid message passes every check
// and reports no failed checks.
func TestAllChecksPass(t *testing.T) {
	s := New()
	msg := mustParse(t, testInvite)
	r := s.Check(msg, int(CheckAll))
	if !r.Passed {
		t.Fatalf("expected all checks to pass, failed: %v", r.FailedChecks)
	}
	if len(r.FailedChecks) != 0 {
		t.Fatalf("expected no failed checks, got %v", r.FailedChecks)
	}
}

// TestCheck_Selective verifies that only requested checks are run.
func TestCheck_Selective(t *testing.T) {
	s := New()
	msg := mustParse(t, testBadCSeq)
	// Only Via is requested - CSeq mismatch must not fail the result.
	r := s.Check(msg, int(CheckVia))
	if !r.Passed {
		t.Fatalf("expected Via-only check to pass, failed: %v", r.FailedChecks)
	}
	// Now request CSeq - it must fail.
	r = s.Check(msg, int(CheckCSeq))
	if r.Passed {
		t.Fatal("expected CSeq-only check to fail for mismatched method")
	}
}

// TestGlobalFunctions exercises the package-level API.
func TestGlobalFunctions(t *testing.T) {
	if got := DefaultSanity(); got != int(CheckAll) {
		t.Fatalf("expected DefaultSanity %d, got %d", int(CheckAll), got)
	}
	Init()
	msg := mustParse(t, testInvite)
	r := Check(msg, DefaultSanity())
	if !r.Passed {
		t.Fatalf("expected global Check to pass, failed: %v", r.FailedChecks)
	}
}
