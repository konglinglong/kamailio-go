// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the siputils module.
 */

package siputils

import (
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// inviteNoTag is an INVITE whose To header carries no tag.
const inviteNoTag = "INVITE sip:bob@biloxi.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.atlanta.com;branch=z9hG4bK776\r\n" +
	"To: Bob <sip:bob@biloxi.com>\r\n" +
	"From: Alice <sip:alice@atlanta.com>;tag=1928301774\r\n" +
	"Call-ID: a84b4c76e66710@pc33.atlanta.com\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n"

// inviteWithTag is the same INVITE but with a To tag.
const inviteWithTag = "INVITE sip:bob@biloxi.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.atlanta.com;branch=z9hG4bK776\r\n" +
	"To: Bob <sip:bob@biloxi.com>;tag=abc123\r\n" +
	"From: Alice <sip:alice@atlanta.com>;tag=1928301774\r\n" +
	"Call-ID: a84b4c76e66710@pc33.atlanta.com\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n"

func parseRaw(t *testing.T, raw string) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg([]byte(raw))
	if err != nil {
		t.Fatalf("ParseMsg failed: %v", err)
	}
	return msg
}

func TestHasToTag(t *testing.T) {
	s := NewSipUtilsModule()

	msg := parseRaw(t, inviteNoTag)
	if s.HasToTag(msg) {
		t.Error("HasToTag(no tag) = true, want false")
	}

	msg2 := parseRaw(t, inviteWithTag)
	if !s.HasToTag(msg2) {
		t.Error("HasToTag(tag=abc123) = false, want true")
	}

	// nil / missing To must not panic.
	if s.HasToTag(nil) {
		t.Error("HasToTag(nil) = true, want false")
	}
}

func TestHasSipUri(t *testing.T) {
	s := NewSipUtilsModule()

	if !s.HasSipUri("sip:alice@atlanta.com") {
		t.Error("HasSipUri(sip:...) = false, want true")
	}
	if !s.HasSipUri("sips:alice@atlanta.com") {
		t.Error("HasSipUri(sips:...) = false, want true")
	}
	if s.HasSipUri("tel:+12125551234") {
		t.Error("HasSipUri(tel:...) = true, want false")
	}
	if s.HasSipUri("http://example.com") {
		t.Error("HasSipUri(http:...) = true, want false")
	}
	if s.HasSipUri("not-a-uri") {
		t.Error("HasSipUri(not-a-uri) = true, want false")
	}
}

func TestHasTelUri(t *testing.T) {
	s := NewSipUtilsModule()

	if !s.HasTelUri("tel:+12125551234") {
		t.Error("HasTelUri(tel:...) = false, want true")
	}
	if !s.HasTelUri("tels:+12125551234") {
		t.Error("HasTelUri(tels:...) = false, want true")
	}
	if s.HasTelUri("sip:alice@atlanta.com") {
		t.Error("HasTelUri(sip:...) = true, want false")
	}
}

func TestIsUriUserE164(t *testing.T) {
	s := NewSipUtilsModule()

	if !s.IsUriUserE164("sip:+12125551234@example.com") {
		t.Error("IsUriUserE164(+12125551234) = false, want true")
	}
	if !s.IsUriUserE164("sip:12125551234@example.com") {
		t.Error("IsUriUserE164(12125551234, no plus) = false, want true")
	}
	if s.IsUriUserE164("sip:alice@example.com") {
		t.Error("IsUriUserE164(alice) = true, want false")
	}
	// 16 digits exceeds the E.164 maximum of 15.
	if s.IsUriUserE164("sip:+1234567890123456@example.com") {
		t.Error("IsUriUserE164(16 digits) = true, want false")
	}
}

func TestGetUriUser(t *testing.T) {
	s := NewSipUtilsModule()

	if got := s.GetUriUser("sip:alice@atlanta.com"); got != "alice" {
		t.Errorf("GetUriUser(alice) = %q, want %q", got, "alice")
	}
	if got := s.GetUriUser("sip:bob:secret@biloxi.com:5060"); got != "bob" {
		t.Errorf("GetUriUser(with password) = %q, want %q", got, "bob")
	}
	if got := s.GetUriUser("sip:atlanta.com"); got != "" {
		t.Errorf("GetUriUser(no user) = %q, want empty", got)
	}
}

func TestGetUriHost(t *testing.T) {
	s := NewSipUtilsModule()

	if got := s.GetUriHost("sip:alice@atlanta.com"); got != "atlanta.com" {
		t.Errorf("GetUriHost = %q, want %q", got, "atlanta.com")
	}
	if got := s.GetUriHost("sip:bob@biloxi.com:5060"); got != "biloxi.com" {
		t.Errorf("GetUriHost(with port) = %q, want %q", got, "biloxi.com")
	}
	// IPv6 literal.
	if got := s.GetUriHost("sip:alice@[2001:db8::1]:5060"); got != "2001:db8::1" {
		t.Errorf("GetUriHost(ipv6) = %q, want %q", got, "2001:db8::1")
	}
}

func TestGetUriParam(t *testing.T) {
	s := NewSipUtilsModule()

	uri := "sip:alice@atlanta.com:5060;transport=udp;lr;user=phone"

	if got := s.GetUriParam(uri, "transport"); got != "udp" {
		t.Errorf("GetUriParam(transport) = %q, want %q", got, "udp")
	}
	if got := s.GetUriParam(uri, "user"); got != "phone" {
		t.Errorf("GetUriParam(user) = %q, want %q", got, "phone")
	}
	if got := s.GetUriParam(uri, "missing"); got != "" {
		t.Errorf("GetUriParam(missing) = %q, want empty", got)
	}
	// Case-insensitive parameter name matching.
	if got := s.GetUriParam(uri, "TRANSPORT"); got != "udp" {
		t.Errorf("GetUriParam(TRANSPORT) = %q, want %q", got, "udp")
	}
}

func TestCompareURI(t *testing.T) {
	s := NewSipUtilsModule()

	if !s.CompareURI("sip:alice@atlanta.com", "sip:alice@atlanta.com") {
		t.Error("CompareURI(same) = false, want true")
	}
	// Host part is case-insensitive.
	if !s.CompareURI("sip:alice@atlanta.com", "sip:alice@ATLANTA.COM") {
		t.Error("CompareURI(host case) = false, want true")
	}
	// Default port (5060) equals explicit 5060.
	if !s.CompareURI("sip:alice@atlanta.com", "sip:alice@atlanta.com:5060") {
		t.Error("CompareURI(default port) = false, want true")
	}
	// Different user.
	if s.CompareURI("sip:alice@atlanta.com", "sip:bob@atlanta.com") {
		t.Error("CompareURI(diff user) = true, want false")
	}
	// Different host.
	if s.CompareURI("sip:alice@atlanta.com", "sip:alice@biloxi.com") {
		t.Error("CompareURI(diff host) = true, want false")
	}
	// Different scheme.
	if s.CompareURI("sip:alice@atlanta.com", "sips:alice@atlanta.com") {
		t.Error("CompareURI(diff scheme) = true, want false")
	}
}
