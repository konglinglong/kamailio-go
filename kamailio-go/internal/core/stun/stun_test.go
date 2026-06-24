// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for STUN protocol support.
 */

package stun

import (
	"encoding/binary"
	"net"
	"testing"
)

// helper to build a transaction id with a known value.
func testTxnID() [12]byte {
	var id [12]byte
	for i := range id {
		id[i] = byte(i + 1)
	}
	return id
}

func TestIsSTUNMessage(t *testing.T) {
	// A valid STUN message has the top two bits of the first byte clear.
	valid := make([]byte, 20)
	valid[0] = 0x00 // 00xxxxxx
	if !IsSTUNMessage(valid) {
		t.Error("expected valid STUN message")
	}

	// First two bits set -> not STUN.
	invalid := make([]byte, 20)
	invalid[0] = 0xc0
	if IsSTUNMessage(invalid) {
		t.Error("expected non-STUN message for 0xc0")
	}

	// Too short.
	if IsSTUNMessage([]byte{0x00, 0x01}) {
		t.Error("expected false for short buffer")
	}
}

func TestEncode(t *testing.T) {
	txnID := testTxnID()
	msg := &STUNMessage{
		Header: STUNHeader{
			MessageType:   BindingRequest,
			TransactionID: txnID,
		},
	}
	AddAttribute(msg, Username, []byte("alice"))

	data := Encode(msg)

	if len(data) < headerSize {
		t.Fatalf("encoded message too short: %d", len(data))
	}
	if got := binary.BigEndian.Uint16(data[0:2]); got != BindingRequest {
		t.Errorf("message type = 0x%04x, want 0x%04x", got, BindingRequest)
	}
	if got := binary.BigEndian.Uint32(data[4:8]); got != MagicCookie {
		t.Errorf("magic cookie = 0x%08x, want 0x%08x", got, MagicCookie)
	}
	var txn [12]byte
	copy(txn[:], data[8:headerSize])
	if txn != txnID {
		t.Errorf("transaction id mismatch")
	}
	// Username "alice" (5 bytes) -> padded to 8 -> body length = 4 + 8 = 12.
	if got := binary.BigEndian.Uint16(data[2:4]); got != 12 {
		t.Errorf("message length = %d, want 12", got)
	}
	// First attribute type and length.
	if got := binary.BigEndian.Uint16(data[headerSize : headerSize+2]); got != Username {
		t.Errorf("attr type = 0x%04x, want 0x%04x", got, Username)
	}
	if got := binary.BigEndian.Uint16(data[headerSize+2 : headerSize+4]); got != 5 {
		t.Errorf("attr length = %d, want 5", got)
	}
}

func TestParseSTUN(t *testing.T) {
	txnID := testTxnID()
	msg := &STUNMessage{
		Header: STUNHeader{
			MessageType:   BindingResponse,
			TransactionID: txnID,
		},
	}
	AddAttribute(msg, Software, []byte("kamailio-go"))
	AddAttribute(msg, Username, []byte("bob"))

	data := Encode(msg)
	parsed, err := ParseSTUN(data)
	if err != nil {
		t.Fatalf("ParseSTUN failed: %v", err)
	}
	if parsed.Header.MessageType != BindingResponse {
		t.Errorf("message type = 0x%04x, want 0x%04x", parsed.Header.MessageType, BindingResponse)
	}
	if parsed.Header.TransactionID != txnID {
		t.Error("transaction id mismatch")
	}
	if len(parsed.Attributes) != 2 {
		t.Fatalf("got %d attributes, want 2", len(parsed.Attributes))
	}
	if parsed.Attributes[0].Type != Software {
		t.Errorf("attr[0] type = 0x%04x, want 0x%04x", parsed.Attributes[0].Type, Software)
	}
	if string(parsed.Attributes[0].Value) != "kamailio-go" {
		t.Errorf("attr[0] value = %q", string(parsed.Attributes[0].Value))
	}
	if parsed.Attributes[1].Type != Username {
		t.Errorf("attr[1] type = 0x%04x, want 0x%04x", parsed.Attributes[1].Type, Username)
	}
	if string(parsed.Attributes[1].Value) != "bob" {
		t.Errorf("attr[1] value = %q", string(parsed.Attributes[1].Value))
	}

	// Error cases.
	if _, err := ParseSTUN([]byte{0x00, 0x01}); err == nil {
		t.Error("expected error for short buffer")
	}
	bad := make([]byte, 20)
	bad[0] = 0xc0 // top bits set
	if _, err := ParseSTUN(bad); err == nil {
		t.Error("expected error for non-STUN message")
	}
	badCookie := make([]byte, 20)
	binary.BigEndian.PutUint16(badCookie[0:2], BindingRequest)
	// leave cookie as zero -> invalid
	if _, err := ParseSTUN(badCookie); err == nil {
		t.Error("expected error for invalid magic cookie")
	}
}

func TestAddAttribute(t *testing.T) {
	msg := &STUNMessage{
		Header: STUNHeader{MessageType: BindingRequest},
	}
	AddAttribute(msg, Username, []byte("alice"))
	AddAttribute(msg, Password, []byte("secret"))

	if len(msg.Attributes) != 2 {
		t.Fatalf("got %d attributes, want 2", len(msg.Attributes))
	}
	if msg.Attributes[0].Type != Username || string(msg.Attributes[0].Value) != "alice" {
		t.Errorf("attr[0] = %+v", msg.Attributes[0])
	}
	if msg.Attributes[1].Type != Password || string(msg.Attributes[1].Value) != "secret" {
		t.Errorf("attr[1] = %+v", msg.Attributes[1])
	}

	// Mutating the original value slice must not affect the stored attribute.
	orig := []byte("alice")
	AddAttribute(msg, Username, orig)
	orig[0] = 'X'
	last := msg.Attributes[len(msg.Attributes)-1]
	if string(last.Value) != "alice" {
		t.Errorf("attribute value mutated externally: %q", string(last.Value))
	}
}

func TestGetAttribute(t *testing.T) {
	msg := &STUNMessage{
		Header: STUNHeader{MessageType: BindingResponse},
	}
	AddAttribute(msg, Software, []byte("v1"))
	AddAttribute(msg, Username, []byte("alice"))

	sw := GetAttribute(msg, Software)
	if sw == nil {
		t.Fatal("expected Software attribute")
	}
	if string(sw.Value) != "v1" {
		t.Errorf("Software value = %q", string(sw.Value))
	}

	if GetAttribute(msg, Fingerprint) != nil {
		t.Error("expected nil for missing Fingerprint attribute")
	}
}

func TestBuildBindingResponse(t *testing.T) {
	txnID := testTxnID()
	ip := net.IPv4(192, 168, 1, 100)
	port := 5060

	data := BuildBindingResponse(txnID, ip, port)

	parsed, err := ParseSTUN(data)
	if err != nil {
		t.Fatalf("ParseSTUN failed: %v", err)
	}
	if parsed.Header.MessageType != BindingResponse {
		t.Errorf("message type = 0x%04x, want 0x%04x", parsed.Header.MessageType, BindingResponse)
	}
	if parsed.Header.TransactionID != txnID {
		t.Error("transaction id mismatch")
	}

	ma := GetAttribute(parsed, MappedAddress)
	if ma == nil {
		t.Fatal("expected Mapped-Address attribute")
	}
	if len(ma.Value) != 8 {
		t.Fatalf("Mapped-Address value length = %d, want 8", len(ma.Value))
	}
	if ma.Value[0] != 0 {
		t.Errorf("reserved byte = 0x%02x, want 0x00", ma.Value[0])
	}
	if ma.Value[1] != familyIPv4 {
		t.Errorf("family = 0x%02x, want 0x%02x", ma.Value[1], familyIPv4)
	}
	if got := binary.BigEndian.Uint16(ma.Value[2:4]); got != uint16(port) {
		t.Errorf("port = %d, want %d", got, port)
	}
	if got := net.IP(ma.Value[4:8]).String(); got != ip.String() {
		t.Errorf("ip = %s, want %s", got, ip.String())
	}
}

func TestBuildXorBindingResponse(t *testing.T) {
	txnID := testTxnID()
	ip := net.IPv4(10, 0, 0, 1)
	port := 3478

	data := BuildXorBindingResponse(txnID, ip, port)

	parsed, err := ParseSTUN(data)
	if err != nil {
		t.Fatalf("ParseSTUN failed: %v", err)
	}
	if parsed.Header.MessageType != BindingResponse {
		t.Errorf("message type = 0x%04x, want 0x%04x", parsed.Header.MessageType, BindingResponse)
	}
	if parsed.Header.TransactionID != txnID {
		t.Error("transaction id mismatch")
	}

	xma := GetAttribute(parsed, XorMappedAddress)
	if xma == nil {
		t.Fatal("expected XOR-Mapped-Address attribute")
	}
	if len(xma.Value) != 8 {
		t.Fatalf("XOR-Mapped-Address value length = %d, want 8", len(xma.Value))
	}
	if xma.Value[1] != familyIPv4 {
		t.Errorf("family = 0x%02x, want 0x%02x", xma.Value[1], familyIPv4)
	}

	// Decode the XOR'd port and address and verify they match the inputs.
	xorPort := binary.BigEndian.Uint16(xma.Value[2:4]) ^ uint16(MagicCookie>>16)
	if xorPort != uint16(port) {
		t.Errorf("decoded port = %d, want %d", xorPort, port)
	}
	xorAddr := binary.BigEndian.Uint32(xma.Value[4:8]) ^ MagicCookie
	if got := net.IPv4(byte(xorAddr>>24), byte(xorAddr>>16), byte(xorAddr>>8), byte(xorAddr)).String(); got != ip.String() {
		t.Errorf("decoded ip = %s, want %s", got, ip.String())
	}
}

func TestBuildXorBindingResponseIPv6(t *testing.T) {
	txnID := testTxnID()
	ip := net.ParseIP("2001:db8::1")
	port := 3478

	data := BuildXorBindingResponse(txnID, ip, port)
	parsed, err := ParseSTUN(data)
	if err != nil {
		t.Fatalf("ParseSTUN failed: %v", err)
	}
	xma := GetAttribute(parsed, XorMappedAddress)
	if xma == nil {
		t.Fatal("expected XOR-Mapped-Address attribute")
	}
	if len(xma.Value) != 20 {
		t.Fatalf("XOR-Mapped-Address value length = %d, want 20", len(xma.Value))
	}
	if xma.Value[1] != familyIPv6 {
		t.Errorf("family = 0x%02x, want 0x%02x", xma.Value[1], familyIPv6)
	}

	// Decode the IPv6 address by XOR with magic cookie + transaction id.
	cookie := make([]byte, 16)
	binary.BigEndian.PutUint32(cookie[0:4], MagicCookie)
	copy(cookie[4:16], txnID[:])
	decoded := make([]byte, 16)
	for i := 0; i < 16; i++ {
		decoded[i] = xma.Value[4+i] ^ cookie[i]
	}
	if got := net.IP(decoded).String(); got != ip.String() {
		t.Errorf("decoded ip = %s, want %s", got, ip.String())
	}
}

func TestRoundTrip(t *testing.T) {
	txnID := testTxnID()
	orig := &STUNMessage{
		Header: STUNHeader{
			MessageType:   BindingResponse,
			TransactionID: txnID,
		},
	}
	AddAttribute(orig, Software, []byte("kamailio-go STUN"))
	AddAttribute(orig, Username, []byte("round-trip-user"))
	AddAttribute(orig, Fingerprint, []byte{0x12, 0x34, 0x56, 0x78})

	data := Encode(orig)
	parsed, err := ParseSTUN(data)
	if err != nil {
		t.Fatalf("ParseSTUN failed: %v", err)
	}
	if parsed.Header.MessageType != orig.Header.MessageType {
		t.Errorf("message type mismatch: 0x%04x vs 0x%04x", parsed.Header.MessageType, orig.Header.MessageType)
	}
	if parsed.Header.MessageLength != orig.Header.MessageLength {
		t.Errorf("message length mismatch: %d vs %d", parsed.Header.MessageLength, orig.Header.MessageLength)
	}
	if parsed.Header.TransactionID != orig.Header.TransactionID {
		t.Error("transaction id mismatch")
	}
	if len(parsed.Attributes) != len(orig.Attributes) {
		t.Fatalf("attribute count mismatch: %d vs %d", len(parsed.Attributes), len(orig.Attributes))
	}
	for i, attr := range orig.Attributes {
		got := parsed.Attributes[i]
		if got.Type != attr.Type {
			t.Errorf("attr[%d] type mismatch: 0x%04x vs 0x%04x", i, got.Type, attr.Type)
		}
		if got.Length != attr.Length {
			t.Errorf("attr[%d] length mismatch: %d vs %d", i, got.Length, attr.Length)
		}
		if string(got.Value) != string(attr.Value) {
			t.Errorf("attr[%d] value mismatch: %q vs %q", i, string(got.Value), string(attr.Value))
		}
	}
}
