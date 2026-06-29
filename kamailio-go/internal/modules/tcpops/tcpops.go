// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * TCP operations module - TCP connection inspection and management.
 * Port of the kamailio tcpops module (src/modules/tcpops).
 *
 * The tcpops module exposes operations on the TCP connections used to
 * transport SIP: looking up a connection by id or by source address,
 * closing a connection, adjusting its lifetime and toggling keepalive.
 *
 * It is safe for concurrent use: the connection map is guarded by a
 * read/write lock and the process-wide singleton is guarded by a mutex.
 */

package tcpops

import (
	"strconv"
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// TCPConnInfo describes a single TCP connection tracked by the module.
type TCPConnInfo struct {
	ID         int
	LocalAddr  string
	RemoteAddr string
	State      string
	Lifetime   int
	Keepalive  bool
}

// TCPOpsModule tracks TCP connections and exposes tcpops operations.
// C: struct module tcpops
type TCPOpsModule struct {
	mu      sync.RWMutex
	conns   map[int]*TCPConnInfo
	nextID  int
	keepDef bool
}

// New creates a TCPOpsModule with empty connection storage.
func New() *TCPOpsModule {
	return &TCPOpsModule{conns: make(map[int]*TCPConnInfo)}
}

// AddConnection registers a new TCP connection and returns its info. It is
// the entry point used by the transport layer to make a connection visible
// to tcpops; the id is assigned by the module.
//
//	C: tcpops_conn_add() analogue
func (m *TCPOpsModule) AddConnection(localAddr, remoteAddr string) *TCPConnInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conns == nil {
		m.conns = make(map[int]*TCPConnInfo)
	}
	m.nextID++
	c := &TCPConnInfo{
		ID:         m.nextID,
		LocalAddr:  localAddr,
		RemoteAddr: remoteAddr,
		State:      "established",
		Lifetime:   0,
		Keepalive:  m.keepDef,
	}
	m.conns[c.ID] = c
	return c
}

// GetConnectionByID returns the connection identified by id, or nil.
//
//	C: tcp_get_conn() / tcpops_get_con()
func (m *TCPOpsModule) GetConnectionByID(id int) *TCPConnInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.conns[id]
}

// GetConnectionBySource returns the first connection whose remote address
// matches ip:port, or nil when none matches.
//
//	C: tcpops_get_con_by_source() analogue
func (m *TCPOpsModule) GetConnectionBySource(ip string, port int) *TCPConnInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	want := ip + ":" + strconv.Itoa(port)
	for _, c := range m.conns {
		if c.RemoteAddr == want {
			return c
		}
	}
	return nil
}

// CloseConnection removes the connection identified by id. Returns true
// when a connection was closed.
//
//	C: tcpops_close_connection()
func (m *TCPOpsModule) CloseConnection(id int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conns[id]
	if !ok {
		return false
	}
	c.State = "closed"
	delete(m.conns, id)
	return true
}

// SetConnectionLifetime updates the lifetime (in seconds) of the
// connection identified by id. Returns true when the connection exists.
//
//	C: tcpops_set_connection_lifetime()
func (m *TCPOpsModule) SetConnectionLifetime(id int, seconds int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conns[id]
	if !ok {
		return false
	}
	c.Lifetime = seconds
	return true
}

// SetKeepalive toggles the keepalive flag of the connection identified by
// id. Returns true when the connection exists.
//
//	C: tcpops_set_keepalive()
func (m *TCPOpsModule) SetKeepalive(id int, enable bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conns[id]
	if !ok {
		return false
	}
	c.Keepalive = enable
	return true
}

// ConnectionCount returns the number of tracked connections.
func (m *TCPOpsModule) ConnectionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.conns)
}

// ListConnections returns a snapshot of every tracked connection.
func (m *TCPOpsModule) ListConnections() []*TCPConnInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*TCPConnInfo, 0, len(m.conns))
	for _, c := range m.conns {
		out = append(out, c)
	}
	return out
}

// IsPersistent reports whether the transport carrying msg should be
// treated as persistent. A connection is persistent when it uses a
// connection-oriented transport (TCP, TLS, SCTP, WS, WSS) as recorded in
// the top Via header, or when the message carries a "Connection:
// keep-alive" header.
//
//	C: tcpops_is_persistent() analogue
func (m *TCPOpsModule) IsPersistent(msg *parser.SIPMsg) bool {
	if msg == nil {
		return false
	}
	via := msg.HdrVia1
	if via == nil {
		via = msg.GetHeaderByType(parser.HdrVia)
	}
	if via != nil {
		vb, err := parser.ParseVia(via.Body)
		if err == nil && vb != nil {
			t := strings.ToLower(vb.Transport.String())
			switch t {
			case "tcp", "tls", "sctp", "ws", "wss":
				return true
			}
		}
	}
	// "Connection" is not a standard tracked header type, so fall back to a
	// linear scan for a Connection header.
	for _, h := range msg.Headers {
		if strings.EqualFold(h.Name.String(), "Connection") {
			if strings.Contains(strings.ToLower(h.Body.String()), "keep-alive") {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu     sync.RWMutex
	defaultTCPOps *TCPOpsModule
)

// DefaultTCPOps returns the process-wide TCPOpsModule, creating it on first
// use.
func DefaultTCPOps() *TCPOpsModule {
	defaultMu.RLock()
	m := defaultTCPOps
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultTCPOps == nil {
		defaultTCPOps = New()
	}
	return defaultTCPOps
}

// Init (re)initialises the process-wide TCPOpsModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultTCPOps = New()
}
