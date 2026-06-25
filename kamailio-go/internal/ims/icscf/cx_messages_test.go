// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the I-CSCF Cx UAR/LIR message builders and parsers.
 */

package icscf

import (
	"testing"

	"github.com/kamailio/kamailio-go/internal/modules/cdp"
)

func TestBuildUARMinimal(t *testing.T) {
	msg := BuildUAR(&UARRequest{
		DestinationRealm: "home.net",
		OriginHost:       "sip:icscf.home.net",
		OriginRealm:     "home.net",
		PublicIdentity:  "sip:user@home.net",
		PrivateIdentity: "user@home.net",
		VisitedNetworkID: "visited.net",
		AuthorizationType: AuthzRegistrationAndCapabilities,
		UARFlags:         UARFlagEmergencyRegistration,
	}, 1, 2)

	if msg.CommandCode != CmdCxUAR {
		t.Errorf("CommandCode = %d, want %d", msg.CommandCode, CmdCxUAR)
	}
	if msg.ApplicationID != cdp.App3GPPCx {
		t.Errorf("ApplicationID = %d, want %d (Cx)", msg.ApplicationID, cdp.App3GPPCx)
	}
	if msg.ApplicationID != 16777216 {
		t.Errorf("App3GPPCx value = %d, want 16777216", msg.ApplicationID)
	}
	if msg.Flags&cdp.CmdFlagRequest == 0 {
		t.Errorf("Flags = 0x%02X, want Request bit set", msg.Flags)
	}
	if msg.HopByHopID != 1 || msg.EndToEndID != 2 {
		t.Errorf("identifiers = (%d, %d), want (1, 2)", msg.HopByHopID, msg.EndToEndID)
	}

	codes := avpCodes(msg)
	for _, want := range []uint32{
		cdp.AVPCodeOriginHost, cdp.AVPCodeOriginRealm, cdp.AVPCodeDestinationRealm,
		cdp.AVPCodeVendorSpecificApplicationID, cdp.AVPCodeAuthSessionState,
		cdp.AVPCodeUserName, AVPPublicIdentity, AVPVisitedNetworkID,
		AVPUARFlags, AVPAuthorizationType,
	} {
		if !contains(codes, want) {
			t.Errorf("UAR missing AVP %d", want)
		}
	}
}

func TestBuildUAROmitsEmptyOptional(t *testing.T) {
	msg := BuildUAR(&UARRequest{
		DestinationRealm: "home.net",
		OriginHost:       "sip:icscf.home.net",
		OriginRealm:     "home.net",
		PublicIdentity:  "sip:user@home.net",
	}, 0, 0)
	codes := avpCodes(msg)
	// UAR-Flags and Authorization-Type should be absent when zero.
	if contains(codes, AVPUARFlags) {
		t.Errorf("UAR-Flags should be absent when zero")
	}
	if contains(codes, AVPAuthorizationType) {
		t.Errorf("Authorization-Type should be absent when zero")
	}
	// User-Name should be absent when empty.
	if contains(codes, cdp.AVPCodeUserName) {
		t.Errorf("User-Name should be absent when empty")
	}
}

func TestBuildUARNilSafe(t *testing.T) {
	msg := BuildUAR(nil, 1, 2)
	if msg == nil {
		t.Fatalf("BuildUAR(nil) returned nil")
	}
	if msg.CommandCode != CmdCxUAR {
		t.Errorf("CommandCode = %d", msg.CommandCode)
	}
}

func TestBuildCxVendorSpecificAppContents(t *testing.T) {
	avp := buildCxVendorSpecificApp()
	// The grouped AVP value should contain exactly two inner AVPs:
	// Vendor-Id (3GPP) and Auth-Application-Id (Cx).
	vendor, app := parseVendorSpecificAppForTest(t, avp.Value)
	if vendor != cdp.VendorID3GPP {
		t.Errorf("Vendor-Id = %d, want %d", vendor, cdp.VendorID3GPP)
	}
	if app != cdp.App3GPPCx {
		t.Errorf("Auth-Application-Id = %d, want %d", app, cdp.App3GPPCx)
	}
}

func TestBuildLIRMinimal(t *testing.T) {
	msg := BuildLIR(&LIRRequest{
		DestinationRealm: "home.net",
		OriginHost:       "sip:icscf.home.net",
		OriginRealm:     "home.net",
		PublicIdentity:  "sip:user@home.net",
	}, 1, 2)
	if msg.CommandCode != CmdCxLIR {
		t.Errorf("LIR CommandCode = %d, want %d", msg.CommandCode, CmdCxLIR)
	}
	if msg.ApplicationID != cdp.App3GPPCx {
		t.Errorf("LIR ApplicationID = %d", msg.ApplicationID)
	}
	codes := avpCodes(msg)
	if !contains(codes, AVPPublicIdentity) {
		t.Errorf("LIR missing Public-Identity AVP")
	}
}

func TestBuildLIRNilSafe(t *testing.T) {
	msg := BuildLIR(nil, 0, 0)
	if msg == nil {
		t.Fatalf("BuildLIR(nil) returned nil")
	}
}

// ---------------------------------------------------------------------------
// ParseUAA / ParseLIA — round-trip and result-classification tests
// ---------------------------------------------------------------------------

func TestParseUAASubsequentRegistrationWithServerName(t *testing.T) {
	uaa := &cdp.DiameterMessage{
		CommandCode:   CmdCxUAR,
		ApplicationID: cdp.App3GPPCx,
		AVPs: []cdp.DiameterAVP{
			{Code: cdp.AVPCodeExperimentalResult, Flags: 0,
				Value: buildExperimentalResultGroup(cdp.VendorID3GPP, ExCodeSubsequentRegistration)},
			{Code: AVPServerName, Flags: 0, Value: []byte("sip:scscf1.home.net")},
		},
	}
	got, err := ParseUAA(uaa)
	if err != nil {
		t.Fatalf("ParseUAA: %v", err)
	}
	if !got.HasExperimentalResult {
		t.Errorf("HasExperimentalResult = false, want true")
	}
	if got.ExperimentalResultCode != ExCodeSubsequentRegistration {
		t.Errorf("ExperimentalResultCode = %d, want %d",
			got.ExperimentalResultCode, ExCodeSubsequentRegistration)
	}
	if got.ServerName != "sip:scscf1.home.net" {
		t.Errorf("ServerName = %q", got.ServerName)
	}
	if got.RegistrationCase() != RegistrationCaseSubsequent {
		t.Errorf("RegistrationCase = %s, want subsequent-registration",
			got.RegistrationCase())
	}
}

func TestParseUAAFirstRegistrationWithCapabilities(t *testing.T) {
	uaa := &cdp.DiameterMessage{
		CommandCode:   CmdCxUAR,
		ApplicationID: cdp.App3GPPCx,
		AVPs: []cdp.DiameterAVP{
			{Code: cdp.AVPCodeExperimentalResult, Flags: 0,
				Value: buildExperimentalResultGroup(cdp.VendorID3GPP, ExCodeFirstRegistration)},
			{Code: AVPServerCapabilities, Flags: 0,
				Value: buildServerCapabilitiesGroup([]uint32{1, 2}, []uint32{3, 4})},
		},
	}
	got, err := ParseUAA(uaa)
	if err != nil {
		t.Fatalf("ParseUAA: %v", err)
	}
	if !got.HasServerCapabilities {
		t.Errorf("HasServerCapabilities = false")
	}
	if len(got.MandatoryCaps) != 2 || got.MandatoryCaps[0] != 1 {
		t.Errorf("MandatoryCaps = %v", got.MandatoryCaps)
	}
	if len(got.OptionalCaps) != 2 || got.OptionalCaps[1] != 4 {
		t.Errorf("OptionalCaps = %v", got.OptionalCaps)
	}
	if got.RegistrationCase() != RegistrationCaseFirst {
		t.Errorf("RegistrationCase = %s, want first-registration", got.RegistrationCase())
	}
}

func TestParseUAAUserUnknown(t *testing.T) {
	uaa := &cdp.DiameterMessage{
		CommandCode:   CmdCxUAR,
		ApplicationID: cdp.App3GPPCx,
		AVPs: []cdp.DiameterAVP{
			{Code: cdp.AVPCodeExperimentalResult, Flags: 0,
				Value: buildExperimentalResultGroup(cdp.VendorID3GPP, ExCodeErrorUserUnknown)},
		},
	}
	got, err := ParseUAA(uaa)
	if err != nil {
		t.Fatalf("ParseUAA: %v", err)
	}
	if got.RegistrationCase() != RegistrationCaseUserUnknown {
		t.Errorf("RegistrationCase = %s, want user-unknown", got.RegistrationCase())
	}
	if got.IsSuccess() {
		t.Errorf("IsSuccess = true, want false (4xxx)")
	}
}

func TestParseUAASuccessWithResultCode(t *testing.T) {
	uaa := &cdp.DiameterMessage{
		CommandCode:   CmdCxUAR,
		ApplicationID: cdp.App3GPPCx,
		AVPs: []cdp.DiameterAVP{
			{Code: cdp.AVPCodeResultCode, Flags: 0,
				Value: encodeUint32(cdp.ResultSuccess)},
			{Code: AVPServerName, Flags: 0, Value: []byte("sip:scscf.home.net")},
		},
	}
	got, err := ParseUAA(uaa)
	if err != nil {
		t.Fatalf("ParseUAA: %v", err)
	}
	if !got.IsSuccess() {
		t.Errorf("IsSuccess = false, want true (2001)")
	}
	if got.RegistrationCase() != RegistrationCaseSubsequent {
		t.Errorf("RegistrationCase = %s, want subsequent-registration (Result-Code + Server-Name)",
			got.RegistrationCase())
	}
}

func TestParseUAANil(t *testing.T) {
	if _, err := ParseUAA(nil); err == nil {
		t.Errorf("ParseUAA(nil) should error")
	}
}

func TestParseUAAWrongCommandCode(t *testing.T) {
	msg := &cdp.DiameterMessage{CommandCode: CmdCxLIR}
	if _, err := ParseUAA(msg); err == nil {
		t.Errorf("ParseUAA on LIR should error")
	}
}

func TestRegistrationCaseString(t *testing.T) {
	cases := []struct {
		c    RegistrationCase
		name string
	}{
		{RegistrationCaseFirst, "first-registration"},
		{RegistrationCaseSubsequent, "subsequent-registration"},
		{RegistrationCaseServerSelection, "server-selection"},
		{RegistrationCaseUnregistered, "unregistered-service"},
		{RegistrationCaseUserUnknown, "user-unknown"},
		{RegistrationCaseIdentitiesMismatch, "identities-dont-match"},
		{RegistrationCaseRoamingNotAllowed, "roaming-not-allowed"},
		{RegistrationCaseIdentityNotRegistered, "identity-not-registered"},
		{RegistrationCaseOther, "other"},
	}
	for _, c := range cases {
		if got := c.c.String(); got != c.name {
			t.Errorf("RegistrationCase(%d).String() = %q, want %q",
				int(c.c), got, c.name)
		}
	}
}

func TestParseLIA(t *testing.T) {
	lia := &cdp.DiameterMessage{
		CommandCode:   CmdCxLIR,
		ApplicationID: cdp.App3GPPCx,
		AVPs: []cdp.DiameterAVP{
			{Code: cdp.AVPCodeResultCode, Flags: 0,
				Value: encodeUint32(cdp.ResultSuccess)},
			{Code: AVPServerName, Flags: 0, Value: []byte("sip:scscf1.home.net")},
		},
	}
	got, err := ParseLIA(lia)
	if err != nil {
		t.Fatalf("ParseLIA: %v", err)
	}
	if got.ResultCode != cdp.ResultSuccess {
		t.Errorf("ResultCode = %d", got.ResultCode)
	}
	if got.ServerName != "sip:scscf1.home.net" {
		t.Errorf("ServerName = %q", got.ServerName)
	}
	if !got.IsSuccess() {
		t.Errorf("IsSuccess = false")
	}
}

func TestParseLIANil(t *testing.T) {
	if _, err := ParseLIA(nil); err == nil {
		t.Errorf("ParseLIA(nil) should error")
	}
}

func TestParseLIAWrongCommandCode(t *testing.T) {
	msg := &cdp.DiameterMessage{CommandCode: CmdCxUAR}
	if _, err := ParseLIA(msg); err == nil {
		t.Errorf("ParseLIA on UAR should error")
	}
}

// ---------------------------------------------------------------------------
// Constants sanity
// ---------------------------------------------------------------------------

func TestCommandCodeConstants(t *testing.T) {
	cases := []struct {
		name string
		got  uint32
		want uint32
	}{
		{"UAR", CmdCxUAR, 300},
		{"SAR", CmdCxSAR, 301},
		{"LIR", CmdCxLIR, 302},
		{"MAR", CmdCxMAR, 303},
		{"RTR", CmdCxRTR, 304},
		{"PPR", CmdCxPPR, 305},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

func Test3GPPAVPCodeConstants(t *testing.T) {
	cases := []struct {
		name string
		got  uint32
		want uint32
	}{
		{"VisitedNetworkID", AVPVisitedNetworkID, 600},
		{"PublicIdentity", AVPPublicIdentity, 601},
		{"ServerName", AVPServerName, 602},
		{"ServerCapabilities", AVPServerCapabilities, 603},
		{"MandatoryCapability", AVPMandatoryCapability, 604},
		{"OptionalCapability", AVPOptionalCapability, 605},
		{"UARFlags", AVPUARFlags, 637},
		{"AuthorizationType", AVPAuthorizationType, 623},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

func TestExperimentalResultCodes(t *testing.T) {
	if ExCodeFirstRegistration != 2001 {
		t.Errorf("ExCodeFirstRegistration = %d, want 2001", ExCodeFirstRegistration)
	}
	if ExCodeErrorUserUnknown != 4181 {
		t.Errorf("ExCodeErrorUserUnknown = %d, want 4181", ExCodeErrorUserUnknown)
	}
}

func TestAuthorizationTypeValues(t *testing.T) {
	// C source diameter_ims_code_avp.h:340-342.
	if AuthzRegistration != 0 {
		t.Errorf("AuthzRegistration = %d, want 0", AuthzRegistration)
	}
	if AuthzDeRegistration != 1 {
		t.Errorf("AuthzDeRegistration = %d, want 1", AuthzDeRegistration)
	}
	if AuthzRegistrationAndCapabilities != 2 {
		t.Errorf("AuthzRegistrationAndCapabilities = %d, want 2", AuthzRegistrationAndCapabilities)
	}
}

// ---------------------------------------------------------------------------
// Wire encoding round-trip via cdp module
// ---------------------------------------------------------------------------

func TestUAREncodeDecodeRoundTrip(t *testing.T) {
	orig := BuildUAR(&UARRequest{
		DestinationRealm:  "home.net",
		OriginHost:        "sip:icscf.home.net",
		OriginRealm:       "home.net",
		PublicIdentity:     "sip:user@home.net",
		PrivateIdentity:    "user@home.net",
		VisitedNetworkID:   "visited.net",
		AuthorizationType:  AuthzRegistrationAndCapabilities,
	}, 0x11223344, 0x55667788)

	enc := cdp.EncodeMessage(orig)
	if len(enc) < cdp.HeaderLen {
		t.Fatalf("encoded buffer too short: %d", len(enc))
	}
	dec, err := cdp.DecodeMessage(enc)
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}
	if dec.CommandCode != CmdCxUAR {
		t.Errorf("decoded CommandCode = %d, want %d", dec.CommandCode, CmdCxUAR)
	}
	if dec.ApplicationID != cdp.App3GPPCx {
		t.Errorf("decoded ApplicationID = %d, want %d", dec.ApplicationID, cdp.App3GPPCx)
	}
	if dec.HopByHopID != 0x11223344 || dec.EndToEndID != 0x55667788 {
		t.Errorf("decoded identifiers = (0x%X, 0x%X)", dec.HopByHopID, dec.EndToEndID)
	}
}

func TestLIREncodeDecodeRoundTrip(t *testing.T) {
	orig := BuildLIR(&LIRRequest{
		DestinationRealm: "home.net",
		OriginHost:       "sip:icscf.home.net",
		OriginRealm:      "home.net",
		PublicIdentity:   "sip:user@home.net",
	}, 1, 2)
	enc := cdp.EncodeMessage(orig)
	dec, err := cdp.DecodeMessage(enc)
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}
	if dec.CommandCode != CmdCxLIR {
		t.Errorf("decoded CommandCode = %d", dec.CommandCode)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func avpCodes(msg *cdp.DiameterMessage) []uint32 {
	out := make([]uint32, 0, len(msg.AVPs))
	for i := range msg.AVPs {
		out = append(out, msg.AVPs[i].Code)
	}
	return out
}

func contains(s []uint32, v uint32) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// buildExperimentalResultGroup builds the byte payload of an
// Experimental-Result grouped AVP containing Vendor-Id + Experimental-Result-Code.
func buildExperimentalResultGroup(vendorID, resultCode uint32) []byte {
	out := encodeAVPBytes(cdp.DiameterAVP{
		Code:  cdp.AVPCodeVendorID,
		Flags: cdp.AVPFlagMandatory,
		Value: encodeUint32(vendorID),
	})
	out = append(out, encodeAVPBytes(cdp.DiameterAVP{
		Code:  cdp.AVPCodeExperimentalResultCode,
		Flags: cdp.AVPFlagMandatory,
		Value: encodeUint32(resultCode),
	})...)
	return out
}

// buildServerCapabilitiesGroup builds the byte payload of a
// Server-Capabilities grouped AVP containing one or more
// Mandatory-Capability and Optional-Capability AVPs.
func buildServerCapabilitiesGroup(mandatory, optional []uint32) []byte {
	var out []byte
	for _, m := range mandatory {
		out = append(out, encodeAVPBytes(cdp.DiameterAVP{
			Code:  AVPMandatoryCapability,
			Flags: cdp.AVPFlagMandatory,
			Value: encodeUint32(m),
		})...)
	}
	for _, o := range optional {
		out = append(out, encodeAVPBytes(cdp.DiameterAVP{
			Code:  AVPOptionalCapability,
			Flags: cdp.AVPFlagMandatory,
			Value: encodeUint32(o),
		})...)
	}
	return out
}

// parseVendorSpecificAppForTest extracts the Vendor-Id and
// Auth-Application-Id from a Vendor-Specific-Application-Id grouped AVP.
func parseVendorSpecificAppForTest(t *testing.T, grouped []byte) (vendor, app uint32) {
	t.Helper()
	off := 0
	for off+cdp.AVPHeaderLen <= len(grouped) {
		code := getUint32At(grouped, off)
		flags := grouped[off+4]
		totalLen := int(getUint24(grouped[off+5 : off+8]))
		if totalLen < cdp.AVPHeaderLen || off+totalLen > len(grouped) {
			t.Fatalf("bad AVP length %d at offset %d", totalLen, off)
		}
		headerLen := cdp.AVPHeaderLen
		if flags&cdp.AVPFlagVendor != 0 {
			headerLen = cdp.AVPVendorHeaderLen
		}
		dataStart := off + headerLen
		dataLen := totalLen - headerLen
		data := grouped[dataStart : dataStart+dataLen]
		switch code {
		case cdp.AVPCodeVendorID:
			if len(data) >= 4 {
				vendor = decodeUint32(data)
			}
		case cdp.AVPCodeAuthApplicationID:
			if len(data) >= 4 {
				app = decodeUint32(data)
			}
		}
		off += (totalLen + 3) &^ 3
	}
	return
}

func getUint32At(b []byte, off int) uint32 {
	if off+4 > len(b) {
		return 0
	}
	return uint32(b[off])<<24 | uint32(b[off+1])<<16 | uint32(b[off+2])<<8 | uint32(b[off+3])
}
