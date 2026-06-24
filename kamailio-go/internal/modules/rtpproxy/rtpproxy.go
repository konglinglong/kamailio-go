// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * rtpproxy module - RTP proxy session management.
 *
 * Manages RTP proxy sessions keyed by call ID. Offer/Answer create or
 * update sessions and return a proxy identifier; Delete tears a session
 * down; Ping reports liveness. No real socket I/O is performed. The
 * module is safe for concurrent use.
 */

package rtpproxy

import (
	"fmt"
	"sync"
	"time"
)

// rtpSession is a single RTP proxy session.
type rtpSession struct {
	callID  string
	created time.Time
}

// RTPProxyModule manages RTP proxy sessions.
type RTPProxyModule struct {
	mu       sync.RWMutex
	alive    bool
	sessions map[string]*rtpSession
	seq      uint64
}

// New creates an RTPProxyModule with no sessions.
func New() *RTPProxyModule {
	return &RTPProxyModule{alive: true, sessions: make(map[string]*rtpSession)}
}

// Offer starts or refreshes a session for callID and returns a proxy
// identifier string.
//
//	C: rtpproxy_offer()
func (m *RTPProxyModule) Offer(callID string) (string, error) {
	return m.createOrUpdate(callID)
}

// Answer completes a session for callID and returns a proxy identifier
// string.
//
//	C: rtpproxy_answer()
func (m *RTPProxyModule) Answer(callID string) (string, error) {
	return m.createOrUpdate(callID)
}

// createOrUpdate inserts or refreshes a session and returns its id.
func (m *RTPProxyModule) createOrUpdate(callID string) (string, error) {
	if callID == "" {
		return "", fmt.Errorf("rtpproxy: empty callID")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.alive {
		return "", fmt.Errorf("rtpproxy: not alive")
	}
	m.seq++
	if _, ok := m.sessions[callID]; !ok {
		m.sessions[callID] = &rtpSession{callID: callID, created: time.Now()}
	}
	return fmt.Sprintf("rtpproxy:%s:%d", callID, m.seq), nil
}

// Delete tears down the session for callID. It is not an error if no
// session exists.
//
//	C: rtpproxy_delete()
func (m *RTPProxyModule) Delete(callID string) error {
	if callID == "" {
		return fmt.Errorf("rtpproxy: empty callID")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, callID)
	return nil
}

// Ping reports whether the proxy is alive.
//
//	C: rtpproxy_ping()
func (m *RTPProxyModule) Ping() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.alive
}

// SetAlive toggles the proxy liveness flag.
func (m *RTPProxyModule) SetAlive(alive bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alive = alive
}

// SessionCount returns the number of active sessions.
//
//	C: rtpproxy_session_count()
func (m *RTPProxyModule) SessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu       sync.RWMutex
	defaultRTPProxy *RTPProxyModule
)

// DefaultRTPProxy returns the process-wide RTPProxyModule, creating it on
// first use.
func DefaultRTPProxy() *RTPProxyModule {
	defaultMu.RLock()
	m := defaultRTPProxy
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultRTPProxy == nil {
		defaultRTPProxy = New()
	}
	return defaultRTPProxy
}

// Init (re)initialises the process-wide RTPProxyModule to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultRTPProxy = New()
}
