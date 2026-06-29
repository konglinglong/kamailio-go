// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - StirShaken module tests.
 */

package stirshaken

import (
	"strings"
	"sync"
	"testing"

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

// TestBuildAndParsePassport verifies that a built token round-trips
// through ParsePassportToken with the expected claims.
func TestBuildAndParsePassport(t *testing.T) {
	m := New()
	tok, err := m.BuildPassportToken("+15551234567", "+15557654321", "A")
	if err != nil {
		t.Fatalf("BuildPassportToken: %v", err)
	}
	if strings.Count(tok, ".") != 2 {
		t.Fatalf("expected 2 dots in token, got %q", tok)
	}

	parsed, err := m.ParsePassportToken(tok)
	if err != nil {
		t.Fatalf("ParsePassportToken: %v", err)
	}
	if got := parsed.Header["typ"]; got != "passport" {
		t.Fatalf("expected typ=passport, got %q", got)
	}
	if got := parsed.Header["ppt"]; got != "shaken" {
		t.Fatalf("expected ppt=shaken, got %q", got)
	}
	if got := parsed.Header["alg"]; got != DefaultAlg {
		t.Fatalf("expected alg=%s, got %q", DefaultAlg, got)
	}
	if got := parsed.Payload["att"]; got != "A" {
		t.Fatalf("expected att=A, got %v", got)
	}
	if parsed.Signature == "" {
		t.Fatal("expected non-empty signature")
	}
}

// TestBuildPassportToken_Errors verifies validation of inputs.
func TestBuildPassportToken_Errors(t *testing.T) {
	m := New()
	if _, err := m.BuildPassportToken("", "+15557654321", "A"); err == nil {
		t.Fatal("expected error for empty origTN")
	}
	if _, err := m.BuildPassportToken("+15551234567", "", "A"); err == nil {
		t.Fatal("expected error for empty destTN")
	}
	// Empty attestation defaults to A.
	tok, err := m.BuildPassportToken("+15551234567", "+15557654321", "")
	if err != nil {
		t.Fatalf("expected default attestation, got error: %v", err)
	}
	parsed, err := m.ParsePassportToken(tok)
	if err != nil {
		t.Fatalf("ParsePassportToken: %v", err)
	}
	if got := parsed.Payload["att"]; got != "A" {
		t.Fatalf("expected default att=A, got %v", got)
	}
}

// TestParsePassportToken_Errors verifies malformed tokens are rejected.
func TestParsePassportToken_Errors(t *testing.T) {
	m := New()
	if _, err := m.ParsePassportToken(""); err == nil {
		t.Fatal("expected error for empty token")
	}
	if _, err := m.ParsePassportToken("not.a.valid"); err == nil {
		t.Fatal("expected error for invalid base64")
	}
	if _, err := m.ParsePassportToken("only.two"); err == nil {
		t.Fatal("expected error for two-part token")
	}
}

// TestSignVerifyRoundTrip verifies that Sign + AddIdentityHeader + Verify
// succeeds on a freshly signed message.
func TestSignVerifyRoundTrip(t *testing.T) {
	m := New()
	msg := mustParse(t, testInvite)

	tok, err := m.Sign(msg, "+15551234567", "+15557654321")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if rc := m.AddIdentityHeader(msg, tok); rc != 0 {
		t.Fatalf("AddIdentityHeader returned %d", rc)
	}
	if msg.Identity == nil {
		t.Fatal("Identity quick-ref not set")
	}
	if !strings.Contains(msg.Identity.Body.String(), ";ppt=shaken") {
		t.Fatalf("Identity header missing ppt param: %q", msg.Identity.Body.String())
	}

	ok, err := m.Verify(msg)
	if err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if !ok {
		t.Fatal("expected Verify to succeed")
	}
	if !m.CheckIdentityHeader(msg) {
		t.Fatal("expected CheckIdentityHeader true")
	}
}

// TestVerify_TamperedToken verifies that a modified token fails verification.
func TestVerify_TamperedToken(t *testing.T) {
	m := New()
	msg := mustParse(t, testInvite)

	tok, err := m.BuildPassportToken("+15551234567", "+15557654321", "A")
	if err != nil {
		t.Fatalf("BuildPassportToken: %v", err)
	}
	// Flip the last character of the signature.
	tampered := tok[:len(tok)-1] + "X"
	m.AddIdentityHeader(msg, tampered)

	ok, err := m.Verify(msg)
	if err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if ok {
		t.Fatal("expected Verify to fail for tampered token")
	}
	if m.CheckIdentityHeader(msg) {
		t.Fatal("expected CheckIdentityHeader false for tampered token")
	}
}

// TestVerify_NoIdentity verifies that a message without an Identity header
// fails verification cleanly.
func TestVerify_NoIdentity(t *testing.T) {
	m := New()
	msg := mustParse(t, testInvite)
	ok, err := m.Verify(msg)
	if err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if ok {
		t.Fatal("expected Verify false without Identity header")
	}
	if m.CheckIdentityHeader(msg) {
		t.Fatal("expected CheckIdentityHeader false without Identity header")
	}
}

// TestAddIdentityHeader_Errors verifies nil/empty handling.
func TestAddIdentityHeader_Errors(t *testing.T) {
	m := New()
	if rc := m.AddIdentityHeader(nil, "tok"); rc != -1 {
		t.Fatalf("expected -1 for nil msg, got %d", rc)
	}
	msg := mustParse(t, testInvite)
	if rc := m.AddIdentityHeader(msg, ""); rc != -1 {
		t.Fatalf("expected -1 for empty token, got %d", rc)
	}
}

// TestConfig verifies configuration is applied and read back.
func TestConfig(t *testing.T) {
	cfg := SHakenConfig{
		Authority:       "my-authority",
		PrivateKeyPath:  "/etc/key.pem",
		CertificatePath: "/etc/cert.pem",
		DefaultAlg:      "HS256",
	}
	m := NewWithConfig(cfg)
	got := m.Config()
	if got.Authority != "my-authority" {
		t.Fatalf("expected authority my-authority, got %q", got.Authority)
	}
	if got.PrivateKeyPath != "/etc/key.pem" {
		t.Fatalf("expected private key path, got %q", got.PrivateKeyPath)
	}
	// A token signed with one config should still verify with the same module.
	tok, err := m.BuildPassportToken("+1", "+2", "A")
	if err != nil {
		t.Fatalf("BuildPassportToken: %v", err)
	}
	ok, err := m.verifyToken(tok)
	if err != nil || !ok {
		t.Fatalf("expected token to verify, ok=%v err=%v", ok, err)
	}
}

// TestConfig_DifferentAuthorityFailsVerify verifies that a token signed
// with one authority does not verify under another.
func TestConfig_DifferentAuthorityFailsVerify(t *testing.T) {
	m1 := NewWithConfig(SHakenConfig{Authority: "authority-one"})
	m2 := NewWithConfig(SHakenConfig{Authority: "authority-two"})
	tok, err := m1.BuildPassportToken("+1", "+2", "A")
	if err != nil {
		t.Fatalf("BuildPassportToken: %v", err)
	}
	ok, err := m2.verifyToken(tok)
	if err != nil {
		t.Fatalf("verifyToken error: %v", err)
	}
	if ok {
		t.Fatal("expected token signed by authority-one to fail under authority-two")
	}
}

// TestGlobalFunctions exercises the package-level API.
func TestGlobalFunctions(t *testing.T) {
	Init()
	m := DefaultStirShaken()
	if m == nil {
		t.Fatal("expected non-nil default module")
	}
	msg := mustParse(t, testInvite)
	tok, err := Sign(msg, "+15551234567", "+15557654321")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if rc := AddIdentityHeader(msg, tok); rc != 0 {
		t.Fatalf("AddIdentityHeader returned %d", rc)
	}
	ok, err := Verify(msg)
	if err != nil || !ok {
		t.Fatalf("expected Verify ok, ok=%v err=%v", ok, err)
	}
}

// TestConcurrentAccess exercises the module under the race detector.
func TestConcurrentAccess(t *testing.T) {
	m := NewWithConfig(SHakenConfig{Authority: "race-authority"})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, err := m.BuildPassportToken("+1", "+2", "A")
			if err != nil {
				t.Errorf("BuildPassportToken: %v", err)
				return
			}
			ok, err := m.verifyToken(tok)
			if err != nil || !ok {
				t.Errorf("expected verify ok, ok=%v err=%v", ok, err)
			}
			_ = m.Config()
		}()
	}
	wg.Wait()
}
