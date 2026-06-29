// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the IMS registrar S-CSCF module.
 */

package ims_registrar_scscf

import (
	"encoding/hex"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/ims/auth"
	"github.com/kamailio/kamailio-go/internal/modules/ims_usrloc_scscf"
)

// mustParseMsg parses a raw SIP message, failing the test on error.
func mustParseMsg(t *testing.T, raw []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("failed to parse message: %v", err)
	}
	return msg
}

// makeRegister builds an initial REGISTER (no Authorization).
func makeRegister(cseq int) []byte {
	return []byte("REGISTER sip:example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776reg\r\n" +
		"From: Alice <sip:alice@example.com>;tag=ftag1\r\n" +
		"To: Alice <sip:alice@example.com>\r\n" +
		"Call-ID: reg-call-1@10.0.0.1\r\n" +
		"CSeq: " + itoaCseq(cseq) + " REGISTER\r\n" +
		"Contact: <sip:alice@10.0.0.1:5060>\r\n" +
		"Expires: 3600\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
}

func itoaCseq(n int) string {
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

// makeAuthRegister builds a REGISTER with the supplied Authorization body.
func makeAuthRegister(cseq int, authBody string) []byte {
	return []byte("REGISTER sip:example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776reg2\r\n" +
		"From: Alice <sip:alice@example.com>;tag=ftag1\r\n" +
		"To: Alice <sip:alice@example.com>\r\n" +
		"Call-ID: reg-call-1@10.0.0.1\r\n" +
		"CSeq: " + itoaCseq(cseq) + " REGISTER\r\n" +
		"Contact: <sip:alice@10.0.0.1:5060>\r\n" +
		"Authorization: " + authBody + "\r\n" +
		"Expires: 3600\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
}

// makeDeregister builds a REGISTER with Expires: 0.
func makeDeregister(cseq int) []byte {
	return []byte("REGISTER sip:example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776unreg\r\n" +
		"From: Alice <sip:alice@example.com>;tag=ftag1\r\n" +
		"To: Alice <sip:alice@example.com>\r\n" +
		"Call-ID: reg-call-2@10.0.0.1\r\n" +
		"CSeq: " + itoaCseq(cseq) + " REGISTER\r\n" +
		"Contact: <sip:alice@10.0.0.1:5060>\r\n" +
		"Expires: 0\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
}

// makeDeregisterStar builds a REGISTER with Contact: * and Expires: 0.
func makeDeregisterStar(cseq int) []byte {
	return []byte("REGISTER sip:example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776star\r\n" +
		"From: Alice <sip:alice@example.com>;tag=ftag1\r\n" +
		"To: Alice <sip:alice@example.com>\r\n" +
		"Call-ID: reg-call-3@10.0.0.1\r\n" +
		"CSeq: " + itoaCseq(cseq) + " REGISTER\r\n" +
		"Contact: *\r\n" +
		"Expires: 0\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
}

// parseAuthParams extracts key="value" pairs from a WWW-Authenticate or
// Authorization header value.
func parseAuthParams(header string) map[string]string {
	out := map[string]string{}
	// Drop the leading "Digest " scheme token.
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

// seededAuthVector returns a fixed AuthVector with a known XRES so the
// test can build a matching Authorization response.
func seededAuthVector() *auth.AuthVector {
	return &auth.AuthVector{
		RAND: bytesFromHex("000102030405060708090a0b0c0d0e0f"),
		XRES: bytesFromHex("deadbeefcafef00d1234567890abcdef"),
		CK:   bytesFromHex("11111111111111111111111111111111"),
		IK:   bytesFromHex("22222222222222222222222222222222"),
		AUTN: bytesFromHex("aabbccddeeff00112233445566778899"),
	}
}

func bytesFromHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// seedModule installs a known auth vector and user profile into the
// module's in-memory CxClient.
func seedModule(t *testing.T, m *RegistrarSCSCFModule) {
	t.Helper()
	cx := m.InMemoryCx()
	if cx == nil {
		t.Fatalf("in-memory CxClient not configured")
	}
	cx.SetAuthVector("alice@example.com", seededAuthVector())
	cx.SetUserProfile("sip:alice@example.com", &ims_usrloc_scscf.UserProfile{
		IMPU:         "sip:alice@example.com",
		IMPI:         "alice@example.com",
		ChargingInfo: "charge-1",
	})
}

// runFullAuthFlow performs the two-step AKA registration and returns the
// 200 OK SaveResult (or fails the test).
func runFullAuthFlow(t *testing.T, m *RegistrarSCSCFModule) *SaveResult {
	t.Helper()
	seedModule(t, m)

	// Step 1: initial REGISTER -> 401 challenge.
	msg1 := mustParseMsg(t, makeRegister(1))
	r1, err := m.Save(msg1)
	if err != nil {
		t.Fatalf("Save #1 error: %v", err)
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
	if m.PendingCount() != 1 {
		t.Errorf("expected 1 pending auth, got %d", m.PendingCount())
	}

	// Step 2: REGISTER with Authorization (response = hex(XRES)).
	resp := hex.EncodeToString(seededAuthVector().XRES)
	authBody := "Digest username=\"alice@ims.example.com\", realm=\"ims.example.com\"" +
		", nonce=\"" + nonce + "\", uri=\"sip:example.com\", response=\"" + resp + "\"" +
		", algorithm=AKAv1-MD5, opaque=\"" + opaque + "\""
	msg2 := mustParseMsg(t, makeAuthRegister(2, authBody))
	r2, err := m.Save(msg2)
	if err != nil {
		t.Fatalf("Save #2 error: %v", err)
	}
	if r2.StatusCode != 200 {
		t.Fatalf("expected 200 OK after auth, got %d (%s)", r2.StatusCode, r2.StatusReason)
	}
	if r2.Headers["Service-Route"].String() == "" {
		t.Error("expected Service-Route header in 200 OK")
	}
	if r2.Headers["P-Associated-URI"].String() == "" {
		t.Error("expected P-Associated-URI header in 200 OK")
	}
	if m.PendingCount() != 0 {
		t.Errorf("expected pending auth cleared, got %d", m.PendingCount())
	}
	return r2
}

func TestSave_InitialChallenge(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	seedModule(t, m)
	msg := mustParseMsg(t, makeRegister(1))
	r, err := m.Save(msg)
	if err != nil {
		t.Fatalf("Save error: %v", err)
	}
	if r.StatusCode != 401 {
		t.Errorf("expected 401, got %d", r.StatusCode)
	}
	if r.Headers["WWW-Authenticate"].String() == "" {
		t.Error("expected WWW-Authenticate header")
	}
}

func TestSave_UnknownSubscriber(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	// No auth vector seeded -> MAR returns ErrUnknownSubscriber -> 403.
	msg := mustParseMsg(t, makeRegister(1))
	r, err := m.Save(msg)
	if err == nil {
		t.Error("expected error for unknown subscriber")
	}
	if r.StatusCode != 403 {
		t.Errorf("expected 403 for unknown subscriber, got %d", r.StatusCode)
	}
}

func TestSave_FullAuthFlow(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	runFullAuthFlow(t, m)
	if !m.IsRegistered("sip:alice@example.com") {
		t.Error("expected IsRegistered to be true after full flow")
	}
	contacts := m.RegFetchContacts("sip:alice@example.com")
	if len(contacts) != 1 {
		t.Errorf("expected 1 contact, got %d", len(contacts))
	}
}

func TestSave_AuthResponseStoredContact(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	runFullAuthFlow(t, m)
	contacts := m.RegFetchContacts("sip:alice@example.com")
	if len(contacts) != 1 {
		t.Fatalf("expected 1 contact, got %d", len(contacts))
	}
	c := contacts[0]
	if c.Contact != "sip:alice@10.0.0.1:5060" {
		t.Errorf("contact URI = %q", c.Contact)
	}
	if c.IMSPrivateID != "alice@example.com" {
		t.Errorf("IMPI = %q, want alice@example.com", c.IMSPrivateID)
	}
	if c.RegState != ims_usrloc_scscf.RegStateRegistered {
		t.Errorf("RegState = %q", c.RegState)
	}
	if c.ChargingID == "" {
		t.Error("expected ChargingID to be populated when EnableCharging=true")
	}
}

func TestSave_NoPendingAuth(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	// REGISTER with Authorization but no preceding challenge.
	authBody := "Digest username=\"alice@ims.example.com\", realm=\"ims.example.com\"" +
		", nonce=\"n\", uri=\"sip:example.com\", response=\"x\", opaque=\"o\""
	msg := mustParseMsg(t, makeAuthRegister(1, authBody))
	r, err := m.Save(msg)
	if err == nil {
		t.Error("expected error when no pending auth state")
	}
	if r.StatusCode != 403 {
		t.Errorf("expected 403, got %d", r.StatusCode)
	}
}

func TestSave_OpaqueMismatch(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	seedModule(t, m)
	// First, get a real challenge.
	msg1 := mustParseMsg(t, makeRegister(1))
	r1, _ := m.Save(msg1)
	params := parseAuthParams(r1.Headers["WWW-Authenticate"].String())

	// Now respond with a wrong opaque.
	resp := hex.EncodeToString(seededAuthVector().XRES)
	authBody := "Digest username=\"alice@ims.example.com\", realm=\"ims.example.com\"" +
		", nonce=\"" + params["nonce"] + "\", uri=\"sip:example.com\", response=\"" + resp + "\"" +
		", algorithm=AKAv1-MD5, opaque=\"wrong-opaque\""
	msg2 := mustParseMsg(t, makeAuthRegister(2, authBody))
	r2, err := m.Save(msg2)
	if err == nil {
		t.Error("expected error on opaque mismatch")
	}
	if r2.StatusCode != 403 {
		t.Errorf("expected 403 on opaque mismatch, got %d", r2.StatusCode)
	}
}

func TestSave_WrongResponseRechallenge(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	seedModule(t, m)
	msg1 := mustParseMsg(t, makeRegister(1))
	r1, _ := m.Save(msg1)
	params := parseAuthParams(r1.Headers["WWW-Authenticate"].String())

	// Respond with a wrong response value (not hex(XRES)).
	authBody := "Digest username=\"alice@ims.example.com\", realm=\"ims.example.com\"" +
		", nonce=\"" + params["nonce"] + "\", uri=\"sip:example.com\", response=\"badresponse\"" +
		", algorithm=AKAv1-MD5, opaque=\"" + params["opaque"] + "\""
	msg2 := mustParseMsg(t, makeAuthRegister(2, authBody))
	r2, err := m.Save(msg2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Wrong response should trigger a re-challenge (401), not 403.
	if r2.StatusCode != 401 {
		t.Errorf("expected 401 re-challenge on wrong response, got %d", r2.StatusCode)
	}
}

func TestSave_MaxAttemptsForbidden(t *testing.T) {
	m := NewRegistrarSCSCFModuleWithConfig(func() Config {
		c := DefaultConfig()
		c.MaxAuthAttempts = 1
		return c
	}())
	seedModule(t, m)

	// First challenge.
	msg1 := mustParseMsg(t, makeRegister(1))
	r1, _ := m.Save(msg1)
	params := parseAuthParams(r1.Headers["WWW-Authenticate"].String())

	// One wrong response; MaxAuthAttempts=1 -> 403.
	authBody := "Digest username=\"alice@ims.example.com\", realm=\"ims.example.com\"" +
		", nonce=\"" + params["nonce"] + "\", uri=\"sip:example.com\", response=\"bad\"" +
		", algorithm=AKAv1-MD5, opaque=\"" + params["opaque"] + "\""
	msg2 := mustParseMsg(t, makeAuthRegister(2, authBody))
	r2, _ := m.Save(msg2)
	if r2.StatusCode != 403 {
		t.Errorf("expected 403 after max attempts, got %d", r2.StatusCode)
	}
}

func TestSave_DeregisterExpires0(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	runFullAuthFlow(t, m)
	if !m.IsRegistered("sip:alice@example.com") {
		t.Fatal("precondition: user should be registered")
	}
	msg := mustParseMsg(t, makeDeregister(3))
	r, err := m.Save(msg)
	if err != nil {
		t.Fatalf("deregister error: %v", err)
	}
	if r.StatusCode != 200 {
		t.Errorf("expected 200 on deregister, got %d", r.StatusCode)
	}
	if m.IsRegistered("sip:alice@example.com") {
		t.Error("expected IsRegistered to be false after deregister")
	}
}

func TestSave_DeregisterContactStar(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	runFullAuthFlow(t, m)
	msg := mustParseMsg(t, makeDeregisterStar(3))
	r, err := m.Save(msg)
	if err != nil {
		t.Fatalf("deregister * error: %v", err)
	}
	if r.StatusCode != 200 {
		t.Errorf("expected 200 on deregister *, got %d", r.StatusCode)
	}
	if m.IsRegistered("sip:alice@example.com") {
		t.Error("expected IsRegistered to be false after deregister *")
	}
}

func TestSave_DeregisterUnknown(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	msg := mustParseMsg(t, makeDeregister(1))
	r, _ := m.Save(msg)
	if r.StatusCode != 404 {
		t.Errorf("expected 404 deregistering unknown AOR, got %d", r.StatusCode)
	}
}

func TestSave_NilMessage(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	_, err := m.Save(nil)
	if err == nil {
		t.Error("expected error for nil message")
	}
}

func TestSave_NotRegister(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	invite := []byte("INVITE sip:bob@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776inv\r\n" +
		"From: <sip:alice@example.com>;tag=t1\r\n" +
		"To: <sip:bob@example.com>\r\n" +
		"Call-ID: c@10.0.0.1\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Content-Length: 0\r\n\r\n")
	msg := mustParseMsg(t, invite)
	_, err := m.Save(msg)
	if err == nil {
		t.Error("expected error for non-REGISTER")
	}
}

func TestSave_NoToHeader(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	msg := []byte("REGISTER sip:example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776noto\r\n" +
		"From: <sip:alice@example.com>;tag=t1\r\n" +
		"Call-ID: c@10.0.0.1\r\n" +
		"CSeq: 1 REGISTER\r\n" +
		"Contact: <sip:alice@10.0.0.1>\r\n" +
		"Expires: 3600\r\n" +
		"Content-Length: 0\r\n\r\n")
	parsed := mustParseMsg(t, msg)
	r, err := m.Save(parsed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.StatusCode != 400 {
		t.Errorf("expected 400 for missing To, got %d", r.StatusCode)
	}
}

func TestLookup(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	runFullAuthFlow(t, m)
	// R-URI = sip:alice@example.com
	msg := mustParseMsg(t, []byte("INVITE sip:alice@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.2;branch=z9hG4bK776lkp\r\n" +
		"From: <sip:bob@example.com>;tag=t1\r\n" +
		"To: <sip:alice@example.com>\r\n" +
		"Call-ID: c@10.0.0.2\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Content-Length: 0\r\n\r\n"))
	contacts, err := m.Lookup(msg)
	if err != nil {
		t.Fatalf("Lookup error: %v", err)
	}
	if len(contacts) != 1 {
		t.Fatalf("expected 1 contact, got %d", len(contacts))
	}
	if contacts[0] != "sip:alice@10.0.0.1:5060" {
		t.Errorf("contact = %q", contacts[0])
	}
}

func TestLookup_NoRURI(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	_, err := m.Lookup(&parser.SIPMsg{})
	if err == nil {
		t.Error("expected error for message with no R-URI")
	}
}

func TestLookup_NilMessage(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	_, err := m.Lookup(nil)
	if err == nil {
		t.Error("expected error for nil message")
	}
}

func TestIsRegistered(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	if m.IsRegistered("sip:nobody@example.com") {
		t.Error("expected IsRegistered false for unknown IMPU")
	}
	runFullAuthFlow(t, m)
	if !m.IsRegistered("sip:alice@example.com") {
		t.Error("expected IsRegistered true after registration")
	}
}

func TestIsAuthorised(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	if m.IsAuthorised(nil) {
		t.Error("nil message should not be authorised")
	}
	msg := mustParseMsg(t, makeRegister(1))
	if m.IsAuthorised(msg) {
		t.Error("REGISTER without Authorization should not be authorised")
	}
	authMsg := mustParseMsg(t, makeAuthRegister(1, "Digest username=\"a\""))
	if !m.IsAuthorised(authMsg) {
		t.Error("REGISTER with Digest Authorization should be authorised")
	}
}

func TestAssignRealm(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	m.AssignRealm("newrealm.example.com")
	if m.Realm() != "newrealm.example.com" {
		t.Errorf("Realm = %q", m.Realm())
	}
}

func TestSetRealm(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	m.SetRealm("via setter")
	if m.Realm() != "via setter" {
		t.Errorf("Realm = %q", m.Realm())
	}
}

func TestAddPAssertedIdentity(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	v, err := m.AddPAssertedIdentity("sip:alice@example.com")
	if err != nil {
		t.Fatalf("AddPAssertedIdentity error: %v", err)
	}
	s := v.String()
	if !strings.Contains(s, "sip:alice@example.com") {
		t.Errorf("PAI value = %q", s)
	}
}

func TestAddPAssertedIdentity_Empty(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	_, err := m.AddPAssertedIdentity("")
	if err == nil {
		t.Error("expected error for empty URI")
	}
}

func TestRegFetchContacts(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	runFullAuthFlow(t, m)
	contacts := m.RegFetchContacts("sip:alice@example.com")
	if len(contacts) != 1 {
		t.Fatalf("expected 1 contact, got %d", len(contacts))
	}
	if contacts[0].AOR != "sip:alice@example.com" {
		t.Errorf("AOR = %q", contacts[0].AOR)
	}
}

func TestRegFetchContacts_Empty(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	if contacts := m.RegFetchContacts("sip:nobody@example.com"); len(contacts) != 0 {
		t.Errorf("expected 0 contacts, got %d", len(contacts))
	}
}

func TestDeregister(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	runFullAuthFlow(t, m)
	if err := m.Deregister("sip:alice@example.com"); err != nil {
		t.Fatalf("Deregister error: %v", err)
	}
	if m.IsRegistered("sip:alice@example.com") {
		t.Error("expected IsRegistered false after Deregister")
	}
}

func TestDeregister_Unknown(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	if err := m.Deregister("sip:nobody@example.com"); err == nil {
		t.Error("expected error deregistering unknown AOR")
	}
}

func TestCleanupStalePending(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	seedModule(t, m)
	// Trigger a challenge to create pending state.
	_ = mustParseMsg(t, makeRegister(1))
	_, _ = m.Save(mustParseMsg(t, makeRegister(1)))
	if m.PendingCount() != 1 {
		t.Fatalf("expected 1 pending, got %d", m.PendingCount())
	}
	removed := m.CleanupStalePending(0) // 0 = remove all older than now
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}
	if m.PendingCount() != 0 {
		t.Errorf("expected 0 pending after cleanup, got %d", m.PendingCount())
	}
}

func TestInMemoryCxClient(t *testing.T) {
	c := NewInMemoryCxClient()
	av := seededAuthVector()
	c.SetAuthVector("alice@example.com", av)
	got, err := c.MAR("alice@example.com", "realm")
	if err != nil {
		t.Fatalf("MAR error: %v", err)
	}
	// Verify it returns a clone, not the seed.
	got.XRES[0] = 0xff
	if av.XRES[0] == 0xff {
		t.Error("MAR should return a clone, not the seed vector")
	}
	// Unknown subscriber.
	if _, err := c.MAR("nobody", "realm"); err == nil {
		t.Error("expected error for unknown IMPI")
	}
}

func TestInMemoryCxClient_Profile(t *testing.T) {
	c := NewInMemoryCxClient()
	p := &ims_usrloc_scscf.UserProfile{IMPU: "sip:alice@example.com", IMPI: "alice"}
	c.SetUserProfile("sip:alice@example.com", p)
	got, err := c.SAR("alice", "sip:alice@example.com", "scscf")
	if err != nil {
		t.Fatalf("SAR error: %v", err)
	}
	if got.IMPU != "sip:alice@example.com" {
		t.Errorf("IMPU = %q", got.IMPU)
	}
	if _, err := c.SAR("x", "sip:nobody", "scscf"); err == nil {
		t.Error("expected error for unknown IMPU")
	}
}

func TestSetCxClient(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	custom := NewInMemoryCxClient()
	m.SetCxClient(custom)
	if m.CxClient() != custom {
		t.Error("SetCxClient did not install the custom client")
	}
	// Setting nil should restore an in-memory client.
	m.SetCxClient(nil)
	if m.InMemoryCx() == nil {
		t.Error("expected in-memory client after nil set")
	}
}

func TestSetUsrloc(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	custom := ims_usrloc_scscf.NewUsrlocSCSCFModule()
	m.SetUsrloc(custom)
	if m.Usrloc() != custom {
		t.Error("SetUsrloc did not install the custom backend")
	}
	// Setting nil should restore a fresh backend.
	m.SetUsrloc(nil)
	if m.Usrloc() == nil {
		t.Error("expected non-nil usrloc after nil set")
	}
}

func TestUsrlocAccessor(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	if m.Usrloc() == nil {
		t.Error("Usrloc should not be nil on a new module")
	}
}

func TestCxClientAccessor(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	if m.CxClient() == nil {
		t.Error("CxClient should not be nil on a new module")
	}
}

func TestInMemoryCx_Accessor(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	if m.InMemoryCx() == nil {
		t.Error("expected in-memory CxClient on default module")
	}
	m.SetCxClient(stubCxClient{})
	if m.InMemoryCx() != nil {
		t.Error("expected nil InMemoryCx when a custom client is set")
	}
}

// stubCxClient is a CxClient that is not an *InMemoryCxClient.
type stubCxClient struct{}

func (stubCxClient) MAR(impi, realm string) (*auth.AuthVector, error) {
	return nil, ErrUnknownSubscriber
}
func (stubCxClient) SAR(impi, impu, serverName string) (*ims_usrloc_scscf.UserProfile, error) {
	return nil, ErrUnknownSubscriber
}

func TestNewWithConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Realm = "custom.example.com"
	cfg.MaxContacts = 5
	m := NewRegistrarSCSCFModuleWithConfig(cfg)
	if m.Realm() != "custom.example.com" {
		t.Errorf("Realm = %q", m.Realm())
	}
}

func TestDefaultSingleton(t *testing.T) {
	if DefaultSCSCFRegistrar() != DefaultSCSCFRegistrar() {
		t.Error("DefaultSCSCFRegistrar should return same instance")
	}
}

func TestInit_ResetsDefault(t *testing.T) {
	first := DefaultSCSCFRegistrar()
	first.SetRealm("before-init.example.com")
	Init()
	second := DefaultSCSCFRegistrar()
	if second.Realm() == "before-init.example.com" {
		t.Error("Init should reset the default registrar")
	}
	if second.Realm() != DefaultConfig().Realm {
		t.Errorf("Realm = %q, want %q", second.Realm(), DefaultConfig().Realm)
	}
}

func TestIsDeregistration(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		want bool
	}{
		{"expires_zero", makeDeregister(1), true},
		{"contact_star", makeDeregisterStar(1), true},
		{"normal_register", makeRegister(1), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := mustParseMsg(t, tc.raw)
			if got := isDeregistration(msg); got != tc.want {
				t.Errorf("isDeregistration = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsDeregistration_ContactExpires0(t *testing.T) {
	raw := []byte("REGISTER sip:example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776ce0\r\n" +
		"From: <sip:alice@example.com>;tag=t1\r\n" +
		"To: <sip:alice@example.com>\r\n" +
		"Call-ID: c@10.0.0.1\r\n" +
		"CSeq: 1 REGISTER\r\n" +
		"Contact: <sip:alice@10.0.0.1>;expires=0\r\n" +
		"Content-Length: 0\r\n\r\n")
	msg := mustParseMsg(t, raw)
	if !isDeregistration(msg) {
		t.Error("expected Contact with expires=0 to be deregistration")
	}
}

func TestExtractAOR(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"<sip:alice@example.com>", "sip:alice@example.com"},
		{"Alice <sip:alice@example.com>", "sip:alice@example.com"},
		{"sip:bob@example.com", "sip:bob@example.com"},
	}
	for _, c := range cases {
		if got := extractAOR(c.in); got != c.want {
			t.Errorf("extractAOR(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExtractContactURI(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"*", "*"},
		{"<sip:alice@10.0.0.1:5060>", "sip:alice@10.0.0.1:5060"},
		{"<sip:alice@10.0.0.1>;q=0.5", "sip:alice@10.0.0.1"},
		{"sip:bob@1.2.3.4", "sip:bob@1.2.3.4"},
	}
	for _, c := range cases {
		if got := extractContactURI(c.in); got != c.want {
			t.Errorf("extractContactURI(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExtractAuthUsername(t *testing.T) {
	body := `Digest username="alice@ims.example.com", realm="ims.example.com"`
	if got := extractAuthUsername(body); got != "alice@ims.example.com" {
		t.Errorf("extractAuthUsername = %q", got)
	}
	if extractAuthUsername("") != "" {
		t.Error("expected empty for empty body")
	}
}

func TestDeriveIMPI(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"sip:alice@example.com", "alice@example.com"},
		{"sips:alice@example.com", "alice@example.com"},
		{"tel:+1234", "+1234"},
		{"alice@example.com", "alice@example.com"},
	}
	for _, c := range cases {
		if got := deriveIMPI(c.in); got != c.want {
			t.Errorf("deriveIMPI(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := NewRegistrarSCSCFModule()
	seedModule(t, m)
	var wg sync.WaitGroup
	// Concurrent challenges + lookups + is_registered.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			msg := mustParseMsg(t, makeRegister(n + 1))
			_, _ = m.Save(msg)
		}(i)
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.IsRegistered("sip:alice@example.com")
			_ = m.RegFetchContacts("sip:alice@example.com")
		}()
	}
	wg.Wait()
}

func TestParseAuthParams(t *testing.T) {
	h := `Digest realm="ims.example.com", nonce="abc", algorithm=AKAv1-MD5, opaque="xyz"`
	p := parseAuthParams(h)
	if p["realm"] != "ims.example.com" {
		t.Errorf("realm = %q", p["realm"])
	}
	if p["nonce"] != "abc" {
		t.Errorf("nonce = %q", p["nonce"])
	}
	if p["opaque"] != "xyz" {
		t.Errorf("opaque = %q", p["opaque"])
	}
	if p["algorithm"] != "AKAv1-MD5" {
		t.Errorf("algorithm = %q", p["algorithm"])
	}
}

// silence unused import warnings for time when builds prune it.
var _ = time.Second
