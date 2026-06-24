// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * pua_rpc module - Presence User Agent RPC client.
 *
 * Sends PUBLISH/SUBSCRIBE requests to a remote PUA server over an RPC
 * channel. Init configures the server address; SendPublish and
 * SendSubscribe buffer the requests and would dispatch them over the
 * wire in a real deployment. The module is safe for concurrent use.
 */

package pua_rpc

import (
	"errors"
	"sync"
)

// rpcRequest is a buffered RPC request.
type rpcRequest struct {
	method string
	user   string
	body   string
}

// PUARPCModule is an RPC client for a remote PUA server.
type PUARPCModule struct {
	mu        sync.RWMutex
	addr      string
	connected bool
	requests  []rpcRequest
}

// New creates a PUARPCModule with no server configured.
func New() *PUARPCModule {
	return &PUARPCModule{}
}

// Init configures the RPC server address and marks the module connected.
// An empty addr leaves the module disconnected.
//
//	C: pua_rpc_init()
func (m *PUARPCModule) Init(addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addr = addr
	m.connected = addr != ""
}

// IsConnected reports whether the module has an active connection.
//
//	C: pua_rpc_is_connected()
func (m *PUARPCModule) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// SendPublish buffers a PUBLISH request for user with body. Returns an
// error when not connected or user is empty.
//
//	C: pua_rpc_send_publish()
func (m *PUARPCModule) SendPublish(user, body string) error {
	if user == "" {
		return errors.New("pua_rpc: empty user")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return errors.New("pua_rpc: not connected")
	}
	m.requests = append(m.requests, rpcRequest{method: "PUBLISH", user: user, body: body})
	return nil
}

// SendSubscribe buffers a SUBSCRIBE request for user. Returns an error
// when not connected or user is empty.
//
//	C: pua_rpc_send_subscribe()
func (m *PUARPCModule) SendSubscribe(user string) error {
	if user == "" {
		return errors.New("pua_rpc: empty user")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return errors.New("pua_rpc: not connected")
	}
	m.requests = append(m.requests, rpcRequest{method: "SUBSCRIBE", user: user})
	return nil
}

// PendingCount returns the number of buffered requests.
func (m *PUARPCModule) PendingCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.requests)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu     sync.RWMutex
	defaultPUARPC *PUARPCModule
)

// DefaultPUARPC returns the process-wide PUARPCModule, creating it on
// first use.
func DefaultPUARPC() *PUARPCModule {
	defaultMu.RLock()
	m := defaultPUARPC
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultPUARPC == nil {
		defaultPUARPC = New()
	}
	return defaultPUARPC
}

// Init (re)initialises the process-wide PUARPCModule to a fresh,
// unconfigured state. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultPUARPC = New()
}
