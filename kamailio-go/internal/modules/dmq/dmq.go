// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * dmq module - distributed message queue.
 * Port of the kamailio dmq module (src/modules/dmq).
 *
 * The original C module distributes SIP messages between Kamailio
 * instances over a peer mesh using SIP KDMQ requests and a pool of
 * worker processes. This Go counterpart is a *loopback* simulation:
 * peers are tracked in memory and messages sent via Broadcast /
 * SendToPeer are delivered to the local Receive() channel, so the
 * message flow can be exercised without any real network.
 *
 * It is safe for concurrent use.
 */

package dmq

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// inboxCapacity is the buffer size of the local receive channel.
const inboxCapacity = 256

// DMQPeer describes a single DMQ peer node.
type DMQPeer struct {
	Address   string
	Port      int
	Connected bool
}

// DMQMessage is a message distributed over the DMQ mesh.
type DMQMessage struct {
	Type      string
	Body      string
	FromPeer  string
	Timestamp time.Time
}

// DMQConfig configures a DMQModule.
type DMQConfig struct {
	ListenAddr string
	Peers      []string
}

// DMQModule is a loopback distributed message queue.
// It is the Go counterpart of the kamailio dmq module.
type DMQModule struct {
	mu       sync.RWMutex
	peers    map[string]*DMQPeer
	selfAddr string
	inbox    chan *DMQMessage
	closed   bool
}

// New creates a DMQModule with empty peer storage.
func New() *DMQModule {
	return &DMQModule{
		peers: make(map[string]*DMQPeer),
		inbox: make(chan *DMQMessage, inboxCapacity),
	}
}

// Init (re)configures the module from cfg. It resets peer storage and
// pre-registers any peers listed in cfg. A nil cfg applies defaults.
//
//	C: mod_init()
func (m *DMQModule) Init(cfg *DMQConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("dmq: module closed")
	}
	m.peers = make(map[string]*DMQPeer)
	if cfg != nil {
		m.selfAddr = cfg.ListenAddr
		for _, p := range cfg.Peers {
			m.peers[p] = &DMQPeer{Address: p, Connected: true}
		}
	}
	return nil
}

// peerKey builds the map key for a peer.
func peerKey(address string, port int) string {
	return fmt.Sprintf("%s:%d", address, port)
}

// AddPeer registers a peer and marks it connected. If a peer with the
// same address:port already exists it is updated and returned.
//
//	C: add_notification_peer() / dmq_node_list_add()
func (m *DMQModule) AddPeer(address string, port int) *DMQPeer {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.peers == nil {
		m.peers = make(map[string]*DMQPeer)
	}
	key := peerKey(address, port)
	if p, ok := m.peers[key]; ok {
		p.Connected = true
		return p
	}
	p := &DMQPeer{Address: address, Port: port, Connected: true}
	m.peers[key] = p
	return p
}

// RemovePeer removes a peer by address:port. It returns true if the
// peer was present.
func (m *DMQModule) RemovePeer(address string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.peers[address]; ok {
		delete(m.peers, address)
		return true
	}
	return false
}

// Broadcast distributes a message to all connected peers. In this
// loopback simulation the message is delivered once to the local
// Receive() channel. Returns an error if there are no connected peers.
//
//	C: bcast_dmq_message()
func (m *DMQModule) Broadcast(msgType string, body string) error {
	m.mu.RLock()
	hasConnected := false
	for _, p := range m.peers {
		if p.Connected {
			hasConnected = true
			break
		}
	}
	self := m.selfAddr
	m.mu.RUnlock()
	if !hasConnected {
		return errors.New("dmq: no connected peers")
	}
	msg := &DMQMessage{
		Type:      msgType,
		Body:      body,
		FromPeer:  self,
		Timestamp: time.Now(),
	}
	return m.deliver(msg)
}

// SendToPeer sends a message to a specific peer (by address:port key).
// In this loopback simulation the message is delivered to the local
// Receive() channel. Returns an error if the peer is missing or not
// connected.
//
//	C: send_dmq_message()
func (m *DMQModule) SendToPeer(peer string, msgType string, body string) error {
	m.mu.RLock()
	p, ok := m.peers[peer]
	connected := ok && p.Connected
	m.mu.RUnlock()
	if !connected {
		return fmt.Errorf("dmq: peer %q not connected", peer)
	}
	msg := &DMQMessage{
		Type:      msgType,
		Body:      body,
		FromPeer:  peer,
		Timestamp: time.Now(),
	}
	return m.deliver(msg)
}

// deliver puts a message on the local inbox channel.
func (m *DMQModule) deliver(msg *DMQMessage) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errors.New("dmq: module closed")
	}
	m.mu.Unlock()
	select {
	case m.inbox <- msg:
		return nil
	default:
		return errors.New("dmq: inbox full")
	}
}

// Receive returns a channel that receives DMQMessages delivered to this
// node.
func (m *DMQModule) Receive() <-chan *DMQMessage {
	return m.inbox
}

// Peers returns a snapshot of all registered peers.
func (m *DMQModule) Peers() []*DMQPeer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*DMQPeer, 0, len(m.peers))
	for _, p := range m.peers {
		out = append(out, p)
	}
	return out
}

// IsConnected reports whether the peer (by address:port key) is
// currently connected.
func (m *DMQModule) IsConnected(peer string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.peers[peer]
	return ok && p.Connected
}

// Close shuts the module down, closing the receive channel and clearing
// peer storage. It is safe to call multiple times.
func (m *DMQModule) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	close(m.inbox)
	m.peers = make(map[string]*DMQPeer)
}

// --- package-level API ---

var defaultModule = New()

// DefaultDMQ returns the package-level default DMQModule.
func DefaultDMQ() *DMQModule {
	return defaultModule
}

// Init (re)initialises the package-level default module with defaults.
func Init() {
	defaultModule = New()
}
