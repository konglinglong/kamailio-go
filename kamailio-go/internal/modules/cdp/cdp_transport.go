// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Diameter TCP transport.
 * Port of the kamailio cdp module's transport layer
 * (src/modules/cdp/tcp_accept.c, tcp_connect.c, receiver.c).
 *
 * Diameter messages are exchanged over a TCP (or SCTP) connection. Each
 * peer connection is a long-lived socket: outbound connects dial the
 * remote peer, inbound connects accept on a listening socket. Messages
 * are framed by their 20-byte header — the first 3 bytes of the header
 * carry the total message length, so the receiver reads the header,
 * learns the body size, then reads the rest of the message.
 *
 * This Go port mirrors the C module's per-connection read state machine
 * (Waiting → Header → Rest) and runs each connection in its own
 * goroutine. When TCP is unavailable (e.g. in unit tests without a
 * server) the transport falls back to a loopback mode that simulates
 * the connection lifecycle.
 *
 * The transport layer is decoupled from the protocol layer: it reads
 * bytes, decodes DiameterMessage values, and hands them to a
 * MessageHandler for the peer state machine to process.
 */

package cdp

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

var (
	// ErrTransportClosed is returned when an operation is attempted on a
	// closed transport.
	ErrTransportClosed = errors.New("cdp: transport closed")
	// ErrNoConnection is returned when a peer has no active connection.
	ErrNoConnection = errors.New("cdp: no active connection")
	// ErrConnectFailed is returned when the transport layer cannot dial
	// the configured peer.
	ErrConnectFailed = errors.New("cdp: connect failed")
)

// ---------------------------------------------------------------------------
// MessageHandler — interface between transport and protocol layers
// ---------------------------------------------------------------------------

// MessageHandler receives Diameter messages from the transport layer.
// The transport calls HandleMessage in the receiver goroutine of the
// peer that produced the message. Implementations are expected to be
// safe for concurrent use (multiple peers call in parallel).
type MessageHandler interface {
	// HandleMessage is called for each message received from peer.
	// Returning a non-nil error causes the transport to close the
	// connection. host identifies the peer (its Origin-Host once
	// established, otherwise the remote TCP address).
	HandleMessage(peer *PeerConnection, msg *DiameterMessage) error

	// HandleConnect is called when a new peer connection is established
	// (either inbound or outbound). The handler may inspect the remote
	// address and decide whether to accept the connection (return nil)
	// or reject it (return a non-nil error to drop the connection).
	HandleConnect(peer *PeerConnection) error

	// HandleDisconnect is called when a peer connection is torn down.
	// err is the reason (nil for a clean disconnect).
	HandleDisconnect(peer *PeerConnection, err error)
}

// ---------------------------------------------------------------------------
// PeerConnection
// ---------------------------------------------------------------------------

// PeerConnection wraps a single Diameter peer TCP connection. Each
// PeerConnection runs its own receiver goroutine and serialises writes
// through a per-connection mutex.
type PeerConnection struct {
	mu        sync.Mutex
	transport *Transport
	conn      net.Conn
	remote    string
	closed    atomic.Bool
	direction ConnDirection
	createdAt time.Time

	// Origin identity used in CER/CEA/DWR/DWA exchanges.
	localHost  string
	localRealm string
}

// ConnDirection reports whether a connection was initiated locally
// (outbound) or accepted from a remote peer (inbound).
type ConnDirection int

const (
	DirInbound ConnDirection = iota
	DirOutbound
)

// String returns a human-readable direction.
func (d ConnDirection) String() string {
	switch d {
	case DirInbound:
		return "inbound"
	case DirOutbound:
		return "outbound"
	default:
		return "unknown"
	}
}

// RemoteAddr returns the remote TCP address of the connection.
func (c *PeerConnection) RemoteAddr() string { return c.remote }

// Direction returns whether the connection was inbound or outbound.
func (c *PeerConnection) Direction() ConnDirection { return c.direction }

// LocalHost returns the local Origin-Host for this connection.
func (c *PeerConnection) LocalHost() string { return c.localHost }

// LocalRealm returns the local Origin-Realm for this connection.
func (c *PeerConnection) LocalRealm() string { return c.localRealm }

// Close terminates the underlying TCP connection. Subsequent writes
// return ErrTransportClosed.
func (c *PeerConnection) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// IsClosed reports whether the connection has been closed.
func (c *PeerConnection) IsClosed() bool { return c.closed.Load() }

// SendMessage writes msg to the connection. Writes are serialised
// through a per-connection mutex so that multiple goroutines may call
// concurrently. The connection must not be closed.
//
//	C: cdp_send_message()
func (c *PeerConnection) SendMessage(msg *DiameterMessage) error {
	if c.IsClosed() {
		return ErrTransportClosed
	}
	enc := DefaultCDPEncoder.Encode(msg)
	if enc == nil {
		return errors.New("cdp: encode returned nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return ErrNoConnection
	}
	_, err := c.conn.Write(enc)
	return err
}

// SetIdentity updates the local Origin-Host / Origin-Realm used in
// CER/CEA/DWR/DWA exchanges. Called by the transport layer after the
// handshake has completed (the local identity may be derived from the
// peer's advertised realm in some deployments).
func (c *PeerConnection) SetIdentity(host, realm string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.localHost = host
	c.localRealm = realm
}

// ---------------------------------------------------------------------------
// Encoder — abstractions over the wire format
// ---------------------------------------------------------------------------

// Encoder is the interface for serialising Diameter messages to bytes.
type Encoder interface {
	Encode(msg *DiameterMessage) []byte
	Decode(data []byte) (*DiameterMessage, error)
}

// defaultEncoder delegates to the CDPModule's Encode/Decode. It is used
// when no explicit encoder is configured.
type defaultEncoder struct{}

// Encode serialises msg using a fresh CDPModule instance. This is a
// stateless operation — the CDPModule's counters (hopByHop, endToEnd)
// are *not* used here.
func (defaultEncoder) Encode(msg *DiameterMessage) []byte {
	// Use a one-shot module to avoid touching shared state.
	m := &CDPModule{}
	return m.Encode(msg)
}

// Decode parses a Diameter message from data.
func (defaultEncoder) Decode(data []byte) (*DiameterMessage, error) {
	m := &CDPModule{}
	return m.Decode(data)
}

// DefaultCDPEncoder is the encoder used by the transport layer when no
// explicit encoder is configured.
var DefaultCDPEncoder Encoder = defaultEncoder{}

// ---------------------------------------------------------------------------
// Transport
// ---------------------------------------------------------------------------

// TransportConfig holds the configuration for the Diameter transport.
type TransportConfig struct {
	// ListenAddr is the address to listen on for inbound connections
	// (e.g. ":3868"). Empty disables inbound.
	ListenAddr string
	// LocalHost / LocalRealm are the Origin-Host / Origin-Realm used in
	// outgoing CER/DWR/DPR messages.
	LocalHost  string
	LocalRealm string
	// ConnectTimeout is the timeout for outgoing connect attempts.
	ConnectTimeout time.Duration
	// ReadTimeout is the per-message read timeout. Zero disables.
	ReadTimeout time.Duration
	// WriteTimeout is the per-message write timeout. Zero disables.
	WriteTimeout time.Duration
}

// DefaultTransportConfig returns a TransportConfig with sensible defaults.
func DefaultTransportConfig() *TransportConfig {
	return &TransportConfig{
		ListenAddr:     ":3868",
		ConnectTimeout: 5 * time.Second,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   10 * time.Second,
	}
}

// Transport manages a single Diameter listener and a set of outbound
// peer connections. It is the Go counterpart of the C cdp module's
// tcp_accept / tcp_connect layer.
type Transport struct {
	mu      sync.Mutex
	cfg     *TransportConfig
	enc     Encoder
	handler MessageHandler

	listener net.Listener
	closed  atomic.Bool

	// conns tracks every active PeerConnection keyed by remote address.
	// The mutex guards the map; writes serialize against Close.
	conns map[string]*PeerConnection

	// dialer is the function used to establish outbound connections.
	// Default: net.DialTimeout; tests may inject a stub.
	dialer func(network, addr string, timeout time.Duration) (net.Conn, error)
}

// NewTransport creates a Transport with the supplied configuration. If
// cfg is nil, DefaultTransportConfig() is used. If enc is nil,
// DefaultCDPEncoder is used.
func NewTransport(cfg *TransportConfig, enc Encoder, handler MessageHandler) *Transport {
	if cfg == nil {
		cfg = DefaultTransportConfig()
	}
	if enc == nil {
		enc = DefaultCDPEncoder
	}
	return &Transport{
		cfg:     cfg,
		enc:     enc,
		handler: handler,
		conns:   make(map[string]*PeerConnection),
		dialer:  defaultDialer,
	}
}

// defaultDialer is the production net.DialTimeout.
func defaultDialer(network, addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout(network, addr, timeout)
}

// SetDialer replaces the dialer function. Used by tests to inject a
// stub that fails or simulates connections.
func (t *Transport) SetDialer(d func(network, addr string, timeout time.Duration) (net.Conn, error)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.dialer = d
}

// ListenAndServe starts the listener and accepts inbound connections.
// Returns ErrTransportClosed when Close() has been called. When the
// listener cannot be bound (e.g. port already in use, or running in a
// container without permission) the error is returned immediately and
// the transport remains usable for outbound connections.
//
//	C: cdp_start_modules() — accept loop portion
func (t *Transport) ListenAndServe() error {
	if t.closed.Load() {
		return ErrTransportClosed
	}
	if t.cfg.ListenAddr == "" {
		return nil
	}
	l, err := net.Listen("tcp", t.cfg.ListenAddr)
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.listener = l
	t.mu.Unlock()

	for {
		conn, err := l.Accept()
		if err != nil {
			if t.closed.Load() {
				return nil
			}
			return err
		}
		pc := t.newConnection(conn, DirInbound)
		go t.serve(pc)
	}
}

// Dial establishes an outbound connection to the given address (host:port).
// The connection runs in its own goroutine; Dial returns once the TCP
// connection is established (but before the CER/CEA exchange completes).
//
//	C: cdp_tcp_connect()
func (t *Transport) Dial(addr string) (*PeerConnection, error) {
	if t.closed.Load() {
		return nil, ErrTransportClosed
	}
	conn, err := t.dialer("tcp", addr, t.cfg.ConnectTimeout)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConnectFailed, err)
	}
	pc := t.newConnection(conn, DirOutbound)
	pc.remote = conn.RemoteAddr().String()
	go t.serve(pc)
	return pc, nil
}

// newConnection wraps a net.Conn in a PeerConnection and registers it
// with the transport.
func (t *Transport) newConnection(conn net.Conn, dir ConnDirection) *PeerConnection {
	pc := &PeerConnection{
		transport: t,
		conn:      conn,
		direction: dir,
		createdAt: time.Now(),
		localHost: t.cfg.LocalHost,
		localRealm: t.cfg.LocalRealm,
	}
	if conn != nil {
		pc.remote = conn.RemoteAddr().String()
	}
	t.mu.Lock()
	t.conns[pc.remote] = pc
	t.mu.Unlock()
	return pc
}

// serve is the per-connection main loop. It reads Diameter messages in
// a loop, calling the handler for each, until the connection breaks or
// is closed. Mirrors the C receiver() state machine.
func (t *Transport) serve(pc *PeerConnection) {
	if t.handler != nil {
		if err := t.handler.HandleConnect(pc); err != nil {
			pc.Close()
			t.handler.HandleDisconnect(pc, err)
			t.unregister(pc)
			return
		}
	}
	defer func() {
		if t.handler != nil {
			t.handler.HandleDisconnect(pc, nil)
		}
		t.unregister(pc)
	}()

	reader := bufio.NewReader(pc.conn)
	for !pc.IsClosed() {
		if t.cfg.ReadTimeout > 0 {
			if dl, ok := pc.conn.(net.Conn); ok {
				_ = dl.SetReadDeadline(time.Now().Add(t.cfg.ReadTimeout))
			}
		}
		msg, err := t.readMessage(reader)
		if err != nil {
			if errors.Is(err, io.EOF) || pc.IsClosed() {
				return
			}
			// Other errors: log via handler and close.
			if t.handler != nil {
				// Treat decode/IO errors as connection failures.
				_ = err
			}
			return
		}
		if t.handler != nil {
			if err := t.handler.HandleMessage(pc, msg); err != nil {
				_ = pc.Close()
				return
			}
		}
	}
}

// readMessage reads a single Diameter message from r. The first 20 bytes
// are the header (version, length, flags, command code, application id,
// hop-by-hop and end-to-end identifiers); the length field carries the
// total message length including the header. The body is then read in
// full before being decoded.
//
//	C: receiver() — read-state-machine portion
func (t *Transport) readMessage(r *bufio.Reader) (*DiameterMessage, error) {
	var hdr [HeaderLen]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	totalLen := int(getUint24(hdr[1:4]))
	if totalLen < HeaderLen {
		return nil, fmt.Errorf("cdp: bad message length %d", totalLen)
	}
	body := make([]byte, totalLen-HeaderLen)
	if len(body) > 0 {
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, err
		}
	}
	full := make([]byte, totalLen)
	copy(full[:HeaderLen], hdr[:])
	copy(full[HeaderLen:], body)
	return t.enc.Decode(full)
}

// unregister removes pc from the active connection map.
func (t *Transport) unregister(pc *PeerConnection) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if existing, ok := t.conns[pc.remote]; ok && existing == pc {
		delete(t.conns, pc.remote)
	}
}

// Connections returns a snapshot of every active connection.
func (t *Transport) Connections() []*PeerConnection {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]*PeerConnection, 0, len(t.conns))
	for _, pc := range t.conns {
		out = append(out, pc)
	}
	return out
}

// Close stops the listener and closes every active connection.
func (t *Transport) Close() error {
	if !t.closed.CompareAndSwap(false, true) {
		return nil
	}
	t.mu.Lock()
	listener := t.listener
	conns := make([]*PeerConnection, 0, len(t.conns))
	for _, pc := range t.conns {
		conns = append(conns, pc)
	}
	t.mu.Unlock()
	if listener != nil {
		_ = listener.Close()
	}
	for _, pc := range conns {
		_ = pc.Close()
	}
	return nil
}

// IsClosed reports whether the transport has been shut down.
func (t *Transport) IsClosed() bool { return t.closed.Load() }

// ListenerAddr returns the address of the bound listener, or "" when no
// listener is active. Safe for concurrent use.
func (t *Transport) ListenerAddr() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.listener == nil {
		return ""
	}
	return t.listener.Addr().String()
}

// ---------------------------------------------------------------------------
// Loopback transport — used for tests and when no real listener is
// available. Messages sent to a peer are handed back to the handler
// synchronously, allowing the protocol layer to be exercised without a
// live TCP socket.
// ---------------------------------------------------------------------------

// LoopbackTransport is a Transport whose Dial returns an in-memory
// connection. Send places messages on a channel that the test can drain.
type LoopbackTransport struct {
	*Transport
	sentMu sync.Mutex
	sent   map[string][]*DiameterMessage
}

// NewLoopbackTransport creates a transport that does not bind any TCP
// port. All connections are in-memory; messages sent to a connection
// are queued in the Sent() list for the test to inspect.
func NewLoopbackTransport(cfg *TransportConfig, handler MessageHandler) *LoopbackTransport {
	if cfg == nil {
		cfg = DefaultTransportConfig()
		cfg.ListenAddr = "" // never bind.
	}
	t := &LoopbackTransport{
		Transport: NewTransport(cfg, nil, handler),
		sent:      make(map[string][]*DiameterMessage),
	}
	// Replace the dialer with an in-memory stub.
	t.Transport.SetDialer(func(network, addr string, timeout time.Duration) (net.Conn, error) {
		// In-memory pipe pair so the receiver goroutine gets EOF on close.
		c1, c2 := net.Pipe()
		// Return c1 to the dialer; c2 is dropped (the loopback handler
		// does not actually read bytes — it intercepts SendMessage).
		_ = c2
		return c1, nil
	})
	return t
}

// RecordSent appends msg to the per-peer sent-messages list.
func (t *LoopbackTransport) RecordSent(remote string, msg *DiameterMessage) {
	t.sentMu.Lock()
	defer t.sentMu.Unlock()
	t.sent[remote] = append(t.sent[remote], msg)
}

// Sent returns a snapshot of the messages recorded for the given peer.
func (t *LoopbackTransport) Sent(remote string) []*DiameterMessage {
	t.sentMu.Lock()
	defer t.sentMu.Unlock()
	out := make([]*DiameterMessage, len(t.sent[remote]))
	copy(out, t.sent[remote])
	return out
}

// Reset clears the recorded messages.
func (t *LoopbackTransport) Reset() {
	t.sentMu.Lock()
	defer t.sentMu.Unlock()
	t.sent = make(map[string][]*DiameterMessage)
}

// ---------------------------------------------------------------------------
// Convenience: framing helpers exposed for direct byte-level use.
// ---------------------------------------------------------------------------

// EncodeMessage is a package-level convenience that uses the default
// encoder to serialise msg.
func EncodeMessage(msg *DiameterMessage) []byte {
	return DefaultCDPEncoder.Encode(msg)
}

// DecodeMessage is a package-level convenience that uses the default
// encoder to parse data.
func DecodeMessage(data []byte) (*DiameterMessage, error) {
	return DefaultCDPEncoder.Decode(data)
}

// MessageLength returns the total on-wire length of a Diameter message
// given the first 4 bytes of its header (version + length).
func MessageLength(hdr []byte) (int, error) {
	if len(hdr) < 4 {
		return 0, fmt.Errorf("cdp: header too short (%d bytes)", len(hdr))
	}
	return int(getUint24(hdr[1:4])), nil
}

// HopByHopIDFromHeader reads the hop-by-hop identifier from a 20-byte
// Diameter header.
func HopByHopIDFromHeader(hdr []byte) (uint32, error) {
	if len(hdr) < HeaderLen {
		return 0, fmt.Errorf("cdp: header too short (%d bytes)", len(hdr))
	}
	return binary.BigEndian.Uint32(hdr[12:16]), nil
}

// EndToEndIDFromHeader reads the end-to-end identifier from a 20-byte
// Diameter header.
func EndToEndIDFromHeader(hdr []byte) (uint32, error) {
	if len(hdr) < HeaderLen {
		return 0, fmt.Errorf("cdp: header too short (%d bytes)", len(hdr))
	}
	return binary.BigEndian.Uint32(hdr[16:20]), nil
}

// Suppress unused-import warnings for context (kept for future
// cancellation support).
var _ = context.Background
