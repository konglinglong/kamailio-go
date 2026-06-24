// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * RTPMediaServer module - media server session management.
 * Port of the kamailio rtp_media_server module (src/modules/rtp_media_server).
 *
 * rtp_media_server creates media sessions keyed by a generated session id
 * (derived from the supplied Call-ID), lets the script play or record
 * media into a session, and tracks the active session count.
 * DestroySession tears a session down by session id.
 *
 * The module is safe for concurrent use.
 */

package rtp_media_server

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// Session is a single media-server session.
type Session struct {
	ID        string
	CallID    string
	Playing   bool
	Recording bool
	File      string
}

// RTPMediaServerModule manages media sessions.
type RTPMediaServerModule struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	nextID   atomic.Int64
}

// New creates an RTPMediaServerModule.
func New() *RTPMediaServerModule {
	return &RTPMediaServerModule{sessions: make(map[string]*Session)}
}

// CreateSession creates a new session for callID and returns its session
// id. If a session for callID already exists it is replaced.
func (m *RTPMediaServerModule) CreateSession(callID string) string {
	id := fmt.Sprintf("rms-%d", m.nextID.Add(1))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[id] = &Session{ID: id, CallID: callID}
	return id
}

// DestroySession tears down the session with the given session id. It is
// a no-op when no such session exists.
func (m *RTPMediaServerModule) DestroySession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

// PlayMedia marks the session with sessionID as playing file. Returns an
// error when no such session exists.
func (m *RTPMediaServerModule) PlayMedia(sessionID, file string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return errors.New("rtp_media_server: no such session")
	}
	s.Playing = true
	s.Recording = false
	s.File = file
	return nil
}

// RecordMedia marks the session with sessionID as recording to file.
// Returns an error when no such session exists.
func (m *RTPMediaServerModule) RecordMedia(sessionID, file string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return errors.New("rtp_media_server: no such session")
	}
	s.Recording = true
	s.Playing = false
	s.File = file
	return nil
}

// SessionCount returns the number of active sessions.
func (m *RTPMediaServerModule) SessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// GetSession returns the session with the given id (or nil).
func (m *RTPMediaServerModule) GetSession(sessionID string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[sessionID]
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *RTPMediaServerModule
)

// DefaultRTPMediaServer returns the process-wide module, creating it on first use.
func DefaultRTPMediaServer() *RTPMediaServerModule {
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

// Init (re)initialises the process-wide module to a fresh state.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}
