// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SCTP module - SCTP transport simulation.
 * Port of the kamailio sctp module (src/modules/sctp).
 *
 * This implementation simulates an SCTP association: Init records the
 * peer address and opens a receive channel; Send buffers outgoing data
 * (also mirroring it onto the receive channel so tests can read it back);
 * Receive returns the channel of inbound byte slices; IsConnected reports
 * liveness.
 *
 * The module is safe for concurrent use.
 */

package sctp

import (
	"errors"
	"sync"
	"sync/atomic"
)

// SCTPModule simulates an SCTP association.
type SCTPModule struct {
	mu        sync.Mutex
	addr      string
	connected atomic.Bool
	rx        chan []byte
}

// New creates an SCTPModule that is not yet connected.
func New() *SCTPModule {
	return &SCTPModule{}
}

// Init configures the peer address and marks the module connected. It
// (re)creates the receive channel. It mirrors Kamailio's mod_init.
func (m *SCTPModule) Init(addr string) error {
	if addr == "" {
		return errors.New("sctp: empty address")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addr = addr
	m.rx = make(chan []byte, 64)
	m.connected.Store(true)
	return nil
}

// IsConnected reports whether Init has been called (and Close not yet).
func (m *SCTPModule) IsConnected() bool {
	return m.connected.Load()
}

// Send buffers data for transmission and mirrors a copy onto the receive
// channel so callers can read it back. Returns an error when not
// connected or data is empty.
func (m *SCTPModule) Send(data []byte) error {
	if len(data) == 0 {
		return errors.New("sctp: empty data")
	}
	if !m.connected.Load() {
		return errors.New("sctp: not connected")
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	m.mu.Lock()
	rx := m.rx
	m.mu.Unlock()
	select {
	case rx <- cp:
		return nil
	default:
		return errors.New("sctp: receive buffer full")
	}
}

// Receive returns the channel of inbound byte slices. It returns nil when
// the module is not connected.
func (m *SCTPModule) Receive() <-chan []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rx
}

// Close marks the module disconnected. The receive channel is drained and
// closed so pending readers unblock.
func (m *SCTPModule) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected.Load() {
		return
	}
	m.connected.Store(false)
	if m.rx != nil {
		close(m.rx)
		m.rx = nil
	}
}

// Addr returns the configured peer address.
func (m *SCTPModule) Addr() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.addr
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *SCTPModule
)

// DefaultSCTP returns the process-wide module, creating it on first use.
func DefaultSCTP() *SCTPModule {
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

// Init is the package-level (re)initialiser.
func Init(addr string) error { return DefaultSCTP().Init(addr) }

// Send is the package-level wrapper.
func Send(data []byte) error { return DefaultSCTP().Send(data) }

// Receive is the package-level wrapper.
func Receive() <-chan []byte { return DefaultSCTP().Receive() }

// IsConnected is the package-level wrapper.
func IsConnected() bool { return DefaultSCTP().IsConnected() }
