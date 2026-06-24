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
 * once.
 *
 * It is safe for concurrent use: the connection map is guarded by a
 * read/write lock and the process-wide singleton is guarded by a mutex.
 */

package websocket

import (
	"errors"
	"sync"
)

// WSConfig holds the configuration for a WebSocket listener, mirroring the
// modparams of the C websocket module (ws_listen_address, ws_path,
// ws_origin).
type WSConfig struct {
	ListenAddr string
	Path       string
	Origins    []string
}

// WSConnection describes a single live WebSocket connection.
type WSConnection struct {
	ID          string
	RemoteAddr  string
	Subprotocol string
	Closed      bool

	// outbox buffers messages queued for delivery to the remote peer.
	// Broadcast / SendTo append here; the (external) writer drains it.
	outbox [][]byte
}

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
//	C: ws_conn_add() analogue
func (m *WSModule) HandleConnection(connID string, remoteAddr string) *WSConnection {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conns == nil {
		m.conns = make(map[string]*WSConnection)
	}
	c := &WSConnection{
		ID:          connID,
		RemoteAddr:  remoteAddr,
		Subprotocol: "sip",
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
		if c.Closed {
			continue
		}
		c.outbox = append(c.outbox, append([]byte(nil), message...))
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
	if c.Closed {
		return errors.New("websocket: connection closed " + connID)
	}
	c.outbox = append(c.outbox, append([]byte(nil), message...))
	return nil
}

// Close marks the connection identified by connID as closed and removes it
// from the active set. Returns true when a connection was closed.
//
//	C: ws_conn_close() analogue
func (m *WSModule) Close(connID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conns[connID]
	if !ok {
		return errors.New("websocket: unknown connection " + connID)
	}
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
