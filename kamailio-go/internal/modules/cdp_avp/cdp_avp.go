// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * CDP AVP module - Diameter AVP builders and parsers.
 * Port of the kamailio cdp_avp module (src/modules/cdp_avp).
 *
 * The cdp_avp module provides helpers to build and parse the Diameter
 * AVPs used by the IMS core (3GPP TS 29.229 / RFC 6733). It builds on
 * the cdp package's DiameterAVP / DiameterMessage types: the builders
 * return *cdp.DiameterAVP ready to be appended to a message, and the
 * parser reads an AVP from its on-wire form.
 *
 * It is safe for concurrent use; the builders and parsers are stateless
 * apart from the AVPBuilder / AVPParser convenience wrappers.
 */

package cdp_avp

import (
	"encoding/binary"
	"fmt"

	"github.com/kamailio/kamailio-go/internal/modules/cdp"
)

// Standard Diameter AVP codes (RFC 6733 / 3GPP TS 29.229).
const (
	CodeUserName           = 1
	CodeResultCode         = 268
	CodeAuthSessionState   = 277
	CodeSubscriptionID     = 443
	CodeSubscriptionIDType = 450
	CodeSubscriptionIDData = 444
)

// Auth-Session-State enumerated values (RFC 6733).
const (
	AuthSessionNoStateMaintained = 0
	AuthSessionStateMaintained   = 1
)

// Subscription-Id-Type enumerated values (RFC 4006).
const (
	SubIDEndUserE164   = 0
	SubIDEndUserIMSI   = 1
	SubIDEndUserSIPURI = 2
	SubIDEndUserNAI    = 3
)

// AVPBuilder builds Diameter AVPs for the IMS core. It is a stateless
// convenience wrapper around the package-level Build* functions.
type AVPBuilder struct{}

// NewAVPBuilder returns a ready-to-use AVPBuilder.
func NewAVPBuilder() *AVPBuilder { return &AVPBuilder{} }

// AuthSessionState builds an Auth-Session-State AVP (Unsigned32).
func (b *AVPBuilder) AuthSessionState(state int) *cdp.DiameterAVP {
	return BuildAuthSessionState(state)
}

// UserName builds a User-Name AVP (UTF8String).
func (b *AVPBuilder) UserName(name string) *cdp.DiameterAVP {
	return BuildUserName(name)
}

// ResultCode builds a Result-Code AVP (Unsigned32).
func (b *AVPBuilder) ResultCode(code int) *cdp.DiameterAVP {
	return BuildResultCode(code)
}

// SubscriptionID builds a Subscription-Id grouped AVP from an id and its
// type (e.g. "END_USER_E164", "END_USER_IMSI", "END_USER_SIP_URI",
// "END_USER_NAI").
func (b *AVPBuilder) SubscriptionID(id, typ string) *cdp.DiameterAVP {
	return BuildSubscriptionID(id, typ)
}

// AVPParser parses Diameter AVPs from their on-wire form. It is a
// stateless convenience wrapper around ParseAVP.
type AVPParser struct{}

// NewAVPParser returns a ready-to-use AVPParser.
func NewAVPParser() *AVPParser { return &AVPParser{} }

// Parse reads a single AVP from data.
func (p *AVPParser) Parse(data []byte) (*cdp.DiameterAVP, error) {
	return ParseAVP(data)
}

// Encode serialises avp to its on-wire form.
func (p *AVPParser) Encode(avp *cdp.DiameterAVP) []byte {
	return EncodeAVP(avp)
}

// ---------------------------------------------------------------------------
// Builders
// ---------------------------------------------------------------------------

// BuildAuthSessionState builds an Auth-Session-State AVP carrying a
// 32-bit enumerated state value.
func BuildAuthSessionState(state int) *cdp.DiameterAVP {
	v := make([]byte, 4)
	binary.BigEndian.PutUint32(v, uint32(state))
	return &cdp.DiameterAVP{
		Code:  CodeAuthSessionState,
		Flags: cdp.AVPFlagMandatory,
		Value: v,
	}
}

// BuildUserName builds a User-Name AVP carrying a UTF8String.
func BuildUserName(name string) *cdp.DiameterAVP {
	return &cdp.DiameterAVP{
		Code:  CodeUserName,
		Flags: cdp.AVPFlagMandatory,
		Value: []byte(name),
	}
}

// BuildResultCode builds a Result-Code AVP carrying a 32-bit result code.
func BuildResultCode(code int) *cdp.DiameterAVP {
	v := make([]byte, 4)
	binary.BigEndian.PutUint32(v, uint32(code))
	return &cdp.DiameterAVP{
		Code:  CodeResultCode,
		Flags: cdp.AVPFlagMandatory,
		Value: v,
	}
}

// BuildSubscriptionID builds a Subscription-Id grouped AVP. The grouped
// value is the concatenation of a Subscription-Id-Type sub-AVP and a
// Subscription-Id-Data sub-AVP.
func BuildSubscriptionID(id, typ string) *cdp.DiameterAVP {
	typeCode := subscriptionIDTypeCode(typ)
	typeVal := make([]byte, 4)
	binary.BigEndian.PutUint32(typeVal, uint32(typeCode))

	subType := &cdp.DiameterAVP{
		Code:  CodeSubscriptionIDType,
		Flags: cdp.AVPFlagMandatory,
		Value: typeVal,
	}
	subData := &cdp.DiameterAVP{
		Code:  CodeSubscriptionIDData,
		Flags: cdp.AVPFlagMandatory,
		Value: []byte(id),
	}
	grouped := append(EncodeAVP(subType), EncodeAVP(subData)...)
	return &cdp.DiameterAVP{
		Code:  CodeSubscriptionID,
		Flags: cdp.AVPFlagMandatory,
		Value: grouped,
	}
}

// subscriptionIDTypeCode maps a human-readable type name to its
// enumerated value. Unknown types default to END_USER_E164.
func subscriptionIDTypeCode(typ string) int {
	switch typ {
	case "END_USER_E164", "E164":
		return SubIDEndUserE164
	case "END_USER_IMSI", "IMSI":
		return SubIDEndUserIMSI
	case "END_USER_SIP_URI", "SIP_URI":
		return SubIDEndUserSIPURI
	case "END_USER_NAI", "NAI":
		return SubIDEndUserNAI
	default:
		return SubIDEndUserE164
	}
}

// ---------------------------------------------------------------------------
// Parser / codec
// ---------------------------------------------------------------------------

// ParseAVP reads a single AVP from the start of data. It mirrors the
// AVP layout implemented by the cdp package.
func ParseAVP(data []byte) (*cdp.DiameterAVP, error) {
	if len(data) < cdp.AVPHeaderLen {
		return nil, fmt.Errorf("cdp_avp: short avp %d bytes", len(data))
	}
	code := binary.BigEndian.Uint32(data[0:4])
	flags := data[4]
	totalLen := int(getUint24(data[5:8]))
	if totalLen < cdp.AVPHeaderLen || totalLen > len(data) {
		return nil, fmt.Errorf("cdp_avp: bad avp length %d", totalLen)
	}
	vendor := flags&cdp.AVPFlagVendor != 0
	headerLen := cdp.AVPHeaderLen
	if vendor {
		headerLen = cdp.AVPVendorHeaderLen
	}
	if totalLen < headerLen {
		return nil, fmt.Errorf("cdp_avp: avp length %d shorter than header", totalLen)
	}
	avp := &cdp.DiameterAVP{Code: code, Flags: flags}
	if vendor {
		avp.VendorID = binary.BigEndian.Uint32(data[8:12])
	}
	dataLen := totalLen - headerLen
	avp.Value = make([]byte, dataLen)
	copy(avp.Value, data[headerLen:headerLen+dataLen])
	return avp, nil
}

// EncodeAVP serialises a single AVP, padding the value to a 4-byte
// boundary. The AVP Length field reports the unpadded length (header +
// data); the padding bytes are appended to the buffer but not counted
// in the length field. It is the inverse of ParseAVP.
func EncodeAVP(avp *cdp.DiameterAVP) []byte {
	if avp == nil {
		return nil
	}
	vendor := avp.Flags&cdp.AVPFlagVendor != 0
	headerLen := cdp.AVPHeaderLen
	if vendor {
		headerLen = cdp.AVPVendorHeaderLen
	}
	dataLen := len(avp.Value)
	paddedLen := (dataLen + 3) &^ 3 // round up to 4-byte boundary
	totalLen := headerLen + dataLen // length field excludes padding
	bufLen := headerLen + paddedLen // buffer includes padding

	buf := make([]byte, bufLen)
	binary.BigEndian.PutUint32(buf[0:4], avp.Code)
	buf[4] = avp.Flags
	putUint24(buf[5:8], uint32(totalLen))
	if vendor {
		binary.BigEndian.PutUint32(buf[8:12], avp.VendorID)
		copy(buf[12:12+dataLen], avp.Value)
	} else {
		copy(buf[8:8+dataLen], avp.Value)
	}
	return buf
}

// GetAVPByCode returns the first AVP with the given code in msg, or nil
// when msg has no such AVP.
func GetAVPByCode(msg *cdp.DiameterMessage, code int) *cdp.DiameterAVP {
	if msg == nil {
		return nil
	}
	for i := range msg.AVPs {
		if int(msg.AVPs[i].Code) == code {
			return &msg.AVPs[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

// putUint24 writes a 24-bit big-endian integer.
func putUint24(b []byte, v uint32) {
	b[0] = byte(v >> 16)
	b[1] = byte(v >> 8)
	b[2] = byte(v)
}

// getUint24 reads a 24-bit big-endian integer.
func getUint24(b []byte) uint32 {
	return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
}
