// SPDX-License-Identifier-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * MediaProxy module - media proxy session management.
 * Port of the kamailio mediaproxy module (src/modules/mediaproxy).
 *
 * mediaproxy negotiates media proxy sessions for SIP offers/answers. Each
 * Offer/Answer call derives a session id from the message Call-ID and
 * records the negotiated media description. Delete tears a session down
 * by Call-ID; Ping reports liveness.
 *
 * The module is safe for concurrent use.
 */

package mediaproxy

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// Session is a single negotiated media-proxy session.
type Session struct {
	ID       string
	CallID   string
	Offered  bool
	Answered bool
}

// MediaProxyModule manages media-proxy sessions.
type MediaProxyModule struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	nextID   atomic.Int64
	up       atomic.Bool
}

// New creates a MediaProxyModule that is up by default.
func New() *MediaProxyModule {
	m := &MediaProxyModule{sessions: make(map[string]*Session)}
	m.up.Store(true)
	return m
}

// callID extracts the Call-ID from msg, or returns "" when absent.
func callID(msg *parser.SIPMsg) string {
	if msg == nil {
		return ""
	}
	if msg.CallID != nil {
		return msg.CallID.Body.String()
	}
	if h := msg.GetHeaderByType(parser.HdrCallID); h != nil {
		return h.Body.String()
	}
	return ""
}

// Offer creates (or refreshes) a session for the message's Call-ID and
// returns the session id.
func (m *MediaProxyModule) Offer(msg *parser.SIPMsg) (string, error) {
	if !m.up.Load() {
		return "", errors.New("mediaproxy: proxy is down")
	}
	cid := callID(msg)
	if cid == "" {
		return "", errors.New("mediaproxy: message has no Call-ID")
	}
	id := fmt.Sprintf("mp-%d", m.nextID.Add(1))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[cid] = &Session{ID: id, CallID: cid, Offered: true}
	return id, nil
}

// Answer finalises the session for the message's Call-ID, returning its
// session id. Returns an error when no Offer preceded it.
func (m *MediaProxyModule) Answer(msg *parser.SIPMsg) (string, error) {
	if !m.up.Load() {
		return "", errors.New("mediaproxy: proxy is down")
	}
	cid := callID(msg)
	if cid == "" {
		return "", errors.New("mediaproxy: message has no Call-ID")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[cid]
	if !ok {
		return "", errors.New("mediaproxy: no session for Call-ID")
	}
	s.Answered = true
	return s.ID, nil
}

// Delete tears down the session for callID.
func (m *MediaProxyModule) Delete(callID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[callID]; !ok {
		return errors.New("mediaproxy: no such session")
	}
	delete(m.sessions, callID)
	return nil
}

// Ping reports whether the proxy is up.
func (m *MediaProxyModule) Ping() bool { return m.up.Load() }

// SetUp marks the proxy as up or down.
func (m *MediaProxyModule) SetUp(up bool) { m.up.Store(up) }

// Count returns the number of active sessions.
func (m *MediaProxyModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *MediaProxyModule
)

// DefaultMediaProxy returns the process-wide module, creating it on first use.
func DefaultMediaProxy() *MediaProxyModule {
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

// Init (re)initialises the process-wide module.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}
