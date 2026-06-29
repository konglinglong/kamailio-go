// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - SecSipID module tests.
 */

package secsipid

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// A valid INVITE without an Identity header.
var testInvite = []byte("INVITE sip:bob@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.example.com;branch=z9hG4bK776asdhds\r\n" +
	"Max-Forwards: 70\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1928301774\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: a84b4c76e66710@pc33.example.com\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"Contact: <sip:alice@pc33.example.com>\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

func mustParse(t *testing.T, b []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(b)
	if err != nil {
		t.Fatalf("ParseMsg failed: %v", err)
	}
	return msg
}

// TestSignVerifyRoundTrip verifies that a signed token verifies.
func TestSignVerifyRoundTrip(t *testing.T) {
	m := New()
	tok, err := m.Sign("uuid-1234", "+15551234567", "+15557654321")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if strings.Count(tok, ".") != 2 {
		t.Fatalf("expected 2 dots, got %q", tok)
	}
	ok, err := m.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Fatal("expected Verify true")
	}
}

// TestSign_Errors verifies input validation.
func TestSign_Errors(t *testing.T) {
	m := New()
	if _, err := m.Sign("", "+1", "+2"); err == nil {
		t.Fatal("expected error for empty origID")
	}
	if _, err := m.Sign("id", "", "+2"); err == nil {
		t.Fatal("expected error for empty origTN")
	}
	if _, err := m.Sign("id", "+1", ""); err == nil {
		t.Fatal("expected error for empty destTN")
	}
}

// TestVerify_Errors verifies malformed tokens are rejected.
func TestVerify_Errors(t *testing.T) {
	m := New()
	if _, err := m.Verify(""); err == nil {
		t.Fatal("expected error for empty token")
	}
	if _, err := m.Verify("only.two"); err == nil {
		t.Fatal("expected error for two-part token")
	}
	// Valid format but bad signature.
	bad := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ0ZXN0IjoxfQ.badsig"
	ok, err := m.Verify(bad)
	if err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if ok {
		t.Fatal("expected Verify false for bad signature")
	}
}

// TestVerify_Tampered verifies that a modified token fails.
func TestVerify_Tampered(t *testing.T) {
	m := New()
	tok, err := m.Sign("uuid-1234", "+15551234567", "+15557654321")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	// Flip last char of signature.
	tampered := tok[:len(tok)-1] + "X"
	ok, err := m.Verify(tampered)
	if err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if ok {
		t.Fatal("expected Verify false for tampered token")
	}
}

// TestVerify_DifferentKey verifies that a token signed with one key does
// not verify under another.
func TestVerify_DifferentKey(t *testing.T) {
	m1 := NewWithConfig(SecSipIDConfig{PrivateKey: "key-one", PublicKey: "key-one"})
	m2 := NewWithConfig(SecSipIDConfig{PrivateKey: "key-two", PublicKey: "key-two"})
	tok, err := m1.Sign("id", "+1", "+2")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	ok, err := m2.Verify(tok)
	if err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if ok {
		t.Fatal("expected token signed by key-one to fail under key-two")
	}
}

// TestParseIdentity verifies token extraction from Identity header values.
func TestParseIdentity(t *testing.T) {
	tok, err := ParseIdentity("abc.def.ghi;info=<https://x/cert>;alg=HS256;ppt=shaken")
	if err != nil {
		t.Fatalf("ParseIdentity: %v", err)
	}
	if tok != "abc.def.ghi" {
		t.Fatalf("expected abc.def.ghi, got %q", tok)
	}
	if _, err := ParseIdentity(""); err == nil {
		t.Fatal("expected error for empty header")
	}
	if _, err := ParseIdentity(";info=<x>"); err == nil {
		t.Fatal("expected error for header with no token")
	}
	if got := ParseIdentityToken("abc.def.ghi;alg=HS256"); got != "abc.def.ghi" {
		t.Fatalf("expected abc.def.ghi, got %q", got)
	}
	if got := ParseIdentityToken(""); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

// TestBuildIdentityHeader verifies header construction.
func TestBuildIdentityHeader(t *testing.T) {
	m := New()
	hdr := m.BuildIdentityHeader("abc.def.ghi")
	if !strings.HasPrefix(hdr, "abc.def.ghi;") {
		t.Fatalf("unexpected header: %q", hdr)
	}
	if !strings.Contains(hdr, ";alg=HS256") {
		t.Fatalf("missing alg param: %q", hdr)
	}
	if !strings.Contains(hdr, ";ppt=shaken") {
		t.Fatalf("missing ppt param: %q", hdr)
	}
	if got := m.BuildIdentityHeader(""); got != "" {
		t.Fatalf("expected empty for empty token, got %q", got)
	}
}

// TestCheckIdentity verifies end-to-end Identity header handling.
func TestCheckIdentity(t *testing.T) {
	m := New()
	msg := mustParse(t, testInvite)

	// No Identity header -> false, nil error.
	ok, err := m.CheckIdentity(msg)
	if err != nil {
		t.Fatalf("CheckIdentity error: %v", err)
	}
	if ok {
		t.Fatal("expected false without Identity header")
	}

	tok, err := m.Sign("uuid-1234", "+15551234567", "+15557654321")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	hdr := m.BuildIdentityHeader(tok)
	msg.AddHeader("Identity", hdr)
	if msg.Identity == nil {
		msg.Identity = msg.GetHeaderByType(parser.HdrIdentity)
	}

	ok, err = m.CheckIdentity(msg)
	if err != nil {
		t.Fatalf("CheckIdentity error: %v", err)
	}
	if !ok {
		t.Fatal("expected CheckIdentity true after adding valid header")
	}
}

// TestCheckIdentity_Nil verifies nil message handling.
func TestCheckIdentity_Nil(t *testing.T) {
	m := New()
	ok, err := m.CheckIdentity(nil)
	if err == nil {
		t.Fatal("expected error for nil message")
	}
	if ok {
		t.Fatal("expected false for nil message")
	}
}

// TestConfig verifies configuration is applied and read back.
func TestConfig(t *testing.T) {
	cfg := SecSipIDConfig{
		PrivateKey:    "my-key",
		PublicKey:     "my-key",
		DefaultExpire: 120,
	}
	m := NewWithConfig(cfg)
	got := m.Config()
	if got.PrivateKey != "my-key" {
		t.Fatalf("expected private key my-key, got %q", got.PrivateKey)
	}
	if got.DefaultExpire != 120 {
		t.Fatalf("expected expire 120, got %d", got.DefaultExpire)
	}
	// Default expire applied when zero.
	m2 := NewWithConfig(SecSipIDConfig{PrivateKey: "k", PublicKey: "k"})
	if m2.Config().DefaultExpire != DefaultExpire {
		t.Fatalf("expected default expire %d, got %d", DefaultExpire, m2.Config().DefaultExpire)
	}
}

// TestExpiry verifies that an expired token fails verification.
func TestExpiry(t *testing.T) {
	m := NewWithConfig(SecSipIDConfig{PrivateKey: "k", PublicKey: "k", DefaultExpire: 1})
	tok, err := m.Sign("id", "+1", "+2")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	// Wait past expiry.
	time.Sleep(1200 * time.Millisecond)
	ok, err := m.Verify(tok)
	if err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if ok {
		t.Fatal("expected expired token to fail verification")
	}
}

// TestGlobalFunctions exercises the package-level API.
func TestGlobalFunctions(t *testing.T) {
	Init()
	m := DefaultSecSipID()
	if m == nil {
		t.Fatal("expected non-nil default module")
	}
	tok, err := Sign("uuid", "+1", "+2")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	ok, err := Verify(tok)
	if err != nil || !ok {
		t.Fatalf("expected Verify ok, ok=%v err=%v", ok, err)
	}
}

// TestConcurrentAccess exercises the module under the race detector.
func TestConcurrentAccess(t *testing.T) {
	m := NewWithConfig(SecSipIDConfig{PrivateKey: "race-key", PublicKey: "race-key"})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, err := m.Sign("id", "+1", "+2")
			if err != nil {
				t.Errorf("Sign: %v", err)
				return
			}
			ok, err := m.Verify(tok)
			if err != nil || !ok {
				t.Errorf("expected verify ok, ok=%v err=%v", ok, err)
			}
			_ = m.Config()
		}()
	}
	wg.Wait()
}
