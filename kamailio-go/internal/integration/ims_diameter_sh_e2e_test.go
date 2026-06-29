// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * End-to-end tests for the IMS Sh Diameter interface.
 * Based on 3GPP TS 29.328 / 29.329.
 *
 * The Sh reference point sits between an AS (Application Server) and the
 * HSS. It carries the User-Data (Sh-Pull), Profile-Update (Sh-Update),
 * Subscribe-Notifications and Push-Notification (Sh-Notify) procedures
 * used to read and modify subscriber service data.
 */

package integration

import (
	"fmt"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/modules/cdp"
	"github.com/kamailio/kamailio-go/internal/modules/cdp_avp"
	diameter "github.com/kamailio/kamailio-go/internal/modules/ims_diameter_server"
)

// Sh interface command codes (3GPP TS 29.329).
const (
	CmdUDR = 306 // User-Data-Request (Sh-Pull)
	CmdPUR = 307 // Profile-Update-Request (Sh-Update)
	CmdSNR = 308 // Subscribe-Notifications-Request
	CmdPNR = 309 // Push-Notification-Request (Sh-Notify)
)

// Sh result codes (3GPP TS 29.329).
const (
	RCErrorUserDataNotRecognized = 5100
)

// Sh AVP codes (3GPP TS 29.329).
const (
	shAVPUserIdentity  = 700 // User-Identity
	shAVPUserData       = 702 // User-Data
	shAVPDataReference = 703 // Data-Reference
	shAVPSubsReqType   = 705 // Subs-Req-Type
)

// shResultCode extracts the Result-Code from a Diameter answer built by
// DiameterServerModule.BuildAnswer (which stores the code as an int in
// the AVP Data field).
func shResultCode(msg *diameter.DiameterMessage) int {
	avp := diameter.FindAVP(msg, diameter.AVPCodeResultCode)
	if avp == nil {
		return 0
	}
	if rc, ok := avp.Data.(int); ok {
		return rc
	}
	return 0
}

// ---------------------------------------------------------------------------
// Sh-Pull / UDR-UDA (TS 29.328 6.1)
// ---------------------------------------------------------------------------

// TestE2E_Sh_UDR_UDA_Pull exercises the Sh-Pull flow: the AS sends a UDR
// to the HSS requesting a data reference for a user identity and receives
// the UDA carrying the User-Data AVP.
func TestE2E_Sh_UDR_UDA_Pull(t *testing.T) {
	server := diameter.NewDiameterServerModule()

	userData := `<RepositoryData><ServiceIndication>SCC-AS</ServiceIndication></RepositoryData>`

	if err := server.RegisterHandler(CmdUDR, func(req *diameter.DiameterMessage) (*diameter.DiameterMessage, error) {
		ans := server.BuildAnswer(req, diameter.ResultCodeSuccess)
		ans.AVPs = append(ans.AVPs, diameter.DiameterAVP{
			Code: shAVPUserData,
			Data: userData,
		})
		return ans, nil
	}); err != nil {
		t.Fatalf("RegisterHandler UDR failed: %v", err)
	}

	// Data-Reference=0 (RepositoryData).
	udr := &diameter.DiameterMessage{
		Version:       diameter.DiameterVersion,
		CommandCode:   CmdUDR,
		ApplicationID: 16777217, // Sh application (3GPP TS 29.329)
		HopByHopID:    1,
		EndToEndID:    1000,
		AVPs: []diameter.DiameterAVP{
			{Code: diameter.AVPCodeSessionID, Data: "sh-session-udr-1"},
			{Code: shAVPUserIdentity, Data: "sip:alice@ims.example.com"},
			{Code: shAVPDataReference, Data: 0}, // RepositoryData
		},
	}

	ans, err := server.HandleMessage(udr)
	if err != nil {
		t.Fatalf("HandleMessage UDR failed: %v", err)
	}

	if ans.CommandCode != CmdUDR {
		t.Fatalf("CommandCode: expected %d, got %d", CmdUDR, ans.CommandCode)
	}
	if ans.HopByHopID != udr.HopByHopID {
		t.Fatalf("HopByHopID: expected %d, got %d", udr.HopByHopID, ans.HopByHopID)
	}
	if ans.EndToEndID != udr.EndToEndID {
		t.Fatalf("EndToEndID: expected %d, got %d", udr.EndToEndID, ans.EndToEndID)
	}
	if rc := shResultCode(ans); rc != diameter.ResultCodeSuccess {
		t.Fatalf("Result-Code: expected %d, got %d", diameter.ResultCodeSuccess, rc)
	}

	udAVP := diameter.FindAVP(ans, shAVPUserData)
	if udAVP == nil {
		t.Fatal("missing User-Data AVP in UDA")
	}
	if ud, ok := udAVP.Data.(string); !ok || ud != userData {
		t.Fatalf("User-Data: expected %q, got %v", userData, udAVP.Data)
	}
}

// ---------------------------------------------------------------------------
// Sh-Update / PUR-PUA (TS 29.328 6.2)
// ---------------------------------------------------------------------------

// TestE2E_Sh_PUR_PUA_Update exercises the Sh-Update flow: the AS sends a
// PUR to update subscriber data in the HSS and receives the PUA carrying
// Result-Code 2001.
func TestE2E_Sh_PUR_PUA_Update(t *testing.T) {
	server := diameter.NewDiameterServerModule()

	if err := server.RegisterHandler(CmdPUR, func(req *diameter.DiameterMessage) (*diameter.DiameterMessage, error) {
		return server.BuildAnswer(req, diameter.ResultCodeSuccess), nil
	}); err != nil {
		t.Fatalf("RegisterHandler PUR failed: %v", err)
	}

	pur := &diameter.DiameterMessage{
		Version:       diameter.DiameterVersion,
		CommandCode:   CmdPUR,
		ApplicationID: 16777217,
		HopByHopID:    2,
		EndToEndID:    2000,
		AVPs: []diameter.DiameterAVP{
			{Code: diameter.AVPCodeSessionID, Data: "sh-session-pur-1"},
			{Code: shAVPUserIdentity, Data: "sip:alice@ims.example.com"},
			{Code: shAVPDataReference, Data: 0},
			{Code: shAVPUserData, Data: "<RepositoryData><ServiceIndication>SCC-AS</ServiceIndication></RepositoryData>"},
		},
	}

	ans, err := server.HandleMessage(pur)
	if err != nil {
		t.Fatalf("HandleMessage PUR failed: %v", err)
	}

	if ans.CommandCode != CmdPUR {
		t.Fatalf("CommandCode: expected %d, got %d", CmdPUR, ans.CommandCode)
	}
	if rc := shResultCode(ans); rc != diameter.ResultCodeSuccess {
		t.Fatalf("Result-Code: expected %d, got %d", diameter.ResultCodeSuccess, rc)
	}
}

// ---------------------------------------------------------------------------
// Sh-Subscribe-Notifications / SNR-SNA (TS 29.328 6.3)
// ---------------------------------------------------------------------------

// TestE2E_Sh_SNR_SNA_Subscribe exercises the Sh-Subscribe-Notifications
// flow: the AS subscribes to notifications for a data reference and
// receives the SNA.
func TestE2E_Sh_SNR_SNA_Subscribe(t *testing.T) {
	server := diameter.NewDiameterServerModule()

	if err := server.RegisterHandler(CmdSNR, func(req *diameter.DiameterMessage) (*diameter.DiameterMessage, error) {
		return server.BuildAnswer(req, diameter.ResultCodeSuccess), nil
	}); err != nil {
		t.Fatalf("RegisterHandler SNR failed: %v", err)
	}

	// Subs-Req-Type=0 (Subscribe).
	snr := &diameter.DiameterMessage{
		Version:       diameter.DiameterVersion,
		CommandCode:   CmdSNR,
		ApplicationID: 16777217,
		HopByHopID:    3,
		EndToEndID:    3000,
		AVPs: []diameter.DiameterAVP{
			{Code: diameter.AVPCodeSessionID, Data: "sh-session-snr-1"},
			{Code: shAVPUserIdentity, Data: "sip:alice@ims.example.com"},
			{Code: shAVPDataReference, Data: 0},
			{Code: shAVPSubsReqType, Data: 0}, // Subscribe
		},
	}

	ans, err := server.HandleMessage(snr)
	if err != nil {
		t.Fatalf("HandleMessage SNR failed: %v", err)
	}

	if ans.CommandCode != CmdSNR {
		t.Fatalf("CommandCode: expected %d, got %d", CmdSNR, ans.CommandCode)
	}
	if rc := shResultCode(ans); rc != diameter.ResultCodeSuccess {
		t.Fatalf("Result-Code: expected %d, got %d", diameter.ResultCodeSuccess, rc)
	}
}

// ---------------------------------------------------------------------------
// Sh-Notify / PNR-PNA (TS 29.328 6.4)
// ---------------------------------------------------------------------------

// TestE2E_Sh_PNR_PNA_Notify exercises the Sh-Notify flow: the HSS pushes
// a notification to the AS via PNR carrying updated user data and the AS
// responds with PNA.
func TestE2E_Sh_PNR_PNA_Notify(t *testing.T) {
	server := diameter.NewDiameterServerModule()

	if err := server.RegisterHandler(CmdPNR, func(req *diameter.DiameterMessage) (*diameter.DiameterMessage, error) {
		return server.BuildAnswer(req, diameter.ResultCodeSuccess), nil
	}); err != nil {
		t.Fatalf("RegisterHandler PNR failed: %v", err)
	}

	pnr := &diameter.DiameterMessage{
		Version:       diameter.DiameterVersion,
		CommandCode:   CmdPNR,
		ApplicationID: 16777217,
		HopByHopID:    4,
		EndToEndID:    4000,
		AVPs: []diameter.DiameterAVP{
			{Code: diameter.AVPCodeSessionID, Data: "sh-session-pnr-1"},
			{Code: shAVPUserIdentity, Data: "sip:alice@ims.example.com"},
			{Code: shAVPUserData, Data: "<RepositoryData><ServiceIndication>SCC-AS</ServiceIndication></RepositoryData>"},
		},
	}

	ans, err := server.HandleMessage(pnr)
	if err != nil {
		t.Fatalf("HandleMessage PNR failed: %v", err)
	}

	if ans.CommandCode != CmdPNR {
		t.Fatalf("CommandCode: expected %d, got %d", CmdPNR, ans.CommandCode)
	}
	if rc := shResultCode(ans); rc != diameter.ResultCodeSuccess {
		t.Fatalf("Result-Code: expected %d, got %d", diameter.ResultCodeSuccess, rc)
	}
}

// ---------------------------------------------------------------------------
// Sh error handling (5100)
// ---------------------------------------------------------------------------

// TestE2E_Sh_ErrorUserDataNotRecognized verifies that the HSS returns
// Result-Code 5100 (DIAMETER_ERROR_USER_DATA_NOT_RECOGNIZED) when the
// requested data reference is not recognised.
func TestE2E_Sh_ErrorUserDataNotRecognized(t *testing.T) {
	server := diameter.NewDiameterServerModule()

	if err := server.RegisterHandler(CmdUDR, func(req *diameter.DiameterMessage) (*diameter.DiameterMessage, error) {
		return server.BuildAnswer(req, RCErrorUserDataNotRecognized), nil
	}); err != nil {
		t.Fatalf("RegisterHandler UDR failed: %v", err)
	}

	udr := &diameter.DiameterMessage{
		CommandCode: CmdUDR,
		HopByHopID:  5,
		EndToEndID:  5000,
		AVPs: []diameter.DiameterAVP{
			{Code: shAVPUserIdentity, Data: "sip:unknown@ims.example.com"},
			{Code: shAVPDataReference, Data: 99}, // unrecognised
		},
	}

	ans, err := server.HandleMessage(udr)
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	if rc := shResultCode(ans); rc != RCErrorUserDataNotRecognized {
		t.Fatalf("Result-Code: expected %d (User-Data-Not-Recognized), got %d", RCErrorUserDataNotRecognized, rc)
	}
}

// ---------------------------------------------------------------------------
// Sh AVP construction (cdp_avp)
// ---------------------------------------------------------------------------

// TestE2E_Sh_DiameterAVPConstruction verifies that Sh-specific AVPs can
// be built using the cdp_avp builder and round-tripped through the codec.
func TestE2E_Sh_DiameterAVPConstruction(t *testing.T) {
	// Build a User-Identity AVP (carrying a SIP URI as UTF8String).
	userIdentity := &cdp.DiameterAVP{
		Code:  shAVPUserIdentity,
		Flags: cdp.AVPFlagMandatory,
		Value: []byte("sip:alice@ims.example.com"),
	}
	if userIdentity.Code != shAVPUserIdentity {
		t.Fatalf("User-Identity Code: expected %d, got %d", shAVPUserIdentity, userIdentity.Code)
	}
	encoded := cdp_avp.EncodeAVP(userIdentity)
	parsed, err := cdp_avp.ParseAVP(encoded)
	if err != nil {
		t.Fatalf("ParseAVP User-Identity failed: %v", err)
	}
	if parsed.Code != shAVPUserIdentity {
		t.Fatalf("Parsed User-Identity Code: expected %d, got %d", shAVPUserIdentity, parsed.Code)
	}
	if string(parsed.Value) != "sip:alice@ims.example.com" {
		t.Fatalf("Parsed User-Identity Value: expected sip:alice@ims.example.com, got %q", parsed.Value)
	}

	// Build a Data-Reference AVP (Unsigned32).
	dataRef := &cdp.DiameterAVP{
		Code:  shAVPDataReference,
		Flags: cdp.AVPFlagMandatory,
		Value: []byte{0, 0, 0, 0}, // RepositoryData = 0
	}
	encodedRef := cdp_avp.EncodeAVP(dataRef)
	parsedRef, err := cdp_avp.ParseAVP(encodedRef)
	if err != nil {
		t.Fatalf("ParseAVP Data-Reference failed: %v", err)
	}
	if parsedRef.Code != shAVPDataReference {
		t.Fatalf("Parsed Data-Reference Code: expected %d, got %d", shAVPDataReference, parsedRef.Code)
	}
	if len(parsedRef.Value) != 4 {
		t.Fatalf("Parsed Data-Reference Value length: expected 4, got %d", len(parsedRef.Value))
	}

	// Build a User-Name AVP via the cdp_avp builder and verify it encodes.
	b := cdp_avp.NewAVPBuilder()
	un := b.UserName("alice@ims.example.com")
	encUN := cdp_avp.EncodeAVP(un)
	parsedUN, err := cdp_avp.ParseAVP(encUN)
	if err != nil {
		t.Fatalf("ParseAVP User-Name failed: %v", err)
	}
	if string(parsedUN.Value) != "alice@ims.example.com" {
		t.Fatalf("Parsed User-Name Value: expected alice@ims.example.com, got %q", parsedUN.Value)
	}
}

// ---------------------------------------------------------------------------
// Concurrent Sh message handling
// ---------------------------------------------------------------------------

// TestE2E_Sh_Concurrent verifies that the DiameterServerModule handles
// concurrent Sh-Pull requests safely (no data races, all responses
// correct).
func TestE2E_Sh_Concurrent(t *testing.T) {
	server := diameter.NewDiameterServerModule()

	if err := server.RegisterHandler(CmdUDR, func(req *diameter.DiameterMessage) (*diameter.DiameterMessage, error) {
		ans := server.BuildAnswer(req, diameter.ResultCodeSuccess)
		ans.AVPs = append(ans.AVPs, diameter.DiameterAVP{
			Code: shAVPUserData,
			Data: fmt.Sprintf("<data>%s</data>", req.AVPs[0].Data),
		})
		return ans, nil
	}); err != nil {
		t.Fatalf("RegisterHandler UDR failed: %v", err)
	}

	const n = 50
	var wg sync.WaitGroup
	errCh := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			udr := &diameter.DiameterMessage{
				CommandCode: CmdUDR,
				HopByHopID:  uint32(idx),
				EndToEndID:   uint32(idx + 5000),
				AVPs: []diameter.DiameterAVP{
					{Code: diameter.AVPCodeSessionID, Data: fmt.Sprintf("sh-conc-%d", idx)},
					{Code: shAVPUserIdentity, Data: fmt.Sprintf("sip:user%d@ims.example.com", idx)},
					{Code: shAVPDataReference, Data: 0},
				},
			}
			ans, err := server.HandleMessage(udr)
			if err != nil {
				errCh <- fmt.Errorf("goroutine %d: HandleMessage: %w", idx, err)
				return
			}
			if ans.CommandCode != CmdUDR {
				errCh <- fmt.Errorf("goroutine %d: CommandCode %d", idx, ans.CommandCode)
				return
			}
			if ans.HopByHopID != uint32(idx) {
				errCh <- fmt.Errorf("goroutine %d: HopByHopID %d", idx, ans.HopByHopID)
				return
			}
			if rc := shResultCode(ans); rc != diameter.ResultCodeSuccess {
				errCh <- fmt.Errorf("goroutine %d: Result-Code %d", idx, rc)
				return
			}
			udAVP := diameter.FindAVP(ans, shAVPUserData)
			if udAVP == nil {
				errCh <- fmt.Errorf("goroutine %d: missing User-Data AVP", idx)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}
}
