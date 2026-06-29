// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - WebSocket frame layer tests (RFC 6455 §5).
 */

package websocket

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Opcode tests
// ---------------------------------------------------------------------------

func TestOpcode_IsControl(t *testing.T) {
	cases := []struct {
		op     Opcode
		ctrl   bool
		name   string
	}{
		{OpContinuation, false, "continuation"},
		{OpText, false, "text"},
		{OpBinary, false, "binary"},
		{OpClose, true, "close"},
		{OpPing, true, "ping"},
		{OpPong, true, "pong"},
	}
	for _, c := range cases {
		if got := c.op.IsControl(); got != c.ctrl {
			t.Errorf("%s.IsControl() = %v, want %v", c.op, got, c.ctrl)
		}
		if got := c.op.String(); got != c.name {
			t.Errorf("%s.String() = %q, want %q", c.op, got, c.name)
		}
	}
}

func TestOpcode_String_Unknown(t *testing.T) {
	op := Opcode(0xB)
	if !strings.Contains(op.String(), "0xB") {
		t.Errorf("unknown opcode string = %q, want contains 0xB", op.String())
	}
}

// ---------------------------------------------------------------------------
// Frame encode/decode round-trips
// ---------------------------------------------------------------------------

func TestFrame_RoundTrip_ShortPayload(t *testing.T) {
	frame := &Frame{
		FIN:     true,
		Opcode:  OpText,
		Payload: []byte("hello"),
	}
	encoded, err := frame.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(encoded) != 2+5 {
		t.Errorf("encoded length = %d, want 7", len(encoded))
	}
	decoded, err := DecodeFrame(bytes.NewReader(encoded), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !decoded.FIN {
		t.Error("FIN = false, want true")
	}
	if decoded.Opcode != OpText {
		t.Errorf("Opcode = %s, want text", decoded.Opcode)
	}
	if string(decoded.Payload) != "hello" {
		t.Errorf("Payload = %q, want hello", decoded.Payload)
	}
}

func TestFrame_RoundTrip_125ByteBoundary(t *testing.T) {
	// 125 bytes is the largest payload that fits inline in the length byte.
	payload := bytes.Repeat([]byte{'A'}, 125)
	frame := &Frame{FIN: true, Opcode: OpBinary, Payload: payload}
	encoded, err := frame.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := DecodeFrame(bytes.NewReader(encoded), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(decoded.Payload) != 125 {
		t.Errorf("Payload length = %d, want 125", len(decoded.Payload))
	}
	if !bytes.Equal(decoded.Payload, payload) {
		t.Error("Payload mismatch")
	}
}

func TestFrame_RoundTrip_126ByteExt16(t *testing.T) {
	// 126 bytes triggers the 16-bit extended length encoding.
	payload := bytes.Repeat([]byte{'B'}, 126)
	frame := &Frame{FIN: true, Opcode: OpBinary, Payload: payload}
	encoded, err := frame.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(encoded) != 2+2+126 {
		t.Errorf("encoded length = %d, want 130", len(encoded))
	}
	decoded, err := DecodeFrame(bytes.NewReader(encoded), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(decoded.Payload) != 126 {
		t.Errorf("Payload length = %d, want 126", len(decoded.Payload))
	}
}

func TestFrame_RoundTrip_65536ByteExt64(t *testing.T) {
	// 65536 bytes triggers the 64-bit extended length encoding.
	payload := bytes.Repeat([]byte{'C'}, 65536)
	frame := &Frame{FIN: true, Opcode: OpBinary, Payload: payload}
	encoded, err := frame.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(encoded) != 2+8+65536 {
		t.Errorf("encoded length = %d, want %d", len(encoded), 2+8+65536)
	}
	decoded, err := DecodeFrame(bytes.NewReader(encoded), false)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(decoded.Payload) != 65536 {
		t.Errorf("Payload length = %d, want 65536", len(decoded.Payload))
	}
}

func TestFrame_RoundTrip_WithMask(t *testing.T) {
	// Server must receive masked frames; client must mask.
	frame := &Frame{
		FIN:        true,
		Opcode:     OpText,
		Mask:       true,
		MaskingKey: [4]byte{0x12, 0x34, 0x56, 0x78},
		Payload:    []byte("masked payload"),
	}
	encoded, err := frame.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// Decode as a server (server=true expects masked frames).
	decoded, err := DecodeFrame(bytes.NewReader(encoded), true)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !decoded.Mask {
		t.Error("Mask = false, want true")
	}
	if decoded.MaskingKey != frame.MaskingKey {
		t.Errorf("MaskingKey = %v, want %v", decoded.MaskingKey, frame.MaskingKey)
	}
	if string(decoded.Payload) != "masked payload" {
		t.Errorf("Payload = %q, want masked payload", decoded.Payload)
	}
}

func TestFrame_Encode_GeneratesRandomMaskWhenZero(t *testing.T) {
	// When Mask=true and MaskingKey is all-zero, Encode generates a random key.
	frame := &Frame{
		FIN:     true,
		Opcode:  OpText,
		Mask:    true,
		Payload: []byte("hello"),
	}
	encoded, err := frame.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := DecodeFrame(bytes.NewReader(encoded), true)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.MaskingKey == ([4]byte{}) {
		t.Error("mask was not generated (still all-zero)")
	}
	if string(decoded.Payload) != "hello" {
		t.Errorf("Payload = %q, want hello", decoded.Payload)
	}
}

func TestFrame_Encode_ControlPayloadTooLarge(t *testing.T) {
	// Control frames may carry at most 125 bytes (RFC 6455 §5.5).
	payload := make([]byte, 126)
	frame := &Frame{FIN: true, Opcode: OpPing, Payload: payload}
	if _, err := frame.Encode(); err == nil {
		t.Fatal("expected error for oversized control payload")
	}
}

// ---------------------------------------------------------------------------
// DecodeFrame error cases
// ---------------------------------------------------------------------------

func TestDecodeFrame_RSVBitsSet(t *testing.T) {
	// Construct a frame with RSV1 bit set (must be rejected).
	encoded := []byte{0xC2, 0x00} // FIN + RSV1 + binary
	_, err := DecodeFrame(bytes.NewReader(encoded), false)
	if err == nil || !errors.Is(err, ErrProtocol) {
		t.Errorf("expected ErrProtocol for RSV1 set, got %v", err)
	}
}

func TestDecodeFrame_ReservedOpcode(t *testing.T) {
	// Opcode 0xB is reserved (RFC 6455 §5.2).
	encoded := []byte{0x8B, 0x00}
	_, err := DecodeFrame(bytes.NewReader(encoded), false)
	if err == nil || !errors.Is(err, ErrProtocol) {
		t.Errorf("expected ErrProtocol for reserved opcode, got %v", err)
	}
}

func TestDecodeFrame_ServerExpectsMaskedFrame(t *testing.T) {
	// Server side (server=true) must reject unmasked frames.
	encoded := []byte{0x81, 0x05, 'h', 'e', 'l', 'l', 'o'}
	_, err := DecodeFrame(bytes.NewReader(encoded), true)
	if err == nil || !errors.Is(err, ErrProtocol) {
		t.Errorf("expected ErrProtocol for unmasked client frame, got %v", err)
	}
}

func TestDecodeFrame_ClientExpectsUnmaskedFrame(t *testing.T) {
	// Client side (server=false) must reject masked frames.
	encoded := []byte{0x81, 0x85, 0x12, 0x34, 0x56, 0x78, 0x7a, 0x51, 0x36, 0x35, 0x14}
	_, err := DecodeFrame(bytes.NewReader(encoded), false)
	if err == nil || !errors.Is(err, ErrProtocol) {
		t.Errorf("expected ErrProtocol for masked server frame, got %v", err)
	}
}

func TestDecodeFrame_NonMinimal16BitLength(t *testing.T) {
	// A 16-bit length that could have been encoded in 7 bits is non-minimal.
	encoded := []byte{0x82, 0x7E, 0x00, 0x10, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	_, err := DecodeFrame(bytes.NewReader(encoded), false)
	if err == nil || !errors.Is(err, ErrProtocol) {
		t.Errorf("expected ErrProtocol for non-minimal 16-bit length, got %v", err)
	}
}

func TestDecodeFrame_PayloadTooLarge(t *testing.T) {
	// A 64-bit length exceeding MaxFramePayload must be rejected.
	encoded := make([]byte, 10)
	encoded[0] = 0x82
	encoded[1] = 0x7F
	binary.BigEndian.PutUint64(encoded[2:10], uint64(MaxFramePayload+1))
	_, err := DecodeFrame(bytes.NewReader(encoded), false)
	if err == nil || !errors.Is(err, ErrTooBig) {
		t.Errorf("expected ErrTooBig, got %v", err)
	}
}

func TestDecodeFrame_FragmentedControlFrame(t *testing.T) {
	// Control frames must not be fragmented (RFC 6455 §5.5).
	encoded := []byte{0x09, 0x00} // FIN=0, opcode=ping
	_, err := DecodeFrame(bytes.NewReader(encoded), true)
	if err == nil || !errors.Is(err, ErrProtocol) {
		t.Errorf("expected ErrProtocol for fragmented control frame, got %v", err)
	}
}

func TestDecodeFrame_EOF(t *testing.T) {
	// Reading from an empty reader should return an error.
	_, err := DecodeFrame(bytes.NewReader(nil), false)
	if err == nil {
		t.Fatal("expected error for EOF")
	}
}

func TestDecodeFrame_ControlPayloadTooLarge(t *testing.T) {
	// Build a Ping frame with 126-byte payload (invalid).
	encoded := make([]byte, 2+126)
	encoded[0] = 0x89 // FIN + ping
	encoded[1] = 0x7E
	binary.BigEndian.PutUint16(encoded[2:4], 126)
	_, err := DecodeFrame(bytes.NewReader(encoded), false)
	if err == nil || !errors.Is(err, ErrProtocol) {
		t.Errorf("expected ErrProtocol for oversized control payload, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Close-frame helpers
// ---------------------------------------------------------------------------

func TestCloseFrame_WithCodeAndReason(t *testing.T) {
	frame := CloseFrame(StatusNormalClosure, "bye")
	if frame.Opcode != OpClose {
		t.Errorf("Opcode = %s, want close", frame.Opcode)
	}
	if !frame.FIN {
		t.Error("FIN = false, want true")
	}
	code, reason, err := ParseCloseFrame(frame.Payload)
	if err != nil {
		t.Fatalf("ParseCloseFrame: %v", err)
	}
	if code != StatusNormalClosure {
		t.Errorf("code = %d, want %d", code, StatusNormalClosure)
	}
	if reason != "bye" {
		t.Errorf("reason = %q, want bye", reason)
	}
}

func TestCloseFrame_NoCode(t *testing.T) {
	frame := CloseFrame(0, "")
	if len(frame.Payload) != 0 {
		t.Errorf("Payload length = %d, want 0", len(frame.Payload))
	}
	code, reason, err := ParseCloseFrame(frame.Payload)
	if err != nil {
		t.Fatalf("ParseCloseFrame: %v", err)
	}
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if reason != "" {
		t.Errorf("reason = %q, want empty", reason)
	}
}

func TestParseCloseFrame_ShortPayload(t *testing.T) {
	// A single-byte close payload is a protocol error.
	_, _, err := ParseCloseFrame([]byte{0x03})
	if err == nil || !errors.Is(err, ErrProtocol) {
		t.Errorf("expected ErrProtocol for short close payload, got %v", err)
	}
}

func TestParseCloseFrame_InvalidCode(t *testing.T) {
	// Codes below 1000 are reserved (RFC 6455 §7.4.2).
	payload := make([]byte, 2)
	binary.BigEndian.PutUint16(payload, 999)
	_, _, err := ParseCloseFrame(payload)
	if err == nil || !errors.Is(err, ErrProtocol) {
		t.Errorf("expected ErrProtocol for code 999, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Ping/Pong helpers
// ---------------------------------------------------------------------------

func TestPingFrame_OK(t *testing.T) {
	frame, err := PingFrame([]byte("hello"))
	if err != nil {
		t.Fatalf("PingFrame: %v", err)
	}
	if frame.Opcode != OpPing {
		t.Errorf("Opcode = %s, want ping", frame.Opcode)
	}
	if string(frame.Payload) != "hello" {
		t.Errorf("Payload = %q, want hello", frame.Payload)
	}
}

func TestPingFrame_TooLarge(t *testing.T) {
	_, err := PingFrame(make([]byte, 126))
	if err == nil {
		t.Fatal("expected error for oversized ping payload")
	}
}

func TestPongFrame_OK(t *testing.T) {
	frame, err := PongFrame([]byte("hello"))
	if err != nil {
		t.Fatalf("PongFrame: %v", err)
	}
	if frame.Opcode != OpPong {
		t.Errorf("Opcode = %s, want pong", frame.Opcode)
	}
}

func TestPongFrame_TooLarge(t *testing.T) {
	_, err := PongFrame(make([]byte, 126))
	if err == nil {
		t.Fatal("expected error for oversized pong payload")
	}
}

// ---------------------------------------------------------------------------
// xorMask verification (manual)
// ---------------------------------------------------------------------------

func TestXorMask(t *testing.T) {
	key := [4]byte{0x12, 0x34, 0x56, 0x78}
	src := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05}
	dst := make([]byte, len(src))
	xorMask(dst, src, key)
	// Expected: src[i] ^ key[i%4].
	expected := []byte{0x12, 0x35, 0x54, 0x7B, 0x16, 0x31}
	if !bytes.Equal(dst, expected) {
		t.Errorf("xorMask = %v, want %v", dst, expected)
	}
	// Applying xorMask again should restore the original.
	xorMask(dst, dst, key)
	if !bytes.Equal(dst, src) {
		t.Errorf("xorMask twice = %v, want %v (identity)", dst, src)
	}
}
