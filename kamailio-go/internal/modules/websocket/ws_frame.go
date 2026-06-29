// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * WebSocket frame layer (RFC 6455 §5).
 * Port of the kamailio ws_frame.c — encode and decode WebSocket frames,
 * including opcodes, masking, payload-length 7/16/64-bit handling and
 * Close/Ping/Pong helpers.
 *
 * Frames are produced and consumed independently of any particular I/O
 * transport, so they can be unit-tested without a live socket and reused
 * by both server- and client-side code.
 */

package websocket

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// ---------------------------------------------------------------------------
// Opcodes (RFC 6455 §5.2)
// ---------------------------------------------------------------------------

// Opcode is a WebSocket frame opcode (RFC 6455 §5.2).
type Opcode byte

const (
	// OpContinuation is the continuation frame opcode.
	OpContinuation Opcode = 0x0
	// OpText is the text frame opcode.
	OpText Opcode = 0x1
	// OpBinary is the binary frame opcode.
	OpBinary Opcode = 0x2
	// OpClose is the connection-close opcode.
	OpClose Opcode = 0x8
	// OpPing is the ping opcode.
	OpPing Opcode = 0x9
	// OpPong is the pong opcode.
	OpPong Opcode = 0xA
)

// IsControl reports whether op is a control frame opcode (>= 0x8).
// Control frames may not be fragmented (RFC 6455 §5.5).
func (op Opcode) IsControl() bool { return op&0x8 != 0 }

// String returns a human-readable opcode name.
func (op Opcode) String() string {
	switch op {
	case OpContinuation:
		return "continuation"
	case OpText:
		return "text"
	case OpBinary:
		return "binary"
	case OpClose:
		return "close"
	case OpPing:
		return "ping"
	case OpPong:
		return "pong"
	default:
		return fmt.Sprintf("opcode(0x%X)", byte(op))
	}
}

// ---------------------------------------------------------------------------
// Close status codes (RFC 6455 §7.4)
// ---------------------------------------------------------------------------

const (
	// StatusNormalClosure indicates a normal close (RFC 6455 §7.4.1).
	StatusNormalClosure uint16 = 1000
	// StatusGoingAway indicates the endpoint is going away.
	StatusGoingAway uint16 = 1001
	// StatusProtocolError indicates a protocol error (RFC 6455 §7.4.1).
	StatusProtocolError uint16 = 1002
	// StatusUnsupportedData indicates unsupported data type.
	StatusUnsupportedData uint16 = 1003
	// StatusNoStatusReceived is the reserved "no status code present"
	// pseudo-code (never sent on the wire).
	StatusNoStatusReceived uint16 = 1005
	// StatusAbnormalClosure indicates an abnormal closure without close frame.
	StatusAbnormalClosure uint16 = 1006
	// StatusInvalidPayload indicates invalid UTF-8 in a text frame.
	StatusInvalidPayload uint16 = 1007
	// StatusPolicyViolation indicates a policy violation.
	StatusPolicyViolation uint16 = 1008
	// StatusMessageTooBig indicates the message was too large (RFC 6455 §7.4.1).
	StatusMessageTooBig uint16 = 1009
	// StatusInternalError indicates an internal error.
	StatusInternalError uint16 = 1011
)

// MaxControlPayload is the maximum payload length of a control frame
// (RFC 6455 §5.5: control frames must have a payload ≤ 125 bytes).
const MaxControlPayload = 125

// MaxFramePayload is the largest frame payload this implementation will
// accept on read. Mirrors the C module's MAX_RECV_BUFFER_SIZE.
const MaxFramePayload = 256 * 1024

// ---------------------------------------------------------------------------
// Bit masks for the first two frame bytes (RFC 6455 §5.2)
// ---------------------------------------------------------------------------

const (
	byte0MaskFIN     byte = 0x80
	byte0MaskRSV1    byte = 0x40
	byte0MaskRSV2    byte = 0x20
	byte0MaskRSV3    byte = 0x10
	byte0MaskOpcode  byte = 0x0F
	byte1MaskMask    byte = 0x80
	byte1MaskPayload byte = 0x7F
)

// ---------------------------------------------------------------------------
// Frame
// ---------------------------------------------------------------------------

// Frame is a single WebSocket frame (RFC 6455 §5.2).
type Frame struct {
	FIN        bool
	RSV1       bool
	RSV2       bool
	RSV3       bool
	Opcode     Opcode
	Mask       bool
	MaskingKey [4]byte
	Payload    []byte
}

// Encode serialises f to its on-wire form. When f.Mask is true the payload
// is XOR-masked with f.MaskingKey and the mask flag/header are written.
// When f.Mask is true and MaskingKey is all-zero, a random mask is generated
// (caller may rely on this for client-side frames per RFC 6455 §5.3).
//
//	C: encode_and_send_ws_frame() — encoding portion
func (f *Frame) Encode() ([]byte, error) {
	if f.Opcode.IsControl() && len(f.Payload) > MaxControlPayload {
		return nil, fmt.Errorf("websocket: control frame payload %d > %d",
			len(f.Payload), MaxControlPayload)
	}
	if f.Mask && (f.MaskingKey[0]|f.MaskingKey[1]|f.MaskingKey[2]|f.MaskingKey[3]) == 0 {
		// Caller asked for masking but supplied no key — generate one.
		if _, err := readRandom(f.MaskingKey[:]); err != nil {
			return nil, fmt.Errorf("websocket: mask: %w", err)
		}
	}

	// Determine the payload-length encoding (7/16/64 bits).
	payloadLen := len(f.Payload)
	var lengthBytes []byte
	switch {
	case payloadLen <= 125:
		// Encoded inline in byte 1.
	case payloadLen <= 0xFFFF:
		lengthBytes = make([]byte, 2)
		binary.BigEndian.PutUint16(lengthBytes, uint16(payloadLen))
	default:
		lengthBytes = make([]byte, 8)
		binary.BigEndian.PutUint64(lengthBytes, uint64(payloadLen))
	}

	// Compute header size.
	hdrLen := 2
	if len(lengthBytes) == 2 {
		hdrLen += 2
	} else if len(lengthBytes) == 8 {
		hdrLen += 8
	}
	if f.Mask {
		hdrLen += 4
	}

	out := make([]byte, hdrLen+payloadLen)

	// Byte 0: FIN | RSV | opcode.
	var b0 byte
	if f.FIN {
		b0 |= byte0MaskFIN
	}
	if f.RSV1 {
		b0 |= byte0MaskRSV1
	}
	if f.RSV2 {
		b0 |= byte0MaskRSV2
	}
	if f.RSV3 {
		b0 |= byte0MaskRSV3
	}
	b0 |= byte(f.Opcode) & byte0MaskOpcode
	out[0] = b0

	// Byte 1: Mask | length.
	var b1 byte
	if f.Mask {
		b1 |= byte1MaskMask
	}
	switch len(lengthBytes) {
	case 0:
		b1 |= byte(payloadLen)
	case 2:
		b1 |= 126
	case 8:
		b1 |= 127
	}
	out[1] = b1

	// Extended length.
	off := 2
	if len(lengthBytes) > 0 {
		copy(out[off:off+len(lengthBytes)], lengthBytes)
		off += len(lengthBytes)
	}

	// Masking key.
	if f.Mask {
		copy(out[off:off+4], f.MaskingKey[:])
		off += 4
	}

	// Payload (masked if needed).
	if f.Mask {
		xorMask(out[off:], f.Payload, f.MaskingKey)
	} else {
		copy(out[off:], f.Payload)
	}
	return out, nil
}

// xorMask XORs src with key and writes the result to dst (RFC 6455 §5.3).
// dst and src must be the same length; key is cycled modulo 4.
func xorMask(dst, src []byte, key [4]byte) {
	for i := 0; i < len(src); i++ {
		dst[i] = src[i] ^ key[i&3]
	}
}

// DecodeFrame reads and decodes a single WebSocket frame from r.
// The returned Frame.Payload is a fresh allocation; callers may keep it
// without copying. Server-side readers should set server=true to enforce
// that client→server frames are masked (RFC 6455 §5.1); client-side
// readers should set server=false to enforce the opposite.
//
//	C: decode_and_validate_ws_frame()
func DecodeFrame(r io.Reader, server bool) (*Frame, error) {
	// Read the two fixed header bytes.
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, fmt.Errorf("websocket: read header: %w", err)
	}
	f := &Frame{
		FIN:    hdr[0]&byte0MaskFIN != 0,
		RSV1:   hdr[0]&byte0MaskRSV1 != 0,
		RSV2:   hdr[0]&byte0MaskRSV2 != 0,
		RSV3:   hdr[0]&byte0MaskRSV3 != 0,
		Opcode: Opcode(hdr[0] & byte0MaskOpcode),
		Mask:   hdr[1]&byte1MaskMask != 0,
	}

	// RSV bits must be zero unless an extension is negotiated (RFC 6455 §5.2).
	if f.RSV1 || f.RSV2 || f.RSV3 {
		return nil, fmt.Errorf("websocket: rsv bits set (0x%02X): %w", hdr[0], ErrProtocol)
	}

	// Opcode validity.
	if f.Opcode > 0xA {
		return nil, fmt.Errorf("websocket: reserved opcode 0x%X: %w", f.Opcode, ErrProtocol)
	}
	// Control frames must not be fragmented and must have payload ≤ 125.
	if f.Opcode.IsControl() {
		if !f.FIN {
			return nil, fmt.Errorf("websocket: fragmented control frame: %w", ErrProtocol)
		}
	}

	// Mask validation: server must receive masked, client must receive unmasked.
	if server && !f.Mask {
		return nil, fmt.Errorf("websocket: unmasked client frame: %w", ErrProtocol)
	}
	if !server && f.Mask {
		return nil, fmt.Errorf("websocket: masked server frame: %w", ErrProtocol)
	}

	// Payload length.
	length := int(hdr[1] & byte1MaskPayload)
	switch length {
	case 126:
		var lenBuf [2]byte
		if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
			return nil, fmt.Errorf("websocket: read length16: %w", err)
		}
		length = int(binary.BigEndian.Uint16(lenBuf[:]))
		// 16-bit length must be ≥ 126 (else non-minimal encoding).
		if length < 126 {
			return nil, fmt.Errorf("websocket: non-minimal 16-bit length: %w", ErrProtocol)
		}
	case 127:
		var lenBuf [8]byte
		if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
			return nil, fmt.Errorf("websocket: read length64: %w", err)
		}
		// RFC 6455 §5.1: the high bit of the 64-bit length must be zero.
		l := binary.BigEndian.Uint64(lenBuf[:])
		if l&0x8000000000000000 != 0 {
			return nil, fmt.Errorf("websocket: 64-bit length high bit set: %w", ErrProtocol)
		}
		// The C module uses only the lower 32 bits; mirror that for parity.
		length = int(uint32(l))
		if int64(length) != int64(uint32(l)) {
			// Platform-specific: on 32-bit platforms the cast above may
			// have wrapped. Refuse anything that doesn't fit in a Go int.
			return nil, fmt.Errorf("websocket: payload too large: %d: %w", l, ErrProtocol)
		}
		if length < 0x10000 {
			return nil, fmt.Errorf("websocket: non-minimal 64-bit length: %w", ErrProtocol)
		}
	}

	// Cap the payload to protect against malicious peers.
	if length > MaxFramePayload {
		return nil, fmt.Errorf("websocket: payload %d > %d: %w", length, MaxFramePayload, ErrTooBig)
	}
	// Control-frame payloads must be ≤ 125 bytes.
	if f.Opcode.IsControl() && length > MaxControlPayload {
		return nil, fmt.Errorf("websocket: control payload %d > %d: %w",
			length, MaxControlPayload, ErrProtocol)
	}

	// Masking key (if present).
	if f.Mask {
		if _, err := io.ReadFull(r, f.MaskingKey[:]); err != nil {
			return nil, fmt.Errorf("websocket: read mask: %w", err)
		}
	}

	// Payload.
	f.Payload = make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(r, f.Payload); err != nil {
			return nil, fmt.Errorf("websocket: read payload: %w", err)
		}
		if f.Mask {
			// Unmask in place (RFC 6455 §5.3).
			for i := 0; i < length; i++ {
				f.Payload[i] ^= f.MaskingKey[i&3]
			}
		}
	}
	return f, nil
}

// ---------------------------------------------------------------------------
// Close frame helpers (RFC 6455 §7)
// ---------------------------------------------------------------------------

// CloseFrame builds a Close frame with the given status code and optional
// reason. code may be 0 to indicate "no status code" (the payload is empty).
//
//	C: close_connection() — close-frame construction
func CloseFrame(code uint16, reason string) *Frame {
	var payload []byte
	if code != 0 {
		payload = make([]byte, 2+len(reason))
		binary.BigEndian.PutUint16(payload[:2], code)
		copy(payload[2:], reason)
	}
	return &Frame{
		FIN:     true,
		Opcode:  OpClose,
		Payload: payload,
	}
}

// ParseCloseFrame extracts the status code and reason from a Close-frame
// payload. Returns (0, "") when the payload is empty (RFC 6455 §7.1.5).
// Returns (StatusNoStatusReceived, "") when the payload is exactly one byte,
// which is a protocol error — callers should treat this as an error.
//
//	C: handle_close()
func ParseCloseFrame(payload []byte) (code uint16, reason string, err error) {
	switch len(payload) {
	case 0:
		return 0, "", nil
	case 1:
		return StatusNoStatusReceived, "", fmt.Errorf("websocket: short close payload: %w", ErrProtocol)
	default:
		code = binary.BigEndian.Uint16(payload[:2])
		// RFC 6455 §7.4.2: codes 0-999, 1004, 1005, 1006, 1015 are reserved.
		if code < 1000 || code == 1004 || code == 1005 || code == 1006 || code == 1015 {
			return StatusProtocolError, "",
				fmt.Errorf("websocket: invalid close code %d: %w", code, ErrProtocol)
		}
		return code, string(payload[2:]), nil
	}
}

// ---------------------------------------------------------------------------
// Ping/Pong helpers (RFC 6455 §5.5)
// ---------------------------------------------------------------------------

// PingFrame builds a Ping frame with the given application data (≤ 125 bytes).
func PingFrame(data []byte) (*Frame, error) {
	if len(data) > MaxControlPayload {
		return nil, fmt.Errorf("websocket: ping payload %d > %d: %w",
			len(data), MaxControlPayload, ErrProtocol)
	}
	return &Frame{FIN: true, Opcode: OpPing, Payload: data}, nil
}

// PongFrame builds a Pong frame echoing the supplied application data.
func PongFrame(data []byte) (*Frame, error) {
	if len(data) > MaxControlPayload {
		return nil, fmt.Errorf("websocket: pong payload %d > %d: %w",
			len(data), MaxControlPayload, ErrProtocol)
	}
	return &Frame{FIN: true, Opcode: OpPong, Payload: data}, nil
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

var (
	// ErrProtocol indicates a protocol-level error (RFC 6455 §7.4.1 code 1002).
	ErrProtocol = errors.New("websocket: protocol error")
	// ErrTooBig indicates the frame payload exceeds MaxFramePayload (code 1009).
	ErrTooBig = errors.New("websocket: message too big")
)
