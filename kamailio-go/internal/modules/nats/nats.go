// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * nats module - NATS publish/subscribe and request/reply integration.
 *
 * This Go counterpart is an in-memory simulation of a NATS client:
 * it tracks subject subscriptions (with * and > wildcard support) and
 * implements request/reply via reply subjects, so the message flow
 * can be exercised without a real NATS server.
 *
 * It is safe for concurrent use.
 */

package nats

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// NATSConfig holds the connection parameters for a NATS server.
type NATSConfig struct {
	URL      string
	Name     string
	Username string
	Password string
}

// NATSMessage is a single message published to a subject.
type NATSMessage struct {
	Subject string
	Data    []byte
	Reply   string
}

// subscription pairs a subject filter with its handler.
type subscription struct {
	filter  string
	handler func(*NATSMessage)
}

// NATSModule is an in-memory NATS publish/subscribe client.
type NATSModule struct {
	mu        sync.RWMutex
	cfg       NATSConfig
	connected bool
	subs      map[string]*subscription
	closed    bool

	replySeq atomic.Int64
}

// New creates a NATSModule that is not yet connected.
func New() *NATSModule {
	return &NATSModule{
		subs: make(map[string]*subscription),
	}
}

// Init (re)configures the module from cfg. A nil cfg resets the module
// to a disconnected state. A non-nil cfg with a non-empty URL marks the
// module connected.
//
//	C: mod_init() / nats connection
func (m *NATSModule) Init(cfg *NATSConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("nats: module closed")
	}
	m.subs = make(map[string]*subscription)
	if cfg == nil {
		m.cfg = NATSConfig{}
		m.connected = false
		return nil
	}
	m.cfg = *cfg
	if cfg.URL == "" {
		m.connected = false
		return nil
	}
	m.connected = true
	return nil
}

// Publish delivers a message to every subscriber whose subject filter
// matches the subject. Returns an error if the module is closed or not
// connected, or if the subject is empty.
//
//	C: nats_publish()
func (m *NATSModule) Publish(subject string, data []byte) error {
	if subject == "" {
		return errors.New("nats: empty subject")
	}
	msg := &NATSMessage{Subject: subject, Data: data}
	return m.dispatch(msg)
}

// publishReply publishes a reply message to a reply subject. It is the
// internal counterpart used by request/reply responders.
func (m *NATSModule) publishReply(reply string, data []byte) error {
	if reply == "" {
		return errors.New("nats: empty reply subject")
	}
	msg := &NATSMessage{Subject: reply, Data: data, Reply: reply}
	return m.dispatch(msg)
}

// dispatch routes a message to matching subscribers. Handlers are
// invoked outside the lock to avoid re-entrancy deadlocks.
func (m *NATSModule) dispatch(msg *NATSMessage) error {
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return errors.New("nats: module closed")
	}
	if !m.connected {
		m.mu.RUnlock()
		return errors.New("nats: not connected")
	}
	var handlers []func(*NATSMessage)
	for _, s := range m.subs {
		if subjectMatches(s.filter, msg.Subject) {
			handlers = append(handlers, s.handler)
		}
	}
	m.mu.RUnlock()

	for _, h := range handlers {
		h(msg)
	}
	return nil
}

// Subscribe registers a handler for a subject filter (which may contain
// the * and > wildcards). Returns an error if the module is closed or
// not connected, or if the filter is empty.
//
//	C: nats_subscribe()
func (m *NATSModule) Subscribe(subject string, handler func(*NATSMessage)) error {
	if subject == "" {
		return errors.New("nats: empty subject filter")
	}
	if handler == nil {
		return errors.New("nats: nil handler")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("nats: module closed")
	}
	if !m.connected {
		return errors.New("nats: not connected")
	}
	m.subs[subject] = &subscription{filter: subject, handler: handler}
	return nil
}

// Request sends a request to a subject and waits for a single reply,
// delivered to a unique reply subject, up to the given timeout. The
// responder receives a message whose Reply field holds the reply
// subject and responds by publishing to it. Returns the reply data or
// an error if the module is closed, not connected, or the request
// times out.
//
//	C: nats_request()
func (m *NATSModule) Request(subject string, data []byte, timeout time.Duration) ([]byte, error) {
	if subject == "" {
		return nil, errors.New("nats: empty subject")
	}
	if timeout <= 0 {
		return nil, errors.New("nats: non-positive timeout")
	}
	reply := fmt.Sprintf("_INBOX.%d", m.replySeq.Add(1))

	replyCh := make(chan []byte, 1)
	if err := m.Subscribe(reply, func(msg *NATSMessage) {
		select {
		case replyCh <- msg.Data:
		default:
		}
	}); err != nil {
		return nil, err
	}
	defer m.Unsubscribe(reply)

	if err := m.publishRequest(subject, data, reply); err != nil {
		return nil, err
	}

	select {
	case data := <-replyCh:
		return data, nil
	case <-time.After(timeout):
		return nil, errors.New("nats: request timed out")
	}
}

// publishRequest dispatches a request message carrying a reply subject.
func (m *NATSModule) publishRequest(subject string, data []byte, reply string) error {
	msg := &NATSMessage{Subject: subject, Data: data, Reply: reply}
	return m.dispatch(msg)
}

// Unsubscribe removes the subscription for a subject filter. Returns
// an error if the module is closed or not connected. It is not an error
// to unsubscribe a subject that was never subscribed.
//
//	C: nats_unsubscribe()
func (m *NATSModule) Unsubscribe(subject string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("nats: module closed")
	}
	if !m.connected {
		return errors.New("nats: not connected")
	}
	delete(m.subs, subject)
	return nil
}

// Close shuts the module down, clearing subscriptions. It is safe to
// call multiple times.
func (m *NATSModule) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	m.connected = false
	m.subs = make(map[string]*subscription)
}

// IsConnected reports whether the module has been initialised with a
// valid server URL.
func (m *NATSModule) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// SubscribedSubjects returns a snapshot of the currently subscribed
// subject filters. This is primarily useful for tests and inspection.
func (m *NATSModule) SubscribedSubjects() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.subs))
	for s := range m.subs {
		out = append(out, s)
	}
	return out
}

// Config returns a copy of the current configuration.
func (m *NATSModule) Config() NATSConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// subjectMatches reports whether the subscription filter matches the
// published subject. The filter supports the NATS wildcards:
//
//   - matches any token within a single level
//     > matches one or more remaining levels (must be last)
func subjectMatches(filter, subject string) bool {
	if filter == subject {
		return true
	}
	f := splitSubject(filter)
	s := splitSubject(subject)
	for i := 0; i < len(f); i++ {
		if f[i] == ">" {
			// > matches one or more remaining tokens.
			return i < len(s)
		}
		if i >= len(s) {
			return false
		}
		if f[i] == "*" {
			continue
		}
		if f[i] != s[i] {
			return false
		}
	}
	return len(f) == len(s)
}

// splitSubject splits a subject on '.'.
func splitSubject(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

// --- package-level API ---

var defaultMu sync.RWMutex
var defaultModule = New()

// DefaultNATS returns the package-level default NATSModule.
func DefaultNATS() *NATSModule {
	defaultMu.RLock()
	m := defaultModule
	defaultMu.RUnlock()
	return m
}

// Init (re)initialises the package-level default module to a fresh
// state. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultModule = New()
}
