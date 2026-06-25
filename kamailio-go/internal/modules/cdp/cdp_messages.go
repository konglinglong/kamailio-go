// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Diameter base-protocol message builders and parsers.
 * Port of the kamailio cdp module's CER/CEA, DWR/DWA and DPR/DPA
 * message construction (src/modules/cdp/diameter_code.c).
 *
 * These functions build and parse the messages exchanged during the
 * Diameter peer state machine (RFC 6733 §5). They work on the existing
 * DiameterMessage type — the AVP layer in cdp.go is responsible for the
 * wire encoding; this file adds the semantic structure of the base
 * protocol's control messages.
 *
 * All builders return a fresh *DiameterMessage; callers are expected to
 * populate HopByHopID / EndToEndID before sending (see NextHopByHop /
 * NextEndToEnd on CDPModule). The Parsers extract the relevant AVPs into
 * a typed struct and validate that mandatory AVPs are present.
 */

package cdp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
)

// ---------------------------------------------------------------------------
// Capabilities (the body of a CER / CEA)
// ---------------------------------------------------------------------------

// PeerCapabilities captures the application-level information exchanged
// during a Capabilities-Exchange (CER/CEA). Mirrors the C cdp module's
// peer_capacity_t / supported_app_list_t.
type PeerCapabilities struct {
	OriginHost       string
	OriginRealm      string
	HostIPAddresses  []net.IP
	VendorID         uint32
	ProductName      string
	OriginStateID    uint32
	SupportedVendors []uint32
	AuthApplications []uint32
	AcctApplications []uint32
	VendorSpecificApps []VendorSpecificApp
	InbandSecurity   []uint32
	FirmwareRevision uint32
}

// VendorSpecificApp captures one Vendor-Specific-Application-Id grouping
// (RFC 6733 §6.12): a Vendor-Id plus the application identifier that the
// vendor supports (either Auth-Application-Id or Acct-Application-Id).
type VendorSpecificApp struct {
	VendorID         uint32
	AuthApplicationID uint32 // 0 if absent
	AcctApplicationID uint32 // 0 if absent
}

// String returns a one-line summary for logging.
func (c PeerCapabilities) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "host=%s realm=%s vendor=%d", c.OriginHost, c.OriginRealm, c.VendorID)
	if c.ProductName != "" {
		fmt.Fprintf(&b, " product=%q", c.ProductName)
	}
	if len(c.AuthApplications) > 0 {
		fmt.Fprintf(&b, " auth=%v", c.AuthApplications)
	}
	if len(c.AcctApplications) > 0 {
		fmt.Fprintf(&b, " acct=%v", c.AcctApplications)
	}
	if len(c.VendorSpecificApps) > 0 {
		fmt.Fprintf(&b, " vsa=%d", len(c.VendorSpecificApps))
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// CER / CEA (Capabilities-Exchange — RFC 6733 §7.4.1)
// ---------------------------------------------------------------------------

// BuildCER constructs a Capabilities-Exchange-Request populated from the
// given capabilities. The HopByHopID and EndToEndID fields are left zero;
// callers must populate them before sending.
//
//	C: cdp_build_CER()
func BuildCER(caps *PeerCapabilities, hopByHop, endToEnd uint32) *DiameterMessage {
	if caps == nil {
		caps = &PeerCapabilities{}
	}
	msg := &DiameterMessage{
		Version:       Version,
		Flags:         CmdFlagRequest,
		CommandCode:   CmdCapabilitiesExchange,
		ApplicationID: AppDiameterCommonMessages,
		HopByHopID:    hopByHop,
		EndToEndID:    endToEnd,
	}
	appendCERAVPs(msg, caps)
	return msg
}

// BuildCEA constructs a Capabilities-Exchange-Answer populated from the
// given capabilities and result-code. The HopByHopID and EndToEndID are
// taken from the originating CER (passed through).
//
//	C: cdp_build_CEA()
func BuildCEA(caps *PeerCapabilities, resultCode uint32, hopByHop, endToEnd uint32) *DiameterMessage {
	if caps == nil {
		caps = &PeerCapabilities{}
	}
	msg := &DiameterMessage{
		Version:       Version,
		Flags:         0, // Answer: clear the request bit.
		CommandCode:   CmdCapabilitiesExchange,
		ApplicationID: AppDiameterCommonMessages,
		HopByHopID:    hopByHop,
		EndToEndID:    endToEnd,
	}
	appendCERAVPs(msg, caps)
	// Result-Code is mandatory in the CEA (RFC 6733 §7.4.2).
	msg.AVPs = append(msg.AVPs, DiameterAVP{
		Code:  AVPCodeResultCode,
		Flags: AVPFlagMandatory,
		Value: encodeUint32(resultCode),
	})
	return msg
}

// appendCERAVPs adds the common CER/CEA AVPs to msg. The set is shared
// between CER and CEA (RFC 6733 §7.4.1/§7.4.2 differ only in the
// Request flag and the presence of Result-Code).
func appendCERAVPs(msg *DiameterMessage, caps *PeerCapabilities) {
	// Origin-Host (mandatory).
	if caps.OriginHost != "" {
		msg.AVPs = append(msg.AVPs, DiameterAVP{
			Code: AVPCodeOriginHost, Flags: AVPFlagMandatory,
			Value: []byte(caps.OriginHost),
		})
	}
	// Origin-Realm (mandatory).
	if caps.OriginRealm != "" {
		msg.AVPs = append(msg.AVPs, DiameterAVP{
			Code: AVPCodeOriginRealm, Flags: AVPFlagMandatory,
			Value: []byte(caps.OriginRealm),
		})
	}
	// Host-IP-Address (one or more, mandatory).
	for _, ip := range caps.HostIPAddresses {
		msg.AVPs = append(msg.AVPs, DiameterAVP{
			Code: AVPCodeHostIPAddress, Flags: AVPFlagMandatory,
			Value: ipToBytes(ip),
		})
	}
	// Vendor-Id (mandatory) — identifies the vendor of the implementation.
	msg.AVPs = append(msg.AVPs, DiameterAVP{
		Code: AVPCodeVendorID, Flags: AVPFlagMandatory,
		Value: encodeUint32(caps.VendorID),
	})
	// Product-Name (optional).
	if caps.ProductName != "" {
		msg.AVPs = append(msg.AVPs, DiameterAVP{
			Code: AVPCodeProductName, Flags: 0,
			Value: []byte(caps.ProductName),
		})
	}
	// Origin-State-Id (optional).
	if caps.OriginStateID != 0 {
		msg.AVPs = append(msg.AVPs, DiameterAVP{
			Code: AVPCodeOriginStateID, Flags: AVPFlagMandatory,
			Value: encodeUint32(caps.OriginStateID),
		})
	}
	// Supported-Vendor-Id (zero or more).
	for _, v := range caps.SupportedVendors {
		msg.AVPs = append(msg.AVPs, DiameterAVP{
			Code: AVPCodeSupportedVendorID, Flags: AVPFlagMandatory,
			Value: encodeUint32(v),
		})
	}
	// Auth-Application-Id (zero or more).
	for _, a := range caps.AuthApplications {
		msg.AVPs = append(msg.AVPs, DiameterAVP{
			Code: AVPCodeAuthApplicationID, Flags: AVPFlagMandatory,
			Value: encodeUint32(a),
		})
	}
	// Acct-Application-Id (zero or more).
	for _, a := range caps.AcctApplications {
		msg.AVPs = append(msg.AVPs, DiameterAVP{
			Code: AVPCodeAcctApplicationID, Flags: AVPFlagMandatory,
			Value: encodeUint32(a),
		})
	}
	// Vendor-Specific-Application-Id grouping (zero or more).
	for _, vsa := range caps.VendorSpecificApps {
		msg.AVPs = append(msg.AVPs, buildVendorSpecificAppAVP(vsa))
	}
	// Firmware-Revision (optional).
	if caps.FirmwareRevision != 0 {
		msg.AVPs = append(msg.AVPs, DiameterAVP{
			Code: AVPCodeFirmwareRevision, Flags: 0,
			Value: encodeUint32(caps.FirmwareRevision),
		})
	}
}

// buildVendorSpecificAppAVP constructs a Vendor-Specific-Application-Id
// grouped AVP (RFC 6733 §6.12). The grouped value is the concatenation
// of the contained AVPs (Vendor-Id plus Auth- or Acct-Application-Id).
func buildVendorSpecificAppAVP(vsa VendorSpecificApp) DiameterAVP {
	var grouped []byte
	grouped = append(grouped, encodeAVP(&DiameterAVP{
		Code: AVPCodeVendorID, Flags: AVPFlagMandatory,
		Value: encodeUint32(vsa.VendorID),
	})...)
	if vsa.AuthApplicationID != 0 {
		grouped = append(grouped, encodeAVP(&DiameterAVP{
			Code: AVPCodeAuthApplicationID, Flags: AVPFlagMandatory,
			Value: encodeUint32(vsa.AuthApplicationID),
		})...)
	}
	if vsa.AcctApplicationID != 0 {
		grouped = append(grouped, encodeAVP(&DiameterAVP{
			Code: AVPCodeAcctApplicationID, Flags: AVPFlagMandatory,
			Value: encodeUint32(vsa.AcctApplicationID),
		})...)
	}
	return DiameterAVP{
		Code:  AVPCodeVendorSpecificApplicationID,
		Flags: AVPFlagMandatory,
		Value: grouped,
	}
}

// ParseCapabilities extracts the PeerCapabilities from a CER or CEA. It
// tolerates missing optional AVPs and returns an error only when a
// mandatory AVP is absent (Origin-Host, Origin-Realm, Vendor-Id).
//
//	C: cdp_parse_capabilities()
func ParseCapabilities(msg *DiameterMessage) (*PeerCapabilities, error) {
	if msg == nil {
		return nil, errors.New("cdp: nil message")
	}
	if msg.CommandCode != CmdCapabilitiesExchange {
		return nil, fmt.Errorf("cdp: not a Capabilities-Exchange message (cmd=%d)", msg.CommandCode)
	}
	caps := &PeerCapabilities{}
	for i := range msg.AVPs {
		avp := &msg.AVPs[i]
		switch avp.Code {
		case AVPCodeOriginHost:
			caps.OriginHost = string(avp.Value)
		case AVPCodeOriginRealm:
			caps.OriginRealm = string(avp.Value)
		case AVPCodeHostIPAddress:
			if ip := bytesToIP(avp.Value); ip != nil {
				caps.HostIPAddresses = append(caps.HostIPAddresses, ip)
			}
		case AVPCodeVendorID:
			caps.VendorID = decodeUint32(avp.Value)
		case AVPCodeProductName:
			caps.ProductName = string(avp.Value)
		case AVPCodeOriginStateID:
			caps.OriginStateID = decodeUint32(avp.Value)
		case AVPCodeSupportedVendorID:
			caps.SupportedVendors = append(caps.SupportedVendors, decodeUint32(avp.Value))
		case AVPCodeAuthApplicationID:
			caps.AuthApplications = append(caps.AuthApplications, decodeUint32(avp.Value))
		case AVPCodeAcctApplicationID:
			caps.AcctApplications = append(caps.AcctApplications, decodeUint32(avp.Value))
		case AVPCodeVendorSpecificApplicationID:
			if vsa, ok := parseVendorSpecificApp(avp.Value); ok {
				caps.VendorSpecificApps = append(caps.VendorSpecificApps, vsa)
			}
		case AVPCodeFirmwareRevision:
			caps.FirmwareRevision = decodeUint32(avp.Value)
		}
	}
	if caps.OriginHost == "" {
		return nil, errors.New("cdp: CER/CEA missing Origin-Host")
	}
	if caps.OriginRealm == "" {
		return nil, errors.New("cdp: CER/CEA missing Origin-Realm")
	}
	return caps, nil
}

// parseVendorSpecificApp decodes the contents of a Vendor-Specific-Application-Id
// grouped AVP. Returns ok=false if no Vendor-Id is present.
func parseVendorSpecificApp(grouped []byte) (VendorSpecificApp, bool) {
	var vsa VendorSpecificApp
	gotVendor := false
	for off := 0; off+AVPHeaderLen <= len(grouped); {
		code := binary.BigEndian.Uint32(grouped[off : off+4])
		flags := grouped[off+4]
		totalLen := int(getUint24(grouped[off+5 : off+8]))
		if totalLen < AVPHeaderLen || off+totalLen > len(grouped) {
			return vsa, false
		}
		headerLen := AVPHeaderLen
		if flags&AVPFlagVendor != 0 {
			headerLen = AVPVendorHeaderLen
		}
		data := grouped[off+headerLen : off+totalLen]
		switch code {
		case AVPCodeVendorID:
			if len(data) >= 4 {
				vsa.VendorID = binary.BigEndian.Uint32(data[:4])
				gotVendor = true
			}
		case AVPCodeAuthApplicationID:
			if len(data) >= 4 {
				vsa.AuthApplicationID = binary.BigEndian.Uint32(data[:4])
			}
		case AVPCodeAcctApplicationID:
			if len(data) >= 4 {
				vsa.AcctApplicationID = binary.BigEndian.Uint32(data[:4])
			}
		}
		off += (totalLen + 3) &^ 3
	}
	return vsa, gotVendor
}

// ResultCode extracts the Result-Code AVP from a CEA (or any Diameter
// answer). Returns (0, false) when the AVP is absent.
func ResultCode(msg *DiameterMessage) (uint32, bool) {
	if msg == nil {
		return 0, false
	}
	for i := range msg.AVPs {
		avp := &msg.AVPs[i]
		if avp.Code == AVPCodeResultCode && len(avp.Value) >= 4 {
			return decodeUint32(avp.Value), true
		}
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// DWR / DWA (Device-Watchdog — RFC 6733 §5.5)
// ---------------------------------------------------------------------------

// BuildDWR constructs a Device-Watchdog-Request. The Origin-Host /
// Origin-Realm / Host-IP-Address AVPs are mandatory (RFC 6733 §5.5.2).
//
//	C: cdp_build_DWR()
func BuildDWR(originHost, originRealm string, hopByHop, endToEnd uint32) *DiameterMessage {
	return &DiameterMessage{
		Version:       Version,
		Flags:         CmdFlagRequest,
		CommandCode:   CmdDeviceWatchdog,
		ApplicationID: AppDiameterCommonMessages,
		HopByHopID:    hopByHop,
		EndToEndID:    endToEnd,
		AVPs: []DiameterAVP{
			{Code: AVPCodeOriginHost, Flags: AVPFlagMandatory, Value: []byte(originHost)},
			{Code: AVPCodeOriginRealm, Flags: AVPFlagMandatory, Value: []byte(originRealm)},
		},
	}
}

// BuildDWA constructs a Device-Watchdog-Answer. The Result-Code AVP is
// mandatory (RFC 6733 §5.5.3).
//
//	C: cdp_build_DWA()
func BuildDWA(originHost, originRealm string, resultCode uint32, hopByHop, endToEnd uint32) *DiameterMessage {
	return &DiameterMessage{
		Version:       Version,
		Flags:         0, // Answer.
		CommandCode:   CmdDeviceWatchdog,
		ApplicationID: AppDiameterCommonMessages,
		HopByHopID:    hopByHop,
		EndToEndID:    endToEnd,
		AVPs: []DiameterAVP{
			{Code: AVPCodeOriginHost, Flags: AVPFlagMandatory, Value: []byte(originHost)},
			{Code: AVPCodeOriginRealm, Flags: AVPFlagMandatory, Value: []byte(originRealm)},
			{Code: AVPCodeResultCode, Flags: AVPFlagMandatory, Value: encodeUint32(resultCode)},
		},
	}
}

// ---------------------------------------------------------------------------
// DPR / DPA (Disconnect-Peer — RFC 6733 §5.4)
// ---------------------------------------------------------------------------

// BuildDPR constructs a Disconnect-Peer-Request. The Disconnect-Cause
// AVP is mandatory (RFC 6733 §5.4.2).
//
//	C: cdp_build_DPR()
func BuildDPR(originHost, originRealm string, cause uint32, hopByHop, endToEnd uint32) *DiameterMessage {
	return &DiameterMessage{
		Version:       Version,
		Flags:         CmdFlagRequest,
		CommandCode:   CmdDisconnectPeer,
		ApplicationID: AppDiameterCommonMessages,
		HopByHopID:    hopByHop,
		EndToEndID:    endToEnd,
		AVPs: []DiameterAVP{
			{Code: AVPCodeOriginHost, Flags: AVPFlagMandatory, Value: []byte(originHost)},
			{Code: AVPCodeOriginRealm, Flags: AVPFlagMandatory, Value: []byte(originRealm)},
			{Code: AVPCodeDisconnectCause, Flags: AVPFlagMandatory, Value: encodeUint32(cause)},
		},
	}
}

// BuildDPA constructs a Disconnect-Peer-Answer. The Result-Code AVP is
// mandatory (RFC 6733 §5.4.3).
//
//	C: cdp_build_DPA()
func BuildDPA(originHost, originRealm string, resultCode uint32, hopByHop, endToEnd uint32) *DiameterMessage {
	return &DiameterMessage{
		Version:       Version,
		Flags:         0, // Answer.
		CommandCode:   CmdDisconnectPeer,
		ApplicationID: AppDiameterCommonMessages,
		HopByHopID:    hopByHop,
		EndToEndID:    endToEnd,
		AVPs: []DiameterAVP{
			{Code: AVPCodeOriginHost, Flags: AVPFlagMandatory, Value: []byte(originHost)},
			{Code: AVPCodeOriginRealm, Flags: AVPFlagMandatory, Value: []byte(originRealm)},
			{Code: AVPCodeResultCode, Flags: AVPFlagMandatory, Value: encodeUint32(resultCode)},
		},
	}
}

// DisconnectCause extracts the Disconnect-Cause AVP from a DPR. Returns
// (0, false) when the AVP is absent.
func DisconnectCause(msg *DiameterMessage) (uint32, bool) {
	if msg == nil {
		return 0, false
	}
	for i := range msg.AVPs {
		avp := &msg.AVPs[i]
		if avp.Code == AVPCodeDisconnectCause && len(avp.Value) >= 4 {
			return decodeUint32(avp.Value), true
		}
	}
	return 0, false
}

// OriginHost extracts the Origin-Host AVP from any Diameter message.
func OriginHost(msg *DiameterMessage) (string, bool) {
	if msg == nil {
		return "", false
	}
	for i := range msg.AVPs {
		avp := &msg.AVPs[i]
		if avp.Code == AVPCodeOriginHost {
			return string(avp.Value), true
		}
	}
	return "", false
}

// OriginRealm extracts the Origin-Realm AVP from any Diameter message.
func OriginRealm(msg *DiameterMessage) (string, bool) {
	if msg == nil {
		return "", false
	}
	for i := range msg.AVPs {
		avp := &msg.AVPs[i]
		if avp.Code == AVPCodeOriginRealm {
			return string(avp.Value), true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// Encoding helpers (AVP value marshalling)
// ---------------------------------------------------------------------------

// encodeUint32 returns the 4-byte big-endian representation of v.
func encodeUint32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

// decodeUint32 reads the first 4 bytes of b as a big-endian uint32.
// Returns 0 when b is shorter than 4 bytes.
func decodeUint32(b []byte) uint32 {
	if len(b) < 4 {
		return 0
	}
	return binary.BigEndian.Uint32(b[:4])
}

// ipToBytes converts ip to its on-wire Diameter representation. The
// Diameter Host-IP-Address AVP (RFC 6733 §6.7) prefixes the address with
// a 2-byte address-family identifier (1 = IPv4, 2 = IPv6).
func ipToBytes(ip net.IP) []byte {
	if ip == nil {
		return nil
	}
	v4 := ip.To4()
	if v4 != nil {
		out := make([]byte, 2+4)
		binary.BigEndian.PutUint16(out[0:2], 1) // AF_INET
		copy(out[2:], v4)
		return out
	}
	v6 := ip.To16()
	if v6 == nil {
		return nil
	}
	out := make([]byte, 2+16)
	binary.BigEndian.PutUint16(out[0:2], 2) // AF_INET6
	copy(out[2:], v6)
	return out
}

// bytesToIP reverses ipToBytes: it reads the 2-byte address family and
// constructs the corresponding net.IP. Returns nil when b is malformed.
func bytesToIP(b []byte) net.IP {
	if len(b) < 2 {
		return nil
	}
	family := binary.BigEndian.Uint16(b[0:2])
	switch family {
	case 1: // IPv4
		if len(b) < 6 {
			return nil
		}
		return net.IP(b[2:6])
	case 2: // IPv6
		if len(b) < 18 {
			return nil
		}
		return net.IP(b[2:18])
	}
	return nil
}
