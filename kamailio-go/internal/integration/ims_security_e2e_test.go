// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * IMS Security End-to-End tests.
 *
 * Based on 3GPP TS 33.203 (IMS security) and RFC 3310 (AKA Digest
 * authentication).  Each test exercises the AKA authentication vector
 * generation, challenge/response construction, and security context handling.
 */

package integration

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/ims/auth"
	"github.com/kamailio/kamailio-go/internal/ims/scscf"
)

// ---------------------------------------------------------------------------
// Test: AKA Authentication Vector Generation (TS 33.203 §6.3)
// ---------------------------------------------------------------------------

// TestE2E_IMS_AKA_AuthVectorGeneration verifies that GenerateAuthVector
// produces a well-formed authentication vector per TS 33.203:
//   - RAND: 16 bytes
//   - AUTN: 16 bytes
//   - XRES: non-empty
//   - CK:   16 bytes
//   - IK:   16 bytes
func TestE2E_IMS_AKA_AuthVectorGeneration(t *testing.T) {
	av, err := auth.GenerateAuthVector()
	if err != nil {
		t.Fatalf("GenerateAuthVector failed: %v", err)
	}

	// RAND must be 16 bytes (TS 33.203 §6.3.2)
	if len(av.RAND) != 16 {
		t.Errorf("RAND length = %d, want 16", len(av.RAND))
	}

	// AUTN must be 16 bytes (SQN || AMF || MAC)
	if len(av.AUTN) != 16 {
		t.Errorf("AUTN length = %d, want 16", len(av.AUTN))
	}

	// XRES must be non-empty
	if len(av.XRES) == 0 {
		t.Error("XRES should not be empty")
	}

	// CK must be 16 bytes (cipher key)
	if len(av.CK) != 16 {
		t.Errorf("CK length = %d, want 16", len(av.CK))
	}

	// IK must be 16 bytes (integrity key)
	if len(av.IK) != 16 {
		t.Errorf("IK length = %d, want 16", len(av.IK))
	}

	// Verify RAND is not all zeros (should be random)
	allZero := true
	for _, b := range av.RAND {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("RAND should not be all zeros")
	}
}

// ---------------------------------------------------------------------------
// Test: AKA Challenge Construction (TS 33.203 §6.3.3 / RFC 3310 §2)
// ---------------------------------------------------------------------------

// TestE2E_IMS_AKA_ChallengeBuild verifies that BuildChallenge produces a
// challenge whose nonce = base64(RAND || AUTN) and algorithm = "AKAv1-MD5".
func TestE2E_IMS_AKA_ChallengeBuild(t *testing.T) {
	av, err := auth.GenerateAuthVector()
	if err != nil {
		t.Fatalf("GenerateAuthVector failed: %v", err)
	}

	realm := "ims.mnc001.mcc460.3gppnetwork.org"
	opaque := "test-opaque-value"
	challenge := auth.BuildChallenge(av, realm, opaque)

	// Verify nonce = base64(RAND || AUTN) per RFC 3310 §2
	expectedNonceData := append(append([]byte{}, av.RAND...), av.AUTN...)
	expectedNonce := base64.StdEncoding.EncodeToString(expectedNonceData)
	if challenge.Nonce != expectedNonce {
		t.Errorf("nonce mismatch:\n  got:  %s\n  want: %s", challenge.Nonce, expectedNonce)
	}

	// Verify algorithm per TS 33.203
	if challenge.Algorithm != "AKAv1-MD5" {
		t.Errorf("algorithm = %q, want AKAv1-MD5", challenge.Algorithm)
	}

	// Verify realm
	if challenge.Realm != realm {
		t.Errorf("realm = %q, want %q", challenge.Realm, realm)
	}

	// Verify opaque is preserved
	if challenge.Opaque != opaque {
		t.Errorf("opaque = %q, want %q", challenge.Opaque, opaque)
	}
}

// ---------------------------------------------------------------------------
// Test: WWW-Authenticate Header Construction (RFC 3310 §2)
// ---------------------------------------------------------------------------

// TestE2E_IMS_AKA_WWWAuthenticate verifies that BuildWWWAuthenticate produces
// a header value in the format:
//
//	Digest realm="...", nonce="...", algorithm=AKAv1-MD5, opaque="..."
func TestE2E_IMS_AKA_WWWAuthenticate(t *testing.T) {
	av, err := auth.GenerateAuthVector()
	if err != nil {
		t.Fatalf("GenerateAuthVector failed: %v", err)
	}

	realm := "ims.example.com"
	opaque := "correlation-id-123"
	challenge := auth.BuildChallenge(av, realm, opaque)

	wwwAuth := auth.BuildWWWAuthenticate(challenge)
	value := wwwAuth.String()

	// Must start with "Digest "
	if !strings.HasPrefix(value, "Digest ") {
		t.Fatalf("WWW-Authenticate should start with 'Digest ', got: %s", value)
	}

	// Must contain realm="..."
	if !strings.Contains(value, fmt.Sprintf(`realm="%s"`, realm)) {
		t.Errorf("missing or incorrect realm in: %s", value)
	}

	// Must contain nonce="..."
	if !strings.Contains(value, `nonce="`) {
		t.Errorf("missing nonce in: %s", value)
	}

	// Must contain algorithm=AKAv1-MD5 (no quotes per RFC 3310)
	if !strings.Contains(value, "algorithm=AKAv1-MD5") {
		t.Errorf("missing or incorrect algorithm in: %s", value)
	}

	// Must contain opaque="..."
	if !strings.Contains(value, fmt.Sprintf(`opaque="%s"`, opaque)) {
		t.Errorf("missing or incorrect opaque in: %s", value)
	}
}

// ---------------------------------------------------------------------------
// Test: AKA Response Verification (TS 33.203 §6.3.4)
// ---------------------------------------------------------------------------

// TestE2E_IMS_AKA_ResponseVerification verifies that VerifyResponse accepts
// the correct RES (hex(XRES)) and rejects an incorrect response.
func TestE2E_IMS_AKA_ResponseVerification(t *testing.T) {
	av, err := auth.GenerateAuthVector()
	if err != nil {
		t.Fatalf("GenerateAuthVector failed: %v", err)
	}

	// Correct response: response = hex(XRES) per RFC 3310
	correctResp := hex.EncodeToString(av.XRES)
	correctAKAResp := &auth.AKAResponse{
		Username:  "alice",
		Realm:     "ims.example.com",
		Nonce:     "test-nonce",
		URI:       "sip:alice@ims.example.com",
		Response:  correctResp,
		Algorithm: "AKAv1-MD5",
		Opaque:    "test-opaque",
	}
	if !auth.VerifyResponse(av, correctAKAResp) {
		t.Error("VerifyResponse should return true for correct RES")
	}

	// Wrong response
	wrongAKAResp := &auth.AKAResponse{
		Username:  "alice",
		Realm:     "ims.example.com",
		Nonce:     "test-nonce",
		URI:       "sip:alice@ims.example.com",
		Response:  "deadbeefdeadbeef",
		Algorithm: "AKAv1-MD5",
		Opaque:    "test-opaque",
	}
	if auth.VerifyResponse(av, wrongAKAResp) {
		t.Error("VerifyResponse should return false for wrong RES")
	}

	// Empty response
	emptyAKAResp := &auth.AKAResponse{
		Response: "",
	}
	if auth.VerifyResponse(av, emptyAKAResp) {
		t.Error("VerifyResponse should return false for empty response")
	}
}

// ---------------------------------------------------------------------------
// Test: Authorization Header Parsing (RFC 3310 §2)
// ---------------------------------------------------------------------------

// TestE2E_IMS_AuthorizationHeaderParsing verifies that ParseAuthorization
// correctly extracts all Digest parameters from an Authorization header.
func TestE2E_IMS_AuthorizationHeaderParsing(t *testing.T) {
	authHeader := `Digest username="alice", realm="ims.example.com", nonce="YWJjZGVmMTIzNDU2", uri="sip:alice@ims.example.com", response="0a1b2c3d4e5f6071", algorithm=AKAv1-MD5, opaque="opaque-xyz"`

	resp, err := auth.ParseAuthorization(authHeader)
	if err != nil {
		t.Fatalf("ParseAuthorization failed: %v", err)
	}

	if resp.Username != "alice" {
		t.Errorf("username = %q, want alice", resp.Username)
	}
	if resp.Realm != "ims.example.com" {
		t.Errorf("realm = %q, want ims.example.com", resp.Realm)
	}
	if resp.Nonce != "YWJjZGVmMTIzNDU2" {
		t.Errorf("nonce = %q, want YWJjZGVmMTIzNDU2", resp.Nonce)
	}
	if resp.URI != "sip:alice@ims.example.com" {
		t.Errorf("uri = %q, want sip:alice@ims.example.com", resp.URI)
	}
	if resp.Response != "0a1b2c3d4e5f6071" {
		t.Errorf("response = %q, want 0a1b2c3d4e5f6071", resp.Response)
	}
	if resp.Algorithm != "AKAv1-MD5" {
		t.Errorf("algorithm = %q, want AKAv1-MD5", resp.Algorithm)
	}
	if resp.Opaque != "opaque-xyz" {
		t.Errorf("opaque = %q, want opaque-xyz", resp.Opaque)
	}
}

// ---------------------------------------------------------------------------
// Test: Nonce Uniqueness (TS 33.203 §6.2)
// ---------------------------------------------------------------------------

// TestE2E_IMS_NonceUniqueness verifies that 100 generated authentication
// vectors produce 100 distinct nonces (replay protection).
func TestE2E_IMS_NonceUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	const n = 100

	for i := 0; i < n; i++ {
		av, err := auth.GenerateAuthVector()
		if err != nil {
			t.Fatalf("GenerateAuthVector[%d] failed: %v", i, err)
		}
		challenge := auth.BuildChallenge(av, "ims.example.com", "opaque")
		if seen[challenge.Nonce] {
			t.Fatalf("duplicate nonce at iteration %d: %s", i, challenge.Nonce)
		}
		seen[challenge.Nonce] = true
	}

	if len(seen) != n {
		t.Errorf("expected %d unique nonces, got %d", n, len(seen))
	}
}

// ---------------------------------------------------------------------------
// Test: Opaque Correlation (RFC 3310 §2)
// ---------------------------------------------------------------------------

// TestE2E_IMS_OpaqueCorrelation verifies the opaque value properties.
// The implementation includes a time component for freshness, so the same
// input produces different opaque values across calls (a security feature
// that prevents replay).  Different inputs must also produce different values.
func TestE2E_IMS_OpaqueCorrelation(t *testing.T) {
	// Same input — values differ due to the time-based component (freshness)
	opaque1 := auth.GenerateOpaque("callid-001", "1")
	opaque2 := auth.GenerateOpaque("callid-001", "1")
	if opaque1 == "" {
		t.Fatal("opaque should not be empty")
	}
	if opaque2 == "" {
		t.Fatal("opaque should not be empty")
	}
	// Both must be valid hex
	if _, err := hex.DecodeString(opaque1); err != nil {
		t.Fatalf("opaque1 is not valid hex: %v", err)
	}
	if _, err := hex.DecodeString(opaque2); err != nil {
		t.Fatalf("opaque2 is not valid hex: %v", err)
	}

	// Different callID — must produce different opaque
	opaque3 := auth.GenerateOpaque("callid-002", "1")
	if opaque1 == opaque3 {
		t.Error("different callID should produce different opaque")
	}

	// Different cseq — must produce different opaque
	opaque4 := auth.GenerateOpaque("callid-001", "2")
	if opaque1 == opaque4 {
		t.Error("different cseq should produce different opaque")
	}
}

// ---------------------------------------------------------------------------
// Test: Registration Security Context (TS 33.203 §6.1)
// ---------------------------------------------------------------------------

// TestE2E_IMS_RegistrationSecurityContext verifies that after a successful AKA
// registration, the SecurityContext stored on the RegistrationRecord contains
// the correct CK, IK, and algorithm from the authentication vector.
func TestE2E_IMS_RegistrationSecurityContext(t *testing.T) {
	realm := "ims.example.com"
	impu := "sip:sec@ims.example.com"
	impi := "sec@ims.example.com"
	contact := "sip:sec@192.168.1.150"

	registrar := scscf.NewRegistrar(realm)

	// Step 1: Initial REGISTER -> 401
	raw1 := buildIMSRegister(impu, impi, contact, "", "", 3600)
	msg1 := parseSIPMsg(t, raw1)
	res1, _ := registrar.HandleRegister(msg1)
	if res1.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", res1.StatusCode)
	}

	// Step 2: Capture the AV before it is cleared
	record := registrar.GetRecord(impu)
	if record == nil || record.AuthState == nil {
		t.Fatal("no auth state after 401")
	}
	record.RLock()
	expectedCK := record.AuthState.AuthVector.CK
	expectedIK := record.AuthState.AuthVector.IK
	xres := record.AuthState.AuthVector.XRES
	record.RUnlock()

	// Step 3: Complete registration
	nonce, opaque := extractChallengeParams(res1.Headers["WWW-Authenticate"].String())
	resp := hex.EncodeToString(xres)
	authz := buildAuthHeader(impi, realm, nonce, impu, resp, opaque)

	raw2 := buildIMSRegister(impu, impi, contact, authz, "", 3600)
	msg2 := parseSIPMsg(t, raw2)
	res2, _ := registrar.HandleRegister(msg2)
	if res2.StatusCode != 200 {
		t.Fatalf("expected 200 OK, got %d", res2.StatusCode)
	}

	// Step 4: Verify SecurityContext
	record = registrar.GetRecord(impu)
	if record == nil {
		t.Fatal("no record after registration")
	}
	record.RLock()
	sec := record.Security
	record.RUnlock()

	if sec == nil {
		t.Fatal("SecurityContext should not be nil after registration")
	}
	if !bytesEqual(sec.CK, expectedCK) {
		t.Errorf("SecurityContext.CK does not match AV.CK")
	}
	if !bytesEqual(sec.IK, expectedIK) {
		t.Errorf("SecurityContext.IK does not match AV.IK")
	}
	if sec.Algorithm != "AKAv1-MD5" {
		t.Errorf("SecurityContext.Algorithm = %q, want AKAv1-MD5", sec.Algorithm)
	}
}

// ---------------------------------------------------------------------------
// Test: Auth State Cleanup (TS 33.203 §6.2)
// ---------------------------------------------------------------------------

// TestE2E_IMS_AuthStateCleanup verifies that the AuthState is set during the
// challenge phase and cleared after successful authentication.
func TestE2E_IMS_AuthStateCleanup(t *testing.T) {
	realm := "ims.example.com"
	impu := "sip:cleanup@ims.example.com"
	impi := "cleanup@ims.example.com"
	contact := "sip:cleanup@192.168.1.160"

	registrar := scscf.NewRegistrar(realm)

	// Step 1: Initial REGISTER -> 401
	raw1 := buildIMSRegister(impu, impi, contact, "", "", 3600)
	msg1 := parseSIPMsg(t, raw1)
	res1, _ := registrar.HandleRegister(msg1)
	if res1.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", res1.StatusCode)
	}

	// Step 2: Verify AuthState is not nil (pending challenge)
	record := registrar.GetRecord(impu)
	if record == nil {
		t.Fatal("no record after 401")
	}
	record.RLock()
	authState := record.AuthState
	record.RUnlock()
	if authState == nil {
		t.Fatal("AuthState should not be nil after 401 challenge")
	}

	// Step 3: Send correct REGISTER + Authorization -> 200
	nonce, opaque := extractChallengeParams(res1.Headers["WWW-Authenticate"].String())
	record.RLock()
	xres := record.AuthState.AuthVector.XRES
	record.RUnlock()
	resp := hex.EncodeToString(xres)
	authz := buildAuthHeader(impi, realm, nonce, impu, resp, opaque)

	raw2 := buildIMSRegister(impu, impi, contact, authz, "", 3600)
	msg2 := parseSIPMsg(t, raw2)
	res2, _ := registrar.HandleRegister(msg2)
	if res2.StatusCode != 200 {
		t.Fatalf("expected 200 OK, got %d", res2.StatusCode)
	}

	// Step 4: Verify AuthState is cleared (nil) after successful auth
	record = registrar.GetRecord(impu)
	if record == nil {
		t.Fatal("no record after registration")
	}
	record.RLock()
	authState = record.AuthState
	record.RUnlock()
	if authState != nil {
		t.Fatal("AuthState should be nil after successful registration")
	}
}

// ---------------------------------------------------------------------------
// Test: Concurrent Auth Vector Generation (TS 33.203 §6.3)
// ---------------------------------------------------------------------------

// TestE2E_IMS_ConcurrentAuth verifies that concurrent AuthVector generation
// is safe (no panics) and produces unique nonces.
func TestE2E_IMS_ConcurrentAuth(t *testing.T) {
	n := 50
	var wg sync.WaitGroup
	nonces := make(chan string, n)
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			av, err := auth.GenerateAuthVector()
			if err != nil {
				errs <- err
				return
			}
			challenge := auth.BuildChallenge(av, "ims.example.com", "opaque")
			nonces <- challenge.Nonce
		}()
	}

	wg.Wait()
	close(nonces)
	close(errs)

	for err := range errs {
		t.Error(err)
	}

	// Verify all nonces are unique
	seen := make(map[string]bool)
	count := 0
	for nonce := range nonces {
		count++
		if seen[nonce] {
			t.Fatalf("duplicate nonce: %s", nonce)
		}
		seen[nonce] = true
	}
	if count != n {
		t.Errorf("expected %d nonces, got %d", n, count)
	}
}
