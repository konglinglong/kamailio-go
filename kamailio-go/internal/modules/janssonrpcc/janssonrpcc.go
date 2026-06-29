// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Jansson RPC client module - JSON-RPC client mock.
 * Port of the kamailio janssonrpcc module (src/modules/janssonrpcc).
 *
 * The module maintains a connection state and a mock JSON-RPC client.
 * Call returns a synthesised result echoing the method and params. It
 * is safe for concurrent use.
 */

package janssonrpcc

import (
	"errors"
	"sync"
	"sync/atomic"
)

// JanssonRPCCModule maintains a JSON-RPC client connection.
type JanssonRPCCModule struct {
	mu      sync.RWMutex
	addr    string
	connected bool
	calls   atomic.Int64
}

// New creates a JanssonRPCCModule, disconnected.
func New() *JanssonRPCCModule {
	return &JanssonRPCCModule{}
}

// Init configures the server address and marks the client connected.
//
//	C: janssonrpcc_init()
func (m *JanssonRPCCModule) Init(addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addr = addr
	m.connected = true
}

// IsConnected returns true when the client has been initialised.
func (m *JanssonRPCCModule) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// Call invokes a JSON-RPC method with the given params and returns a
// mock result. It returns an error when not connected.
//
//	C: janssonrpcc_call()
func (m *JanssonRPCCModule) Call(method string, params interface{}) (interface{}, error) {
	m.mu.RLock()
	connected := m.connected
	m.mu.RUnlock()
	if !connected {
		return nil, errors.New("janssonrpcc: not connected")
	}
	m.calls.Add(1)
	return map[string]interface{}{
		"method": method,
		"params": params,
		"result": "ok",
	}, nil
}

// CallCount returns the number of successful calls.
func (m *JanssonRPCCModule) CallCount() int64 {
	return m.calls.Load()
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *JanssonRPCCModule
)

// DefaultJanssonRPCC returns the process-wide JanssonRPCCModule.
func DefaultJanssonRPCC() *JanssonRPCCModule {
	defaultMu.RLock()
	m := defaultM
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM == nil {
		defaultM = New()
	}
	return defaultM
}

// Init is the package-level wrapper that (re)initialises the process-wide
// JanssonRPCCModule and configures the address.
func Init(addr string) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
	defaultM.addr = addr
	defaultM.connected = true
}

// Call is the package-level wrapper around DefaultJanssonRPCC().Call.
func Call(method string, params interface{}) (interface{}, error) {
	return DefaultJanssonRPCC().Call(method, params)
}

// IsConnected is the package-level wrapper around DefaultJanssonRPCC().IsConnected.
func IsConnected() bool { return DefaultJanssonRPCC().IsConnected() }
