// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * JSON-RPC C module - JSON-RPC client mock.
 * Port of the kamailio jsonrpcc module (src/modules/jsonrpcc).
 *
 * The module maintains a connection state and a mock JSON-RPC client with
 * Call and Notify operations. Notifications are recorded for inspection.
 * It is safe for concurrent use.
 */

package jsonrpcc

import (
	"errors"
	"sync"
	"sync/atomic"
)

// Notification records a single Notify invocation.
type Notification struct {
	Method string
	Params interface{}
}

// JSONRPCCModule maintains a JSON-RPC client connection.
type JSONRPCCModule struct {
	mu            sync.RWMutex
	addr          string
	connected     bool
	calls         atomic.Int64
	notifications []Notification
}

// New creates a JSONRPCCModule, disconnected.
func New() *JSONRPCCModule {
	return &JSONRPCCModule{}
}

// Init configures the server address and marks the client connected.
//
//	C: jsonrpcc_init()
func (m *JSONRPCCModule) Init(addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addr = addr
	m.connected = true
}

// IsConnected returns true when the client has been initialised.
func (m *JSONRPCCModule) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// Call invokes a JSON-RPC method with the given params and returns a
// mock result. It returns an error when not connected.
//
//	C: jsonrpcc_call()
func (m *JSONRPCCModule) Call(method string, params interface{}) (interface{}, error) {
	m.mu.RLock()
	connected := m.connected
	m.mu.RUnlock()
	if !connected {
		return nil, errors.New("jsonrpcc: not connected")
	}
	m.calls.Add(1)
	return map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"result":  "ok",
	}, nil
}

// Notify sends a JSON-RPC notification (no response expected). It returns
// an error when not connected.
//
//	C: jsonrpcc_notify()
func (m *JSONRPCCModule) Notify(method string, params interface{}) error {
	m.mu.RLock()
	connected := m.connected
	m.mu.RUnlock()
	if !connected {
		return errors.New("jsonrpcc: not connected")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifications = append(m.notifications, Notification{Method: method, Params: params})
	return nil
}

// CallCount returns the number of successful calls.
func (m *JSONRPCCModule) CallCount() int64 {
	return m.calls.Load()
}

// Notifications returns a copy of the recorded notifications.
func (m *JSONRPCCModule) Notifications() []Notification {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Notification, len(m.notifications))
	copy(out, m.notifications)
	return out
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *JSONRPCCModule
)

// DefaultJSONRPCC returns the process-wide JSONRPCCModule.
func DefaultJSONRPCC() *JSONRPCCModule {
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
// JSONRPCCModule and configures the address.
func Init(addr string) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
	defaultM.addr = addr
	defaultM.connected = true
}

// Call is the package-level wrapper around DefaultJSONRPCC().Call.
func Call(method string, params interface{}) (interface{}, error) {
	return DefaultJSONRPCC().Call(method, params)
}

// Notify is the package-level wrapper around DefaultJSONRPCC().Notify.
func Notify(method string, params interface{}) error {
	return DefaultJSONRPCC().Notify(method, params)
}
