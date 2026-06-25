// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * I-CSCF Cx Diameter message builders and parsers.
 * Port of the kamailio ims_icscf module's cxdx_avp.c (the subset of AVP
 * helpers actually invoked by cxdx_uar.c and cxdx_lir.c).
 *
 * Only UAR/UAA and LIR/LIA are built here — the C source ims_icscf never
 * sends SAR/SAA, MAR/MAA, RTR/RTA or PPR/PPA. The SAR/MAR AVP helpers that
 * exist in cxdx_avp.c are dead code (called nowhere in ims_icscf) and are
 * not ported. SAR lives in ims_registrar_scscf, MAR in ims_qos.
 *
 * All builders operate on the existing *DiameterMessage type from the cdp
 * module (Phase 3.3). The AVP wire format is the cdp module's
 * responsibility; this file adds the 3GPP Cx semantic structure.
 */

package icscf

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/kamailio/kamailio-go/internal/modules/cdp"
)

// ---------------------------------------------------------------------------
// 3GPP Cx command codes (src/modules/cdp/diameter_ims_code_cmd.h)
// ---------------------------------------------------------------------------

const (
	// CmdCxUAR is the User-Authorization command code (3GPP TS 29.229 §7.2).
	// UAR/UAA.
	CmdCxUAR uint32 = 300
	// CmdCxSAR is the Server-Assignment command code (3GPP TS 29.229 §7.3).
	// SAR/SAA — not used by ims_icscf but defined for completeness.
	CmdCxSAR uint32 = 301
	// CmdCxLIR is the Location-Info command code (3GPP TS 29.229 §7.4).
	// LIR/LIA.
	CmdCxLIR uint32 = 302
	// CmdCxMAR is the Multimedia-Auth command code (3GPP TS 29.229 §7.5).
	// MAR/MAA — not used by ims_icscf.
	CmdCxMAR uint32 = 303
	// CmdCxRTR is the Registration-Termination command code (3GPP TS 29.229 §7.6).
	// RTR/RTA — not used by ims_icscf.
	CmdCxRTR uint32 = 304
	// CmdCxPPR is the Push-Profile command code (3GPP TS 29.229 §7.7).
	// PPR/PPA — not used by ims_icscf.
	CmdCxPPR uint32 = 305
)

// ---------------------------------------------------------------------------
// 3GPP Cx AVP codes (src/modules/cdp/diameter_ims_code_avp.h)
// ---------------------------------------------------------------------------

const (
	// AVPVisitedNetworkID is the Visited-Network-Identifier AVP (3GPP TS 29.229 §6.3.20).
	AVPVisitedNetworkID uint32 = 600
	// AVPPublicIdentity is the Public-Identity AVP (3GPP TS 29.229 §6.3.17).
	AVPPublicIdentity uint32 = 601
	// AVPServerName is the Server-Name AVP (3GPP TS 29.229 §6.3.18).
	AVPServerName uint32 = 602
	// AVPServerCapabilities is the Server-Capabilities grouped AVP
	// (3GPP TS 29.229 §6.3.19). Contains Mandatory-Capability and
	// Optional-Capability AVPs.
	AVPServerCapabilities uint32 = 603
	// AVPMandatoryCapability is the Mandatory-Capability AVP (3GPP TS 29.229 §6.3.21).
	AVPMandatoryCapability uint32 = 604
	// AVPOptionalCapability is the Optional-Capability AVP (3GPP TS 29.229 §6.3.22).
	AVPOptionalCapability uint32 = 605
	// AVPUARFlags is the UAR-Flags AVP (3GPP TS 29.229 §6.3.39).
	AVPUARFlags uint32 = 637
	// AVPAuthorizationType is the Authorization-Type AVP (3GPP TS 29.229 §6.3.36).
	// C source: src/modules/cdp/diameter_ims_code_avp.h — AVP_IMS_User_Authorization_Type.
	AVPAuthorizationType uint32 = 623
)

// Authorization-Type values (3GPP TS 29.229 §6.3.36, C source
// diameter_ims_code_avp.h:340-342: AVP_IMS_UAR_REGISTRATION=0,
// AVP_IMS_UAR_DE_REGISTRATION=1, AVP_IMS_UAR_REGISTRATION_AND_CAPABILITIES=2).
const (
	AuthzRegistration                 uint32 = 0 // AVP_IMS_UAR_REGISTRATION
	AuthzDeRegistration                uint32 = 1 // AVP_IMS_UAR_DE_REGISTRATION
	AuthzRegistrationAndCapabilities   uint32 = 2 // AVP_IMS_UAR_REGISTRATION_AND_CAPABILITIES
)

// UAR-Flags bit values (3GPP TS 29.229 §6.3.39).
const (
	UARFlagNone                  uint32 = 0
	UARFlagEmergencyRegistration uint32 = 1 // (1<<0)
)

// ---------------------------------------------------------------------------
// Experimental-Result-Code values for UAA/LIA (3GPP TS 29.229 §6.4 / src/modules/cdp/diameter_ims_code_result.h)
// ---------------------------------------------------------------------------

const (
	// ExCodeFirstRegistration indicates a user is registering for the
	// first time (the I-CSCF should ask for S-CSCF capabilities).
	ExCodeFirstRegistration uint32 = 2001
	// ExCodeSubsequentRegistration indicates the user is re-registering
	// and already has an S-CSCF assigned.
	ExCodeSubsequentRegistration uint32 = 2002
	// ExCodeServerSelection indicates the I-CSCF should select an S-CSCF.
	ExCodeServerSelection uint32 = 2003
	// ExCodeUnregisteredService indicates an unregistered user is being served.
	ExCodeUnregisteredService uint32 = 2004
	// ExCodeErrorUserUnknown indicates the HSS does not recognise the user.
	ExCodeErrorUserUnknown uint32 = 4181
	// ExCodeErrorIdentitiesDontMatch indicates the public and private
	// identities don't belong together.
	ExCodeErrorIdentitiesDontMatch uint32 = 4182
	// ExCodeErrorRoamingNotAllowed indicates roaming is forbidden.
	ExCodeErrorRoamingNotAllowed uint32 = 4183
	// ExCodeErrorIdentityNotRegistered indicates the user is not registered
	// (returned by LIA when the public identity has no registration).
	ExCodeErrorIdentityNotRegistered uint32 = 4184
)

// ---------------------------------------------------------------------------
// UAR / UAA (User-Authorization — 3GPP TS 29.229 §7.2)
// ---------------------------------------------------------------------------

// UARRequest captures the inputs for a User-Authorization-Request.
type UARRequest struct {
	// DestinationRealm is the home realm (Cx Destination-Realm AVP).
	DestinationRealm string
	// OriginHost / OriginRealm are the local I-CSCF identity.
	OriginHost  string
	OriginRealm string

	// PrivateIdentity is the User-Name AVP (IMPI).
	PrivateIdentity string
	// PublicIdentity is the Public-Identity AVP (IMPU).
	PublicIdentity string
	// VisitedNetworkID is the Visited-Network-Identifier AVP.
	VisitedNetworkID string

	// AuthorizationType selects REGISTER / REGISTRATION_AND_CAPABILITIES /
	// DE_REGISTRATION. Derived by the C source from the REGISTER's
	// Expires header and the route-block "with capabilities" flag.
	AuthorizationType uint32

	// UARFlags carries the SoR / Emergency-Registration bits.
	UARFlags uint32
}

// BuildUAR constructs a Diameter User-Authorization-Request.
// hopByHop / endToEnd are populated from the cdp module's counters before
// the message is dispatched.
//
//	C: I_perform_user_authorization_request() — message-build portion
func BuildUAR(req *UARRequest, hopByHop, endToEnd uint32) *cdp.DiameterMessage {
	if req == nil {
		req = &UARRequest{}
	}
	msg := &cdp.DiameterMessage{
		Version:       cdp.Version,
		Flags:         cdp.CmdFlagRequest,
		CommandCode:   CmdCxUAR,
		ApplicationID: cdp.App3GPPCx,
		HopByHopID:    hopByHop,
		EndToEndID:    endToEnd,
	}
	// Origin-Host / Origin-Realm / Destination-Realm (mandatory).
	addStringAVP(msg, cdp.AVPCodeOriginHost, cdp.AVPFlagMandatory, req.OriginHost)
	addStringAVP(msg, cdp.AVPCodeOriginRealm, cdp.AVPFlagMandatory, req.OriginRealm)
	addStringAVP(msg, cdp.AVPCodeDestinationRealm, cdp.AVPFlagMandatory, req.DestinationRealm)
	// Vendor-Specific-Application-Id (3GPP / Cx) — mandatory.
	msg.AVPs = append(msg.AVPs, buildCxVendorSpecificApp())
	// Auth-Session-State = 1 (maintained).
	addUint32AVP(msg, cdp.AVPCodeAuthSessionState, cdp.AVPFlagMandatory,
		cdp.AuthSessionStateMaintained)
	// User-Name (IMPI).
	if req.PrivateIdentity != "" {
		addStringAVP(msg, cdp.AVPCodeUserName, cdp.AVPFlagMandatory, req.PrivateIdentity)
	}
	// Public-Identity (IMPU).
	if req.PublicIdentity != "" {
		addStringAVP(msg, AVPPublicIdentity, cdp.AVPFlagMandatory, req.PublicIdentity)
	}
	// Visited-Network-Identifier.
	if req.VisitedNetworkID != "" {
		addStringAVP(msg, AVPVisitedNetworkID, cdp.AVPFlagMandatory, req.VisitedNetworkID)
	}
	// UAR-Flags (optional).
	if req.UARFlags != 0 {
		addUint32AVP(msg, AVPUARFlags, cdp.AVPFlagMandatory, req.UARFlags)
	}
	// Authorization-Type: omitted when AuthorizationType == AuthzRegistration
	// (mirrors C source cxdx_uar.c:333: if(authorization_type != AVP_IMS_UAR_REGISTRATION)).
	if req.AuthorizationType != AuthzRegistration {
		addUint32AVP(msg, AVPAuthorizationType, cdp.AVPFlagMandatory, req.AuthorizationType)
	}
	return msg
}

// UAAResult captures the parsed fields from a User-Authorization-Answer.
type UAAResult struct {
	// ResultCode is the Diameter Result-Code AVP, when present.
	ResultCode uint32
	// ExperimentalResultCode is the 3GPP Experimental-Result-Code AVP,
	// when the HSS returns a 3GPP-specific result (more common than the
	// plain Result-Code for UAA).
	ExperimentalResultCode uint32
	// HasExperimentalResult reports whether Experimental-Result was set.
	HasExperimentalResult bool
	// ServerName is the Server-Name AVP returned by the HSS, when the
	// S-CSCF has already been assigned (SUBSEQUENT_REGISTRATION case).
	ServerName string
	// MandatoryCaps / OptionalCaps are the capability sets returned in
	// the Server-Capabilities grouped AVP.
	MandatoryCaps []int
	OptionalCaps  []int
	// HasServerCapabilities reports whether Server-Capabilities was set.
	HasServerCapabilities bool
}

// ParseUAA extracts the fields above from a User-Authorization-Answer.
//
//	C: async_cdp_uar_callback() — AVP-decoding portion
func ParseUAA(msg *cdp.DiameterMessage) (*UAAResult, error) {
	if msg == nil {
		return nil, errors.New("icscf: nil UAA message")
	}
	if msg.CommandCode != CmdCxUAR {
		return nil, fmt.Errorf("icscf: not a UAR/UAA message (cmd=%d)", msg.CommandCode)
	}
	out := &UAAResult{}
	for i := range msg.AVPs {
		avp := &msg.AVPs[i]
		switch avp.Code {
		case cdp.AVPCodeResultCode:
			if len(avp.Value) >= 4 {
				out.ResultCode = decodeUint32(avp.Value)
			}
		case cdp.AVPCodeExperimentalResult:
			out.HasExperimentalResult = true
			out.ExperimentalResultCode = parseExperimentalResultCode(avp.Value)
		case AVPServerName:
			out.ServerName = string(avp.Value)
		case AVPServerCapabilities:
			out.HasServerCapabilities = true
			parseServerCapabilities(avp.Value, out)
		}
	}
	return out, nil
}

// IsSuccess reports whether the UAA indicates a successful outcome
// (Result-Code 2xxx OR Experimental-Result-Code 2xxx).
func (u *UAAResult) IsSuccess() bool {
	if u == nil {
		return false
	}
	if u.HasExperimentalResult {
		return cdp.ResultClassSuccess(u.ExperimentalResultCode)
	}
	return cdp.ResultClassSuccess(u.ResultCode)
}

// RegistrationCase returns the high-level UAA outcome as seen by the
// I-CSCF's routing logic: FirstRegistration / SubsequentRegistration /
// ServerSelection / UnregisteredService / UserUnknown / IdentitiesMismatch /
// RoamingNotAllowed / IdentityNotRegistered / Other.
//
//	C: async_cdp_uar_callback() — case-by-case switch
func (u *UAAResult) RegistrationCase() RegistrationCase {
	if u == nil {
		return RegistrationCaseOther
	}
	if u.HasExperimentalResult {
		switch u.ExperimentalResultCode {
		case ExCodeFirstRegistration:
			return RegistrationCaseFirst
		case ExCodeSubsequentRegistration:
			return RegistrationCaseSubsequent
		case ExCodeServerSelection:
			return RegistrationCaseServerSelection
		case ExCodeUnregisteredService:
			return RegistrationCaseUnregistered
		case ExCodeErrorUserUnknown:
			return RegistrationCaseUserUnknown
		case ExCodeErrorIdentitiesDontMatch:
			return RegistrationCaseIdentitiesMismatch
		case ExCodeErrorRoamingNotAllowed:
			return RegistrationCaseRoamingNotAllowed
		case ExCodeErrorIdentityNotRegistered:
			return RegistrationCaseIdentityNotRegistered
		}
		return RegistrationCaseOther
	}
	// Plain Result-Code: 2xxx = success path, 5xxx = error.
	switch cdp.ResultCodeClass(u.ResultCode) {
	case 2:
		if u.ServerName != "" {
			return RegistrationCaseSubsequent
		}
		return RegistrationCaseServerSelection
	default:
		return RegistrationCaseOther
	}
}

// RegistrationCase is the high-level outcome of a UAA exchange.
type RegistrationCase int

const (
	RegistrationCaseOther RegistrationCase = iota
	RegistrationCaseFirst
	RegistrationCaseSubsequent
	RegistrationCaseServerSelection
	RegistrationCaseUnregistered
	RegistrationCaseUserUnknown
	RegistrationCaseIdentitiesMismatch
	RegistrationCaseRoamingNotAllowed
	RegistrationCaseIdentityNotRegistered
)

// String returns a human-readable case name.
func (c RegistrationCase) String() string {
	switch c {
	case RegistrationCaseFirst:
		return "first-registration"
	case RegistrationCaseSubsequent:
		return "subsequent-registration"
	case RegistrationCaseServerSelection:
		return "server-selection"
	case RegistrationCaseUnregistered:
		return "unregistered-service"
	case RegistrationCaseUserUnknown:
		return "user-unknown"
	case RegistrationCaseIdentitiesMismatch:
		return "identities-dont-match"
	case RegistrationCaseRoamingNotAllowed:
		return "roaming-not-allowed"
	case RegistrationCaseIdentityNotRegistered:
		return "identity-not-registered"
	default:
		return "other"
	}
}

// ---------------------------------------------------------------------------
// LIR / LIA (Location-Info — 3GPP TS 29.229 §7.4)
// ---------------------------------------------------------------------------

// LIRRequest captures the inputs for a Location-Info-Request.
type LIRRequest struct {
	DestinationRealm string
	OriginHost       string
	OriginRealm      string

	// PublicIdentity is the IMPU being looked up. On originating requests
	// it comes from P-Asserted-Identity; on terminating requests from
	// the Request-URI (mirrors cscf_get_asserted_identity /
	// cscf_get_public_identity_from_requri in the C source).
	PublicIdentity string
}

// BuildLIR constructs a Diameter Location-Info-Request.
//
//	C: I_perform_location_information_request() — message-build portion
func BuildLIR(req *LIRRequest, hopByHop, endToEnd uint32) *cdp.DiameterMessage {
	if req == nil {
		req = &LIRRequest{}
	}
	msg := &cdp.DiameterMessage{
		Version:       cdp.Version,
		Flags:         cdp.CmdFlagRequest,
		CommandCode:   CmdCxLIR,
		ApplicationID: cdp.App3GPPCx,
		HopByHopID:    hopByHop,
		EndToEndID:    endToEnd,
	}
	addStringAVP(msg, cdp.AVPCodeOriginHost, cdp.AVPFlagMandatory, req.OriginHost)
	addStringAVP(msg, cdp.AVPCodeOriginRealm, cdp.AVPFlagMandatory, req.OriginRealm)
	addStringAVP(msg, cdp.AVPCodeDestinationRealm, cdp.AVPFlagMandatory, req.DestinationRealm)
	msg.AVPs = append(msg.AVPs, buildCxVendorSpecificApp())
	addUint32AVP(msg, cdp.AVPCodeAuthSessionState, cdp.AVPFlagMandatory,
		cdp.AuthSessionStateMaintained)
	if req.PublicIdentity != "" {
		addStringAVP(msg, AVPPublicIdentity, cdp.AVPFlagMandatory, req.PublicIdentity)
	}
	return msg
}

// LIAResult captures the parsed fields from a Location-Info-Answer.
type LIAResult struct {
	ResultCode            uint32
	ExperimentalResultCode uint32
	HasExperimentalResult  bool
	ServerName             string
	MandatoryCaps          []int
	OptionalCaps           []int
	HasServerCapabilities  bool
}

// ParseLIA extracts the fields above from a Location-Info-Answer.
//
//	C: async_cdp_lir_callback() — AVP-decoding portion
func ParseLIA(msg *cdp.DiameterMessage) (*LIAResult, error) {
	if msg == nil {
		return nil, errors.New("icscf: nil LIA message")
	}
	if msg.CommandCode != CmdCxLIR {
		return nil, fmt.Errorf("icscf: not a LIR/LIA message (cmd=%d)", msg.CommandCode)
	}
	out := &LIAResult{}
	for i := range msg.AVPs {
		avp := &msg.AVPs[i]
		switch avp.Code {
		case cdp.AVPCodeResultCode:
			if len(avp.Value) >= 4 {
				out.ResultCode = decodeUint32(avp.Value)
			}
		case cdp.AVPCodeExperimentalResult:
			out.HasExperimentalResult = true
			out.ExperimentalResultCode = parseExperimentalResultCode(avp.Value)
		case AVPServerName:
			out.ServerName = string(avp.Value)
		case AVPServerCapabilities:
			out.HasServerCapabilities = true
			parseServerCapabilities(avp.Value, out)
		}
	}
	return out, nil
}

// IsSuccess reports whether the LIA indicates success.
func (l *LIAResult) IsSuccess() bool {
	if l == nil {
		return false
	}
	if l.HasExperimentalResult {
		return cdp.ResultClassSuccess(l.ExperimentalResultCode)
	}
	return cdp.ResultClassSuccess(l.ResultCode)
}

// ---------------------------------------------------------------------------
// Shared AVP helpers
// ---------------------------------------------------------------------------

// buildCxVendorSpecificApp constructs the Vendor-Specific-Application-Id
// grouped AVP for the Cx interface (Vendor-Id=3GPP, Auth-Application-Id=Cx).
// The grouped value is the byte-level concatenation of the inner AVPs
// (Vendor-Id followed by Auth-Application-Id), per RFC 6733 §6.12.
func buildCxVendorSpecificApp() cdp.DiameterAVP {
	inner := append(encodeAVPBytes(cdp.DiameterAVP{
		Code: cdp.AVPCodeVendorID, Flags: cdp.AVPFlagMandatory,
		Value: encodeUint32(cdp.VendorID3GPP),
	}), encodeAVPBytes(cdp.DiameterAVP{
		Code: cdp.AVPCodeAuthApplicationID, Flags: cdp.AVPFlagMandatory,
		Value: encodeUint32(cdp.App3GPPCx),
	})...)
	return cdp.DiameterAVP{
		Code:  cdp.AVPCodeVendorSpecificApplicationID,
		Flags: cdp.AVPFlagMandatory,
		Value: inner,
	}
}

// parseExperimentalResultCode extracts the Experimental-Result-Code AVP
// from inside an Experimental-Result grouped AVP. The grouped value is the
// concatenation of Vendor-Id + Experimental-Result-Code AVPs.
func parseExperimentalResultCode(grouped []byte) uint32 {
	for off := 0; off+cdp.AVPHeaderLen <= len(grouped); {
		code := binary.BigEndian.Uint32(grouped[off : off+4])
		flags := grouped[off+4]
		totalLen := int(getUint24(grouped[off+5 : off+8]))
		if totalLen < cdp.AVPHeaderLen || off+totalLen > len(grouped) {
			return 0
		}
		headerLen := cdp.AVPHeaderLen
		if flags&cdp.AVPFlagVendor != 0 {
			headerLen = cdp.AVPVendorHeaderLen
		}
		data := grouped[off+headerLen : off+totalLen]
		if code == cdp.AVPCodeExperimentalResultCode && len(data) >= 4 {
			return binary.BigEndian.Uint32(data[:4])
		}
		off += (totalLen + 3) &^ 3
	}
	return 0
}

// parseServerCapabilities extracts Mandatory-Capability and
// Optional-Capability AVPs from inside a Server-Capabilities grouped AVP.
// It writes the result into the supplied out parameter (either *UAAResult
// or *LIAResult). The C source stores these as int arrays on the candidate
// list entry; we mirror that here.
func parseServerCapabilities(grouped []byte, out interface{}) {
	for off := 0; off+cdp.AVPHeaderLen <= len(grouped); {
		code := binary.BigEndian.Uint32(grouped[off : off+4])
		flags := grouped[off+4]
		totalLen := int(getUint24(grouped[off+5 : off+8]))
		if totalLen < cdp.AVPHeaderLen || off+totalLen > len(grouped) {
			return
		}
		headerLen := cdp.AVPHeaderLen
		if flags&cdp.AVPFlagVendor != 0 {
			headerLen = cdp.AVPVendorHeaderLen
		}
		data := grouped[off+headerLen : off+totalLen]
		cap := -1
		if len(data) >= 4 {
			cap = int(binary.BigEndian.Uint32(data[:4]))
		}
		switch code {
		case AVPMandatoryCapability:
			switch o := out.(type) {
			case *UAAResult:
				if cap >= 0 {
					o.MandatoryCaps = append(o.MandatoryCaps, cap)
				}
			case *LIAResult:
				if cap >= 0 {
					o.MandatoryCaps = append(o.MandatoryCaps, cap)
				}
			}
		case AVPOptionalCapability:
			switch o := out.(type) {
			case *UAAResult:
				if cap >= 0 {
					o.OptionalCaps = append(o.OptionalCaps, cap)
				}
			case *LIAResult:
				if cap >= 0 {
					o.OptionalCaps = append(o.OptionalCaps, cap)
				}
			}
		}
		off += (totalLen + 3) &^ 3
	}
}

// ---------------------------------------------------------------------------
// Tiny encode/decode helpers — local to avoid the cdp package's unexported
// encode/decode helpers, which we cannot reach from this import path.
// ---------------------------------------------------------------------------

func addStringAVP(msg *cdp.DiameterMessage, code uint32, flags byte, value string) {
	if value == "" {
		return
	}
	msg.AVPs = append(msg.AVPs, cdp.DiameterAVP{
		Code:  code,
		Flags: flags,
		Value: []byte(value),
	})
}

func addUint32AVP(msg *cdp.DiameterMessage, code uint32, flags byte, value uint32) {
	msg.AVPs = append(msg.AVPs, cdp.DiameterAVP{
		Code:  code,
		Flags: flags,
		Value: encodeUint32(value),
	})
}

func encodeUint32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func decodeUint32(b []byte) uint32 {
	if len(b) < 4 {
		return 0
	}
	return binary.BigEndian.Uint32(b[:4])
}

// getUint24 reads a 3-byte big-endian unsigned integer from b (3 bytes).
func getUint24(b []byte) uint32 {
	if len(b) < 3 {
		return 0
	}
	return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
}

// encodeAVPBytes serialises a single AVP, padding the value to a 4-byte
// boundary as required by RFC 6733. Mirrors the cdp module's private
// encodeAVP() — duplicated here because that function is unexported and
// this package lives outside the cdp module's import path.
//
// AVP wire format (RFC 6733 §6.1, no Vendor flag):
//
//	+---------------------+---------------------+
//	| AVP Code (4 bytes)  | Flags (1 byte)      |
//	+---------------------+---------------------+
//	| AVP Length (3 bytes, includes header)      |
//	+---------------------+---------------------+
//	| AVP Data (variable, padded to 4 bytes)    |
//	+---------------------+---------------------+
func encodeAVPBytes(avp cdp.DiameterAVP) []byte {
	vendor := avp.Flags&cdp.AVPFlagVendor != 0
	headerLen := cdp.AVPHeaderLen
	if vendor {
		headerLen = cdp.AVPVendorHeaderLen
	}
	dataLen := len(avp.Value)
	paddedLen := (dataLen + 3) &^ 3
	totalLen := headerLen + dataLen
	bufLen := headerLen + paddedLen

	buf := make([]byte, bufLen)
	binary.BigEndian.PutUint32(buf[0:4], avp.Code)
	buf[4] = avp.Flags
	// Length field uses 3 bytes big-endian, no leading byte.
	buf[5] = byte(totalLen >> 16)
	buf[6] = byte(totalLen >> 8)
	buf[7] = byte(totalLen)
	if vendor {
		binary.BigEndian.PutUint32(buf[8:12], avp.VendorID)
		copy(buf[12:12+dataLen], avp.Value)
	} else {
		copy(buf[8:8+dataLen], avp.Value)
	}
	return buf
}
