// SPDX-License-Identifier-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * LRKProxy module - lightweight RTP/media proxy session management.
 * Port of the kamailio lrkproxy module (src/modules/lrkproxy).
 *
 * lrkproxy negotiates media proxy sessions for SIP offers/answers. Each
 * Offer/Answer call derives a session id from the message Call-ID and
 * records the negotiated media description. Delete tears a session down
 * by Call-ID; Ping reports liveness.
 *
 * The module is safe for concurrent use.
 */

package lrkproxy

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// Session is a single negotiated media-proxy session.
type Session struct {
	ID        string
	CallID    string
	SDP       string
	Created   bool
	Answered  bool
}

// LRKProxyModule manages media-proxy sessions.
type LRKProxyModule struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	nextID   atomic.Int64
	up       atomic.Bool
}

// New creates an LRKProxyModule that is up by default.
func New() *LRKProxyModule {
	m := &LRKProxyModule{sessions: make(map[string]*Session)}
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
// returns the session id. It returns an error when the message is nil or
// has no Call-ID, or when the proxy is down.
func (m *LRKProxyModule) Offer(msg *parser.SIPMsg) (string, error) {
	if !m.up.Load() {
		return "", errors.New("lrkproxy: proxy is down")
	}
	cid := callID(msg)
	if cid == "" {
		return "", errors.New("lrkproxy: message has no Call-ID")
	}
	id := fmt.Sprintf("lrk-%d", m.nextID.Add(1))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[cid] = &Session{ID: id, CallID: cid, Created: true}
	return id, nil
}

// Answer finalises the session for the message's Call-ID, returning its
// session id. Returns an error when no Offer preceded it.
func (m *LRKProxyModule) Answer(msg *parser.SIPMsg) (string, error) {
	if !m.up.Load() {
		return "", errors.New("lrkproxy: proxy is down")
	}
	cid := callID(msg)
	if cid == "" {
		return "", errors.New("lrkproxy: message has no Call-ID")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[cid]
	if !ok {
		return "", errors.New("lrkproxy: no session for Call-ID")
	}
	s.Answered = true
	return s.ID, nil
}

// Delete tears down the session for callID. It returns an error when no
// such session exists.
func (m *LRKProxyModule) Delete(callID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[callID]; !ok {
		return errors.New("lrkproxy: no such session")
	}
	delete(m.sessions, callID)
	return nil
}

// Ping reports whether the proxy is up.
func (m *LRKProxyModule) Ping() bool {
	return m.up.Load()
}

// SetUp marks the proxy as up.
func (m *LRKProxyModule) SetUp(up bool) { m.up.Store(up) }

// Count returns the number of active sessions.
func (m *LRKProxyModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *LRKProxyModule
)

// DefaultLRKProxy returns the process-wide module, creating it on first use.
func DefaultLRKProxy() *LRKProxyModule {
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
