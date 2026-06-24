// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * CDP module - C Diameter Peer, the Diameter base protocol stack.
 * Port of the kamailio cdp module (src/modules/cdp).
 *
 * The CDP module maintains a set of Diameter peers and encodes/decodes
 * Diameter messages on the wire. A Diameter message carries a fixed
 * 20-byte header (version, length, command flags, command code,
 * application id, hop-by-hop and end-to-end identifiers) followed by a
 * sequence of AVPs. Each AVP carries a code, flags, an optional vendor
 * id and a variable-length value, padded to a 4-byte boundary.
 *
 * This Go port models the peer table in memory and provides Encode/Decode
 * for the Diameter base protocol (RFC 6733). Send is a loopback: it
 * requires the peer to be connected and echoes the message back so that
 * request/response flows can be exercised in tests.
 *
 * It is safe for concurrent use.
 */

package cdp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// Diameter protocol constants (RFC 6733).
const (
	// Version is the Diameter protocol version (1).
	Version = 1
	// HeaderLen is the fixed length of a Diameter message header.
	HeaderLen = 20
	// AVPHeaderLen is the fixed length of an AVP header without vendor id.
	AVPHeaderLen = 8
	// AVPVendorHeaderLen is the length of an AVP header with vendor id.
	AVPVendorHeaderLen = 12
)

// AVP flag bits.
const (
	AVPFlagVendor    = 0x80 // Vendor-Specific
	AVPFlagMandatory = 0x40 // Mandatory
	AVPFlagReserved  = 0x20 // Reserved, must be zero
)

// Command flag bits.
const (
	CmdFlagRequest = 0x80
	CmdFlagProxy   = 0x40
	CmdFlagError   = 0x20
	CmdFlagRetrans = 0x10
)

// DiameterPeer describes a single Diameter peer (Kamailio peer entry).
type DiameterPeer struct {
	Host      string
	Realm     string
	IP        string
	Port      int
	Connected bool
}

// DiameterAVP is a single Diameter AVP (RFC 6733 section 4.3).
type DiameterAVP struct {
	Code     uint32
	Flags    uint8
	VendorID uint32
	Value    []byte
}

// DiameterMessage is a Diameter message (RFC 6733 section 3.2).
type DiameterMessage struct {
	Version       uint8
	Flags         uint8
	CommandCode   uint32
	ApplicationID uint32
	HopByHopID    uint32
	EndToEndID    uint32
	AVPs          []DiameterAVP
}

// CDPModule maintains the set of Diameter peers and the next hop-by-hop
// identifier.
type CDPModule struct {
	mu       sync.RWMutex
	peers    map[string]*DiameterPeer
	hopByHop atomic.Uint64
}

// NewCDPModule creates a CDPModule with empty peer storage.
func NewCDPModule() *CDPModule {
	return &CDPModule{peers: make(map[string]*DiameterPeer)}
}

// AddPeer registers a Diameter peer and returns the assigned id (the
// index of the peer in insertion order). A peer with the same host
// replaces the existing entry. Returns -1 when peer is nil or has no
// host.
//
//	C: add_peer()
func (m *CDPModule) AddPeer(peer *DiameterPeer) int {
	if peer == nil || peer.Host == "" {
		return -1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.peers == nil {
		m.peers = make(map[string]*DiameterPeer)
	}
	m.peers[peer.Host] = peer
	return len(m.peers)
}

// RemovePeer removes the peer identified by host. Returns true when a
// peer was removed.
func (m *CDPModule) RemovePeer(host string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.peers[host]; !ok {
		return false
	}
	delete(m.peers, host)
	return true
}

// Peers returns a snapshot of all registered peers.
func (m *CDPModule) Peers() []*DiameterPeer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*DiameterPeer, 0, len(m.peers))
	for _, p := range m.peers {
		out = append(out, p)
	}
	return out
}

// IsConnected reports whether a peer with the given host is registered
// and marked connected.
func (m *CDPModule) IsConnected(host string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.peers[host]
	return ok && p.Connected
}

// Send delivers msg to the peer identified by host and returns the
// response. In this in-process port the peer must be connected and the
// response is a synthetic answer: the request flags are cleared and the
// message is echoed back with the same AVPs. Returns an error when the
// peer is unknown or not connected.
//
//	C: cdp_send()
func (m *CDPModule) Send(peer string, msg *DiameterMessage) (*DiameterMessage, error) {
	if msg == nil {
		return nil, errors.New("cdp: nil message")
	}
	m.mu.RLock()
	p, ok := m.peers[peer]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("cdp: unknown peer %q", peer)
	}
	if !p.Connected {
		return nil, fmt.Errorf("cdp: peer %q not connected", peer)
	}
	// Echo an answer: clear the request flag, keep the identifiers so
	// the request/response can be correlated.
	resp := &DiameterMessage{
		Version:       msg.Version,
		Flags:         msg.Flags &^ CmdFlagRequest,
		CommandCode:   msg.CommandCode,
		ApplicationID: msg.ApplicationID,
		HopByHopID:    msg.HopByHopID,
		EndToEndID:    msg.EndToEndID,
		AVPs:          append([]DiameterAVP(nil), msg.AVPs...),
	}
	return resp, nil
}

// Encode serialises msg to its on-wire form (RFC 6733 section 3.2).
func (m *CDPModule) Encode(msg *DiameterMessage) []byte {
	if msg == nil {
		return nil
	}
	version := msg.Version
	if version == 0 {
		version = Version
	}
	var avpBuf []byte
	for i := range msg.AVPs {
		avpBuf = append(avpBuf, encodeAVP(&msg.AVPs[i])...)
	}
	totalLen := HeaderLen + len(avpBuf)
	buf := make([]byte, totalLen)
	buf[0] = version
	putUint24(buf[1:4], uint32(totalLen))
	buf[4] = msg.Flags
	putUint24(buf[5:8], msg.CommandCode)
	binary.BigEndian.PutUint32(buf[8:12], msg.ApplicationID)
	binary.BigEndian.PutUint32(buf[12:16], msg.HopByHopID)
	binary.BigEndian.PutUint32(buf[16:20], msg.EndToEndID)
	copy(buf[HeaderLen:], avpBuf)
	return buf
}

// Decode parses a Diameter message from its on-wire form.
func (m *CDPModule) Decode(data []byte) (*DiameterMessage, error) {
	if len(data) < HeaderLen {
		return nil, fmt.Errorf("cdp: short message %d bytes", len(data))
	}
	version := data[0]
	if version != Version {
		return nil, fmt.Errorf("cdp: unsupported version %d", version)
	}
	totalLen := int(getUint24(data[1:4]))
	if totalLen < HeaderLen || totalLen > len(data) {
		return nil, fmt.Errorf("cdp: bad message length %d (buffer %d)", totalLen, len(data))
	}
	msg := &DiameterMessage{
		Version:       version,
		Flags:         data[4],
		CommandCode:   getUint24(data[5:8]),
		ApplicationID: binary.BigEndian.Uint32(data[8:12]),
		HopByHopID:    binary.BigEndian.Uint32(data[12:16]),
		EndToEndID:    binary.BigEndian.Uint32(data[16:20]),
	}
	avps, err := decodeAVPs(data[HeaderLen:totalLen])
	if err != nil {
		return nil, err
	}
	msg.AVPs = avps
	return msg, nil
}

// NextHopByHop returns the next hop-by-hop identifier and advances the
// counter. Exposed so callers can correlate request/response pairs.
func (m *CDPModule) NextHopByHop() uint32 {
	return uint32(m.hopByHop.Add(1))
}

// ---------------------------------------------------------------------------
// AVP encode / decode
// ---------------------------------------------------------------------------

// encodeAVP serialises a single AVP, padding the value to a 4-byte
// boundary as required by RFC 6733. The AVP Length field reports the
// unpadded length (header + data); the padding bytes are appended to
// the buffer but not counted in the length field.
func encodeAVP(avp *DiameterAVP) []byte {
	vendor := avp.Flags&AVPFlagVendor != 0
	headerLen := AVPHeaderLen
	if vendor {
		headerLen = AVPVendorHeaderLen
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

// decodeAVPs parses a sequence of AVPs from a message body.
func decodeAVPs(data []byte) ([]DiameterAVP, error) {
	var avps []DiameterAVP
	for off := 0; off+AVPHeaderLen <= len(data); {
		code := binary.BigEndian.Uint32(data[off : off+4])
		flags := data[off+4]
		totalLen := int(getUint24(data[off+5 : off+8]))
		if totalLen < AVPHeaderLen || off+totalLen > len(data) {
			return nil, fmt.Errorf("cdp: bad avp length %d at offset %d", totalLen, off)
		}
		vendor := flags&AVPFlagVendor != 0
		headerLen := AVPHeaderLen
		if vendor {
			headerLen = AVPVendorHeaderLen
		}
		if totalLen < headerLen {
			return nil, fmt.Errorf("cdp: avp length %d shorter than header", totalLen)
		}
		avp := DiameterAVP{Code: code, Flags: flags}
		dataStart := off + headerLen
		dataLen := totalLen - headerLen
		if vendor {
			avp.VendorID = binary.BigEndian.Uint32(data[off+8 : off+12])
		}
		avp.Value = make([]byte, dataLen)
		copy(avp.Value, data[dataStart:dataStart+dataLen])
		avps = append(avps, avp)
		// AVPs are padded to a 4-byte boundary.
		off += (totalLen + 3) &^ 3
	}
	return avps, nil
}

// ---------------------------------------------------------------------------
// 24-bit integer helpers (Diameter uses 3-byte length / command-code fields)
// ---------------------------------------------------------------------------

func putUint24(b []byte, v uint32) {
	b[0] = byte(v >> 16)
	b[1] = byte(v >> 8)
	b[2] = byte(v)
}

func getUint24(b []byte) uint32 {
	return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultCM *CDPModule
)

// DefaultCDP returns the process-wide CDPModule, creating one on first use.
func DefaultCDP() *CDPModule {
	defaultMu.RLock()
	c := defaultCM
	defaultMu.RUnlock()
	if c != nil {
		return c
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultCM == nil {
		defaultCM = NewCDPModule()
	}
	return defaultCM
}

// Init (re)initialises the process-wide CDPModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultCM = NewCDPModule()
}
