// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the IMS authentication module (ims_auth).
 */

package ims_auth

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/ims/auth"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func mustParseMsg(t *testing.T, raw []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("failed to parse message: %v", err)
	}
	return msg
}

func itoaN(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}

func bytesFromHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// seededVector returns a fixed AuthVector with a known XRES so the test can
// build a matching Authorization response.
func seededVector() *auth.AuthVector {
	return &auth.AuthVector{
		RAND: bytesFromHex("000102030405060708090a0b0c0d0e0f"),
		XRES: bytesFromHex("deadbeefcafef00d1234567890abcdef"),
		CK:   bytesFromHex("11111111111111111111111111111111"),
		IK:   bytesFromHex("22222222222222222222222222222222"),
		AUTN: bytesFromHex("aabbccddeeff00112233445566778899"),
	}
}

// makeRegister builds an initial REGISTER (no Authorization).
func makeRegister(cseq int) []byte {
	return []byte("REGISTER sip:example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776reg\r\n" +
		"From: Alice <sip:alice@example.com>;tag=ftag1\r\n" +
		"To: Alice <sip:alice@example.com>\r\n" +
		"Call-ID: reg-call-1@10.0.0.1\r\n" +
		"CSeq: " + itoaN(cseq) + " REGISTER\r\n" +
		"Contact: <sip:alice@10.0.0.1:5060>\r\n" +
		"Expires: 3600\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
}

// makeAuthRegister builds a REGISTER with the supplied Authorization body.
func makeAuthRegister(cseq int, authBody string) []byte {
	return []byte("REGISTER sip:example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776reg2\r\n" +
		"From: Alice <sip:alice@example.com>;tag=ftag1\r\n" +
		"To: Alice <sip:alice@example.com>\r\n" +
		"Call-ID: reg-call-1@10.0.0.1\r\n" +
		"CSeq: " + itoaN(cseq) + " REGISTER\r\n" +
		"Contact: <sip:alice@10.0.0.1:5060>\r\n" +
		"Authorization: " + authBody + "\r\n" +
		"Expires: 3600\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
}

// parseAuthParams extracts key="value" pairs from a WWW-Authenticate or
// Authorization header value.
func parseAuthParams(header string) map[string]string {
	out := map[string]string{}
	body := strings.TrimSpace(header)
	if idx := strings.IndexByte(body, ' '); idx >= 0 {
		body = body[idx+1:]
	}
	for _, part := range strings.Split(body, ",") {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(kv[0]))
		val := strings.Trim(strings.TrimSpace(kv[1]), "\"")
		out[key] = val
	}
	return out
}

// seedVector installs a known auth vector for "alice@example.com" into the
// module's in-memory client.
func seedVector(t *testing.T, m *AuthModule) {
	t.Helper()
	c := m.InMemoryClient()
	if c == nil {
		t.Fatalf("in-memory AuthClient not configured")
	}
	c.SetAuthVector("alice@example.com", seededVector())
}

// runFullAuthFlow performs the two-step AKA authentication and returns the
// 200 OK AuthResult (or fails the test).
func runFullAuthFlow(t *testing.T, m *AuthModule) *AuthResult {
	t.Helper()
	seedVector(t, m)

	// Step 1: initial REGISTER (no Authorization) -> 401 challenge.
	msg1 := mustParseMsg(t, makeRegister(1))
	r1, err := m.Authenticate(msg1)
	if err != nil {
		t.Fatalf("Authenticate #1 error: %v", err)
	}
	if r1.StatusCode != 401 {
		t.Fatalf("expected 401 on initial REGISTER, got %d", r1.StatusCode)
	}
	wwwAuth := r1.Headers["WWW-Authenticate"].String()
	if wwwAuth == "" {
		t.Fatal("expected WWW-Authenticate header in 401")
	}
	params := parseAuthParams(wwwAuth)
	opaque := params["opaque"]
	nonce := params["nonce"]
	if opaque == "" || nonce == "" {
		t.Fatalf("missing opaque/nonce in WWW-Authenticate: %q", wwwAuth)
	}
	if !m.HasPendingChallenge("alice@example.com") {
		t.Error("expected pending challenge for alice@example.com")
	}
	if m.PendingCount() != 1 {
		t.Errorf("expected 1 pending auth, got %d", m.PendingCount())
	}

	// Step 2: REGISTER with Authorization (response = hex(XRES)).
	resp := hex.EncodeToString(seededVector().XRES)
	authBody := "Digest username=\"alice@example.com\", realm=\"ims.example.com\"" +
		", nonce=\"" + nonce + "\", uri=\"sip:example.com\", response=\"" + resp + "\"" +
		", algorithm=AKAv1-MD5, opaque=\"" + opaque + "\""
	msg2 := mustParseMsg(t, makeAuthRegister(2, authBody))
	r2, err := m.Authenticate(msg2)
	if err != nil {
		t.Fatalf("Authenticate #2 error: %v", err)
	}
	if r2.StatusCode != 200 {
		t.Fatalf("expected 200 OK after auth, got %d (%s)", r2.StatusCode, r2.StatusReason)
	}
	if !r2.Authenticated {
		t.Error("expected Authenticated=true on 200 OK")
	}
	if m.PendingCount() != 0 {
		t.Errorf("expected pending auth cleared, got %d", m.PendingCount())
	}
	return r2
}

// ---------------------------------------------------------------------------
// InMemoryAuthClient
// ---------------------------------------------------------------------------

func TestInMemoryAuthClient_MAR_ReturnsClone(t *testing.T) {
	c := NewInMemoryAuthClient()
	av := seededVector()
	c.SetAuthVector("alice@example.com", av)

	got, err := c.MAR("alice@example.com", "ims.example.com")
	if err != nil {
		t.Fatalf("MAR error: %v", err)
	}
	// Mutate the returned vector; the stored one must be unaffected.
	got.XRES[0] ^= 0xff

	got2, err := c.MAR("alice@example.com", "ims.example.com")
	if err != nil {
		t.Fatalf("MAR #2 error: %v", err)
	}
	if got2.XRES[0] == 0xff^seededVector().XRES[0] {
		t.Fatal("InMemoryAuthClient.MAR did not return a clone")
	}
}

func TestInMemoryAuthClient_MAR_UnknownSubscriber(t *testing.T) {
	c := NewInMemoryAuthClient()
	_, err := c.MAR("nobody@example.com", "ims.example.com")
	if !errors.Is(err, ErrUnknownSubscriber) {
		t.Errorf("expected ErrUnknownSubscriber, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetAuthVector
// ---------------------------------------------------------------------------

func TestGetAuthVector_CacheMiss_FetchesFromHSS(t *testing.T) {
	m := NewAuthModule()
	seedVector(t, m)

	av, err := m.GetAuthVector("alice@example.com", "ims.example.com")
	if err != nil {
		t.Fatalf("GetAuthVector error: %v", err)
	}
	if av == nil || len(av.XRES) == 0 {
		t.Fatal("expected non-nil auth vector")
	}
	if m.CacheSize() != 1 {
		t.Errorf("expected cache size 1, got %d", m.CacheSize())
	}
}

func TestGetAuthVector_CacheHit_NoSecondFetch(t *testing.T) {
	m := NewAuthModule()
	seedVector(t, m)

	// First call populates the cache.
	av1, err := m.GetAuthVector("alice@example.com", "ims.example.com")
	if err != nil {
		t.Fatalf("GetAuthVector #1 error: %v", err)
	}
	// Remove the vector from the HSS; the cached copy must still be served.
	m.InMemoryClient().SetAuthVector("alice@example.com", &auth.AuthVector{
		RAND: bytesFromHex("ffffffffffffffffffffffffffffffff"),
		XRES: bytesFromHex("ffffffffffffffffffffffffffffffff"),
	})
	av2, err := m.GetAuthVector("alice@example.com", "ims.example.com")
	if err != nil {
		t.Fatalf("GetAuthVector #2 error: %v", err)
	}
	if !bytes.Equal(av1.XRES, av2.XRES) {
		t.Error("expected cached vector to be returned unchanged on second call")
	}
}

func TestGetAuthVector_EmptyIMPI(t *testing.T) {
	m := NewAuthModule()
	_, err := m.GetAuthVector("", "ims.example.com")
	if err == nil {
		t.Error("expected error for empty IMPI")
	}
}

func TestGetAuthVector_UnknownSubscriber(t *testing.T) {
	m := NewAuthModule()
	// No vector seeded for "bob@example.com".
	_, err := m.GetAuthVector("bob@example.com", "ims.example.com")
	if !errors.Is(err, ErrUnknownSubscriber) {
		t.Errorf("expected ErrUnknownSubscriber, got %v", err)
	}
}

func TestGetAuthVector_CacheTTLExpiry(t *testing.T) {
	m := NewAuthModuleWithConfig(Config{
		Realm:           "ims.example.com",
		CacheTTL:        30 * time.Millisecond,
		MaxAttempts:     3,
		ChallengeExpiry: 5 * time.Minute,
	})
	seedVector(t, m)

	av1, err := m.GetAuthVector("alice@example.com", "ims.example.com")
	if err != nil {
		t.Fatalf("GetAuthVector #1 error: %v", err)
	}
	// Swap the stored vector so we can detect a cache miss on re-fetch.
	m.InMemoryClient().SetAuthVector("alice@example.com", &auth.AuthVector{
		RAND: bytesFromHex("ffffffffffffffffffffffffffffffff"),
		XRES: bytesFromHex("ffffffffffffffffffffffffffffffff"),
	})
	time.Sleep(50 * time.Millisecond)

	av2, err := m.GetAuthVector("alice@example.com", "ims.example.com")
	if err != nil {
		t.Fatalf("GetAuthVector #2 error: %v", err)
	}
	if bytes.Equal(av1.XRES, av2.XRES) {
		t.Error("expected re-fetch after TTL expiry, got stale cached vector")
	}
}

// ---------------------------------------------------------------------------
// BuildChallenge
// ---------------------------------------------------------------------------

func TestBuildChallenge_ReturnsChallengeAndOpaque(t *testing.T) {
	m := NewAuthModule()
	seedVector(t, m)

	wwwAuth, opaque, err := m.BuildChallenge("alice@example.com", "ims.example.com")
	if err != nil {
		t.Fatalf("BuildChallenge error: %v", err)
	}
	if opaque == "" {
		t.Error("expected non-empty opaque")
	}
	params := parseAuthParams(wwwAuth.String())
	if params["realm"] != "ims.example.com" {
		t.Errorf("realm = %q, want ims.example.com", params["realm"])
	}
	if params["nonce"] == "" {
		t.Error("expected non-empty nonce")
	}
	if params["algorithm"] != "AKAv1-MD5" {
		t.Errorf("algorithm = %q, want AKAv1-MD5", params["algorithm"])
	}
	if params["opaque"] != opaque {
		t.Errorf("opaque mismatch: header %q vs returned %q", params["opaque"], opaque)
	}
	if !m.HasPendingChallenge("alice@example.com") {
		t.Error("expected pending challenge after BuildChallenge")
	}
}

func TestBuildChallenge_UnknownSubscriber(t *testing.T) {
	m := NewAuthModule()
	_, _, err := m.BuildChallenge("nobody@example.com", "ims.example.com")
	if !errors.Is(err, ErrUnknownSubscriber) {
		t.Errorf("expected ErrUnknownSubscriber, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Verify
// ---------------------------------------------------------------------------

func TestVerify_AuthOK(t *testing.T) {
	m := NewAuthModule()
	seedVector(t, m)

	_, opaque, err := m.BuildChallenge("alice@example.com", "ims.example.com")
	if err != nil {
		t.Fatalf("BuildChallenge error: %v", err)
	}
	resp := hex.EncodeToString(seededVector().XRES)
	authBody := "Digest username=\"alice@example.com\", realm=\"ims.example.com\"" +
		", nonce=\"x\", uri=\"sip:example.com\", response=\"" + resp + "\"" +
		", algorithm=AKAv1-MD5, opaque=\"" + opaque + "\""
	msg := mustParseMsg(t, makeAuthRegister(2, authBody))

	status, err := m.Verify(msg)
	if err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if status != AuthOK {
		t.Errorf("expected AuthOK, got %s", status)
	}
	if m.HasPendingChallenge("alice@example.com") {
		t.Error("expected pending cleared after AuthOK")
	}
}

func TestVerify_AuthFailed_WrongResponse(t *testing.T) {
	m := NewAuthModuleWithConfig(Config{
		Realm:           "ims.example.com",
		CacheTTL:        5 * time.Minute,
		MaxAttempts:     3,
		ChallengeExpiry: 5 * time.Minute,
	})
	seedVector(t, m)

	_, opaque, err := m.BuildChallenge("alice@example.com", "ims.example.com")
	if err != nil {
		t.Fatalf("BuildChallenge error: %v", err)
	}
	// Wrong response.
	authBody := "Digest username=\"alice@example.com\", realm=\"ims.example.com\"" +
		", nonce=\"x\", uri=\"sip:example.com\", response=\"00000000000000000000000000000000\"" +
		", algorithm=AKAv1-MD5, opaque=\"" + opaque + "\""
	msg := mustParseMsg(t, makeAuthRegister(2, authBody))

	status, _ := m.Verify(msg)
	if status != AuthFailed {
		t.Errorf("expected AuthFailed, got %s", status)
	}
	// Pending must be restored so the caller can retry.
	if !m.HasPendingChallenge("alice@example.com") {
		t.Error("expected pending restored after wrong response (attempts < max)")
	}
}

func TestVerify_AuthFailed_OpaqueMismatch(t *testing.T) {
	m := NewAuthModule()
	seedVector(t, m)

	_, _, err := m.BuildChallenge("alice@example.com", "ims.example.com")
	if err != nil {
		t.Fatalf("BuildChallenge error: %v", err)
	}
	resp := hex.EncodeToString(seededVector().XRES)
	// Wrong opaque.
	authBody := "Digest username=\"alice@example.com\", realm=\"ims.example.com\"" +
		", nonce=\"x\", uri=\"sip:example.com\", response=\"" + resp + "\"" +
		", algorithm=AKAv1-MD5, opaque=\"wrong-opaque\""
	msg := mustParseMsg(t, makeAuthRegister(2, authBody))

	status, _ := m.Verify(msg)
	if status != AuthFailed {
		t.Errorf("expected AuthFailed, got %s", status)
	}
	// Pending restored so caller can retry with correct opaque.
	if !m.HasPendingChallenge("alice@example.com") {
		t.Error("expected pending restored after opaque mismatch")
	}
}

func TestVerify_AuthNoPending(t *testing.T) {
	m := NewAuthModule()
	resp := hex.EncodeToString(seededVector().XRES)
	authBody := "Digest username=\"alice@example.com\", realm=\"ims.example.com\"" +
		", nonce=\"x\", uri=\"sip:example.com\", response=\"" + resp + "\"" +
		", algorithm=AKAv1-MD5, opaque=\"x\""
	msg := mustParseMsg(t, makeAuthRegister(2, authBody))

	status, err := m.Verify(msg)
	if err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if status != AuthNoPending {
		t.Errorf("expected AuthNoPending, got %s", status)
	}
}

func TestVerify_AuthMalformed_NilMessage(t *testing.T) {
	m := NewAuthModule()
	status, err := m.Verify(nil)
	if status != AuthMalformed {
		t.Errorf("expected AuthMalformed, got %s", status)
	}
	if err == nil {
		t.Error("expected error for nil message")
	}
}

func TestVerify_AuthMalformed_NoAuthorization(t *testing.T) {
	m := NewAuthModule()
	msg := mustParseMsg(t, makeRegister(1))
	status, err := m.Verify(msg)
	if status != AuthMalformed {
		t.Errorf("expected AuthMalformed, got %s", status)
	}
	if err == nil {
		t.Error("expected error for missing Authorization")
	}
}

func TestVerify_AuthMalformed_NoUsername(t *testing.T) {
	m := NewAuthModule()
	// Authorization without username.
	authBody := "Digest realm=\"ims.example.com\", nonce=\"x\", uri=\"sip:example.com\"" +
		", response=\"x\", algorithm=AKAv1-MD5, opaque=\"x\""
	msg := mustParseMsg(t, makeAuthRegister(2, authBody))
	status, err := m.Verify(msg)
	if status != AuthMalformed {
		t.Errorf("expected AuthMalformed, got %s", status)
	}
	if err == nil {
		t.Error("expected error for missing username")
	}
}

func TestVerify_MaxAttempts_Lockout(t *testing.T) {
	m := NewAuthModuleWithConfig(Config{
		Realm:           "ims.example.com",
		CacheTTL:        5 * time.Minute,
		MaxAttempts:     3,
		ChallengeExpiry: 5 * time.Minute,
	})
	seedVector(t, m)

	_, opaque, err := m.BuildChallenge("alice@example.com", "ims.example.com")
	if err != nil {
		t.Fatalf("BuildChallenge error: %v", err)
	}
	authBody := "Digest username=\"alice@example.com\", realm=\"ims.example.com\"" +
		", nonce=\"x\", uri=\"sip:example.com\", response=\"00000000000000000000000000000000\"" +
		", algorithm=AKAv1-MD5, opaque=\"" + opaque + "\""
	msg := mustParseMsg(t, makeAuthRegister(2, authBody))

	// Attempt 1: attempts=1, 1 < 3 -> AuthFailed, pending restored.
	s1, _ := m.Verify(msg)
	if s1 != AuthFailed {
		t.Fatalf("attempt 1: expected AuthFailed, got %s", s1)
	}
	if !m.HasPendingChallenge("alice@example.com") {
		t.Fatal("attempt 1: expected pending restored")
	}
	// Attempt 2: attempts=2, 2 < 3 -> AuthFailed, pending restored.
	s2, _ := m.Verify(msg)
	if s2 != AuthFailed {
		t.Fatalf("attempt 2: expected AuthFailed, got %s", s2)
	}
	if !m.HasPendingChallenge("alice@example.com") {
		t.Fatal("attempt 2: expected pending restored")
	}
	// Attempt 3: attempts=3, 3 >= 3 -> AuthFailed, pending DELETED.
	s3, _ := m.Verify(msg)
	if s3 != AuthFailed {
		t.Fatalf("attempt 3: expected AuthFailed, got %s", s3)
	}
	if m.HasPendingChallenge("alice@example.com") {
		t.Fatal("attempt 3: expected pending cleared after lockout")
	}
	// Subsequent call: AuthNoPending.
	s4, _ := m.Verify(msg)
	if s4 != AuthNoPending {
		t.Errorf("after lockout: expected AuthNoPending, got %s", s4)
	}
}

// ---------------------------------------------------------------------------
// Authenticate
// ---------------------------------------------------------------------------

func TestAuthenticate_NoAuthorization_Challenge(t *testing.T) {
	m := NewAuthModule()
	seedVector(t, m)
	msg := mustParseMsg(t, makeRegister(1))

	r, err := m.Authenticate(msg)
	if err != nil {
		t.Fatalf("Authenticate error: %v", err)
	}
	if r.StatusCode != 401 {
		t.Errorf("expected 401, got %d", r.StatusCode)
	}
	if r.Headers["WWW-Authenticate"].String() == "" {
		t.Error("expected WWW-Authenticate header")
	}
	if m.PendingCount() != 1 {
		t.Errorf("expected 1 pending, got %d", m.PendingCount())
	}
}

func TestAuthenticate_UnknownSubscriber_Forbidden(t *testing.T) {
	m := NewAuthModule()
	// A REGISTER whose To URI yields an IMPI unknown to the HSS.
	raw := []byte("REGISTER sip:example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776nopi\r\n" +
		"From: <sip:example.com>\r\n" +
		"To: <sip:example.com>\r\n" +
		"Call-ID: c@10.0.0.1\r\n" +
		"CSeq: 1 REGISTER\r\n" +
		"Contact: <sip:10.0.0.1:5060>\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
	msg := mustParseMsg(t, raw)
	r, err := m.Authenticate(msg)
	if r == nil || r.StatusCode != 403 {
		t.Errorf("expected 403, got %+v", r)
	}
	if err == nil {
		t.Error("expected error for unknown subscriber")
	}
}

func TestAuthenticate_FullFlow_OK(t *testing.T) {
	m := NewAuthModule()
	runFullAuthFlow(t, m)
}

func TestAuthenticate_WrongResponse_Forbidden_PendingRestored(t *testing.T) {
	m := NewAuthModuleWithConfig(Config{
		Realm:           "ims.example.com",
		CacheTTL:        5 * time.Minute,
		MaxAttempts:     3,
		ChallengeExpiry: 5 * time.Minute,
	})
	seedVector(t, m)

	// Step 1: challenge.
	msg1 := mustParseMsg(t, makeRegister(1))
	r1, _ := m.Authenticate(msg1)
	if r1.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", r1.StatusCode)
	}
	params := parseAuthParams(r1.Headers["WWW-Authenticate"].String())
	opaque := params["opaque"]
	nonce := params["nonce"]

	// Step 2: wrong response -> 403, pending restored.
	authBody := "Digest username=\"alice@example.com\", realm=\"ims.example.com\"" +
		", nonce=\"" + nonce + "\", uri=\"sip:example.com\", response=\"00000000000000000000000000000000\"" +
		", algorithm=AKAv1-MD5, opaque=\"" + opaque + "\""
	msg2 := mustParseMsg(t, makeAuthRegister(2, authBody))
	r2, _ := m.Authenticate(msg2)
	if r2.StatusCode != 403 {
		t.Errorf("expected 403 on wrong response, got %d", r2.StatusCode)
	}
	if !m.HasPendingChallenge("alice@example.com") {
		t.Error("expected pending restored after wrong response")
	}

	// Step 3: correct response -> 200.
	resp := hex.EncodeToString(seededVector().XRES)
	authBodyOK := "Digest username=\"alice@example.com\", realm=\"ims.example.com\"" +
		", nonce=\"" + nonce + "\", uri=\"sip:example.com\", response=\"" + resp + "\"" +
		", algorithm=AKAv1-MD5, opaque=\"" + opaque + "\""
	msg3 := mustParseMsg(t, makeAuthRegister(3, authBodyOK))
	r3, err := m.Authenticate(msg3)
	if err != nil {
		t.Fatalf("Authenticate #3 error: %v", err)
	}
	if r3.StatusCode != 200 {
		t.Errorf("expected 200 after correct response, got %d", r3.StatusCode)
	}
	if !r3.Authenticated {
		t.Error("expected Authenticated=true")
	}
}

func TestAuthenticate_MaxAttempts_Forbidden_PendingCleared(t *testing.T) {
	m := NewAuthModuleWithConfig(Config{
		Realm:           "ims.example.com",
		CacheTTL:        5 * time.Minute,
		MaxAttempts:     2,
		ChallengeExpiry: 5 * time.Minute,
	})
	seedVector(t, m)

	// Challenge.
	msg1 := mustParseMsg(t, makeRegister(1))
	r1, _ := m.Authenticate(msg1)
	params := parseAuthParams(r1.Headers["WWW-Authenticate"].String())
	opaque := params["opaque"]
	nonce := params["nonce"]

	authBody := "Digest username=\"alice@example.com\", realm=\"ims.example.com\"" +
		", nonce=\"" + nonce + "\", uri=\"sip:example.com\", response=\"00000000000000000000000000000000\"" +
		", algorithm=AKAv1-MD5, opaque=\"" + opaque + "\""
	msg := mustParseMsg(t, makeAuthRegister(2, authBody))

	// Attempt 1: 403, pending restored.
	r2, _ := m.Authenticate(msg)
	if r2.StatusCode != 403 {
		t.Errorf("attempt 1: expected 403, got %d", r2.StatusCode)
	}
	if !m.HasPendingChallenge("alice@example.com") {
		t.Error("attempt 1: expected pending restored")
	}
	// Attempt 2: 403, pending DELETED (attempts >= MaxAttempts).
	r3, _ := m.Authenticate(msg)
	if r3.StatusCode != 403 {
		t.Errorf("attempt 2: expected 403, got %d", r3.StatusCode)
	}
	if m.HasPendingChallenge("alice@example.com") {
		t.Error("attempt 2: expected pending cleared after lockout")
	}
}

func TestAuthenticate_NoPending_Rechallenge(t *testing.T) {
	m := NewAuthModule()
	seedVector(t, m)

	// Build an Authorization-carrying REGISTER WITHOUT a prior challenge.
	resp := hex.EncodeToString(seededVector().XRES)
	authBody := "Digest username=\"alice@example.com\", realm=\"ims.example.com\"" +
		", nonce=\"x\", uri=\"sip:example.com\", response=\"" + resp + "\"" +
		", algorithm=AKAv1-MD5, opaque=\"x\""
	msg := mustParseMsg(t, makeAuthRegister(1, authBody))

	r, err := m.Authenticate(msg)
	if err != nil {
		t.Fatalf("Authenticate error: %v", err)
	}
	// AuthNoPending path -> re-challenge -> 401.
	if r.StatusCode != 401 {
		t.Errorf("expected 401 re-challenge, got %d", r.StatusCode)
	}
	if r.Headers["WWW-Authenticate"].String() == "" {
		t.Error("expected WWW-Authenticate on re-challenge")
	}
	if !m.HasPendingChallenge("alice@example.com") {
		t.Error("expected pending stored after re-challenge")
	}
}

func TestAuthenticate_NilMessage(t *testing.T) {
	m := NewAuthModule()
	r, err := m.Authenticate(nil)
	if r != nil {
		t.Errorf("expected nil result, got %+v", r)
	}
	if err == nil {
		t.Error("expected error for nil message")
	}
}

func TestAuthenticate_MalformedAuthorization_BadRequest(t *testing.T) {
	m := NewAuthModule()
	// Authorization that is not a Digest.
	authBody := "Basic dXNlcjpwYXNz"
	msg := mustParseMsg(t, makeAuthRegister(1, authBody))
	r, err := m.Authenticate(msg)
	if r == nil {
		t.Fatal("expected non-nil result")
	}
	if r.StatusCode != 400 {
		t.Errorf("expected 400, got %d", r.StatusCode)
	}
	if err == nil {
		t.Error("expected error for malformed Authorization")
	}
}

// ---------------------------------------------------------------------------
// IsAuthorised
// ---------------------------------------------------------------------------

func TestIsAuthorised(t *testing.T) {
	m := NewAuthModule()
	if m.IsAuthorised(nil) {
		t.Error("nil message should not be authorised")
	}
	msgNoAuth := mustParseMsg(t, makeRegister(1))
	if m.IsAuthorised(msgNoAuth) {
		t.Error("message without Authorization should not be authorised")
	}
	msgAuth := mustParseMsg(t, makeAuthRegister(2,
		"Digest username=\"alice@example.com\", realm=\"x\""))
	if !m.IsAuthorised(msgAuth) {
		t.Error("message with Digest Authorization should be authorised")
	}
	msgNonDigest := mustParseMsg(t, makeAuthRegister(3, "Basic dXNlcjpwYXNz"))
	if m.IsAuthorised(msgNonDigest) {
		t.Error("non-Digest Authorization should not be authorised")
	}
}

// ---------------------------------------------------------------------------
// HasPendingChallenge / PendingCount
// ---------------------------------------------------------------------------

func TestHasPendingChallenge_And_PendingCount(t *testing.T) {
	m := NewAuthModule()
	seedVector(t, m)
	if m.PendingCount() != 0 {
		t.Errorf("expected 0 pending, got %d", m.PendingCount())
	}
	if m.HasPendingChallenge("alice@example.com") {
		t.Error("expected no pending challenge before BuildChallenge")
	}
	_, _, err := m.BuildChallenge("alice@example.com", "ims.example.com")
	if err != nil {
		t.Fatalf("BuildChallenge error: %v", err)
	}
	if !m.HasPendingChallenge("alice@example.com") {
		t.Error("expected pending challenge after BuildChallenge")
	}
	if m.PendingCount() != 1 {
		t.Errorf("expected 1 pending, got %d", m.PendingCount())
	}
}

// ---------------------------------------------------------------------------
// CacheVector / ClearCache / CacheSize / CleanupExpired
// ---------------------------------------------------------------------------

func TestCacheVector_And_CacheSize(t *testing.T) {
	m := NewAuthModule()
	if m.CacheSize() != 0 {
		t.Errorf("expected 0 cache size, got %d", m.CacheSize())
	}
	m.CacheVector("alice@example.com", seededVector())
	if m.CacheSize() != 1 {
		t.Errorf("expected 1 cache entry, got %d", m.CacheSize())
	}
	// Cached vector should be served without HSS.
	m.InMemoryClient().SetAuthVector("alice@example.com", &auth.AuthVector{
		XRES: bytesFromHex("ff"),
	})
	av, err := m.GetAuthVector("alice@example.com", "ims.example.com")
	if err != nil {
		t.Fatalf("GetAuthVector error: %v", err)
	}
	if !bytes.Equal(av.XRES, seededVector().XRES) {
		t.Error("expected cached vector, got HSS value")
	}
}

func TestClearCache(t *testing.T) {
	m := NewAuthModule()
	m.CacheVector("alice@example.com", seededVector())
	m.CacheVector("bob@example.com", seededVector())
	if m.CacheSize() != 2 {
		t.Fatalf("expected 2 entries, got %d", m.CacheSize())
	}
	m.ClearCache()
	if m.CacheSize() != 0 {
		t.Errorf("expected 0 after clear, got %d", m.CacheSize())
	}
}

func TestCleanupExpired_Cache(t *testing.T) {
	m := NewAuthModuleWithConfig(Config{
		Realm:           "ims.example.com",
		CacheTTL:        20 * time.Millisecond,
		MaxAttempts:     3,
		ChallengeExpiry: 5 * time.Minute,
	})
	seedVector(t, m)
	if _, err := m.GetAuthVector("alice@example.com", "ims.example.com"); err != nil {
		t.Fatalf("GetAuthVector error: %v", err)
	}
	if m.CacheSize() != 1 {
		t.Fatalf("expected 1 cache entry, got %d", m.CacheSize())
	}
	time.Sleep(40 * time.Millisecond)
	removed := m.CleanupExpired()
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}
	if m.CacheSize() != 0 {
		t.Errorf("expected 0 cache entries after cleanup, got %d", m.CacheSize())
	}
}

func TestCleanupExpired_Pending(t *testing.T) {
	m := NewAuthModuleWithConfig(Config{
		Realm:           "ims.example.com",
		CacheTTL:        5 * time.Minute,
		MaxAttempts:     3,
		ChallengeExpiry: 20 * time.Millisecond,
	})
	seedVector(t, m)
	if _, _, err := m.BuildChallenge("alice@example.com", "ims.example.com"); err != nil {
		t.Fatalf("BuildChallenge error: %v", err)
	}
	if !m.HasPendingChallenge("alice@example.com") {
		t.Fatal("expected pending challenge")
	}
	time.Sleep(40 * time.Millisecond)
	removed := m.CleanupExpired()
	if removed < 1 {
		t.Errorf("expected >=1 removed, got %d", removed)
	}
	if m.HasPendingChallenge("alice@example.com") {
		t.Error("expected stale pending challenge removed")
	}
}

// ---------------------------------------------------------------------------
// Configuration / singletons
// ---------------------------------------------------------------------------

func TestSetRealm_And_Realm(t *testing.T) {
	m := NewAuthModule()
	if m.Realm() != "ims.example.com" {
		t.Errorf("default realm = %q, want ims.example.com", m.Realm())
	}
	m.SetRealm("ims.other.com")
	if m.Realm() != "ims.other.com" {
		t.Errorf("realm = %q, want ims.other.com", m.Realm())
	}
}

func TestSetAuthClient_And_InMemoryClient(t *testing.T) {
	m := NewAuthModule()
	if m.InMemoryClient() == nil {
		t.Fatal("expected non-nil InMemoryClient by default")
	}
	m.SetAuthClient(nil)
	if m.InMemoryClient() == nil {
		t.Error("expected SetAuthClient(nil) to fall back to in-memory client")
	}
	c := NewInMemoryAuthClient()
	m.SetAuthClient(c)
	if m.InMemoryClient() != c {
		t.Error("expected injected client returned by InMemoryClient")
	}
}

func TestDefaultAuth_Singleton(t *testing.T) {
	a := DefaultAuth()
	b := DefaultAuth()
	if a != b {
		t.Error("DefaultAuth must return the same instance")
	}
}

func TestInit_Reset(t *testing.T) {
	a := DefaultAuth()
	a.CacheVector("alice@example.com", seededVector())
	if a.CacheSize() != 1 {
		t.Fatalf("expected 1 cache entry, got %d", a.CacheSize())
	}
	Init()
	b := DefaultAuth()
	if b.CacheSize() != 0 {
		t.Errorf("expected 0 cache entries after Init, got %d", b.CacheSize())
	}
}

// ---------------------------------------------------------------------------
// AuthStatus.String
// ---------------------------------------------------------------------------

func TestAuthStatus_String(t *testing.T) {
	cases := []struct {
		status AuthStatus
		want   string
	}{
		{AuthOK, "ok"},
		{AuthFailed, "failed"},
		{AuthNoPending, "no-pending"},
		{AuthMalformed, "malformed"},
	}
	for _, c := range cases {
		if got := c.status.String(); got != c.want {
			t.Errorf("%d: String() = %q, want %q", int(c.status), got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// deriveIMPI / extractAuthUsername
// ---------------------------------------------------------------------------

func TestDeriveIMPI(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"sip:alice@example.com", "alice@example.com"},
		{"sips:alice@example.com", "alice@example.com"},
		{"tel:+1234", "+1234"},
		{"alice@example.com", "alice@example.com"},
		{"  sip:alice@example.com  ", "alice@example.com"},
	}
	for _, c := range cases {
		if got := deriveIMPI(c.in); got != c.want {
			t.Errorf("deriveIMPI(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExtractAuthUsername(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`Digest username="alice@example.com", realm="x"`, "alice@example.com"},
		{`Digest username=alice, realm="x"`, "alice"},
		{`Digest realm="x"`, ""},
		{``, ""},
		{`digest USERNAME="Bob"`, "Bob"},
	}
	for _, c := range cases {
		if got := extractAuthUsername(c.in); got != c.want {
			t.Errorf("extractAuthUsername(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Concurrent access
// ---------------------------------------------------------------------------

func TestConcurrentAccess(t *testing.T) {
	m := NewAuthModule()
	seedVector(t, m)

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			impi := "alice@example.com"
			// Concurrent reads.
			_ = m.HasPendingChallenge(impi)
			_ = m.PendingCount()
			_ = m.CacheSize()
			// Concurrent cache writes.
			m.CacheVector(impi, seededVector())
			// Concurrent GetAuthVector.
			_, _ = m.GetAuthVector(impi, "ims.example.com")
		}(i)
	}
	wg.Wait()
	// Mixed challenge/verify under concurrency.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			wwwAuth, opaque, err := m.BuildChallenge("alice@example.com", "ims.example.com")
			if err != nil {
				return
			}
			params := parseAuthParams(wwwAuth.String())
			resp := hex.EncodeToString(seededVector().XRES)
			authBody := "Digest username=\"alice@example.com\", realm=\"ims.example.com\"" +
				", nonce=\"" + params["nonce"] + "\", uri=\"sip:example.com\", response=\"" + resp + "\"" +
				", algorithm=AKAv1-MD5, opaque=\"" + opaque + "\""
			msg := mustParseMsg(t, makeAuthRegister(id+10, authBody))
			_, _ = m.Authenticate(msg)
		}(i)
	}
	wg.Wait()
	m.CleanupExpired()
}
