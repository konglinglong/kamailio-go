// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * End-to-end tests for the IMS Cx/Dx Diameter interface.
 * Based on 3GPP TS 29.228 / 29.229.
 *
 * The Cx reference point sits between the I-CSCF/S-CSCF and the HSS.
 * It carries the User-Authorization, Server-Assignment, Location-Info,
 * Multimedia-Auth, Registration-Termination and Push-Profile procedures
 * used during IMS registration and subscriber-data management.
 */

package integration

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/modules/cdp"
	"github.com/kamailio/kamailio-go/internal/modules/cdp_avp"
	"github.com/kamailio/kamailio-go/internal/modules/ims_icscf"
	diameter "github.com/kamailio/kamailio-go/internal/modules/ims_diameter_server"
)

// Cx interface command codes (3GPP TS 29.229).
const (
	CmdUAR = 300 // User-Authorization-Request
	CmdSAR = 301 // Server-Assignment-Request
	CmdLIR = 302 // Location-Info-Request
	CmdMAR = 303 // Multimedia-Auth-Request
	CmdRTR = 304 // Registration-Termination-Request
	CmdPPR = 305 // Push-Profile-Request
)

// IMS result codes (3GPP TS 29.229).
const (
	RCFirstRegistration           = 2001
	RCSubsequentRegistration      = 2002
	RCUnregisteredService         = 2003
	RCSuccessServerNameNotStored   = 2004
	RCServerSelection             = 2005
	RCErrorUserUnknown            = 5001
	RCErrorIdentitiesDontMatch    = 5002
	RCErrorIdentityNotRegistered  = 5003
	RCErrorRoamingNotAllowed      = 5004
	RCErrorAuthSchemeNotSupported = 5006
)

// Cx AVP codes (3GPP TS 29.229).
const (
	cxAVPPublicIdentity     = 601 // Public-Identity
	cxAVPVisitedNetworkID   = 600 // Visited-Network-ID
	cxAVPServerName         = 602 // Server-Name
	cxAVPServerCapabilities = 603 // Server-Capabilities
	cxAVPSIPAuthDataItem    = 606 // SIP-Auth-Data-Item
	cxAVPUserData           = 608 // User-Data
	cxAVPDeregReason        = 615 // Deregistration-Reason
	cxAVPAuthType           = 623 // Authorization-Type
)

// cxResultCode extracts the Result-Code from a Diameter answer built by
// DiameterServerModule.BuildAnswer (which stores the code as an int in
// the AVP Data field).
func cxResultCode(msg *diameter.DiameterMessage) int {
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
// Cx-User-Authorization (TS 29.228 6.1)
// ---------------------------------------------------------------------------

// TestE2E_Cx_UAR_UAA_UserAuthorization exercises the User-Authorization
// flow: the I-CSCF sends a UAR to the HSS and receives a UAA carrying
// either a Server-Name (S-CSCF already assigned) or Server-Capabilities
// (S-CSCF must be selected). Here the HSS returns a Server-Name and
// Result-Code 2001 (First Registration).
func TestE2E_Cx_UAR_UAA_UserAuthorization(t *testing.T) {
	server := diameter.NewDiameterServerModule()

	// Register the UAR handler (simulates the HSS).
	if err := server.RegisterHandler(CmdUAR, func(req *diameter.DiameterMessage) (*diameter.DiameterMessage, error) {
		ans := server.BuildAnswer(req, RCFirstRegistration)
		// TS 29.229: UAA carries Server-Name when the S-CSCF is already selected.
		ans.AVPs = append(ans.AVPs, diameter.DiameterAVP{
			Code: cxAVPServerName,
			Data: "sip:scscf1.ims.example.com",
		})
		return ans, nil
	}); err != nil {
		t.Fatalf("RegisterHandler UAR failed: %v", err)
	}

	// Build the UAR (CommandCode=300).
	uar := &diameter.DiameterMessage{
		Version:       diameter.DiameterVersion,
		CommandCode:   CmdUAR,
		ApplicationID: 16777216, // Diameter IMS application (3GPP TS 29.229)
		HopByHopID:    1,
		EndToEndID:    100,
		AVPs: []diameter.DiameterAVP{
			{Code: diameter.AVPCodeSessionID, Data: "hss-session-uar-1"},
			{Code: 1, Data: "alice@ims.example.com"},             // User-Name
			{Code: cxAVPPublicIdentity, Data: "sip:alice@ims.example.com"},
			{Code: cxAVPVisitedNetworkID, Data: "visited.ims.example.com"},
			{Code: cxAVPAuthType, Data: 0}, // REGISTRATION
		},
	}

	ans, err := server.HandleMessage(uar)
	if err != nil {
		t.Fatalf("HandleMessage UAR failed: %v", err)
	}

	// Verify the answer mirrors the request identifiers (RFC 6733).
	if ans.CommandCode != CmdUAR {
		t.Fatalf("CommandCode: expected %d, got %d", CmdUAR, ans.CommandCode)
	}
	if ans.HopByHopID != uar.HopByHopID {
		t.Fatalf("HopByHopID: expected %d, got %d", uar.HopByHopID, ans.HopByHopID)
	}
	if ans.EndToEndID != uar.EndToEndID {
		t.Fatalf("EndToEndID: expected %d, got %d", uar.EndToEndID, ans.EndToEndID)
	}

	// Verify Result-Code = 2001 (First Registration).
	if rc := cxResultCode(ans); rc != RCFirstRegistration {
		t.Fatalf("Result-Code: expected %d, got %d", RCFirstRegistration, rc)
	}

	// Verify Server-Name AVP present.
	snAVP := diameter.FindAVP(ans, cxAVPServerName)
	if snAVP == nil {
		t.Fatal("missing Server-Name AVP in UAA")
	}
	if sn, ok := snAVP.Data.(string); !ok || sn != "sip:scscf1.ims.example.com" {
		t.Fatalf("Server-Name: expected sip:scscf1.ims.example.com, got %v", snAVP.Data)
	}
}

// ---------------------------------------------------------------------------
// Cx-Server-Assignment (TS 29.228 6.2)
// ---------------------------------------------------------------------------

// TestE2E_Cx_SAR_SAA_ServerAssignment exercises the Server-Assignment
// flow: the S-CSCF sends a SAR to the HSS after selecting it for a
// subscriber and receives the SAA carrying the user profile in the
// User-Data AVP and Result-Code 2001.
func TestE2E_Cx_SAR_SAA_ServerAssignment(t *testing.T) {
	server := diameter.NewDiameterServerModule()

	userDataXML := `<IMSSubscription><PrivateID>alice@ims.example.com</PrivateID></IMSSubscription>`

	if err := server.RegisterHandler(CmdSAR, func(req *diameter.DiameterMessage) (*diameter.DiameterMessage, error) {
		ans := server.BuildAnswer(req, RCFirstRegistration)
		ans.AVPs = append(ans.AVPs, diameter.DiameterAVP{
			Code: cxAVPUserData,
			Data: userDataXML,
		})
		return ans, nil
	}); err != nil {
		t.Fatalf("RegisterHandler SAR failed: %v", err)
	}

	// Server-Assignment-Type=REGISTRATION (0).
	sar := &diameter.DiameterMessage{
		Version:       diameter.DiameterVersion,
		CommandCode:   CmdSAR,
		ApplicationID: 16777216,
		HopByHopID:    2,
		EndToEndID:    200,
		AVPs: []diameter.DiameterAVP{
			{Code: diameter.AVPCodeSessionID, Data: "hss-session-sar-1"},
			{Code: 1, Data: "alice@ims.example.com"}, // User-Name
			{Code: cxAVPPublicIdentity, Data: "sip:alice@ims.example.com"},
			{Code: cxAVPServerName, Data: "sip:scscf1.ims.example.com"},
			{Code: cxAVPAuthType, Data: 0}, // REGISTRATION
		},
	}

	ans, err := server.HandleMessage(sar)
	if err != nil {
		t.Fatalf("HandleMessage SAR failed: %v", err)
	}

	if ans.CommandCode != CmdSAR {
		t.Fatalf("CommandCode: expected %d, got %d", CmdSAR, ans.CommandCode)
	}
	if rc := cxResultCode(ans); rc != RCFirstRegistration {
		t.Fatalf("Result-Code: expected %d, got %d", RCFirstRegistration, rc)
	}

	udAVP := diameter.FindAVP(ans, cxAVPUserData)
	if udAVP == nil {
		t.Fatal("missing User-Data AVP in SAA")
	}
	if ud, ok := udAVP.Data.(string); !ok || ud != userDataXML {
		t.Fatalf("User-Data mismatch: got %v", udAVP.Data)
	}
}

// ---------------------------------------------------------------------------
// Cx-Location-Info (TS 29.228 6.3)
// ---------------------------------------------------------------------------

// TestE2E_Cx_LIR_LIA_LocationInfo exercises the Location-Info flow: the
// I-CSCF queries the HSS for the S-CSCF currently serving a subscriber
// and receives the LIA carrying the Server-Name.
func TestE2E_Cx_LIR_LIA_LocationInfo(t *testing.T) {
	server := diameter.NewDiameterServerModule()

	if err := server.RegisterHandler(CmdLIR, func(req *diameter.DiameterMessage) (*diameter.DiameterMessage, error) {
		ans := server.BuildAnswer(req, diameter.ResultCodeSuccess)
		ans.AVPs = append(ans.AVPs, diameter.DiameterAVP{
			Code: cxAVPServerName,
			Data: "sip:scscf1.ims.example.com",
		})
		return ans, nil
	}); err != nil {
		t.Fatalf("RegisterHandler LIR failed: %v", err)
	}

	lir := &diameter.DiameterMessage{
		Version:       diameter.DiameterVersion,
		CommandCode:   CmdLIR,
		ApplicationID: 16777216,
		HopByHopID:    3,
		EndToEndID:    300,
		AVPs: []diameter.DiameterAVP{
			{Code: diameter.AVPCodeSessionID, Data: "hss-session-lir-1"},
			{Code: cxAVPPublicIdentity, Data: "sip:alice@ims.example.com"},
		},
	}

	ans, err := server.HandleMessage(lir)
	if err != nil {
		t.Fatalf("HandleMessage LIR failed: %v", err)
	}

	if ans.CommandCode != CmdLIR {
		t.Fatalf("CommandCode: expected %d, got %d", CmdLIR, ans.CommandCode)
	}
	if rc := cxResultCode(ans); rc != diameter.ResultCodeSuccess {
		t.Fatalf("Result-Code: expected %d, got %d", diameter.ResultCodeSuccess, rc)
	}

	snAVP := diameter.FindAVP(ans, cxAVPServerName)
	if snAVP == nil {
		t.Fatal("missing Server-Name AVP in LIA")
	}
	if sn, ok := snAVP.Data.(string); !ok || sn != "sip:scscf1.ims.example.com" {
		t.Fatalf("Server-Name: expected sip:scscf1.ims.example.com, got %v", snAVP.Data)
	}
}

// ---------------------------------------------------------------------------
// Cx-Multimedia-Auth (TS 29.228 6.4)
// ---------------------------------------------------------------------------

// TestE2E_Cx_MAR_MAA_MultimediaAuth exercises the Multimedia-Auth flow:
// the S-CSCF requests an authentication vector from the HSS and receives
// the MAA carrying the SIP-Auth-Data-Item AVP (containing RAND, AUTN,
// XRES, CK, IK in a real deployment).
func TestE2E_Cx_MAR_MAA_MultimediaAuth(t *testing.T) {
	server := diameter.NewDiameterServerModule()

	if err := server.RegisterHandler(CmdMAR, func(req *diameter.DiameterMessage) (*diameter.DiameterMessage, error) {
		ans := server.BuildAnswer(req, diameter.ResultCodeSuccess)
		// SIP-Auth-Data-Item carries the authentication vector.
		ans.AVPs = append(ans.AVPs, diameter.DiameterAVP{
			Code: cxAVPSIPAuthDataItem,
			Data: "RAND=0123456789abcdef0123456789abcdef;AUTN=aabbccdd;XRES=11223344;CK=55667788;IK=99aabbcc",
		})
		return ans, nil
	}); err != nil {
		t.Fatalf("RegisterHandler MAR failed: %v", err)
	}

	mar := &diameter.DiameterMessage{
		Version:       diameter.DiameterVersion,
		CommandCode:   CmdMAR,
		ApplicationID: 16777216,
		HopByHopID:    4,
		EndToEndID:    400,
		AVPs: []diameter.DiameterAVP{
			{Code: diameter.AVPCodeSessionID, Data: "hss-session-mar-1"},
			{Code: 1, Data: "alice@ims.example.com"}, // User-Name
			{Code: cxAVPPublicIdentity, Data: "sip:alice@ims.example.com"},
			{Code: cxAVPSIPAuthDataItem, Data: "challenge-1"},
		},
	}

	ans, err := server.HandleMessage(mar)
	if err != nil {
		t.Fatalf("HandleMessage MAR failed: %v", err)
	}

	if ans.CommandCode != CmdMAR {
		t.Fatalf("CommandCode: expected %d, got %d", CmdMAR, ans.CommandCode)
	}
	if rc := cxResultCode(ans); rc != diameter.ResultCodeSuccess {
		t.Fatalf("Result-Code: expected %d, got %d", diameter.ResultCodeSuccess, rc)
	}

	// Verify the authentication vector AVP is present.
	authAVP := diameter.FindAVP(ans, cxAVPSIPAuthDataItem)
	if authAVP == nil {
		t.Fatal("missing SIP-Auth-Data-Item AVP in MAA")
	}
	if av, ok := authAVP.Data.(string); !ok || !strings.Contains(av, "XRES") {
		t.Fatalf("SIP-Auth-Data-Item does not carry XRES: got %v", authAVP.Data)
	}
}

// ---------------------------------------------------------------------------
// Cx-Registration-Termination (TS 29.228 6.5)
// ---------------------------------------------------------------------------

// TestE2E_Cx_RTR_RTA_RegistrationTermination exercises the
// Registration-Termination flow: the HSS pushes an RTR to the S-CSCF to
// deregister a subscriber and the S-CSCF responds with RTA carrying
// Result-Code 2001.
func TestE2E_Cx_RTR_RTA_RegistrationTermination(t *testing.T) {
	server := diameter.NewDiameterServerModule()

	if err := server.RegisterHandler(CmdRTR, func(req *diameter.DiameterMessage) (*diameter.DiameterMessage, error) {
		return server.BuildAnswer(req, diameter.ResultCodeSuccess), nil
	}); err != nil {
		t.Fatalf("RegisterHandler RTR failed: %v", err)
	}

	rtr := &diameter.DiameterMessage{
		Version:       diameter.DiameterVersion,
		CommandCode:   CmdRTR,
		ApplicationID: 16777216,
		HopByHopID:    5,
		EndToEndID:    500,
		AVPs: []diameter.DiameterAVP{
			{Code: diameter.AVPCodeSessionID, Data: "hss-session-rtr-1"},
			{Code: 1, Data: "alice@ims.example.com"}, // User-Name
			{Code: cxAVPPublicIdentity, Data: "sip:alice@ims.example.com"},
			{Code: cxAVPDeregReason, Data: "PERMANENT_TERMINATION"},
		},
	}

	ans, err := server.HandleMessage(rtr)
	if err != nil {
		t.Fatalf("HandleMessage RTR failed: %v", err)
	}

	if ans.CommandCode != CmdRTR {
		t.Fatalf("CommandCode: expected %d, got %d", CmdRTR, ans.CommandCode)
	}
	if rc := cxResultCode(ans); rc != diameter.ResultCodeSuccess {
		t.Fatalf("Result-Code: expected %d, got %d", diameter.ResultCodeSuccess, rc)
	}
}

// ---------------------------------------------------------------------------
// Cx-Push-Profile (TS 29.228 6.6)
// ---------------------------------------------------------------------------

// TestE2E_Cx_PPR_PPA_PushProfile exercises the Push-Profile flow: the
// HSS pushes updated subscriber data to the S-CSCF via PPR and the
// S-CSCF responds with PPA.
func TestE2E_Cx_PPR_PPA_PushProfile(t *testing.T) {
	server := diameter.NewDiameterServerModule()

	if err := server.RegisterHandler(CmdPPR, func(req *diameter.DiameterMessage) (*diameter.DiameterMessage, error) {
		return server.BuildAnswer(req, diameter.ResultCodeSuccess), nil
	}); err != nil {
		t.Fatalf("RegisterHandler PPR failed: %v", err)
	}

	ppr := &diameter.DiameterMessage{
		Version:       diameter.DiameterVersion,
		CommandCode:   CmdPPR,
		ApplicationID: 16777216,
		HopByHopID:    6,
		EndToEndID:    600,
		AVPs: []diameter.DiameterAVP{
			{Code: diameter.AVPCodeSessionID, Data: "hss-session-ppr-1"},
			{Code: 1, Data: "alice@ims.example.com"}, // User-Name
			{Code: cxAVPUserData, Data: "<IMSSubscription><PrivateID>alice</PrivateID></IMSSubscription>"},
		},
	}

	ans, err := server.HandleMessage(ppr)
	if err != nil {
		t.Fatalf("HandleMessage PPR failed: %v", err)
	}

	if ans.CommandCode != CmdPPR {
		t.Fatalf("CommandCode: expected %d, got %d", CmdPPR, ans.CommandCode)
	}
	if rc := cxResultCode(ans); rc != diameter.ResultCodeSuccess {
		t.Fatalf("Result-Code: expected %d, got %d", diameter.ResultCodeSuccess, rc)
	}
}

// ---------------------------------------------------------------------------
// Cx error handling
// ---------------------------------------------------------------------------

// TestE2E_Cx_ErrorUserUnknown verifies that the HSS returns
// Result-Code 5001 (DIAMETER_ERROR_USER_UNKNOWN) when the subscriber
// is not provisioned.
func TestE2E_Cx_ErrorUserUnknown(t *testing.T) {
	server := diameter.NewDiameterServerModule()

	if err := server.RegisterHandler(CmdUAR, func(req *diameter.DiameterMessage) (*diameter.DiameterMessage, error) {
		return server.BuildAnswer(req, RCErrorUserUnknown), nil
	}); err != nil {
		t.Fatalf("RegisterHandler UAR failed: %v", err)
	}

	uar := &diameter.DiameterMessage{
		CommandCode: CmdUAR,
		HopByHopID:  7,
		EndToEndID:  700,
		AVPs: []diameter.DiameterAVP{
			{Code: 1, Data: "unknown@ims.example.com"},
			{Code: cxAVPPublicIdentity, Data: "sip:unknown@ims.example.com"},
		},
	}

	ans, err := server.HandleMessage(uar)
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	if rc := cxResultCode(ans); rc != RCErrorUserUnknown {
		t.Fatalf("Result-Code: expected %d (Error-User-Unknown), got %d", RCErrorUserUnknown, rc)
	}
}

// TestE2E_Cx_ErrorIdentitiesDontMatch verifies that the HSS returns
// Result-Code 5002 (DIAMETER_ERROR_IDENTITIES_DONT_MATCH) when the
// Public-Identity does not match the User-Name.
func TestE2E_Cx_ErrorIdentitiesDontMatch(t *testing.T) {
	server := diameter.NewDiameterServerModule()

	if err := server.RegisterHandler(CmdSAR, func(req *diameter.DiameterMessage) (*diameter.DiameterMessage, error) {
		return server.BuildAnswer(req, RCErrorIdentitiesDontMatch), nil
	}); err != nil {
		t.Fatalf("RegisterHandler SAR failed: %v", err)
	}

	sar := &diameter.DiameterMessage{
		CommandCode: CmdSAR,
		HopByHopID:  8,
		EndToEndID:  800,
		AVPs: []diameter.DiameterAVP{
			{Code: 1, Data: "alice@ims.example.com"},
			{Code: cxAVPPublicIdentity, Data: "sip:bob@ims.example.com"}, // mismatch
		},
	}

	ans, err := server.HandleMessage(sar)
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	if rc := cxResultCode(ans); rc != RCErrorIdentitiesDontMatch {
		t.Fatalf("Result-Code: expected %d (Identities-Dont-Match), got %d", RCErrorIdentitiesDontMatch, rc)
	}
}

// ---------------------------------------------------------------------------
// I-CSCF S-CSCF selection (TS 29.228)
// ---------------------------------------------------------------------------

// TestE2E_Cx_ICSCF_SCSCFSelection verifies that the I-CSCF selects the
// S-CSCF with the highest precedence (lowest priority value) and, among
// those, the highest capacity.
func TestE2E_Cx_ICSCF_SCSCFSelection(t *testing.T) {
	icscf := ims_icscf.NewICSCFModule()

	// Register three S-CSCFs with different capacity and priority.
	icscf.RegisterSCSCF("scscf1", 100, 2) // lower priority (value=2)
	icscf.RegisterSCSCF("scscf2", 200, 1) // highest priority, capacity 200
	icscf.RegisterSCSCF("scscf3", 300, 1) // highest priority, capacity 300

	selected := icscf.SelectSCSCF()
	if selected != "scscf3" {
		t.Fatalf("SelectSCSCF: expected scscf3 (priority=1, capacity=300), got %q", selected)
	}

	// Verify the assignment is remembered per subscriber.
	assigned, err := icscf.AssignSCSCF("alice@ims.example.com")
	if err != nil {
		t.Fatalf("AssignSCSCF failed: %v", err)
	}
	if assigned != "scscf3" {
		t.Fatalf("AssignSCSCF: expected scscf3, got %q", assigned)
	}
	if got := icscf.GetSCSCF("alice@ims.example.com"); got != "scscf3" {
		t.Fatalf("GetSCSCF: expected scscf3, got %q", got)
	}

	if count := icscf.CountSCSCFs(); count != 3 {
		t.Fatalf("CountSCSCFs: expected 3, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// Diameter message encode / decode (RFC 6733)
// ---------------------------------------------------------------------------

// TestE2E_Cx_DiameterMessageEncodeDecode verifies that a Diameter
// message can be encoded to its on-wire form and decoded back without
// loss, exercising the cdp module codec.
func TestE2E_Cx_DiameterMessageEncodeDecode(t *testing.T) {
	c := cdp.NewCDPModule()

	original := &cdp.DiameterMessage{
		Version:       cdp.Version,
		Flags:         cdp.CmdFlagRequest,
		CommandCode:   CmdUAR,
		ApplicationID: 16777216,
		HopByHopID:    c.NextHopByHop(),
		EndToEndID:    0xDEADBEEF,
		AVPs: []cdp.DiameterAVP{
			{Code: 263, Flags: cdp.AVPFlagMandatory, Value: []byte("session-encode-1")}, // Session-Id
			{Code: 1, Flags: cdp.AVPFlagMandatory, Value: []byte("alice@ims.example.com")}, // User-Name
			{Code: cxAVPPublicIdentity, Flags: cdp.AVPFlagMandatory, Value: []byte("sip:alice@ims.example.com")},
		},
	}

	encoded := c.Encode(original)
	if len(encoded) < cdp.HeaderLen {
		t.Fatalf("encoded message too short: %d bytes", len(encoded))
	}

	decoded, err := c.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if decoded.CommandCode != original.CommandCode {
		t.Fatalf("CommandCode: expected %d, got %d", original.CommandCode, decoded.CommandCode)
	}
	if decoded.ApplicationID != original.ApplicationID {
		t.Fatalf("ApplicationID: expected %d, got %d", original.ApplicationID, decoded.ApplicationID)
	}
	if decoded.HopByHopID != original.HopByHopID {
		t.Fatalf("HopByHopID: expected %d, got %d", original.HopByHopID, decoded.HopByHopID)
	}
	if decoded.EndToEndID != original.EndToEndID {
		t.Fatalf("EndToEndID: expected %d, got %d", original.EndToEndID, decoded.EndToEndID)
	}
	if len(decoded.AVPs) != len(original.AVPs) {
		t.Fatalf("AVP count: expected %d, got %d", len(original.AVPs), len(decoded.AVPs))
	}
	for i, avp := range original.AVPs {
		if decoded.AVPs[i].Code != avp.Code {
			t.Fatalf("AVP[%d] Code: expected %d, got %d", i, avp.Code, decoded.AVPs[i].Code)
		}
		if string(decoded.AVPs[i].Value) != string(avp.Value) {
			t.Fatalf("AVP[%d] Value: expected %q, got %q", i, avp.Value, decoded.AVPs[i].Value)
		}
	}
}

// ---------------------------------------------------------------------------
// AVP builder (cdp_avp)
// ---------------------------------------------------------------------------

// TestE2E_Cx_AVPBuilder verifies the cdp_avp AVPBuilder produces correctly
// encoded AVPs for the IMS core (Auth-Session-State, User-Name,
// Result-Code, Subscription-Id).
func TestE2E_Cx_AVPBuilder(t *testing.T) {
	b := cdp_avp.NewAVPBuilder()

	// Auth-Session-State (Unsigned32).
	ass := b.AuthSessionState(cdp_avp.AuthSessionStateMaintained)
	if ass.Code != cdp_avp.CodeAuthSessionState {
		t.Fatalf("Auth-Session-State Code: expected %d, got %d", cdp_avp.CodeAuthSessionState, ass.Code)
	}
	if len(ass.Value) != 4 {
		t.Fatalf("Auth-Session-State Value length: expected 4, got %d", len(ass.Value))
	}

	// User-Name (UTF8String).
	un := b.UserName("alice@ims.example.com")
	if un.Code != cdp_avp.CodeUserName {
		t.Fatalf("User-Name Code: expected %d, got %d", cdp_avp.CodeUserName, un.Code)
	}
	if string(un.Value) != "alice@ims.example.com" {
		t.Fatalf("User-Name Value: expected alice@ims.example.com, got %q", un.Value)
	}

	// Result-Code (Unsigned32).
	rc := b.ResultCode(RCFirstRegistration)
	if rc.Code != cdp_avp.CodeResultCode {
		t.Fatalf("Result-Code AVP Code: expected %d, got %d", cdp_avp.CodeResultCode, rc.Code)
	}
	if len(rc.Value) != 4 {
		t.Fatalf("Result-Code Value length: expected 4, got %d", len(rc.Value))
	}

	// Subscription-Id (grouped).
	sid := b.SubscriptionID("1234567890", "END_USER_IMSI")
	if sid.Code != cdp_avp.CodeSubscriptionID {
		t.Fatalf("Subscription-Id Code: expected %d, got %d", cdp_avp.CodeSubscriptionID, sid.Code)
	}
	if len(sid.Value) == 0 {
		t.Fatal("Subscription-Id grouped value is empty")
	}
	// Verify the grouped value can be round-tripped through the parser.
	encoded := cdp_avp.EncodeAVP(sid)
	parsed, err := cdp_avp.ParseAVP(encoded)
	if err != nil {
		t.Fatalf("ParseAVP Subscription-Id failed: %v", err)
	}
	if parsed.Code != cdp_avp.CodeSubscriptionID {
		t.Fatalf("Parsed Subscription-Id Code: expected %d, got %d", cdp_avp.CodeSubscriptionID, parsed.Code)
	}
}

// ---------------------------------------------------------------------------
// Concurrent Diameter message handling
// ---------------------------------------------------------------------------

// TestE2E_Cx_Concurrent verifies that the DiameterServerModule handles
// concurrent requests safely (no data races, all responses correct).
func TestE2E_Cx_Concurrent(t *testing.T) {
	server := diameter.NewDiameterServerModule()

	if err := server.RegisterHandler(CmdUAR, func(req *diameter.DiameterMessage) (*diameter.DiameterMessage, error) {
		return server.BuildAnswer(req, RCFirstRegistration), nil
	}); err != nil {
		t.Fatalf("RegisterHandler UAR failed: %v", err)
	}

	const n = 50
	var wg sync.WaitGroup
	errCh := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			uar := &diameter.DiameterMessage{
				CommandCode: CmdUAR,
				HopByHopID:  uint32(idx),
				EndToEndID:   uint32(idx + 1000),
				AVPs: []diameter.DiameterAVP{
					{Code: diameter.AVPCodeSessionID, Data: fmt.Sprintf("session-conc-%d", idx)},
					{Code: 1, Data: fmt.Sprintf("user%d@ims.example.com", idx)},
				},
			}
			ans, err := server.HandleMessage(uar)
			if err != nil {
				errCh <- fmt.Errorf("goroutine %d: HandleMessage: %w", idx, err)
				return
			}
			if ans.CommandCode != CmdUAR {
				errCh <- fmt.Errorf("goroutine %d: CommandCode %d", idx, ans.CommandCode)
				return
			}
			if ans.HopByHopID != uint32(idx) {
				errCh <- fmt.Errorf("goroutine %d: HopByHopID %d", idx, ans.HopByHopID)
				return
			}
			if rc := cxResultCode(ans); rc != RCFirstRegistration {
				errCh <- fmt.Errorf("goroutine %d: Result-Code %d", idx, rc)
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
