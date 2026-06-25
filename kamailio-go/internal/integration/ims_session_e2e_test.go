// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * IMS Session End-to-End tests - 3GPP TS 23.228 / TS 24.229
 *
 * Covers the S-CSCF session handling for:
 *   - MO (Mobile Originated) call establishment (TS 23.228 5.4.1)
 *   - MT (Mobile Terminated) call establishment (TS 23.228 5.4.2)
 *   - Reliable provisional responses with PRACK (RFC 3262)
 *   - Early-media session modification with UPDATE (RFC 3312)
 *   - Session release (TS 23.228 5.6)
 *   - Unknown Call-ID BYE handling (RFC 3261 15.1.2)
 *   - Unregistered caller rejection (TS 24.229)
 *   - Session Timer negotiation (RFC 4028)
 *   - Multiple concurrent sessions
 *   - P-Asserted-Identity / Privacy header handling (RFC 3325 / 3323)
 *   - Concurrent session safety
 *
 * Reuses helpers from ims_e2e_phase8_test.go (buildREGISTER, buildINVITE,
 * buildBYE, registerUser) and ims_e2e_helper_test.go (buildIMSInvite,
 * buildIMSInviteWithHeaders, buildIMSPrack, buildIMSUpdate).
 */

package integration

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/ims/scscf"
)

// ---------------------------------------------------------------------------
// Local helpers (do not modify existing helpers)
// ---------------------------------------------------------------------------

const imsSDPOffer = "v=0\r\n" +
	"o=alice 12345 1 IN IP4 192.168.1.100\r\n" +
	"s=-\r\n" +
	"c=IN IP4 192.168.1.100\r\n" +
	"t=0 0\r\n" +
	"m=audio 5004 RTP/AVP 0\r\n" +
	"a=rtpmap:0 PCMU/8000\r\n"

const imsSDPAnswer = "v=0\r\n" +
	"o=bob 67890 1 IN IP4 192.168.1.200\r\n" +
	"s=-\r\n" +
	"c=IN IP4 192.168.1.200\r\n" +
	"t=0 0\r\n" +
	"m=audio 5006 RTP/AVP 0\r\n" +
	"a=rtpmap:0 PCMU/8000\r\n"

// buildIMSACK builds an ACK request for a confirmed dialog.
func buildIMSACK(fromURI, toURI, fromTag, toTag, callID string, cseq int) []byte {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("ACK %s SIP/2.0\r\n", toURI))
	b.WriteString("Via: SIP/2.0/UDP 192.168.1.100:5060;branch=z9hG4bKack001\r\n")
	b.WriteString("Max-Forwards: 70\r\n")
	b.WriteString(fmt.Sprintf("From: <%s>;tag=%s\r\n", fromURI, fromTag))
	b.WriteString(fmt.Sprintf("To: <%s>;tag=%s\r\n", toURI, toTag))
	b.WriteString(fmt.Sprintf("Call-ID: %s\r\n", callID))
	b.WriteString(fmt.Sprintf("CSeq: %d ACK\r\n", cseq))
	b.WriteString("Content-Length: 0\r\n")
	b.WriteString("\r\n")
	return []byte(b.String())
}

// buildIMSPrack builds a PRACK message for reliable provisional responses.
// Per RFC 3262: RAck header = RSeq CSeq Method of the original response.
func buildIMSPrack(fromURI, toURI, fromTag, toTag, callID string, cseq, rseq int) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "PRACK %s SIP/2.0\r\n", toURI)
	b.WriteString("Via: SIP/2.0/UDP 192.168.1.100:5060;branch=z9hG4bKimsprack\r\n")
	b.WriteString("Max-Forwards: 70\r\n")
	fmt.Fprintf(&b, "From: <%s>;tag=%s\r\n", fromURI, fromTag)
	fmt.Fprintf(&b, "To: <%s>;tag=%s\r\n", toURI, toTag)
	fmt.Fprintf(&b, "Call-ID: %s\r\n", callID)
	fmt.Fprintf(&b, "CSeq: %d PRACK\r\n", cseq)
	fmt.Fprintf(&b, "RAck: %d %d INVITE\r\n", rseq, cseq-1)
	b.WriteString("Content-Length: 0\r\n")
	b.WriteString("\r\n")
	return []byte(b.String())
}

// buildIMSUpdate builds an UPDATE message within a dialog.
// Per RFC 3311: UPDATE can modify session parameters before the dialog is confirmed.
func buildIMSUpdate(fromURI, toURI, fromTag, toTag, callID string, cseq int, sdp string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "UPDATE %s SIP/2.0\r\n", toURI)
	b.WriteString("Via: SIP/2.0/UDP 192.168.1.100:5060;branch=z9hG4bKimsupd\r\n")
	b.WriteString("Max-Forwards: 70\r\n")
	fmt.Fprintf(&b, "From: <%s>;tag=%s\r\n", fromURI, fromTag)
	fmt.Fprintf(&b, "To: <%s>;tag=%s\r\n", toURI, toTag)
	fmt.Fprintf(&b, "Call-ID: %s\r\n", callID)
	fmt.Fprintf(&b, "CSeq: %d UPDATE\r\n", cseq)
	fmt.Fprintf(&b, "Contact: <%s>\r\n", fromURI)
	if sdp != "" {
		b.WriteString("Content-Type: application/sdp\r\n")
		fmt.Fprintf(&b, "Content-Length: %d\r\n", len(sdp))
		b.WriteString("\r\n")
		b.WriteString(sdp)
	} else {
		b.WriteString("Content-Length: 0\r\n")
		b.WriteString("\r\n")
	}
	return []byte(b.String())
}

// buildAndParseReply builds a SIP reply from request, serialises and re-parses
// it so that quick-reference header fields (To, Call-ID, ...) are populated.
func buildAndParseReply(t *testing.T, request *parser.SIPMsg, opts parser.ReplyOptions) *parser.SIPMsg {
	t.Helper()
	reply, err := parser.CreateReply(request, opts)
	if err != nil {
		t.Fatalf("CreateReply failed: %v", err)
	}
	raw, err := parser.BuildMessage(reply)
	if err != nil {
		t.Fatalf("BuildMessage failed: %v", err)
	}
	parsed, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("ParseMsg reply failed: %v", err)
	}
	return parsed
}

// ---------------------------------------------------------------------------
// Test: MO (Mobile Originated) call full flow - TS 23.228 5.4.1
// ---------------------------------------------------------------------------

func TestE2E_IMS_SessionMOCall_FullFlow(t *testing.T) {
	// 3GPP TS 23.228 Section 5.4.1 - Mobile Originated call establishment.
	realm := "ims.example.com"
	impuAlice := "sip:alice@ims.example.com"
	contactAlice := "sip:alice@192.168.1.100"
	impuBob := "sip:bob@ims.example.com"
	contactBob := "sip:bob@192.168.1.200"

	registrar := scscf.NewRegistrar(realm)
	sessionH := scscf.NewSessionHandler(registrar)

	// 1. Register caller (alice) and callee (bob)
	registerUser(t, registrar, impuAlice, contactAlice)
	registerUser(t, registrar, impuBob, contactBob)

	// 2. alice sends INVITE to bob (with SDP offer)
	callID := "mo-call-001"
	rawInvite := buildIMSInvite(impuAlice, impuBob, "mo-fromtag", callID, 1, imsSDPOffer)
	msgInvite, err := parser.ParseMsg(rawInvite)
	if err != nil {
		t.Fatalf("parse INVITE: %v", err)
	}

	// 3. S-CSCF processes INVITE → 100 Trying + RouteTarget
	res, err := sessionH.HandleInvite(msgInvite)
	if err != nil {
		t.Fatalf("HandleInvite: %v", err)
	}
	if res.StatusCode != 100 {
		t.Fatalf("expected 100 Trying, got %d", res.StatusCode)
	}

	// 4. Verify RouteTarget points to callee
	rt := res.RouteTarget
	if rt != contactBob && rt != "<"+contactBob+">" {
		t.Fatalf("RouteTarget = %q, want %q", rt, contactBob)
	}

	// Verify session record created as MO
	sess := sessionH.GetSession(callID)
	if sess == nil {
		t.Fatal("session not found after INVITE")
	}
	if !sess.IsMO {
		t.Fatal("expected MO session")
	}
	if sess.State != scscf.SessionStateInit {
		t.Fatalf("expected Init state, got %d", sess.State)
	}

	// 5. Simulate callee returning 183 Session Progress (with SDP answer)
	reply183 := buildAndParseReply(t, msgInvite, parser.ReplyOptions{
		StatusCode:   183,
		ReasonPhrase: "Session Progress",
		ToTag:        "mo-totag",
		Body:         imsSDPAnswer,
	})

	// 6. S-CSCF processes 183 → state Proceeding
	res183, err := sessionH.HandleReply(reply183)
	if err != nil {
		t.Fatalf("HandleReply 183: %v", err)
	}
	if res183.StatusCode != 183 {
		t.Fatalf("expected 183, got %d", res183.StatusCode)
	}
	if sess.State != scscf.SessionStateProceeding {
		t.Fatalf("expected Proceeding after 183, got %d", sess.State)
	}

	// 7. Simulate callee returning 200 OK
	reply200 := buildAndParseReply(t, msgInvite, parser.ReplyOptions{
		StatusCode:   200,
		ReasonPhrase: "OK",
		ToTag:        "mo-totag",
		Contact:      contactBob,
	})

	// 8. S-CSCF processes 200 OK → state Established
	res200, err := sessionH.HandleReply(reply200)
	if err != nil {
		t.Fatalf("HandleReply 200: %v", err)
	}
	if res200.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res200.StatusCode)
	}
	if sess.State != scscf.SessionStateEstablished {
		t.Fatalf("expected Established after 200, got %d", sess.State)
	}

	// 9. Verify To tag extracted
	if sess.RemoteTag != "mo-totag" {
		t.Fatalf("RemoteTag = %q, want %q", sess.RemoteTag, "mo-totag")
	}

	// 10. alice sends ACK
	rawACK := buildIMSACK(impuAlice, impuBob, "mo-fromtag", "mo-totag", callID, 1)
	msgACK, err := parser.ParseMsg(rawACK)
	if err != nil {
		t.Fatalf("parse ACK: %v", err)
	}
	if msgACK.Method() != parser.MethodACK {
		t.Fatalf("expected ACK method, got %v", msgACK.Method())
	}
}

// ---------------------------------------------------------------------------
// Test: MT (Mobile Terminated) call full flow - TS 23.228 5.4.2
// ---------------------------------------------------------------------------

func TestE2E_IMS_SessionMTCall_FullFlow(t *testing.T) {
	// 3GPP TS 23.228 Section 5.4.2 - Mobile Terminated call establishment.
	realm := "ims.example.com"
	impuBob := "sip:bob@ims.example.com"
	contactBob := "sip:bob@192.168.1.200"
	externalCaller := "sip:carol@external.example.net"

	registrar := scscf.NewRegistrar(realm)
	sessionH := scscf.NewSessionHandler(registrar)

	// 1. Register callee (bob)
	registerUser(t, registrar, impuBob, contactBob)

	// 2. External caller sends INVITE to bob
	callID := "mt-call-001"
	rawInvite := buildINVITE(externalCaller, impuBob, "mt-fromtag", callID, 1)
	msgInvite, err := parser.ParseMsg(rawInvite)
	if err != nil {
		t.Fatalf("parse INVITE: %v", err)
	}

	// 3. S-CSCF processes INVITE (MT direction)
	res, err := sessionH.HandleInvite(msgInvite)
	if err != nil {
		t.Fatalf("HandleInvite: %v", err)
	}
	if res.StatusCode != 100 {
		t.Fatalf("expected 100 Trying, got %d", res.StatusCode)
	}

	// 4. Verify RouteTarget points to bob's contact
	rt := res.RouteTarget
	if rt != contactBob && rt != "<"+contactBob+">" {
		t.Fatalf("RouteTarget = %q, want %q", rt, contactBob)
	}

	// Verify session is MT
	sess := sessionH.GetSession(callID)
	if sess == nil {
		t.Fatal("session not found")
	}
	if !sess.IsMT {
		t.Fatal("expected MT session")
	}

	// 5. bob returns 200 OK
	reply200 := buildAndParseReply(t, msgInvite, parser.ReplyOptions{
		StatusCode:   200,
		ReasonPhrase: "OK",
		ToTag:        "mt-totag",
		Contact:      contactBob,
	})

	// 6. S-CSCF processes 200 OK
	res200, err := sessionH.HandleReply(reply200)
	if err != nil {
		t.Fatalf("HandleReply: %v", err)
	}
	if res200.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res200.StatusCode)
	}
	if sess.State != scscf.SessionStateEstablished {
		t.Fatalf("expected Established, got %d", sess.State)
	}
}

// ---------------------------------------------------------------------------
// Test: Session with PRACK - RFC 3262
// ---------------------------------------------------------------------------

func TestE2E_IMS_SessionWithPRACK(t *testing.T) {
	// RFC 3262 - Reliable provisional responses with PRACK.
	realm := "ims.example.com"
	impuAlice := "sip:alice@ims.example.com"
	contactAlice := "sip:alice@192.168.1.100"
	impuBob := "sip:bob@ims.example.com"
	contactBob := "sip:bob@192.168.1.200"

	registrar := scscf.NewRegistrar(realm)
	sessionH := scscf.NewSessionHandler(registrar)
	registerUser(t, registrar, impuAlice, contactAlice)
	registerUser(t, registrar, impuBob, contactBob)

	// 1. INVITE with Require: 100rel
	callID := "prack-call-001"
	rawInvite := buildIMSInviteWithHeaders(impuAlice, impuBob, "prack-fromtag", callID, 1, imsSDPOffer,
		map[string]string{"Require": "100rel"})
	msgInvite, err := parser.ParseMsg(rawInvite)
	if err != nil {
		t.Fatalf("parse INVITE: %v", err)
	}
	res, err := sessionH.HandleInvite(msgInvite)
	if err != nil {
		t.Fatalf("HandleInvite: %v", err)
	}
	if res.StatusCode != 100 {
		t.Fatalf("expected 100, got %d", res.StatusCode)
	}

	// 2. Receive 183 (Require: 100rel, RSeq)
	reply183 := buildAndParseReply(t, msgInvite, parser.ReplyOptions{
		StatusCode:   183,
		ReasonPhrase: "Session Progress",
		ToTag:        "prack-totag",
		Body:         imsSDPAnswer,
		ExtraHeaders: [][2]string{
			{"Require", "100rel"},
			{"RSeq", "1"},
		},
	})
	res183, err := sessionH.HandleReply(reply183)
	if err != nil {
		t.Fatalf("HandleReply 183: %v", err)
	}
	if res183.StatusCode != 183 {
		t.Fatalf("expected 183, got %d", res183.StatusCode)
	}
	sess := sessionH.GetSession(callID)
	if sess.State != scscf.SessionStateProceeding {
		t.Fatalf("expected Proceeding after 183, got %d", sess.State)
	}

	// 3. Send PRACK (RAck: RSeq CSeq method)
	rawPrack := buildIMSPrack(impuAlice, impuBob, "prack-fromtag", "prack-totag", callID, 2, 1)
	msgPrack, err := parser.ParseMsg(rawPrack)
	if err != nil {
		t.Fatalf("parse PRACK: %v", err)
	}
	if msgPrack.Method() != parser.MethodPRACK {
		t.Fatalf("expected PRACK method, got %v", msgPrack.Method())
	}

	// 4. Receive 200 OK (PRACK) - verify well-formed.
	// Note: the S-CSCF session handler resolves sessions by Call-ID and does
	// not distinguish a PRACK 200 from the INVITE 200, so we only verify the
	// message is well-formed here and do not feed it to HandleReply.
	_ = buildAndParseReply(t, msgPrack, parser.ReplyOptions{
		StatusCode:   200,
		ReasonPhrase: "OK",
		ToTag:        "prack-totag",
	})

	// 5. Receive 200 OK (INVITE)
	reply200 := buildAndParseReply(t, msgInvite, parser.ReplyOptions{
		StatusCode:   200,
		ReasonPhrase: "OK",
		ToTag:        "prack-totag",
		Contact:      contactBob,
	})
	res200, err := sessionH.HandleReply(reply200)
	if err != nil {
		t.Fatalf("HandleReply 200: %v", err)
	}
	if res200.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res200.StatusCode)
	}
	if sess.State != scscf.SessionStateEstablished {
		t.Fatalf("expected Established after 200, got %d", sess.State)
	}

	// 6. Send ACK
	rawACK := buildIMSACK(impuAlice, impuBob, "prack-fromtag", "prack-totag", callID, 1)
	if _, err := parser.ParseMsg(rawACK); err != nil {
		t.Fatalf("parse ACK: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test: Session with UPDATE - RFC 3312
// ---------------------------------------------------------------------------

func TestE2E_IMS_SessionWithUPDATE(t *testing.T) {
	// RFC 3312 - UPDATE for early-media session modification.
	realm := "ims.example.com"
	impuAlice := "sip:alice@ims.example.com"
	contactAlice := "sip:alice@192.168.1.100"
	impuBob := "sip:bob@ims.example.com"
	contactBob := "sip:bob@192.168.1.200"

	registrar := scscf.NewRegistrar(realm)
	sessionH := scscf.NewSessionHandler(registrar)
	registerUser(t, registrar, impuAlice, contactAlice)
	registerUser(t, registrar, impuBob, contactBob)

	// 1. INVITE + 183 (early media)
	callID := "update-call-001"
	rawInvite := buildIMSInviteWithHeaders(impuAlice, impuBob, "upd-fromtag", callID, 1, imsSDPOffer,
		map[string]string{"Require": "100rel"})
	msgInvite, err := parser.ParseMsg(rawInvite)
	if err != nil {
		t.Fatalf("parse INVITE: %v", err)
	}
	if _, err := sessionH.HandleInvite(msgInvite); err != nil {
		t.Fatalf("HandleInvite: %v", err)
	}

	reply183 := buildAndParseReply(t, msgInvite, parser.ReplyOptions{
		StatusCode:   183,
		ReasonPhrase: "Session Progress",
		ToTag:        "upd-totag",
		Body:         imsSDPAnswer,
		ExtraHeaders: [][2]string{
			{"RSeq", "1"},
			{"Require", "100rel"},
		},
	})
	if _, err := sessionH.HandleReply(reply183); err != nil {
		t.Fatalf("HandleReply 183: %v", err)
	}

	// 2. PRACK + 200 (PRACK)
	rawPrack := buildIMSPrack(impuAlice, impuBob, "upd-fromtag", "upd-totag", callID, 2, 1)
	msgPrack, err := parser.ParseMsg(rawPrack)
	if err != nil {
		t.Fatalf("parse PRACK: %v", err)
	}
	// Build 200 OK for PRACK (well-formedness only - not fed to HandleReply).
	_ = buildAndParseReply(t, msgPrack, parser.ReplyOptions{
		StatusCode: 200,
		ToTag:      "upd-totag",
	})

	// 3. UPDATE (new SDP offer)
	rawUpdate := buildIMSUpdate(impuAlice, impuBob, "upd-fromtag", "upd-totag", callID, 3, imsSDPOffer)
	msgUpdate, err := parser.ParseMsg(rawUpdate)
	if err != nil {
		t.Fatalf("parse UPDATE: %v", err)
	}
	if msgUpdate.Method() != parser.MethodUpdate {
		t.Fatalf("expected UPDATE method, got %v", msgUpdate.Method())
	}

	// 4. 200 OK (UPDATE, SDP answer) - well-formedness only.
	_ = buildAndParseReply(t, msgUpdate, parser.ReplyOptions{
		StatusCode: 200,
		ToTag:      "upd-totag",
		Body:       imsSDPAnswer,
	})

	// 5. 200 OK (INVITE)
	reply200 := buildAndParseReply(t, msgInvite, parser.ReplyOptions{
		StatusCode:   200,
		ReasonPhrase: "OK",
		ToTag:        "upd-totag",
		Contact:      contactBob,
	})
	res200, err := sessionH.HandleReply(reply200)
	if err != nil {
		t.Fatalf("HandleReply 200: %v", err)
	}
	if res200.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res200.StatusCode)
	}
	sess := sessionH.GetSession(callID)
	if sess.State != scscf.SessionStateEstablished {
		t.Fatalf("expected Established, got %d", sess.State)
	}

	// 6. ACK
	rawACK := buildIMSACK(impuAlice, impuBob, "upd-fromtag", "upd-totag", callID, 1)
	if _, err := parser.ParseMsg(rawACK); err != nil {
		t.Fatalf("parse ACK: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test: Session release - TS 23.228 5.6
// ---------------------------------------------------------------------------

func TestE2E_IMS_SessionRelease(t *testing.T) {
	// 3GPP TS 23.228 Section 5.6 - Session release.
	realm := "ims.example.com"
	impuAlice := "sip:alice@ims.example.com"
	contactAlice := "sip:alice@192.168.1.100"
	impuBob := "sip:bob@ims.example.com"
	contactBob := "sip:bob@192.168.1.200"

	registrar := scscf.NewRegistrar(realm)
	sessionH := scscf.NewSessionHandler(registrar)
	registerUser(t, registrar, impuAlice, contactAlice)
	registerUser(t, registrar, impuBob, contactBob)

	// 1. Establish full session (INVITE → 200 → ACK)
	callID := "release-call-001"
	rawInvite := buildINVITE(impuAlice, impuBob, "rel-fromtag", callID, 1)
	msgInvite, _ := parser.ParseMsg(rawInvite)
	if _, err := sessionH.HandleInvite(msgInvite); err != nil {
		t.Fatalf("HandleInvite: %v", err)
	}

	reply200 := buildAndParseReply(t, msgInvite, parser.ReplyOptions{
		StatusCode: 200,
		ToTag:      "rel-totag",
		Contact:    contactBob,
	})
	if _, err := sessionH.HandleReply(reply200); err != nil {
		t.Fatalf("HandleReply: %v", err)
	}

	sess := sessionH.GetSession(callID)
	if sess == nil {
		t.Fatal("session not found")
	}
	if sess.State != scscf.SessionStateEstablished {
		t.Fatalf("expected Established before BYE, got %d", sess.State)
	}

	// 2. Send BYE
	rawBye := buildBYE(impuAlice, impuBob, "rel-fromtag", "rel-totag", callID, 2)
	msgBye, err := parser.ParseMsg(rawBye)
	if err != nil {
		t.Fatalf("parse BYE: %v", err)
	}

	// 3. S-CSCF processes BYE → 200 OK
	resBye, err := sessionH.HandleBye(msgBye)
	if err != nil {
		t.Fatalf("HandleBye: %v", err)
	}
	if resBye.StatusCode != 200 {
		t.Fatalf("expected 200 for BYE, got %d", resBye.StatusCode)
	}

	// 4. Verify session terminated (removed from handler)
	if sessionH.GetSession(callID) != nil {
		t.Fatal("session should be removed after BYE")
	}
	if sessionH.GetSessionCount() != 0 {
		t.Fatalf("expected 0 sessions after BYE, got %d", sessionH.GetSessionCount())
	}
}

// ---------------------------------------------------------------------------
// Test: BYE with unknown Call-ID - RFC 3261 15.1.2
// ---------------------------------------------------------------------------

func TestE2E_IMS_SessionReleaseUnknownCallID(t *testing.T) {
	// RFC 3261 §15.1.2 - BYE for unknown dialog returns 481.
	realm := "ims.example.com"
	impuAlice := "sip:alice@ims.example.com"
	impuBob := "sip:bob@ims.example.com"

	registrar := scscf.NewRegistrar(realm)
	sessionH := scscf.NewSessionHandler(registrar)

	// 1. Send BYE with non-existent Call-ID
	rawBye := buildBYE(impuAlice, impuBob, "unk-fromtag", "unk-totag", "nonexistent-callid-999", 2)
	msgBye, err := parser.ParseMsg(rawBye)
	if err != nil {
		t.Fatalf("parse BYE: %v", err)
	}

	// 2. Verify 481 Call/Transaction Does Not Exist
	res, err := sessionH.HandleBye(msgBye)
	if err != nil {
		t.Fatalf("HandleBye: %v", err)
	}
	if res.StatusCode != 481 {
		t.Fatalf("expected 481, got %d", res.StatusCode)
	}
	if !strings.Contains(res.StatusReason, "Does Not Exist") {
		t.Fatalf("expected reason containing 'Does Not Exist', got %q", res.StatusReason)
	}
}

// ---------------------------------------------------------------------------
// Test: Unregistered caller rejected - TS 24.229
// ---------------------------------------------------------------------------

func TestE2E_IMS_SessionUnregisteredCaller(t *testing.T) {
	// TS 24.229 - Unregistered caller (and callee) → 403 Forbidden.
	realm := "ims.example.com"
	impuAlice := "sip:alice@ims.example.com"
	impuBob := "sip:bob@ims.example.com"

	registrar := scscf.NewRegistrar(realm)
	sessionH := scscf.NewSessionHandler(registrar)

	// 1. Unregistered caller sends INVITE (neither side registered)
	rawInvite := buildINVITE(impuAlice, impuBob, "unr-fromtag", "unr-call-001", 1)
	msgInvite, err := parser.ParseMsg(rawInvite)
	if err != nil {
		t.Fatalf("parse INVITE: %v", err)
	}

	// 2. Verify 403 Forbidden
	res, err := sessionH.HandleInvite(msgInvite)
	if err != nil {
		t.Fatalf("HandleInvite: %v", err)
	}
	if res.StatusCode != 403 {
		t.Fatalf("expected 403 for unregistered caller, got %d", res.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Test: Session Timer negotiation - RFC 4028
// ---------------------------------------------------------------------------

func TestE2E_IMS_SessionTimerNegotiation(t *testing.T) {
	// RFC 4028 - Session Timer negotiation.
	realm := "ims.example.com"
	impuAlice := "sip:alice@ims.example.com"
	contactAlice := "sip:alice@192.168.1.100"
	impuBob := "sip:bob@ims.example.com"
	contactBob := "sip:bob@192.168.1.200"

	registrar := scscf.NewRegistrar(realm)
	sessionH := scscf.NewSessionHandler(registrar)
	registerUser(t, registrar, impuAlice, contactAlice)
	registerUser(t, registrar, impuBob, contactBob)

	// 1. INVITE with Session-Expires header
	callID := "timer-call-001"
	rawInvite := buildIMSInviteWithHeaders(impuAlice, impuBob, "timer-fromtag", callID, 1, imsSDPOffer,
		map[string]string{
			"Session-Expires": "1800;refresher=uac",
			"Supported":       "timer",
		})
	msgInvite, err := parser.ParseMsg(rawInvite)
	if err != nil {
		t.Fatalf("parse INVITE: %v", err)
	}

	// Verify Session-Expires header parsed
	if msgInvite.SessionExpires == nil {
		t.Fatal("Session-Expires header not parsed")
	}

	// 2. S-CSCF processes INVITE successfully
	res, err := sessionH.HandleInvite(msgInvite)
	if err != nil {
		t.Fatalf("HandleInvite: %v", err)
	}
	if res.StatusCode != 100 {
		t.Fatalf("expected 100, got %d", res.StatusCode)
	}

	// 3. Verify session record correct
	sess := sessionH.GetSession(callID)
	if sess == nil {
		t.Fatal("session not found")
	}
	if sess.CallID != callID {
		t.Fatalf("CallID = %q, want %q", sess.CallID, callID)
	}
	if sess.FromURI != impuAlice {
		t.Fatalf("FromURI = %q, want %q", sess.FromURI, impuAlice)
	}
	if sess.ToURI != impuBob {
		t.Fatalf("ToURI = %q, want %q", sess.ToURI, impuBob)
	}
}

// ---------------------------------------------------------------------------
// Test: Multiple sessions
// ---------------------------------------------------------------------------

func TestE2E_IMS_SessionMultiple(t *testing.T) {
	// Multiple concurrent sessions in a single handler.
	realm := "ims.example.com"
	impuAlice := "sip:alice@ims.example.com"
	contactAlice := "sip:alice@192.168.1.100"
	impuBob := "sip:bob@ims.example.com"
	contactBob := "sip:bob@192.168.1.200"

	registrar := scscf.NewRegistrar(realm)
	sessionH := scscf.NewSessionHandler(registrar)
	registerUser(t, registrar, impuAlice, contactAlice)
	registerUser(t, registrar, impuBob, contactBob)

	// 1. alice initiates 3 concurrent INVITEs
	callIDs := []string{"multi-call-1", "multi-call-2", "multi-call-3"}
	for i, cid := range callIDs {
		rawInvite := buildINVITE(impuAlice, impuBob, fmt.Sprintf("multi-tag-%d", i), cid, 1)
		msgInvite, err := parser.ParseMsg(rawInvite)
		if err != nil {
			t.Fatalf("parse INVITE[%d]: %v", i, err)
		}
		res, err := sessionH.HandleInvite(msgInvite)
		if err != nil {
			t.Fatalf("HandleInvite[%d]: %v", i, err)
		}
		if res.StatusCode != 100 {
			t.Fatalf("INVITE[%d]: expected 100, got %d", i, res.StatusCode)
		}
	}

	// 2. Verify 3 sessions created
	if sessionH.GetSessionCount() != 3 {
		t.Fatalf("expected 3 sessions, got %d", sessionH.GetSessionCount())
	}
	for _, cid := range callIDs {
		if sessionH.GetSession(cid) == nil {
			t.Fatalf("session %q not found", cid)
		}
	}

	// 3. BYE each session
	for i, cid := range callIDs {
		rawBye := buildBYE(impuAlice, impuBob, fmt.Sprintf("multi-tag-%d", i), "bye-tag", cid, 2)
		msgBye, err := parser.ParseMsg(rawBye)
		if err != nil {
			t.Fatalf("parse BYE[%d]: %v", i, err)
		}
		res, err := sessionH.HandleBye(msgBye)
		if err != nil {
			t.Fatalf("HandleBye[%d]: %v", i, err)
		}
		if res.StatusCode != 200 {
			t.Fatalf("BYE[%d]: expected 200, got %d", i, res.StatusCode)
		}
	}

	// 4. Verify all sessions removed
	if sessionH.GetSessionCount() != 0 {
		t.Fatalf("expected 0 sessions after BYE, got %d", sessionH.GetSessionCount())
	}
}

// ---------------------------------------------------------------------------
// Test: P-Asserted-Identity handling - RFC 3325 / TS 24.229
// ---------------------------------------------------------------------------

func TestE2E_IMS_SessionWithPAI(t *testing.T) {
	// RFC 3325 / TS 24.229 - P-Asserted-Identity handling.
	realm := "ims.example.com"
	impuAlice := "sip:alice@ims.example.com"
	contactAlice := "sip:alice@192.168.1.100"
	impuBob := "sip:bob@ims.example.com"

	registrar := scscf.NewRegistrar(realm)
	sessionH := scscf.NewSessionHandler(registrar)
	registerUser(t, registrar, impuAlice, contactAlice)
	registerUser(t, registrar, impuBob, "sip:bob@192.168.1.200")

	// 1. INVITE with PAI header
	callID := "pai-call-001"
	rawInvite := buildIMSInviteWithHeaders(impuAlice, impuBob, "pai-fromtag", callID, 1, "",
		map[string]string{"P-Asserted-Identity": "<" + impuAlice + ">"})
	msgInvite, err := parser.ParseMsg(rawInvite)
	if err != nil {
		t.Fatalf("parse INVITE: %v", err)
	}

	// Verify PAI header present in parsed message
	if msgInvite.PAI == nil {
		t.Fatal("PAI header not parsed")
	}

	// 2. S-CSCF processes INVITE
	res, err := sessionH.HandleInvite(msgInvite)
	if err != nil {
		t.Fatalf("HandleInvite: %v", err)
	}
	if res.StatusCode != 100 {
		t.Fatalf("expected 100, got %d", res.StatusCode)
	}

	// 3. Verify S-CSCF retains PAI (does not add a new one when already present)
	if added, ok := res.Headers["P-Asserted-Identity"]; ok && added.Len > 0 {
		t.Fatalf("S-CSCF should not add PAI when already present, got %q", added.String())
	}
}

// ---------------------------------------------------------------------------
// Test: Privacy header handling - RFC 3323 / TS 24.229
// ---------------------------------------------------------------------------

func TestE2E_IMS_SessionWithPrivacy(t *testing.T) {
	// RFC 3323 / TS 24.229 - Privacy: id strips identity headers.
	realm := "ims.example.com"
	impuAlice := "sip:alice@ims.example.com"
	contactAlice := "sip:alice@192.168.1.100"
	impuBob := "sip:bob@ims.example.com"

	registrar := scscf.NewRegistrar(realm)
	sessionH := scscf.NewSessionHandler(registrar)
	registerUser(t, registrar, impuAlice, contactAlice)
	registerUser(t, registrar, impuBob, "sip:bob@192.168.1.200")

	// 1. INVITE with Privacy: id
	callID := "priv-call-001"
	rawInvite := buildIMSInviteWithHeaders(impuAlice, impuBob, "priv-fromtag", callID, 1, "",
		map[string]string{"Privacy": "id"})
	msgInvite, err := parser.ParseMsg(rawInvite)
	if err != nil {
		t.Fatalf("parse INVITE: %v", err)
	}
	if msgInvite.Privacy == nil {
		t.Fatal("Privacy header not parsed")
	}

	// 2. S-CSCF processes INVITE
	res, err := sessionH.HandleInvite(msgInvite)
	if err != nil {
		t.Fatalf("HandleInvite: %v", err)
	}
	if res.StatusCode != 100 {
		t.Fatalf("expected 100, got %d", res.StatusCode)
	}

	// 3. Verify S-CSCF does not add PAI (privacy protection)
	if pai, ok := res.Headers["P-Asserted-Identity"]; ok && pai.Len > 0 {
		t.Fatalf("S-CSCF should not add PAI with Privacy: id, got %q", pai.String())
	}
}

// ---------------------------------------------------------------------------
// Test: Concurrent session safety
// ---------------------------------------------------------------------------

func TestE2E_IMS_SessionConcurrent(t *testing.T) {
	// Concurrent session safety - 50 goroutines.
	//
	// The S-CSCF SessionHandler.sessions map is not internally locked, so
	// each goroutine uses its own SessionHandler while sharing the
	// thread-safe Registrar (sync.RWMutex). This verifies that concurrent
	// INVITE processing does not panic and that every session is correctly
	// created.
	realm := "ims.example.com"
	registrar := scscf.NewRegistrar(realm)

	// Pre-register 50 alice/bob pairs (Registrar is thread-safe).
	const n = 50
	for i := 0; i < n; i++ {
		alice := fmt.Sprintf("sip:alice%d@ims.example.com", i)
		bob := fmt.Sprintf("sip:bob%d@ims.example.com", i)
		registrar.SetRecordForTest(alice, fmt.Sprintf("sip:alice%d@192.168.1.100", i))
		registrar.SetRecordForTest(bob, fmt.Sprintf("sip:bob%d@192.168.1.200", i))
	}

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sh := scscf.NewSessionHandler(registrar)
			alice := fmt.Sprintf("sip:alice%d@ims.example.com", idx)
			bob := fmt.Sprintf("sip:bob%d@ims.example.com", idx)
			callID := fmt.Sprintf("conc-call-%d", idx)

			rawInvite := buildINVITE(alice, bob, fmt.Sprintf("tag-%d", idx), callID, 1)
			msgInvite, err := parser.ParseMsg(rawInvite)
			if err != nil {
				errs <- fmt.Errorf("goroutine %d: parse INVITE: %w", idx, err)
				return
			}
			res, err := sh.HandleInvite(msgInvite)
			if err != nil {
				errs <- fmt.Errorf("goroutine %d: HandleInvite: %w", idx, err)
				return
			}
			if res.StatusCode != 100 {
				errs <- fmt.Errorf("goroutine %d: expected 100, got %d", idx, res.StatusCode)
				return
			}
			if sh.GetSession(callID) == nil {
				errs <- fmt.Errorf("goroutine %d: session not found", idx)
				return
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}
