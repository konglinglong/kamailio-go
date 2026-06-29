// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * IMS Diameter Server module - Diameter protocol server for the IMS
 * Cx/Dx/Sh/Rx reference points.
 * Port of the kamailio ims_diameter_server module (src/modules/ims_diameter_server).
 *
 * The ims_diameter_server module accepts Diameter requests, dispatches
 * them to per-command-code handlers and tracks the resulting sessions.
 * Each session records the Session-Id, the command code, the origin and
 * destination hosts and the AVPs exchanged. Answers are built from the
 * matching request, preserving the Hop-by-Hop and End-to-End identifiers
 * required by RFC 6733.
 *
 * It is safe for concurrent use.
 */

package ims_diameter_server

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Diameter protocol constants (RFC 6733).
const (
	DiameterVersion    = 1
	FlagRequest       = 0x80
	FlagProxiable     = 0x40
	FlagError         = 0x20
	FlagRetransmit    = 0x10

	// Result-Code values (RFC 6733 §7.1).
	ResultCodeSuccess          = 2001
	ResultCodeLimitedSuccess   = 2002
	ResultCodeCommandUnsupported = 3001
	ResultCodeUnableToDeliver  = 3002
	ResultCodeUnknownSessionID = 5002

	// AVP codes commonly used across IMS interfaces.
	AVPCodeSessionID       = 263
	AVPCodeOriginHost      = 264
	AVPCodeOriginRealm     = 296
	AVPCodeDestinationHost = 293
	AVPCodeResultCode      = 268
)

// Config holds the Diameter server configuration.
type Config struct {
	ListenAddr     string
	OriginHost     string
	OriginRealm    string
	VendorID       int
	ProductName    string
	MaxConnections int
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() Config {
	return Config{
		ListenAddr:     "127.0.0.1:3868",
		OriginHost:     "kamailio-go.local",
		OriginRealm:    "local",
		VendorID:       0,
		ProductName:    "kamailio-go",
		MaxConnections: 100,
	}
}

// DiameterAVP represents a single Diameter AVP (RFC 6733 §4).
type DiameterAVP struct {
	Code     uint32
	Flags    uint8
	VendorID uint32
	Data     interface{}
}

// DiameterMessage represents a Diameter request or answer.
type DiameterMessage struct {
	Version       int
	CommandCode   int
	ApplicationID int
	HopByHopID    uint32
	EndToEndID    uint32
	AVPs          []DiameterAVP
}

// DiameterHandler processes a Diameter request and returns the answer.
type DiameterHandler func(req *DiameterMessage) (*DiameterMessage, error)

// DiameterSession captures the state of a single Diameter session.
type DiameterSession struct {
	SessionID       string
	CommandCode     int
	OriginHost      string
	DestinationHost string
	StartTime       time.Time
	AVPs            map[string]interface{}

	updatedAt time.Time
}

// DiameterServerModule maintains the Diameter listeners, handlers and
// active sessions.
type DiameterServerModule struct {
	mu        sync.RWMutex
	config    Config
	listeners []net.Listener
	handlers  map[int]DiameterHandler
	sessions  map[string]*DiameterSession
	counter   atomic.Uint64
	started   bool
}

// NewDiameterServerModule creates a DiameterServerModule with the default
// configuration and empty handler/session storage.
func NewDiameterServerModule() *DiameterServerModule {
	return &DiameterServerModule{
		config:   DefaultConfig(),
		handlers: make(map[int]DiameterHandler),
		sessions: make(map[string]*DiameterSession),
	}
}

// NewDiameterServerModuleWithConfig creates a DiameterServerModule with
// the supplied configuration.
func NewDiameterServerModuleWithConfig(cfg Config) *DiameterServerModule {
	m := NewDiameterServerModule()
	m.config = cfg
	return m
}

// SetConfig replaces the module configuration. The server must be
// stopped before changing the listen address.
func (m *DiameterServerModule) SetConfig(cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = cfg
}

// GetConfig returns a copy of the current configuration.
func (m *DiameterServerModule) GetConfig() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// Start binds the Diameter listener on the configured address. It is
// safe to call repeatedly; subsequent calls when already started are
// no-ops. Returns an error when the listener cannot be created.
//
//	C: mod_init() / cdp_start()
func (m *DiameterServerModule) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return nil
	}
	addr := m.config.ListenAddr
	if addr == "" {
		addr = "127.0.0.1:3868"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("ims_diameter_server: listen %q: %w", addr, err)
	}
	m.listeners = append(m.listeners, ln)
	m.started = true
	go m.acceptLoop(ln)
	return nil
}

// acceptLoop accepts inbound connections until the listener is closed.
// Connections are not fully parsed here; the loop exists so Start()
// mirrors a real server. Message handling is exercised through
// HandleMessage.
func (m *DiameterServerModule) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		// Limit concurrent connections.
		m.mu.RLock()
		max := m.config.MaxConnections
		m.mu.RUnlock()
		if max > 0 && m.SessionCount() >= max {
			conn.Close()
			continue
		}
		go func(c net.Conn) {
			defer c.Close()
			// A real implementation would parse Diameter frames here.
			buf := make([]byte, 1024)
			for {
				if _, err := c.Read(buf); err != nil {
					return
				}
			}
		}(conn)
	}
}

// Stop closes all listeners and marks the server stopped.
//
//	C: mod_destroy()
func (m *DiameterServerModule) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var firstErr error
	for _, ln := range m.listeners {
		if err := ln.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	m.listeners = nil
	m.started = false
	return firstErr
}

// IsStarted reports whether the server is currently listening.
func (m *DiameterServerModule) IsStarted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started
}

// RegisterHandler registers a handler for the given Diameter command
// code, replacing any previously registered handler. A nil handler
// removes the registration.
//
//	C: cdp_register_callback()
func (m *DiameterServerModule) RegisterHandler(commandCode int, handler DiameterHandler) error {
	if commandCode <= 0 {
		return errors.New("ims_diameter_server: invalid command code")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.handlers == nil {
		m.handlers = make(map[int]DiameterHandler)
	}
	if handler == nil {
		delete(m.handlers, commandCode)
		return nil
	}
	m.handlers[commandCode] = handler
	return nil
}

// HandleMessage dispatches a Diameter request to the registered handler.
// When no handler is registered for the command code an answer with
// Result-Code Command-Unsupported is returned. The request's
// Hop-by-Hop / End-to-End identifiers are preserved on the answer.
//
//	C: callback_cdp_request()
func (m *DiameterServerModule) HandleMessage(msg *DiameterMessage) (*DiameterMessage, error) {
	if msg == nil {
		return nil, errors.New("ims_diameter_server: nil message")
	}
	m.mu.RLock()
	handler, ok := m.handlers[msg.CommandCode]
	m.mu.RUnlock()
	if !ok || handler == nil {
		return m.BuildAnswer(msg, ResultCodeCommandUnsupported), nil
	}
	return handler(msg)
}

// CreateSession creates and stores a Diameter session keyed by sessionID.
// If a session already exists it is returned unchanged.
func (m *DiameterServerModule) CreateSession(sessionID string, commandCode int) *DiameterSession {
	if sessionID == "" {
		sessionID = fmt.Sprintf("dia-%d", m.counter.Add(1))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions == nil {
		m.sessions = make(map[string]*DiameterSession)
	}
	if s, ok := m.sessions[sessionID]; ok {
		return s
	}
	now := time.Now()
	s := &DiameterSession{
		SessionID:   sessionID,
		CommandCode: commandCode,
		OriginHost:  m.config.OriginHost,
		AVPs:        make(map[string]interface{}),
		StartTime:   now,
		updatedAt:   now,
	}
	m.sessions[sessionID] = s
	return s
}

// GetSession returns the session identified by sessionID, or nil.
func (m *DiameterServerModule) GetSession(sessionID string) *DiameterSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[sessionID]
}

// RemoveSession removes the session identified by sessionID.
func (m *DiameterServerModule) RemoveSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

// SessionCount returns the number of tracked sessions.
func (m *DiameterServerModule) SessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// ListSessions returns a snapshot of all tracked sessions.
func (m *DiameterServerModule) ListSessions() []*DiameterSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*DiameterSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	return out
}

// BuildAnswer constructs a Diameter answer for the given request, copying
// the Hop-by-Hop / End-to-End identifiers and command code and adding a
// Result-Code AVP carrying resultCode.
//
//	C: AAABuildAnswer()
func (m *DiameterServerModule) BuildAnswer(req *DiameterMessage, resultCode int) *DiameterMessage {
	ans := &DiameterMessage{
		Version:       DiameterVersion,
		CommandCode:   req.CommandCode,
		ApplicationID: req.ApplicationID,
		HopByHopID:    req.HopByHopID,
		EndToEndID:    req.EndToEndID,
		AVPs:          []DiameterAVP{},
	}
	// Copy the Session-Id AVP when present (RFC 6733 §6.2).
	for _, avp := range req.AVPs {
		if avp.Code == AVPCodeSessionID {
			ans.AVPs = append(ans.AVPs, avp)
			break
		}
	}
	// Origin-Host / Origin-Realm from configuration.
	m.mu.RLock()
	host := m.config.OriginHost
	realm := m.config.OriginRealm
	m.mu.RUnlock()
	if host == "" {
		host = "kamailio-go.local"
	}
	if realm == "" {
		realm = "local"
	}
	ans.AVPs = append(ans.AVPs,
		DiameterAVP{Code: AVPCodeOriginHost, Data: host},
		DiameterAVP{Code: AVPCodeOriginRealm, Data: realm},
		DiameterAVP{Code: AVPCodeResultCode, Data: resultCode},
	)
	return ans
}

// FindAVP returns the first AVP with the given code in msg, or nil.
func FindAVP(msg *DiameterMessage, code uint32) *DiameterAVP {
	if msg == nil {
		return nil
	}
	for i := range msg.AVPs {
		if msg.AVPs[i].Code == code {
			return &msg.AVPs[i]
		}
	}
	return nil
}

// AddAVP appends an AVP to msg and returns the updated message.
func AddAVP(msg *DiameterMessage, avp DiameterAVP) *DiameterMessage {
	if msg == nil {
		return nil
	}
	msg.AVPs = append(msg.AVPs, avp)
	return msg
}

// CleanupExpired removes sessions whose last update is older than ttl.
func (m *DiameterServerModule) CleanupExpired(ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for key, s := range m.sessions {
		if now.Sub(s.updatedAt) > ttl {
			delete(m.sessions, key)
		}
	}
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultDM *DiameterServerModule
)

// DefaultDiameterServer returns the process-wide DiameterServerModule,
// creating one on first use.
func DefaultDiameterServer() *DiameterServerModule {
	defaultMu.RLock()
	d := defaultDM
	defaultMu.RUnlock()
	if d != nil {
		return d
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultDM == nil {
		defaultDM = NewDiameterServerModule()
	}
	return defaultDM
}

// Init (re)initialises the process-wide DiameterServerModule to a fresh
// state, mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultDM = NewDiameterServerModule()
}
