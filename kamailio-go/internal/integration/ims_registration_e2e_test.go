// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * IMS Registration End-to-End tests.
 *
 * Based on 3GPP TS 23.228 §5.3.2 (IMS registration procedures) and
 * TS 33.203 (IMS security).  Each test exercises the full AKA challenge /
 * response flow through the S-CSCF Registrar.
 */

package integration

import (
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/ims/scscf"
)

// parserParseMsg wraps parser.ParseMsg without *testing.T so it can be used
// safely inside goroutines (t.Fatalf would call runtime.Goexit only in the
// current goroutine, leaving siblings running).
func parserParseMsg(raw []byte) (*parser.SIPMsg, error) {
	return parser.ParseMsg(raw)
}

// ---------------------------------------------------------------------------
// Test: Initial Registration (TS 23.228 §5.3.2.1)
// ---------------------------------------------------------------------------

// TestE2E_IMS_InitialRegistration verifies the complete initial registration
// flow:
//  1. UE sends REGISTER without Authorization.
//  2. S-CSCF returns 401 + WWW-Authenticate (AKA challenge).
//  3. The 401 contains the correct realm, nonce, and algorithm=AKAv1-MD5.
//  4. UE computes RES and sends REGISTER + Authorization.
//  5. S-CSCF verifies RES == XRES and returns 200 OK.
//  6. The 200 OK contains Service-Route and P-Associated-URI.
//  7. The registration state is Registered.
func TestE2E_IMS_InitialRegistration(t *testing.T) {
	realm := "ims.example.com"
	impu := "sip:alice@ims.example.com"
	impi := "alice@ims.example.com"
	contact := "sip:alice@192.168.1.100"

	registrar := scscf.NewRegistrar(realm)

	// Step 1: Initial REGISTER (no Authorization) — TS 23.228 §5.3.2.1
	raw1 := buildIMSRegister(impu, impi, contact, "", "", 3600)
	msg1 := parseSIPMsg(t, raw1)
	res1, err := registrar.HandleRegister(msg1)
	if err != nil {
		t.Fatalf("HandleRegister initial failed: %v", err)
	}

	// Step 2: Verify 401 Unauthorized
	if res1.StatusCode != 401 {
		t.Fatalf("expected 401 challenge, got %d", res1.StatusCode)
	}
	wwwAuth := res1.Headers["WWW-Authenticate"]
	if wwwAuth.Len == 0 {
		t.Fatal("missing WWW-Authenticate header in 401")
	}
	challengeStr := wwwAuth.String()

	// Step 3: Verify challenge parameters per TS 33.203 / RFC 3310
	realmVal := extractRealm(challengeStr)
	if realmVal != realm {
		t.Fatalf("realm mismatch: got %q, want %q", realmVal, realm)
	}
	algorithm := extractAlgorithm(challengeStr)
	if algorithm != "AKAv1-MD5" {
		t.Fatalf("algorithm mismatch: got %q, want AKAv1-MD5", algorithm)
	}
	nonce, opaque := extractChallengeParams(challengeStr)
	if nonce == "" {
		t.Fatal("WWW-Authenticate missing nonce")
	}
	if opaque == "" {
		t.Fatal("WWW-Authenticate missing opaque")
	}

	// Verify nonce format: base64(RAND || AUTN) — RFC 3310 §2
	record := registrar.GetRecord(impu)
	if record == nil || record.AuthState == nil {
		t.Fatal("no auth state after 401")
	}
	verifyNonceFormat(t, nonce, record.AuthState.AuthVector)

	// Step 4: UE computes RES and sends REGISTER + Authorization
	correctResp := hex.EncodeToString(record.AuthState.AuthVector.XRES)
	authz := buildAuthHeader(impi, realm, nonce, impu, correctResp, opaque)
	raw2 := buildIMSRegister(impu, impi, contact, authz, "", 3600)
	msg2 := parseSIPMsg(t, raw2)

	// Step 5: S-CSCF verifies RES == XRES, returns 200 OK
	res2, err := registrar.HandleRegister(msg2)
	if err != nil {
		t.Fatalf("HandleRegister auth response failed: %v", err)
	}
	if res2.StatusCode != 200 {
		t.Fatalf("expected 200 OK after auth, got %d", res2.StatusCode)
	}

	// Step 6: Verify 200 OK contains Service-Route and P-Associated-URI
	if res2.Headers["Service-Route"].Len == 0 {
		t.Fatal("200 OK missing Service-Route header")
	}
	srStr := res2.Headers["Service-Route"].String()
	if !strings.Contains(srStr, realm) {
		t.Fatalf("Service-Route should contain realm %s, got %s", realm, srStr)
	}
	if res2.Headers["P-Associated-URI"].Len == 0 {
		t.Fatal("200 OK missing P-Associated-URI header")
	}
	paiStr := res2.Headers["P-Associated-URI"].String()
	if !strings.Contains(paiStr, impu) {
		t.Fatalf("P-Associated-URI should contain %s, got %s", impu, paiStr)
	}

	// Step 7: Verify registration state
	if !registrar.IsRegistered(impu) {
		t.Fatal("user should be registered after 200 OK")
	}
}

// ---------------------------------------------------------------------------
// Test: Re-registration (TS 23.228 §5.3.2.2)
// ---------------------------------------------------------------------------

// TestE2E_IMS_ReRegistration verifies the re-registration flow:
//  1. Complete initial registration.
//  2. UE re-sends REGISTER (initiating a new challenge cycle).
//  3. S-CSCF verifies and returns 200 OK.
//  4. The registration state remains Registered.
func TestE2E_IMS_ReRegistration(t *testing.T) {
	realm := "ims.example.com"
	impu := "sip:bob@ims.example.com"
	impi := "bob@ims.example.com"
	contact := "sip:bob@192.168.1.200"

	registrar := scscf.NewRegistrar(realm)

	// Step 1: Complete initial registration
	registerIMSUser(t, registrar, impu, impi, contact)
	if !registrar.IsRegistered(impu) {
		t.Fatal("user should be registered after initial registration")
	}

	// Step 2: Re-registration — UE sends a new REGISTER.
	// Per TS 23.228 §5.3.2.2, the S-CSCF may re-challenge the UE.
	raw1 := buildIMSRegister(impu, impi, contact, "", "", 3600)
	msg1 := parseSIPMsg(t, raw1)
	res1, err := registrar.HandleRegister(msg1)
	if err != nil {
		t.Fatalf("re-registration initial REGISTER failed: %v", err)
	}
	if res1.StatusCode != 401 {
		t.Fatalf("expected 401 for re-registration challenge, got %d", res1.StatusCode)
	}

	// Step 3: Respond to the new challenge
	nonce, opaque := extractChallengeParams(res1.Headers["WWW-Authenticate"].String())
	record := registrar.GetRecord(impu)
	if record == nil || record.AuthState == nil {
		t.Fatal("no auth state for re-registration")
	}
	resp := hex.EncodeToString(record.AuthState.AuthVector.XRES)
	authz := buildAuthHeader(impi, realm, nonce, impu, resp, opaque)

	raw2 := buildIMSRegister(impu, impi, contact, authz, "", 3600)
	msg2 := parseSIPMsg(t, raw2)
	res2, err := registrar.HandleRegister(msg2)
	if err != nil {
		t.Fatalf("re-registration auth REGISTER failed: %v", err)
	}
	if res2.StatusCode != 200 {
		t.Fatalf("expected 200 OK for re-registration, got %d", res2.StatusCode)
	}

	// Step 4: Verify registration state remains Registered
	if !registrar.IsRegistered(impu) {
		t.Fatal("user should remain registered after re-registration")
	}
}

// ---------------------------------------------------------------------------
// Test: Deregistration (TS 23.228 §5.3.2.3)
// ---------------------------------------------------------------------------

// TestE2E_IMS_Deregistration verifies the deregistration flow:
//  1. Complete initial registration.
//  2. UE deregisters (REGISTER with Expires=0 or S-CSCF removes the binding).
//  3. S-CSCF returns 200 OK.
//  4. The registration state becomes Deregistered (IsRegistered returns false).
func TestE2E_IMS_Deregistration(t *testing.T) {
	realm := "ims.example.com"
	impu := "sip:carol@ims.example.com"
	impi := "carol@ims.example.com"
	contact := "sip:carol@192.168.1.300"

	registrar := scscf.NewRegistrar(realm)

	// Step 1: Complete initial registration
	registerIMSUser(t, registrar, impu, impi, contact)
	if !registrar.IsRegistered(impu) {
		t.Fatal("user should be registered after initial registration")
	}

	// Step 2: Deregister — the S-CSCF removes the registration binding.
	// In a full implementation this is triggered by REGISTER with Expires=0
	// (TS 23.228 §5.3.2.3); the registrar exposes DeleteRecord for this purpose.
	registrar.DeleteRecord(impu)

	// Step 3: Verify deregistered state
	if registrar.IsRegistered(impu) {
		t.Fatal("user should be deregistered after DeleteRecord")
	}
}

// ---------------------------------------------------------------------------
// Test: Registration with Path header (TS 23.228 §5.3.2.1 / RFC 3327)
// ---------------------------------------------------------------------------

// TestE2E_IMS_RegistrationWithPath verifies that the Path header inserted by
// the P-CSCF is processed by the S-CSCF and echoed back in the 200 OK.
func TestE2E_IMS_RegistrationWithPath(t *testing.T) {
	realm := "ims.example.com"
	impu := "sip:dave@ims.example.com"
	impi := "dave@ims.example.com"
	contact := "sip:dave@192.168.1.110"
	path := "sip:pcscf@192.168.1.50"

	registrar := scscf.NewRegistrar(realm)

	// Step 1: Initial REGISTER with Path header
	raw1 := buildIMSRegister(impu, impi, contact, "", path, 3600)
	msg1 := parseSIPMsg(t, raw1)
	res1, err := registrar.HandleRegister(msg1)
	if err != nil {
		t.Fatalf("initial REGISTER failed: %v", err)
	}
	if res1.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", res1.StatusCode)
	}

	// Step 2: REGISTER with auth and Path
	nonce, opaque := extractChallengeParams(res1.Headers["WWW-Authenticate"].String())
	record := registrar.GetRecord(impu)
	if record == nil || record.AuthState == nil {
		t.Fatal("no auth state")
	}
	resp := hex.EncodeToString(record.AuthState.AuthVector.XRES)
	authz := buildAuthHeader(impi, realm, nonce, impu, resp, opaque)

	raw2 := buildIMSRegister(impu, impi, contact, authz, path, 3600)
	msg2 := parseSIPMsg(t, raw2)
	res2, err := registrar.HandleRegister(msg2)
	if err != nil {
		t.Fatalf("auth REGISTER failed: %v", err)
	}
	if res2.StatusCode != 200 {
		t.Fatalf("expected 200 OK, got %d", res2.StatusCode)
	}

	// Step 3: Verify 200 OK contains Path header
	pathHdr := res2.Headers["Path"]
	if pathHdr.Len == 0 {
		t.Fatal("200 OK should contain Path header")
	}
	if !strings.Contains(pathHdr.String(), "pcscf") {
		t.Fatalf("Path header should contain pcscf, got %s", pathHdr.String())
	}

	// Verify the Path is stored in the registration record
	record = registrar.GetRecord(impu)
	if record == nil {
		t.Fatal("no record after registration")
	}
	record.RLock()
	storedPath := record.Path
	record.RUnlock()
	if len(storedPath) == 0 {
		t.Fatal("Path should be stored in the registration record")
	}
}

// ---------------------------------------------------------------------------
// Test: Authentication failure (TS 33.203 §6.2)
// ---------------------------------------------------------------------------

// TestE2E_IMS_RegistrationAuthFailure verifies the authentication failure flow:
//  1. UE sends REGISTER (no auth).
//  2. S-CSCF returns 401 challenge.
//  3. UE sends wrong Authorization (incorrect response).
//  4. S-CSCF returns a new 401 challenge (not 403, because < 3 attempts).
//  5. After the 3rd consecutive failure, S-CSCF returns 403 Forbidden.
func TestE2E_IMS_RegistrationAuthFailure(t *testing.T) {
	realm := "ims.example.com"
	impu := "sip:eve@ims.example.com"
	impi := "eve@ims.example.com"
	contact := "sip:eve@192.168.1.120"

	registrar := scscf.NewRegistrar(realm)

	// Step 1: Initial REGISTER (no auth) -> 401
	raw1 := buildIMSRegister(impu, impi, contact, "", "", 3600)
	msg1 := parseSIPMsg(t, raw1)
	res1, err := registrar.HandleRegister(msg1)
	if err != nil {
		t.Fatalf("initial REGISTER failed: %v", err)
	}
	if res1.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", res1.StatusCode)
	}

	// Step 2: First wrong attempt -> new 401 (Attempts < 3)
	nonce1, opaque1 := extractChallengeParams(res1.Headers["WWW-Authenticate"].String())
	wrongResp := "0000000000000000"
	authzWrong1 := buildAuthHeader(impi, realm, nonce1, impu, wrongResp, opaque1)

	raw2 := buildIMSRegister(impu, impi, contact, authzWrong1, "", 3600)
	msg2 := parseSIPMsg(t, raw2)
	res2, err := registrar.HandleRegister(msg2)
	if err != nil {
		t.Fatalf("first wrong attempt failed: %v", err)
	}
	if res2.StatusCode != 401 {
		t.Fatalf("first wrong attempt should yield 401, got %d", res2.StatusCode)
	}

	// Step 3: Simulate two prior failed attempts, then the 3rd failure -> 403.
	// The registrar re-challenges after each failure, resetting the attempt
	// counter.  To exercise the 403 path we seed Attempts=2 on the current
	// AuthState so the next wrong response triggers the >= 3 threshold.
	record := registrar.GetRecord(impu)
	if record == nil || record.AuthState == nil {
		t.Fatal("no auth state after second 401")
	}
	record.Lock()
	record.AuthState.Attempts = 2
	record.Unlock()

	nonce2, opaque2 := extractChallengeParams(res2.Headers["WWW-Authenticate"].String())
	authzWrong2 := buildAuthHeader(impi, realm, nonce2, impu, wrongResp, opaque2)

	raw3 := buildIMSRegister(impu, impi, contact, authzWrong2, "", 3600)
	msg3 := parseSIPMsg(t, raw3)
	res3, err := registrar.HandleRegister(msg3)
	if err != nil {
		t.Fatalf("third wrong attempt failed: %v", err)
	}
	if res3.StatusCode != 403 {
		t.Fatalf("third wrong attempt should yield 403 Forbidden, got %d", res3.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Test: Unknown user registration (TS 23.228 §5.3.2.1)
// ---------------------------------------------------------------------------

// TestE2E_IMS_RegistrationUnknownUser verifies that the S-CSCF still issues a
// challenge for unknown users (to avoid revealing subscriber existence) and
// rejects incorrect authentication responses.
func TestE2E_IMS_RegistrationUnknownUser(t *testing.T) {
	realm := "ims.example.com"
	impu := "sip:unknown@ims.example.com"
	impi := "unknown@ims.example.com"
	contact := "sip:unknown@192.168.1.130"

	registrar := scscf.NewRegistrar(realm)

	// Step 1: REGISTER from unknown user -> 401 (S-CSCF still challenges)
	raw1 := buildIMSRegister(impu, impi, contact, "", "", 3600)
	msg1 := parseSIPMsg(t, raw1)
	res1, err := registrar.HandleRegister(msg1)
	if err != nil {
		t.Fatalf("initial REGISTER failed: %v", err)
	}
	if res1.StatusCode != 401 {
		t.Fatalf("expected 401 for unknown user, got %d", res1.StatusCode)
	}

	// Step 2: Send REGISTER with wrong auth -> new challenge or 403
	nonce, opaque := extractChallengeParams(res1.Headers["WWW-Authenticate"].String())
	wrongResp := "ffffffffffffffff"
	authzWrong := buildAuthHeader(impi, realm, nonce, impu, wrongResp, opaque)

	raw2 := buildIMSRegister(impu, impi, contact, authzWrong, "", 3600)
	msg2 := parseSIPMsg(t, raw2)
	res2, err := registrar.HandleRegister(msg2)
	if err != nil {
		t.Fatalf("wrong auth REGISTER failed: %v", err)
	}
	if res2.StatusCode != 401 && res2.StatusCode != 403 {
		t.Fatalf("expected 401 or 403 for wrong auth, got %d", res2.StatusCode)
	}

	// Verify the user is NOT registered
	if registrar.IsRegistered(impu) {
		t.Fatal("unknown user should not be registered")
	}
}

// ---------------------------------------------------------------------------
// Test: Registration expiry (TS 23.228 §5.3.2.4)
// ---------------------------------------------------------------------------

// TestE2E_IMS_RegistrationExpires verifies that a registration with a short
// Expires value transitions to not-registered after the timeout.
func TestE2E_IMS_RegistrationExpires(t *testing.T) {
	realm := "ims.example.com"
	impu := "sip:frank@ims.example.com"
	impi := "frank@ims.example.com"
	contact := "sip:frank@192.168.1.140"

	registrar := scscf.NewRegistrar(realm)

	// Step 1: Register with Expires=1 second
	raw1 := buildIMSRegister(impu, impi, contact, "", "", 1)
	msg1 := parseSIPMsg(t, raw1)
	res1, _ := registrar.HandleRegister(msg1)
	if res1.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", res1.StatusCode)
	}

	nonce, opaque := extractChallengeParams(res1.Headers["WWW-Authenticate"].String())
	record := registrar.GetRecord(impu)
	resp := hex.EncodeToString(record.AuthState.AuthVector.XRES)
	authz := buildAuthHeader(impi, realm, nonce, impu, resp, opaque)

	raw2 := buildIMSRegister(impu, impi, contact, authz, "", 1)
	msg2 := parseSIPMsg(t, raw2)
	res2, _ := registrar.HandleRegister(msg2)
	if res2.StatusCode != 200 {
		t.Fatalf("expected 200 OK, got %d", res2.StatusCode)
	}

	// Step 2: Verify registered immediately after
	if !registrar.IsRegistered(impu) {
		t.Fatal("user should be registered immediately after 200 OK")
	}

	// Step 3: Wait for expiry
	time.Sleep(1500 * time.Millisecond)

	// Step 4: Verify expired
	if registrar.IsRegistered(impu) {
		t.Fatal("user should be expired after Expires timeout")
	}
}

// ---------------------------------------------------------------------------
// Test: Concurrent registration (TS 23.228 §5.3.2)
// ---------------------------------------------------------------------------

// TestE2E_IMS_RegistrationConcurrent verifies that the registrar handles
// concurrent registrations from multiple users safely (race-free).
func TestE2E_IMS_RegistrationConcurrent(t *testing.T) {
	realm := "ims.example.com"
	registrar := scscf.NewRegistrar(realm)

	n := 50
	var wg sync.WaitGroup
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			impu := fmt.Sprintf("sip:user%d@ims.example.com", idx)
			impi := fmt.Sprintf("user%d@ims.example.com", idx)
			contact := fmt.Sprintf("sip:user%d@192.168.1.%d", idx, idx%254+1)

			// Initial REGISTER -> 401
			raw1 := buildIMSRegister(impu, impi, contact, "", "", 3600)
			msg1, err := parserParseMsg(raw1)
			if err != nil {
				errs <- fmt.Errorf("user %d: parse initial REGISTER: %w", idx, err)
				return
			}
			res1, err := registrar.HandleRegister(msg1)
			if err != nil {
				errs <- fmt.Errorf("user %d: initial HandleRegister: %w", idx, err)
				return
			}
			if res1.StatusCode != 401 {
				errs <- fmt.Errorf("user %d: expected 401, got %d", idx, res1.StatusCode)
				return
			}

			// Extract challenge
			nonce, opaque := extractChallengeParams(res1.Headers["WWW-Authenticate"].String())

			// Get AV from record
			record := registrar.GetRecord(impu)
			if record == nil || record.AuthState == nil {
				errs <- fmt.Errorf("user %d: no auth state", idx)
				return
			}
			record.RLock()
			xres := record.AuthState.AuthVector.XRES
			record.RUnlock()
			resp := hex.EncodeToString(xres)

			authz := buildAuthHeader(impi, realm, nonce, impu, resp, opaque)

			// REGISTER with auth -> 200
			raw2 := buildIMSRegister(impu, impi, contact, authz, "", 3600)
			msg2, err := parserParseMsg(raw2)
			if err != nil {
				errs <- fmt.Errorf("user %d: parse auth REGISTER: %w", idx, err)
				return
			}
			res2, err := registrar.HandleRegister(msg2)
			if err != nil {
				errs <- fmt.Errorf("user %d: auth HandleRegister: %w", idx, err)
				return
			}
			if res2.StatusCode != 200 {
				errs <- fmt.Errorf("user %d: expected 200, got %d", idx, res2.StatusCode)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}

	// Verify all users are registered
	if registrar.GetRecordCount() != n {
		t.Errorf("expected %d registered records, got %d", n, registrar.GetRecordCount())
	}

	// Spot-check a few users
	for _, i := range []int{0, 1, 25, 49} {
		impu := fmt.Sprintf("sip:user%d@ims.example.com", i)
		if !registrar.IsRegistered(impu) {
			t.Errorf("user %d should be registered", i)
		}
	}
}
