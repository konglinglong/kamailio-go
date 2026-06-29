// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Erlang interface module - Erlang node communication mock.
 * Port of the kamailio erlang module (src/modules/erlang).
 *
 * The module maintains a connection state and a FIFO message mailbox.
 * Send enqueues a message addressed to a node; Receive dequeues the
 * oldest message. It is safe for concurrent use.
 */

package erlang

import (
	"errors"
	"sync"
)

// ErlangModule maintains an Erlang node connection and mailbox.
type ErlangModule struct {
	mu       sync.RWMutex
	mailboxMu sync.Mutex
	node     string
	mailbox  []string
}

// New creates an ErlangModule with empty storage, disconnected.
func New() *ErlangModule {
	return &ErlangModule{}
}

// Init configures the Erlang node name and marks the module connected.
//
//	C: erl_init()
func (m *ErlangModule) Init(node string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.node = node
}

// IsConnected returns true when a node name has been configured.
func (m *ErlangModule) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.node != ""
}

// Send enqueues a message addressed to the given node. It returns an
// error when the module is not connected.
//
//	C: erl_send()
func (m *ErlangModule) Send(node, msg string) error {
	if !m.IsConnected() {
		return errors.New("erlang: not connected")
	}
	if msg == "" {
		return errors.New("erlang: empty message")
	}
	m.mailboxMu.Lock()
	defer m.mailboxMu.Unlock()
	m.mailbox = append(m.mailbox, node+":"+msg)
	return nil
}

// Receive dequeues and returns the oldest message, or "" when the
// mailbox is empty.
//
//	C: erl_receive()
func (m *ErlangModule) Receive() string {
	m.mailboxMu.Lock()
	defer m.mailboxMu.Unlock()
	if len(m.mailbox) == 0 {
		return ""
	}
	msg := m.mailbox[0]
	m.mailbox = m.mailbox[1:]
	return msg
}

// Pending returns the number of messages waiting in the mailbox.
func (m *ErlangModule) Pending() int {
	m.mailboxMu.Lock()
	defer m.mailboxMu.Unlock()
	return len(m.mailbox)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *ErlangModule
)

// DefaultErlang returns the process-wide ErlangModule.
func DefaultErlang() *ErlangModule {
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
// ErlangModule and configures the node name.
func Init(node string) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
	defaultM.node = node
}

// Send is the package-level wrapper around DefaultErlang().Send.
func Send(node, msg string) error { return DefaultErlang().Send(node, msg) }

// Receive is the package-level wrapper around DefaultErlang().Receive.
func Receive() string { return DefaultErlang().Receive() }

// IsConnected is the package-level wrapper around DefaultErlang().IsConnected.
func IsConnected() bool { return DefaultErlang().IsConnected() }
