// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SEAS module - SIP Express Application Server interface.
 * Port of the kamailio seas module (src/modules/seas).
 *
 * The SEAS module forwards SIP messages to external Application Servers
 * registered by name. This Go counterpart tracks the registered AS
 * instances and records the messages dispatched to them; the actual
 * network transport is delegated to a pluggable Transport function so
 * the module is usable and testable out of the box.
 *
 * It is safe for concurrent use.
 */
package seas

import (
	"fmt"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// appServer is a registered Application Server.
type appServer struct {
	name string
	addr string
}

// sentMsg records a message dispatched to an AS.
type sentMsg struct {
	as      string
	payload string
}

// Transport delivers a SIP message payload to the named AS address.
// The default transport is a no-op that always succeeds; tests may inject
// a mock to assert delivery or simulate failures.
type Transport func(asName, asAddr string, msg *parser.SIPMsg) error

// defaultTransport always succeeds without doing any I/O.
func defaultTransport(asName, asAddr string, msg *parser.SIPMsg) error {
	return nil
}

// SEASModule implements the SEAS module functionality.
// C: struct module seas
type SEASModule struct {
	mu        sync.RWMutex
	servers   map[string]*appServer
	sent      []sentMsg
	transport Transport
}

// NewSEASModule creates a SEASModule with the default (no-op) transport.
func NewSEASModule() *SEASModule {
	return &SEASModule{
		servers:   make(map[string]*appServer),
		transport: defaultTransport,
	}
}

// SetTransport replaces the message transport. Passing nil restores the
// default (no-op) transport.
func (m *SEASModule) SetTransport(t Transport) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t == nil {
		m.transport = defaultTransport
		return
	}
	m.transport = t
}

// RegisterAS registers an Application Server under name reachable at addr.
// Re-registering an existing name updates its address.
// C: as_register()
func (m *SEASModule) RegisterAS(name string, addr string) {
	if name == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.servers == nil {
		m.servers = make(map[string]*appServer)
	}
	m.servers[name] = &appServer{name: name, addr: addr}
}

// UnregisterAS removes the Application Server registered under name.
// C: as_unregister()
func (m *SEASModule) UnregisterAS(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.servers, name)
}

// IsASRegistered reports whether an AS named name is currently registered.
// C: as_is_registered()
func (m *SEASModule) IsASRegistered(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.servers[name]
	return ok
}

// SendToAS dispatches msg to the Application Server named as. Returns an
// error when msg is nil or the AS is not registered.
// C: seas_send_to_as()
func (m *SEASModule) SendToAS(as string, msg *parser.SIPMsg) error {
	if msg == nil {
		return fmt.Errorf("seas: nil message")
	}
	m.mu.RLock()
	srv, ok := m.servers[as]
	transport := m.transport
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("seas: unknown AS %q", as)
	}
	if err := transport(as, srv.addr, msg); err != nil {
		return err
	}
	payload := ""
	if len(msg.Buf) > 0 {
		payload = string(msg.Buf)
	}
	m.mu.Lock()
	m.sent = append(m.sent, sentMsg{as: as, payload: payload})
	m.mu.Unlock()
	return nil
}

// SentCount returns the number of messages dispatched via SendToAS.
func (m *SEASModule) SentCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sent)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu    sync.RWMutex
	defaultSEAS  *SEASModule
)

// DefaultSEAS returns the process-wide SEASModule, creating one on first use.
func DefaultSEAS() *SEASModule {
	defaultMu.RLock()
	m := defaultSEAS
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultSEAS == nil {
		defaultSEAS = NewSEASModule()
	}
	return defaultSEAS
}

// Init (re)initialises the process-wide SEASModule to a fresh state.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultSEAS = NewSEASModule()
}
