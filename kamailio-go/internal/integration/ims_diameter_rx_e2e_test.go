// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * End-to-end tests for the IMS Rx/Ro Diameter interface.
 * Based on 3GPP TS 29.214 (Rx) and TS 32.299 (Ro).
 *
 * The Rx reference point sits between the P-CSCF and the PCRF and carries
 * the AA-Request / Session-Termination / Abort-Session procedures used to
 * authorise QoS resources for SIP media. The Ro reference point sits
 * between the IMS core and the Online Charging System and carries the
 * online charging (quota) procedures.
 */

package integration

import (
	"fmt"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/modules/ims_charging"
	"github.com/kamailio/kamailio-go/internal/modules/ims_ocs"
	"github.com/kamailio/kamailio-go/internal/modules/ims_qos"
)

// Rx interface command codes (3GPP TS 29.214).
const (
	CmdAAR = 265 // AA-Request
	CmdSTR = 275 // Session-Termination-Request
	CmdASR = 274 // Abort-Session-Request
)

// rxBuildINVITE builds and parses a SIP INVITE with the given Call-ID and
// From-tag, suitable for QoS authorisation.
func rxBuildINVITE(callID, fromTag string) *parser.SIPMsg {
	raw := fmt.Sprintf(
		"INVITE sip:bob@ims.example.com SIP/2.0\r\n"+
			"Via: SIP/2.0/UDP 192.168.1.100:5060;branch=z9hG4bKrx-invite\r\n"+
			"Max-Forwards: 70\r\n"+
			"From: <sip:alice@ims.example.com>;tag=%s\r\n"+
			"To: <sip:bob@ims.example.com>\r\n"+
			"Call-ID: %s\r\n"+
			"CSeq: 1 INVITE\r\n"+
			"Contact: <sip:alice@192.168.1.100>\r\n"+
			"Content-Type: application/sdp\r\n"+
			"Content-Length: 0\r\n"+
			"\r\n", fromTag, callID)
	msg, err := parser.ParseMsg([]byte(raw))
	if err != nil {
		panic(fmt.Sprintf("rxBuildINVITE: parse failed: %v", err))
	}
	return msg
}

// rxBuildINVITEWithSDP builds and parses a SIP INVITE carrying an SDP body
// with both audio and video media descriptions.
func rxBuildINVITEWithSDP(callID, fromTag string) *parser.SIPMsg {
	sdp := "v=0\r\n" +
		"o=alice 123 456 IN IP4 192.168.1.100\r\n" +
		"s=-\r\n" +
		"c=IN IP4 192.168.1.100\r\n" +
		"t=0 0\r\n" +
		"m=audio 5004 RTP/AVP 0\r\n" +
		"a=rtpmap:0 PCMU/8000\r\n" +
		"m=video 5006 RTP/AVP 31\r\n" +
		"a=rtpmap:31 H261/90000\r\n"
	raw := fmt.Sprintf(
		"INVITE sip:bob@ims.example.com SIP/2.0\r\n"+
			"Via: SIP/2.0/UDP 192.168.1.100:5060;branch=z9hG4bKrx-video\r\n"+
			"Max-Forwards: 70\r\n"+
			"From: <sip:alice@ims.example.com>;tag=%s\r\n"+
			"To: <sip:bob@ims.example.com>\r\n"+
			"Call-ID: %s\r\n"+
			"CSeq: 1 INVITE\r\n"+
			"Contact: <sip:alice@192.168.1.100>\r\n"+
			"Content-Type: application/sdp\r\n"+
			"Content-Length: %d\r\n"+
			"\r\n"+
			"%s", fromTag, callID, len(sdp), sdp)
	msg, err := parser.ParseMsg([]byte(raw))
	if err != nil {
		panic(fmt.Sprintf("rxBuildINVITEWithSDP: parse failed: %v", err))
	}
	return msg
}

// ---------------------------------------------------------------------------
// Rx AAR/AAA - QoS resource authorisation (TS 29.214 4.1)
// ---------------------------------------------------------------------------

// TestE2E_Rx_AAR_AAA_QoSAuthorization verifies that the QoSModule
// authorises the media components of a SIP INVITE and creates a QoS
// session in the "authorized" state with an audio media component.
func TestE2E_Rx_AAR_AAA_QoSAuthorization(t *testing.T) {
	qm := ims_qos.NewQoSModule()

	msg := rxBuildINVITE("rx-aar-callid-1", "rx-aar-tag-1")
	session, err := qm.Authorize(msg)
	if err != nil {
		t.Fatalf("Authorize failed: %v", err)
	}

	if session.CallID != "rx-aar-callid-1" {
		t.Fatalf("CallID: expected rx-aar-callid-1, got %q", session.CallID)
	}
	if session.Status != ims_qos.StatusAuthorized {
		t.Fatalf("Status: expected %q, got %q", ims_qos.StatusAuthorized, session.Status)
	}
	if len(session.MediaComponents) == 0 {
		t.Fatal("MediaComponents is empty")
	}

	// Verify the default audio component is present.
	var hasAudio bool
	for _, mc := range session.MediaComponents {
		if mc.MediaType == "audio" {
			hasAudio = true
			if mc.Status != ims_qos.StatusAuthorized {
				t.Fatalf("audio component Status: expected %q, got %q", ims_qos.StatusAuthorized, mc.Status)
			}
		}
	}
	if !hasAudio {
		t.Fatal("no audio media component in authorised session")
	}
}

// ---------------------------------------------------------------------------
// Rx QoS with video media
// ---------------------------------------------------------------------------

// TestE2E_Rx_QoSSessionWithVideo verifies that a session carrying an SDP
// body with audio and video media can be authorised. The QoSModule
// creates a default audio component; the test verifies the session is
// authorised and contains media components.
func TestE2E_Rx_QoSSessionWithVideo(t *testing.T) {
	qm := ims_qos.NewQoSModule()

	msg := rxBuildINVITEWithSDP("rx-video-callid-1", "rx-video-tag-1")
	session, err := qm.Authorize(msg)
	if err != nil {
		t.Fatalf("Authorize failed: %v", err)
	}

	if session.Status != ims_qos.StatusAuthorized {
		t.Fatalf("Status: expected %q, got %q", ims_qos.StatusAuthorized, session.Status)
	}
	if len(session.MediaComponents) == 0 {
		t.Fatal("MediaComponents is empty for video session")
	}

	// Verify at least the audio component is present.
	var hasAudio bool
	for _, mc := range session.MediaComponents {
		if mc.MediaType == "audio" {
			hasAudio = true
		}
	}
	if !hasAudio {
		t.Fatal("no audio media component in video session")
	}
}

// ---------------------------------------------------------------------------
// Rx QoS revoke
// ---------------------------------------------------------------------------

// TestE2E_Rx_QoSRevoke verifies that revoking a QoS session marks both
// the session and its media components as "revoked".
func TestE2E_Rx_QoSRevoke(t *testing.T) {
	qm := ims_qos.NewQoSModule()

	msg := rxBuildINVITE("rx-revoke-callid-1", "rx-revoke-tag-1")
	session, err := qm.Authorize(msg)
	if err != nil {
		t.Fatalf("Authorize failed: %v", err)
	}

	if err := qm.RevokeSession(session.CallID, session.FromTag); err != nil {
		t.Fatalf("RevokeSession failed: %v", err)
	}

	updated := qm.GetSession(session.CallID, session.FromTag)
	if updated == nil {
		t.Fatal("session not found after revoke")
	}
	if updated.Status != ims_qos.StatusRevoked {
		t.Fatalf("Status: expected %q, got %q", ims_qos.StatusRevoked, updated.Status)
	}
	for i, mc := range updated.MediaComponents {
		if mc.Status != ims_qos.StatusRevoked {
			t.Fatalf("MediaComponent[%d] Status: expected %q, got %q", i, ims_qos.StatusRevoked, mc.Status)
		}
	}
}

// ---------------------------------------------------------------------------
// Rx QoS get session
// ---------------------------------------------------------------------------

// TestE2E_Rx_QoSGetSession verifies that an authorised session can be
// retrieved by its Call-ID and From-tag.
func TestE2E_Rx_QoSGetSession(t *testing.T) {
	qm := ims_qos.NewQoSModule()

	msg := rxBuildINVITE("rx-get-callid-1", "rx-get-tag-1")
	session, err := qm.Authorize(msg)
	if err != nil {
		t.Fatalf("Authorize failed: %v", err)
	}

	got := qm.GetSession(session.CallID, session.FromTag)
	if got == nil {
		t.Fatal("GetSession returned nil for existing session")
	}
	if got.CallID != session.CallID {
		t.Fatalf("CallID: expected %q, got %q", session.CallID, got.CallID)
	}
	if got.FromTag != session.FromTag {
		t.Fatalf("FromTag: expected %q, got %q", session.FromTag, got.FromTag)
	}

	// Verify a non-existent session returns nil.
	if qm.GetSession("nonexistent", "nonexistent") != nil {
		t.Fatal("GetSession returned non-nil for non-existent session")
	}
}

// ---------------------------------------------------------------------------
// Rx QoS remove session
// ---------------------------------------------------------------------------

// TestE2E_Rx_QoSRemoveSession verifies that a session can be revoked
// (the closest equivalent to removal in the QoSModule API) and is
// subsequently marked as revoked.
func TestE2E_Rx_QoSRemoveSession(t *testing.T) {
	qm := ims_qos.NewQoSModule()

	msg := rxBuildINVITE("rx-remove-callid-1", "rx-remove-tag-1")
	session, err := qm.Authorize(msg)
	if err != nil {
		t.Fatalf("Authorize failed: %v", err)
	}

	// QoSModule has no RemoveSession; RevokeSession is the closest
	// equivalent and marks the session as revoked.
	if err := qm.RevokeSession(session.CallID, session.FromTag); err != nil {
		t.Fatalf("RevokeSession failed: %v", err)
	}

	got := qm.GetSession(session.CallID, session.FromTag)
	if got == nil {
		t.Fatal("session disappeared after revoke")
	}
	if got.Status != ims_qos.StatusRevoked {
		t.Fatalf("Status: expected %q, got %q", ims_qos.StatusRevoked, got.Status)
	}
}

// ---------------------------------------------------------------------------
// Rx QoS list sessions
// ---------------------------------------------------------------------------

// TestE2E_Rx_QoSListSessions verifies that multiple authorised sessions
// are all returned by List.
func TestE2E_Rx_QoSListSessions(t *testing.T) {
	qm := ims_qos.NewQoSModule()

	for i := 0; i < 3; i++ {
		msg := rxBuildINVITE(
			fmt.Sprintf("rx-list-callid-%d", i),
			fmt.Sprintf("rx-list-tag-%d", i),
		)
		if _, err := qm.Authorize(msg); err != nil {
			t.Fatalf("Authorize[%d] failed: %v", i, err)
		}
	}

	list := qm.List()
	if len(list) != 3 {
		t.Fatalf("List count: expected 3, got %d", len(list))
	}
	if qm.Count() != 3 {
		t.Fatalf("Count: expected 3, got %d", qm.Count())
	}
}

// ---------------------------------------------------------------------------
// Rx QoS concurrent authorisation
// ---------------------------------------------------------------------------

// TestE2E_Rx_QoSConcurrent verifies that concurrent Authorize calls are
// handled safely and all sessions are created.
func TestE2E_Rx_QoSConcurrent(t *testing.T) {
	qm := ims_qos.NewQoSModule()

	const n = 50
	var wg sync.WaitGroup
	errCh := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			msg := rxBuildINVITE(
				fmt.Sprintf("rx-conc-callid-%d", idx),
				fmt.Sprintf("rx-conc-tag-%d", idx),
			)
			session, err := qm.Authorize(msg)
			if err != nil {
				errCh <- fmt.Errorf("goroutine %d: Authorize: %w", idx, err)
				return
			}
			if session.Status != ims_qos.StatusAuthorized {
				errCh <- fmt.Errorf("goroutine %d: Status %q", idx, session.Status)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}

	if qm.Count() != n {
		t.Fatalf("Count after concurrent authorise: expected %d, got %d", n, qm.Count())
	}
}

// ---------------------------------------------------------------------------
// Ro charging session (TS 32.299)
// ---------------------------------------------------------------------------

// TestE2E_Rx_ChargingSession verifies the Ro charging session lifecycle:
// a session is started (Active), then terminated (Terminated).
func TestE2E_Rx_ChargingSession(t *testing.T) {
	cm := ims_charging.NewChargingModule()

	msg := rxBuildINVITE("rx-charge-callid-1", "rx-charge-tag-1")
	session := cm.StartSession(msg, "alice@ims.example.com", ims_charging.DirectionMO)
	if session == nil {
		t.Fatal("StartSession returned nil")
	}

	if session.Status != ims_charging.StatusActive {
		t.Fatalf("Status: expected %q, got %q", ims_charging.StatusActive, session.Status)
	}
	if session.Subscriber != "alice@ims.example.com" {
		t.Fatalf("Subscriber: expected alice@ims.example.com, got %q", session.Subscriber)
	}
	if session.Direction != ims_charging.DirectionMO {
		t.Fatalf("Direction: expected %q, got %q", ims_charging.DirectionMO, session.Direction)
	}
	if session.ChargingID == "" {
		t.Fatal("ChargingID is empty")
	}

	// Terminate the session.
	if err := cm.EndSession(session.CallID, session.FromTag); err != nil {
		t.Fatalf("EndSession failed: %v", err)
	}

	got := cm.GetSession(session.CallID, session.FromTag)
	if got == nil {
		t.Fatal("session not found after EndSession")
	}
	if got.Status != ims_charging.StatusTerminated {
		t.Fatalf("Status: expected %q, got %q", ims_charging.StatusTerminated, got.Status)
	}
}

// ---------------------------------------------------------------------------
// OCS quota management (TS 32.299)
// ---------------------------------------------------------------------------

// TestE2E_Rx_OCSQuotaManagement verifies the OCS quota lifecycle: units
// are requested, usage is recorded, and the granted/used counters track
// correctly.
func TestE2E_Rx_OCSQuotaManagement(t *testing.T) {
	om := ims_ocs.NewOCSModule()

	// Request quota for a subscriber/service pair.
	session, err := om.RequestUnits("alice@ims.example.com", "voice", 1000)
	if err != nil {
		t.Fatalf("RequestUnits failed: %v", err)
	}

	if session.GrantedUnits != 1000 {
		t.Fatalf("GrantedUnits: expected 1000, got %d", session.GrantedUnits)
	}
	if session.UsedUnits != 0 {
		t.Fatalf("UsedUnits: expected 0, got %d", session.UsedUnits)
	}
	if session.Status != ims_ocs.StatusActive {
		t.Fatalf("Status: expected %q, got %q", ims_ocs.StatusActive, session.Status)
	}

	// Record usage.
	if err := om.UpdateUsage(session.SessionID, 400); err != nil {
		t.Fatalf("UpdateUsage failed: %v", err)
	}

	got := om.GetSession(session.SessionID)
	if got == nil {
		t.Fatal("GetSession returned nil")
	}
	if got.UsedUnits != 400 {
		t.Fatalf("UsedUnits: expected 400, got %d", got.UsedUnits)
	}
	if got.Status != ims_ocs.StatusActive {
		t.Fatalf("Status after partial usage: expected %q, got %q", ims_ocs.StatusActive, got.Status)
	}

	// Exhaust the quota.
	if err := om.UpdateUsage(session.SessionID, 600); err != nil {
		t.Fatalf("UpdateUsage (exhaust) failed: %v", err)
	}
	got = om.GetSession(session.SessionID)
	if got.Status != ims_ocs.StatusExhausted {
		t.Fatalf("Status after exhaustion: expected %q, got %q", ims_ocs.StatusExhausted, got.Status)
	}

	// Terminate the session.
	if err := om.Terminate(session.SessionID); err != nil {
		t.Fatalf("Terminate failed: %v", err)
	}
	got = om.GetSession(session.SessionID)
	if got.Status != ims_ocs.StatusTerminated {
		t.Fatalf("Status after terminate: expected %q, got %q", ims_ocs.StatusTerminated, got.Status)
	}
}
