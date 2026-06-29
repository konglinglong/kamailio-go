// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * STUN protocol support - matching Kamailio's C stun.c/stun.h
 * Implements RFC 5389 STUN message parsing and building,
 * including Binding Response and XOR-Mapped-Address support.
 */

package stun

import (
	"encoding/binary"
	"errors"
	"net"
)

// MagicCookie is the STUN magic cookie identifying RFC 5389 messages.
const MagicCookie uint32 = 0x2112A442

// Header size on the wire: type(2) + length(2) + magic cookie(4) + transaction id(12).
const headerSize = 20

// STUN message type constants.
const (
	BindingRequest         = 0x0001
	BindingResponse        = 0x0101
	BindingErrorResponse   = 0x0111
	SharedSecretRequest    = 0x0002
	SharedSecretResponse   = 0x0102
	SharedSecretErrorResponse = 0x0112
)

// STUN attribute type constants.
const (
	MappedAddress      = 0x0001
	ResponseAddress    = 0x0002
	ChangeRequest      = 0x0003
	SourceAddress      = 0x0004
	ChangedAddress     = 0x0005
	Username           = 0x0006
	Password           = 0x0007
	MessageIntegrity   = 0x0008
	ErrorCode          = 0x0009
	UnknownAttributes = 0x000A
	ReflectedFrom      = 0x000B
	XorMappedAddress   = 0x0020
	Software           = 0x8022
	AlternateServer    = 0x8023
	Fingerprint        = 0x8028
)

// Address family values used inside Mapped-Address / XOR-Mapped-Address.
const (
	familyIPv4 = 0x01
	familyIPv6 = 0x02
)

// STUNHeader represents a STUN message header. The magic cookie is implicit in
// the wire format (always MagicCookie) and therefore not stored here.
type STUNHeader struct {
	MessageType   uint16
	MessageLength uint16
	TransactionID [12]byte
}

// STUNAttribute represents a single TLV attribute. Value holds the raw,
// unpadded attribute value (padding is applied only on the wire).
type STUNAttribute struct {
	Type   uint16
	Length uint16
	Value  []byte
}

// STUNMessage represents a parsed STUN message.
type STUNMessage struct {
	Header     STUNHeader
	Attributes []STUNAttribute
}

// IsSTUNMessage reports whether data looks like a STUN message: the first two
// bits of the first byte must be zero (per RFC 5389) and the buffer must be
// large enough to hold a header.
func IsSTUNMessage(data []byte) bool {
	if len(data) < headerSize {
		return false
	}
	// The two most significant bits of a STUN message type must be zero.
	return data[0]&0xc0 == 0
}

// ParseSTUN parses a STUN message from data.
func ParseSTUN(data []byte) (*STUNMessage, error) {
	if len(data) < headerSize {
		return nil, errors.New("stun: message too short for header")
	}
	if !IsSTUNMessage(data) {
		return nil, errors.New("stun: not a STUN message (first two bits set)")
	}

	msg := &STUNMessage{}
	msg.Header.MessageType = binary.BigEndian.Uint16(data[0:2])
	msg.Header.MessageLength = binary.BigEndian.Uint16(data[2:4])

	cookie := binary.BigEndian.Uint32(data[4:8])
	if cookie != MagicCookie {
		return nil, errors.New("stun: invalid magic cookie")
	}

	copy(msg.Header.TransactionID[:], data[8:headerSize])

	// MessageLength covers the body (attributes) only.
	bodyLen := int(msg.Header.MessageLength)
	if headerSize+bodyLen > len(data) {
		return nil, errors.New("stun: declared length exceeds buffer")
	}

	off := headerSize
	end := headerSize + bodyLen
	for off+4 <= end {
		attrType := binary.BigEndian.Uint16(data[off : off+2])
		attrLen := binary.BigEndian.Uint16(data[off+2 : off+4])
		off += 4
		if off+int(attrLen) > end {
			return nil, errors.New("stun: attribute length exceeds message body")
		}
		val := make([]byte, attrLen)
		copy(val, data[off:off+int(attrLen)])
		msg.Attributes = append(msg.Attributes, STUNAttribute{
			Type:   attrType,
			Length: attrLen,
			Value:  val,
		})
		// Attributes are padded to 4-byte boundaries.
		off += paddedLen(int(attrLen))
	}

	return msg, nil
}

// Encode serializes a STUNMessage to its wire representation.
func Encode(msg *STUNMessage) []byte {
	// Compute total body length (attributes with padding).
	bodyLen := 0
	for _, attr := range msg.Attributes {
		bodyLen += 4 + paddedLen(len(attr.Value))
	}

	buf := make([]byte, headerSize+bodyLen)
	binary.BigEndian.PutUint16(buf[0:2], msg.Header.MessageType)
	binary.BigEndian.PutUint16(buf[2:4], uint16(bodyLen))
	binary.BigEndian.PutUint32(buf[4:8], MagicCookie)
	copy(buf[8:headerSize], msg.Header.TransactionID[:])

	off := headerSize
	for _, attr := range msg.Attributes {
		binary.BigEndian.PutUint16(buf[off:off+2], attr.Type)
		binary.BigEndian.PutUint16(buf[off+2:off+4], uint16(len(attr.Value)))
		off += 4
		copy(buf[off:off+len(attr.Value)], attr.Value)
		off += paddedLen(len(attr.Value))
	}

	// Keep the in-memory header length in sync with the encoded body.
	msg.Header.MessageLength = uint16(bodyLen)
	return buf
}

// AddAttribute appends a new attribute to msg.
func AddAttribute(msg *STUNMessage, attrType uint16, value []byte) {
	v := make([]byte, len(value))
	copy(v, value)
	msg.Attributes = append(msg.Attributes, STUNAttribute{
		Type:   attrType,
		Length: uint16(len(v)),
		Value:  v,
	})
}

// GetAttribute returns the first attribute of attrType in msg, or nil if none.
func GetAttribute(msg *STUNMessage, attrType uint16) *STUNAttribute {
	for i := range msg.Attributes {
		if msg.Attributes[i].Type == attrType {
			return &msg.Attributes[i]
		}
	}
	return nil
}

// BuildBindingResponse builds a Binding Response carrying a Mapped-Address
// attribute for the given mapped IP and port.
func BuildBindingResponse(transactionID [12]byte, mappedIP net.IP, mappedPort int) []byte {
	msg := &STUNMessage{
		Header: STUNHeader{
			MessageType:   BindingResponse,
			TransactionID: transactionID,
		},
	}
	AddAttribute(msg, MappedAddress, encodeMappedAddress(mappedIP, mappedPort, false, transactionID))
	return Encode(msg)
}

// BuildXorBindingResponse builds a Binding Response carrying an XOR-Mapped-Address
// attribute for the given mapped IP and port.
func BuildXorBindingResponse(transactionID [12]byte, mappedIP net.IP, mappedPort int) []byte {
	msg := &STUNMessage{
		Header: STUNHeader{
			MessageType:   BindingResponse,
			TransactionID: transactionID,
		},
	}
	AddAttribute(msg, XorMappedAddress, encodeMappedAddress(mappedIP, mappedPort, true, transactionID))
	return Encode(msg)
}

// encodeMappedAddress encodes a Mapped-Address (xor=false) or XOR-Mapped-Address
// (xor=true) attribute value.
func encodeMappedAddress(ip net.IP, port int, xor bool, txnID [12]byte) []byte {
	if v4 := ip.To4(); v4 != nil {
		val := make([]byte, 8)
		val[0] = 0
		val[1] = familyIPv4
		portVal := uint16(port)
		if xor {
			portVal ^= uint16(MagicCookie >> 16)
		}
		binary.BigEndian.PutUint16(val[2:4], portVal)
		addr := binary.BigEndian.Uint32(v4)
		if xor {
			addr ^= MagicCookie
		}
		binary.BigEndian.PutUint32(val[4:8], addr)
		return val
	}

	// IPv6
	ip16 := ip.To16()
	val := make([]byte, 20)
	val[0] = 0
	val[1] = familyIPv6
	portVal := uint16(port)
	if xor {
		portVal ^= uint16(MagicCookie >> 16)
	}
	binary.BigEndian.PutUint16(val[2:4], portVal)
	if xor {
		// XOR with magic cookie (4 bytes) followed by transaction id (12 bytes).
		cookie := make([]byte, 16)
		binary.BigEndian.PutUint32(cookie[0:4], MagicCookie)
		copy(cookie[4:16], txnID[:])
		for i := 0; i < 16; i++ {
			val[4+i] = ip16[i] ^ cookie[i]
		}
	} else {
		copy(val[4:20], ip16)
	}
	return val
}

// paddedLen returns n rounded up to the next 4-byte boundary.
func paddedLen(n int) int {
	return (n + 3) &^ 3
}
