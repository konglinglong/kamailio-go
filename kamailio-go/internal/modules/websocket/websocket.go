// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * WebSocket module - SIP-over-WebSocket connection management.
 * Port of the kamailio websocket module (src/modules/websocket).
 *
 * The WebSocket module transports SIP over WebSocket connections (RFC 7118).
 * This Go counterpart tracks live WebSocket connections identified by a
 * connection id, allows broadcasting a message to every connection, sending
 * to a single connection, and closing connections individually or all at
 * once. It also exposes the RFC 6455 frame layer (ws_frame.go) and the
 * handshake layer (ws_handshake.go) used by the real I/O path.
 *
 * It is safe for concurrent use: the connection map is guarded by a
 * read/write lock and the process-wide singleton is guarded by a mutex.
 */

package websocket

import (
	"errors"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Connection state machine (mirrors C ws_connection_state)
// ---------------------------------------------------------------------------

// ConnState is the lifecycle state of a WebSocket connection (RFC 6455 §7).
type ConnState int

const (
	// StateConnecting: the connection is being established (client-side).
	StateConnecting ConnState = iota
	// StateOpen: the handshake has completed; frames can be exchanged.
	StateOpen
	// StateClosing: a Close frame has been sent; awaiting the peer's Close.
	StateClosing
	// StateRemoving: the connection is being torn down and removed.
	StateRemoving
	// StateClosed: the connection has been fully closed.
	StateClosed
)

// String returns a human-readable state name.
func (s ConnState) String() string {
	switch s {
	case StateConnecting:
		return "connecting"
	case StateOpen:
		return "open"
	case StateClosing:
		return "closing"
	case StateRemoving:
		return "removing"
	case StateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// Role is the WebSocket connection role (server or client).
type Role int

const (
	// RoleServer accepts incoming connections and enforces masked frames.
	RoleServer Role = iota
	// RoleClient initiates outbound connections and masks outgoing frames.
	RoleClient
)

// String returns a human-readable role name.
func (r Role) String() string {
	switch r {
	case RoleServer:
		return "server"
	case RoleClient:
		return "client"
	default:
		return "unknown"
	}
}

// ---------------------------------------------------------------------------
// Keepalive mechanism (mirrors C ws_keepalive_mechanism_t)
// ---------------------------------------------------------------------------

// KeepaliveMechanism selects how WebSocket keepalive is performed.
type KeepaliveMechanism int

const (
	// KeepaliveNone disables keepalive.
	KeepaliveNone KeepaliveMechanism = iota
	// KeepalivePing sends Ping frames and expects Pong replies.
	KeepalivePing
	// KeepalivePong sends unsolicited Pong frames.
	KeepalivePong
	// KeepaliveConcheck performs a connection-level check only.
	KeepaliveConcheck
)

// String returns a human-readable mechanism name.
func (m KeepaliveMechanism) String() string {
	switch m {
	case KeepaliveNone:
		return "none"
	case KeepalivePing:
		return "ping"
	case KeepalivePong:
		return "pong"
	case KeepaliveConcheck:
		return "concheck"
	default:
		return "unknown"
	}
}

// ---------------------------------------------------------------------------
// Defaults (mirrors C ws_defaults.h)
// ---------------------------------------------------------------------------

const (
	// DefaultKeepaliveTimeout is the default keepalive timeout in seconds.
	DefaultKeepaliveTimeout = 180
	// DefaultKeepaliveInterval is the default keepalive interval in seconds.
	DefaultKeepaliveInterval = 60
	// DefaultTimerInterval is the default cleanup timer interval in seconds.
	DefaultTimerInterval = 1
	// DefaultRmDelayInterval is the default delay before removing a closed
	// connection from the table.
	DefaultRmDelayInterval = 5
)

// ---------------------------------------------------------------------------
// Config (mirrors C modparams + cfg_group_websocket)
// ---------------------------------------------------------------------------

// WSConfig holds the configuration for a WebSocket listener, mirroring the
// modparams of the C websocket module (ws_listen_address, ws_path,
// ws_origin, ws_sub_protocols, ws_cors_mode, ws_keepalive_mechanism,
// ws_keepalive_timeout, ws_keepalive_interval, ws_ping_application_data).
type WSConfig struct {
	ListenAddr string
	Path       string
	Origins    []string

	// Subprotocols lists the subprotocols the server is willing to
	// negotiate. Defaults to DefaultSubprotocols when empty.
	Subprotocols []string
	// Cors controls how the Access-Control-Allow-Origin response header
	// is sent during the handshake.
	Cors CorsMode
	// RequireSubprotocol, when true, rejects handshakes that do not
	// negotiate a subprotocol (RFC 7118 §3 requires "sip").
	RequireSubprotocol bool
	// KeepaliveMechanism selects the keepalive mechanism (default Ping).
	KeepaliveMechanism KeepaliveMechanism
	// KeepaliveTimeout is the keepalive timeout in seconds (default 180).
	KeepaliveTimeout int
	// KeepaliveInterval is the keepalive interval in seconds (default 60).
	KeepaliveInterval int
	// PingApplicationData is the application data carried in Ping frames
	// (must be 1-125 bytes; empty = use the default signature).
	PingApplicationData string
	// TimerInterval is the cleanup timer interval in seconds.
	TimerInterval int
	// RmDelayInterval is the delay before removing a closed connection.
	RmDelayInterval int
}

// DefaultWSConfig returns a WSConfig populated with the C defaults.
func DefaultWSConfig() *WSConfig {
	return &WSConfig{
		Path:               "/",
		Subprotocols:       append([]string(nil), DefaultSubprotocols...),
		KeepaliveMechanism: KeepalivePing,
		KeepaliveTimeout:   DefaultKeepaliveTimeout,
		KeepaliveInterval:  DefaultKeepaliveInterval,
		TimerInterval:      DefaultTimerInterval,
		RmDelayInterval:    DefaultRmDelayInterval,
	}
}

// ---------------------------------------------------------------------------
// WSConnection
// ---------------------------------------------------------------------------

// WSConnection describes a single live WebSocket connection.
type WSConnection struct {
	ID          string
	RemoteAddr  string
	Subprotocol string
	// Closed is retained for backward compatibility; it is true when
	// State == StateClosed. New code should use State instead.
	Closed bool

	// State machine fields (mirrors C ws_connection_t).
	state    ConnState
	role     Role
	awaitPong bool
	createdAt time.Time
	lastUsed  time.Time

	// outbox buffers messages queued for delivery to the remote peer.
	// Broadcast / SendTo append here; the (external) writer drains it.
	outbox [][]byte
}

// State returns the connection's current lifecycle state.
func (c *WSConnection) State() ConnState { return c.state }

// Role returns the connection role (server or client).
func (c *WSConnection) Role() Role { return c.role }

// CreatedAt returns the connection's creation timestamp.
func (c *WSConnection) CreatedAt() time.Time { return c.createdAt }

// LastUsed returns the timestamp of the most recent activity.
func (c *WSConnection) LastUsed() time.Time { return c.lastUsed }

// touch updates the last-used timestamp to now.
func (c *WSConnection) touch() { c.lastUsed = time.Now() }

// AwaitingPong reports whether the connection is awaiting a Pong reply.
func (c *WSConnection) AwaitingPong() bool { return c.awaitPong }

// WSModule tracks WebSocket connections and provides broadcast / send /
// close operations. It is the Go counterpart of the C websocket module.
type WSModule struct {
	mu    sync.RWMutex
	cfg   *WSConfig
	conns map[string]*WSConnection
}

// New creates a WSModule with empty connection storage.
func New() *WSModule {
	return &WSModule{conns: make(map[string]*WSConnection)}
}

// Init configures the module with the supplied WSConfig. A nil config is
// accepted and leaves the module with empty defaults. It mirrors the C
// module's mod_init.
//
//	C: mod_init()
func (m *WSModule) Init(cfg *WSConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
	if m.conns == nil {
		m.conns = make(map[string]*WSConnection)
	}
	return nil
}

// HandleConnection registers a new WebSocket connection identified by
// connID and remoteAddr, returning the resulting WSConnection. If a
// connection with the same id already exists it is replaced.
//
// The new connection is initialised with State=StateOpen, Role=RoleServer
// and the default "sip" subprotocol — mirroring the C wsconn_add() path
// used by the server-side handshake handler.
//
//	C: wsconn_add() analogue
func (m *WSModule) HandleConnection(connID string, remoteAddr string) *WSConnection {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conns == nil {
		m.conns = make(map[string]*WSConnection)
	}
	now := time.Now()
	c := &WSConnection{
		ID:          connID,
		RemoteAddr:  remoteAddr,
		Subprotocol: "sip",
		state:       StateOpen,
		role:        RoleServer,
		createdAt:   now,
		lastUsed:    now,
	}
	m.conns[connID] = c
	return c
}

// Broadcast sends message to every non-closed connection. Returns an error
// only when there are no connections to deliver to.
//
//	C: ws_broadcast() analogue
func (m *WSModule) Broadcast(message []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.conns) == 0 {
		return errors.New("websocket: no connections")
	}
	delivered := 0
	for _, c := range m.conns {
		if c.Closed || c.state == StateClosed {
			continue
		}
		c.outbox = append(c.outbox, append([]byte(nil), message...))
		c.touch()
		delivered++
	}
	if delivered == 0 {
		return errors.New("websocket: no active connections")
	}
	return nil
}

// SendTo sends message to the connection identified by connID. Returns an
// error when the connection does not exist or is already closed.
//
//	C: ws_send() analogue
func (m *WSModule) SendTo(connID string, message []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conns[connID]
	if !ok {
		return errors.New("websocket: unknown connection " + connID)
	}
	if c.Closed || c.state == StateClosed {
		return errors.New("websocket: connection closed " + connID)
	}
	c.outbox = append(c.outbox, append([]byte(nil), message...))
	c.touch()
	return nil
}

// Close marks the connection identified by connID as closed and removes it
// from the active set. Returns true when a connection was closed.
//
// The connection's State is transitioned StateOpen → StateClosed and the
// Closed flag is set for backward compatibility. Mirrors the C
// wsconn_rm() / wsconn_close_now() path that drops a connection without
// exchanging a Close frame.
//
//	C: wsconn_rm() analogue
func (m *WSModule) Close(connID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conns[connID]
	if !ok {
		return errors.New("websocket: unknown connection " + connID)
	}
	c.state = StateClosed
	c.Closed = true
	delete(m.conns, connID)
	return nil
}

// Connections returns a snapshot of every currently registered connection.
func (m *WSModule) Connections() []*WSConnection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*WSConnection, 0, len(m.conns))
	for _, c := range m.conns {
		out = append(out, c)
	}
	return out
}

// Count returns the number of currently registered connections.
func (m *WSModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.conns)
}

// CloseAll closes and removes every registered connection.
//
//	C: ws_close_all() analogue
func (m *WSModule) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.conns {
		c.state = StateClosed
		c.Closed = true
	}
	m.conns = make(map[string]*WSConnection)
}

// DrainOutbox returns and clears the pending messages queued for the
// connection identified by connID. It is intended for the external writer
// goroutine that actually pushes bytes onto the wire. Returns nil when the
// connection does not exist.
func (m *WSModule) DrainOutbox(connID string) [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conns[connID]
	if !ok {
		return nil
	}
	pending := c.outbox
	c.outbox = nil
	return pending
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultWS *WSModule
)

// DefaultWS returns the process-wide WSModule, creating it on first use.
func DefaultWS() *WSModule {
	defaultMu.RLock()
	m := defaultWS
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultWS == nil {
		defaultWS = New()
	}
	return defaultWS
}

// Init (re)initialises the process-wide WSModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultWS != nil {
		defaultWS.CloseAll()
	}
	defaultWS = New()
}
