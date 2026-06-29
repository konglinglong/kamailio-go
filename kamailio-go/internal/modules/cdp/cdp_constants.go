// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Diameter protocol constants (RFC 6733).
 * Port of the kamailio cdp module constants (src/modules/cdp/diameter.h
 * and various *_defs.h files).
 *
 * This file collects the command-code, AVP-code and Result-Code constants
 * used throughout the CDP module. Numerical values mirror the IANA
 * Diameter Assigned Numbers registry and the 3GPP TS 29.229 / 29.272
 * definitions used by Kamailio.
 */

package cdp

// ---------------------------------------------------------------------------
// Command codes (RFC 6733 §3.1 / IANA Diameter Assigned Numbers)
// ---------------------------------------------------------------------------

const (
	// CmdCapabilitiesExchange is the Capabilities-Exchange command (RFC 6733 §7.4.1).
	// CER/CEA.
	CmdCapabilitiesExchange uint32 = 257
	// CmdReauth is the Re-Auth command (RFC 6733 §7.2.1). RAR/RAA.
	CmdReauth uint32 = 258
	// CmdAccounting is the Accounting command (RFC 6733 §9.7). ACR/ACA.
	CmdAccounting uint32 = 271
	// CmdCreditControl is the Credit-Control command (RFC 4006). CCR/CCA.
	CmdCreditControl uint32 = 272
	// CmdSessionTermination is the Session-Termination command (RFC 6733 §8.4). STR/STA.
	CmdSessionTermination uint32 = 275
	// CmdDeviceWatchdog is the Device-Watchdog command (RFC 6733 §5.5). DWR/DWA.
	CmdDeviceWatchdog uint32 = 280
	// CmdDisconnectPeer is the Disconnect-Peer command (RFC 6733 §5.4). DPR/DPA.
	CmdDisconnectPeer uint32 = 282
)

// CommandCode is a convenience type that attaches a String() method to a
// Diameter command-code value. It is *not* a new wire format — the wire
// format remains a plain uint32 (see DiameterMessage.CommandCode). Use
// CommandCode(msg.CommandCode).String() to get a readable name.
type CommandCode uint32

// String returns a human-readable command name.
func (cmd CommandCode) String() string {
	switch uint32(cmd) {
	case CmdCapabilitiesExchange:
		return "Capabilities-Exchange"
	case CmdReauth:
		return "Re-Auth"
	case CmdAccounting:
		return "Accounting"
	case CmdCreditControl:
		return "Credit-Control"
	case CmdSessionTermination:
		return "Session-Termination"
	case CmdDeviceWatchdog:
		return "Device-Watchdog"
	case CmdDisconnectPeer:
		return "Disconnect-Peer"
	default:
		return "unknown"
	}
}

// ---------------------------------------------------------------------------
// Application IDs (RFC 6733 §2.4 / IANA)
// ---------------------------------------------------------------------------

const (
	// AppDiameterCommonMessages is the Diameter Common Messages application
	// (RFC 6733) — used by CER/CEA, DWR/DWA, DPR/DPA.
	AppDiameterCommonMessages uint32 = 0
	// AppNASREQ is the NASREQ application (RFC 7155).
	AppNASREQ uint32 = 1
	// AppMobileIPv4 is the Mobile IPv4 application (RFC 5778).
	AppMobileIPv4 uint32 = 2
	// AppDiameterBaseAccounting is the Diameter Base Accounting application
	// (RFC 6733 §9).
	AppDiameterBaseAccounting uint32 = 3
	// AppDiameterCreditControl is the Diameter Credit-Control application
	// (RFC 4006).
	AppDiameterCreditControl uint32 = 4
	// AppDiameterSIP is the Diameter SIP application (RFC 4740).
	AppDiameterSIP uint32 = 6
	// App3GPPSh is the 3GPP Sh interface application (3GPP TS 29.329).
	// Vendor-Specific application id; the leading 0x10000 bit indicates the
	// 3GPP vendor-id (10415) in the most-significant nibble for readability.
	App3GPPSh uint32 = 16777217 // 0x01000001
	// App3GPPCx is the 3GPP Cx interface application (3GPP TS 29.229).
	// C source: src/modules/cdp/diameter_ims_code_app.h:51 — IMS_Cx = 16777216.
	// Same value is reused by 3GPP for the Dx interface (SLF lookup) per TS 29.229.
	App3GPPCx uint32 = 16777216 // 0x01000000
	// App3GPPRx is the 3GPP Rx interface application (3GPP TS 29.214).
	App3GPPRx uint32 = 16777236 // 0x01000024
)

// VendorID3GPP is the 3GPP vendor identifier (3GPP TS 29.329).
const VendorID3GPP uint32 = 10415

// ---------------------------------------------------------------------------
// AVP codes (RFC 6733 §6.x / 3GPP TS 29.229 / 3GPP TS 29.272)
// ---------------------------------------------------------------------------

const (
	// AVPCodeOriginHost is the Origin-Host AVP code (RFC 6733 §6.3).
	AVPCodeOriginHost uint32 = 264
	// AVPCodeOriginRealm is the Origin-Realm AVP code (RFC 6733 §6.4).
	AVPCodeOriginRealm uint32 = 296
	// AVPCodeDestinationHost is the Destination-Host AVP code (RFC 6733 §6.5).
	AVPCodeDestinationHost uint32 = 293
	// AVPCodeDestinationRealm is the Destination-Realm AVP code (RFC 6733 §6.6).
	AVPCodeDestinationRealm uint32 = 283
	// AVPCodeHostIPAddress is the Host-IP-Address AVP code (RFC 6733 §6.7).
	AVPCodeHostIPAddress uint32 = 257
	// AVPCodeVendorID is the Vendor-Id AVP code (RFC 6733 §6.11).
	AVPCodeVendorID uint32 = 266
	// AVPCodeProductName is the Product-Name AVP code (RFC 6733 §6.13).
	AVPCodeProductName uint32 = 269
	// AVPCodeOriginStateID is the Origin-State-Id AVP code (RFC 6733 §6.15).
	AVPCodeOriginStateID uint32 = 278
	// AVPCodeSupportedVendorID is the Supported-Vendor-Id AVP code (RFC 6733 §6.16).
	AVPCodeSupportedVendorID uint32 = 265
	// AVPCodeAuthApplicationID is the Auth-Application-Id AVP code (RFC 6733 §6.9).
	AVPCodeAuthApplicationID uint32 = 258
	// AVPCodeAcctApplicationID is the Acct-Application-Id AVP code (RFC 6733 §6.10).
	AVPCodeAcctApplicationID uint32 = 259
	// AVPCodeInbandSecurityID is the Inband-Security-Id AVP code (RFC 6733 §6.10).
	AVPCodeInbandSecurityID uint32 = 299
	// AVPCodeVendorSpecificApplicationID is the Vendor-Specific-Application-Id
	// AVP code (RFC 6733 §6.12).
	AVPCodeVendorSpecificApplicationID uint32 = 260
	// AVPCodeResultCode is the Result-Code AVP code (RFC 6733 §6.7.1 ish).
	AVPCodeResultCode uint32 = 268
	// AVPCodeExperimentalResult is the Experimental-Result AVP code.
	AVPCodeExperimentalResult uint32 = 297
	// AVPCodeExperimentalResultCode is the Experimental-Result-Code AVP
	// code (RFC 6733 §7.6). Carried inside the Experimental-Result
	// grouped AVP; the value range is vendor-specific (3GPP uses 2xxx
	// for success, 4xxx for failures — see 3GPP TS 29.229 §6.4).
	AVPCodeExperimentalResultCode uint32 = 298
	// AVPCodeErrorReportingHost is the Error-Reporting-Host AVP code.
	AVPCodeErrorReportingHost uint32 = 294
	// AVPCodeErrorMessage is the Error-Message AVP code.
	AVPCodeErrorMessage uint32 = 281
	// AVPCodeFailedAVP is the Failed-AVP AVP code.
	AVPCodeFailedAVP uint32 = 279
	// AVPCodeRouteRecord is the Route-Record AVP code (RFC 6733 §6.8).
	AVPCodeRouteRecord uint32 = 282
	// AVPCodeProxyInfo is the Proxy-Info AVP code (RFC 6733 §6.10.2).
	AVPCodeProxyInfo uint32 = 284
	// AVPCodeDisconnectCause is the Disconnect-Cause AVP code (RFC 6733 §5.4.2).
	AVPCodeDisconnectCause uint32 = 273
	// AVPCodeFirmwareRevision is the Firmware-Revision AVP code
	// (note: 3GPP uses 391 for the Sh-specific variant; the base
	// Diameter value is 267 — Kamailio's CDP uses 267).
	AVPCodeFirmwareRevision uint32 = 267
	// AVPCodeSessionID is the Session-Id AVP code (RFC 6733 §8.1).
	AVPCodeSessionID uint32 = 263
	// AVPCodeSessionBinding is the Session-Binding AVP code.
	AVPCodeSessionBinding uint32 = 272
	// AVPCodeSessionTimeout is the Session-Timeout AVP code (RFC 6733 §8.8).
	AVPCodeSessionTimeout uint32 = 271
	// AVPCodeUserName is the User-Name AVP code (RFC 6733 §6.1).
	AVPCodeUserName uint32 = 1
	// AVPCodeAuthSessionState is the Auth-Session-State AVP code (RFC 6733 §8.11).
	AVPCodeAuthSessionState uint32 = 277
)

// DisconnectCause values (RFC 6733 §5.4.2).
const (
	// DisconnectCauseRebooting indicates the peer is restarting.
	DisconnectCauseRebooting uint32 = 0
	// DisconnectCauseBusy indicates the peer is too busy to handle traffic.
	DisconnectCauseBusy uint32 = 1
	// DisconnectCauseDoNotWantToTalkToYou indicates the peer is refusing
	// further communication (RFC 6733 §5.4.3).
	DisconnectCauseDoNotWantToTalkToYou uint32 = 2
)

// ---------------------------------------------------------------------------
// Result-Code (RFC 6733 §7.x)
// ---------------------------------------------------------------------------

const (
	// ResultMultiRoundAuth indicates that an additional round of
	// authentication is required (1xxx class).
	ResultMultiRoundAuth uint32 = 1001
	// ResultSuccess indicates that the request was successfully completed
	// (2xxx class).
	ResultSuccess uint32 = 2001
	// ResultLimitedSuccess indicates a partial success that requires the
	// application to take further action (2xxx class).
	ResultLimitedSuccess uint32 = 2002
	// ResultCommandUnsupported indicates the receiving peer does not
	// recognise the command code (3xxx class, protocol errors).
	ResultCommandUnsupported uint32 = 3001
	// ResultUnableToDeliver indicates the message could not be delivered
	// to the destination.
	ResultUnableToDeliver uint32 = 3002
	// ResultRealmNotServed indicates the realm was not recognised.
	ResultRealmNotServed uint32 = 3003
	// ResultTooBusy indicates the peer is too busy to honour the request.
	ResultTooBusy uint32 = 3004
	// ResultRoutingError indicates a routing-loop was detected.
	ResultRoutingError uint32 = 3005
	// ResultUnknownPeer indicates the peer is unknown in the routing table.
	ResultUnknownPeer uint32 = 3018
	// ResultAVPUnsupported indicates the AVP is unrecognised and the M-bit
	// is set (4xxx class, transient failures).
	ResultAVPUnsupported uint32 = 5001
	// ResultAVPNotAllowed indicates the AVP was not allowed in this message.
	ResultAVPNotAllowed uint32 = 5008
	// ResultAuthNoSubscriptions indicates the user has no subscriptions.
	ResultAuthNoSubscriptions uint32 = 4181
	// ResultAuthTooManyChallenges indicates too many auth challenges were
	// sent (4xxx class).
	ResultAuthTooManyChallenges uint32 = 4182
	// ResultUnableToComply indicates the peer cannot comply with the request.
	ResultUnableToComply uint32 = 5031
	// ResultUnknownSessionID indicates the Session-Id was not recognised
	// (5xxx class, permanent failures).
	ResultUnknownSessionID uint32 = 5002
	// ResultAuthorizationRejected indicates authorization was rejected.
	ResultAuthorizationRejected uint32 = 5003
)

// ResultCodeClass returns the result-code class number 1..5 (RFC 6733 §7).
//   - 1xxx: Informational
//   - 2xxx: Success
//   - 3xxx: Protocol errors
//   - 4xxx: Transient failures
//   - 5xxx: Permanent failures
func ResultCodeClass(code uint32) int { return int(code / 1000) }

// ResultClassInfo reports whether code is in the informational class.
func ResultClassInfo(code uint32) bool { return ResultCodeClass(code) == 1 }

// ResultClassSuccess reports whether code is in the success class.
func ResultClassSuccess(code uint32) bool { return ResultCodeClass(code) == 2 }

// ResultClassProtocolError reports whether code is in the protocol-error class.
func ResultClassProtocolError(code uint32) bool { return ResultCodeClass(code) == 3 }

// ResultClassTransientFailure reports whether code is in the transient-failure class.
func ResultClassTransientFailure(code uint32) bool { return ResultCodeClass(code) == 4 }

// ResultClassPermanentFailure reports whether code is in the permanent-failure class.
func ResultClassPermanentFailure(code uint32) bool { return ResultCodeClass(code) == 5 }

// IsSuccessResult is a convenience wrapper for ResultClassSuccess.
func IsSuccessResult(code uint32) bool { return ResultClassSuccess(code) }

// ---------------------------------------------------------------------------
// Auth-Session-State values (RFC 6733 §8.11)
// ---------------------------------------------------------------------------

const (
	AuthSessionNoStateMaintained uint32 = 0
	AuthSessionStateMaintained   uint32 = 1
)
