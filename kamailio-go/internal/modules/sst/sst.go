// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * sst module - SIP Session Timer (RFC 4028), matching C sst.c.
 *
 * Parses Session-Expires / Min-SE headers, validates the negotiated
 * interval against a configurable minimum, tracks active sessions and
 * generates session-refresh requests (INVITE or UPDATE).
 *
 * All exported state is guarded by a sync.RWMutex so the module is safe
 * for concurrent use.
 */

package sst

import (
	"fmt"
	"sync"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// DefaultMinSE is the default minimum Session-Expires value (seconds),
// matching Kamailio's default of 90 seconds.
const DefaultMinSE = 90

// SSTModule implements the SIP Session Timer (RFC 4028). It is safe for
// concurrent use.
type SSTModule struct {
	mu sync.RWMutex
	// configuration
	minSE          int    // minimum Session-Expires (default 90)
	acceptMinSE    int    // accepted minimum SE
	rejectTooSmall bool   // reject too-small SE instead of clamping
	sstFlag        int    // flag bit used to enable SST
	method         string // refresh method: "INVITE" or "UPDATE"
	// state
	sessions map[string]*Session
}

// Session records a single active session timer.
type Session struct {
	CallID      string
	Expires     int       // Session-Expires value (seconds)
	Refresher   string    // "uac" or "uas"
	Method      string    // refresh method ("INVITE" or "UPDATE")
	StartTime   time.Time
	LastRefresh time.Time
}

// New creates an SSTModule with default configuration and empty state.
func New() *SSTModule {
	return &SSTModule{
		minSE:          DefaultMinSE,
		acceptMinSE:    DefaultMinSE,
		rejectTooSmall: true,
		method:         "INVITE",
		sessions:       make(map[string]*Session),
	}
}

// SetMinSE configures the minimum Session-Expires value.
func (m *SSTModule) SetMinSE(seconds int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.minSE = seconds
	if m.acceptMinSE < seconds {
		m.acceptMinSE = seconds
	}
}

// SetAcceptMinSE configures the accepted minimum SE.
func (m *SSTModule) SetAcceptMinSE(seconds int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acceptMinSE = seconds
}

// SetRejectTooSmall configures whether too-small SE values are rejected.
func (m *SSTModule) SetRejectTooSmall(reject bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rejectTooSmall = reject
}

// SetMethod configures the refresh method ("INVITE" or "UPDATE").
func (m *SSTModule) SetMethod(method string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.method = method
}

// SetSSTFlag configures the flag bit used to enable SST.
func (m *SSTModule) SetSSTFlag(flag int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sstFlag = flag
}

// CheckSessionExpires reports whether expires is at least minSE.
//
//	C: sst_check_min_se()
func (m *SSTModule) CheckSessionExpires(expires int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return expires >= m.minSE
}

// HandleRequest parses the Session-Expires and Min-SE headers of a request,
// validates the negotiated interval and registers/updates the session.
// Returns the negotiated Session-Expires value.
//
//	C: sst_handler_request()
func (m *SSTModule) HandleRequest(msg *parser.SIPMsg) (int, error) {
	if msg == nil {
		return 0, fmt.Errorf("sst: nil message")
	}
	if msg.SessionExpires == nil {
		return 0, fmt.Errorf("sst: no Session-Expires header")
	}
	se, err := parser.ParseSessionExpires(msg.SessionExpires)
	if err != nil {
		return 0, fmt.Errorf("sst: parse Session-Expires: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Honour a Min-SE header if present: the effective minimum is the
	// larger of the configured minSE and the request's Min-SE.
	effectiveMin := m.minSE
	if msg.MinSE != nil {
		if minSE, err := parser.ParseMinSE(msg.MinSE); err == nil && minSE.Seconds > effectiveMin {
			effectiveMin = minSE.Seconds
		}
	}
	if se.Seconds < effectiveMin {
		if m.rejectTooSmall {
			return 0, fmt.Errorf("sst: Session-Expires %d < min %d", se.Seconds, effectiveMin)
		}
		se.Seconds = effectiveMin
	}

	callID := callIDString(msg)
	m.registerSession(callID, se.Seconds, se.Refresher, m.method)
	return se.Seconds, nil
}

// HandleResponse parses the Session-Expires header of a response and
// updates the matching session. Returns the negotiated Session-Expires
// value.
//
//	C: sst_handler_response()
func (m *SSTModule) HandleResponse(msg *parser.SIPMsg) (int, error) {
	if msg == nil {
		return 0, fmt.Errorf("sst: nil message")
	}
	if msg.SessionExpires == nil {
		return 0, fmt.Errorf("sst: no Session-Expires header")
	}
	se, err := parser.ParseSessionExpires(msg.SessionExpires)
	if err != nil {
		return 0, fmt.Errorf("sst: parse Session-Expires: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	callID := callIDString(msg)
	// Preserve the configured refresh method for an existing session.
	method := m.method
	if s, ok := m.sessions[callID]; ok && s.Method != "" {
		method = s.Method
	}
	m.registerSession(callID, se.Seconds, se.Refresher, method)
	return se.Seconds, nil
}

// registerSession inserts or updates the session for callID. The caller
// must hold m.mu.
func (m *SSTModule) registerSession(callID string, expires int, refresher parser.RefresherType, method string) {
	if m.sessions == nil {
		m.sessions = make(map[string]*Session)
	}
	now := time.Now()
	s, ok := m.sessions[callID]
	if !ok {
		s = &Session{CallID: callID, StartTime: now}
		m.sessions[callID] = s
	}
	s.Expires = expires
	s.Refresher = refresherName(refresher)
	s.Method = method
	s.LastRefresh = now
}

// GenerateRefresh builds a session-refresh request (INVITE or UPDATE) for
// the session identified by callID. Returns an error when no such session
// exists.
//
//	C: sst_refresh()
func (m *SSTModule) GenerateRefresh(callID string) (*parser.SIPMsg, error) {
	if callID == "" {
		return nil, fmt.Errorf("sst: empty call-id")
	}
	m.mu.RLock()
	s, ok := m.sessions[callID]
	m.mu.RUnlock()
	if !ok || s == nil {
		return nil, fmt.Errorf("sst: unknown session %q", callID)
	}

	method := s.Method
	if method == "" {
		method = m.method
	}
	methodValue := methodValueOf(method)
	refresher := refresherType(s.Refresher)
	seHeader := parser.BuildSessionExpires(s.Expires, refresher)

	// Build the refresh as a raw SIP message and parse it so all quick
	// references are wired up consistently.
	branch := fmt.Sprintf("z9hG4bK%d", time.Now().UnixNano())
	tag := fmt.Sprintf("sst-%d", time.Now().UnixNano())
	raw := method + " sip:refresh@kamailio-go SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 0.0.0.0:5060;branch=" + branch + "\r\n" +
		"From: <sip:refresh@kamailio-go>;tag=" + tag + "\r\n" +
		"To: <sip:refresh@kamailio-go>\r\n" +
		"Call-ID: " + callID + "\r\n" +
		"CSeq: 1 " + method + "\r\n" +
		"Session-Expires: " + seHeader + "\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n"

	refresh, err := parser.ParseMsg([]byte(raw))
	if err != nil {
		return nil, fmt.Errorf("sst: build refresh: %w", err)
	}
	// Ensure the parsed method value matches the intended method even when
	// the parser did not classify it.
	if refresh.FirstLine != nil && refresh.FirstLine.Req != nil {
		refresh.FirstLine.Req.MethodValue = methodValue
	}
	return refresh, nil
}

// AddSession registers a session. If a session with the same CallID already
// exists it is replaced.
func (m *SSTModule) AddSession(s *Session) {
	if s == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions == nil {
		m.sessions = make(map[string]*Session)
	}
	if s.StartTime.IsZero() {
		s.StartTime = time.Now()
	}
	if s.LastRefresh.IsZero() {
		s.LastRefresh = s.StartTime
	}
	m.sessions[s.CallID] = s
}

// RemoveSession removes the session identified by callID. It is a no-op
// when no such session exists.
func (m *SSTModule) RemoveSession(callID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, callID)
}

// GetSession returns the session for callID, or nil if none is registered.
func (m *SSTModule) GetSession(callID string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[callID]
}

// ListSessions returns a snapshot of all registered sessions.
func (m *SSTModule) ListSessions() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	return out
}

// CleanupExpired removes sessions whose LastRefresh + Expires is in the
// past.
//
//	C: sst_timer_expired()
func (m *SSTModule) CleanupExpired() {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		if s.Expires <= 0 {
			delete(m.sessions, id)
			continue
		}
		if !s.LastRefresh.IsZero() && now.After(s.LastRefresh.Add(time.Duration(s.Expires)*time.Second)) {
			delete(m.sessions, id)
		}
	}
}

// -----------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------

// callIDString returns the Call-ID of a message, or empty string.
func callIDString(msg *parser.SIPMsg) string {
	if msg.CallID != nil {
		return msg.CallID.Body.String()
	}
	return ""
}

// refresherName converts a parser.RefresherType to "uac"/"uas".
func refresherName(rt parser.RefresherType) string {
	switch rt {
	case parser.RefresherUAC:
		return "uac"
	case parser.RefresherUAS:
		return "uas"
	default:
		return ""
	}
}

// refresherType converts "uac"/"uas" to a parser.RefresherType.
func refresherType(name string) parser.RefresherType {
	switch name {
	case "uac":
		return parser.RefresherUAC
	case "uas":
		return parser.RefresherUAS
	default:
		return parser.RefresherUnknown
	}
}

// methodValueOf maps a method name to a parser.RequestMethod.
func methodValueOf(method string) parser.RequestMethod {
	switch method {
	case "INVITE":
		return parser.MethodInvite
	case "UPDATE":
		return parser.MethodUpdate
	default:
		return parser.MethodOther
	}
}

// -----------------------------------------------------------------------
// process-wide singleton (mirrors the C module's global state)
// -----------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultS  *SSTModule
)

// DefaultSST returns the process-wide SSTModule, creating it on first use.
func DefaultSST() *SSTModule {
	defaultMu.RLock()
	m := defaultS
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultS == nil {
		defaultS = New()
	}
	return defaultS
}

// Init (re)initialises the process-wide SSTModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultS = New()
}

// CheckSessionExpires is the package-level wrapper.
func CheckSessionExpires(expires int) bool { return DefaultSST().CheckSessionExpires(expires) }

// HandleRequest is the package-level wrapper.
func HandleRequest(msg *parser.SIPMsg) (int, error) { return DefaultSST().HandleRequest(msg) }

// HandleResponse is the package-level wrapper.
func HandleResponse(msg *parser.SIPMsg) (int, error) { return DefaultSST().HandleResponse(msg) }

// GenerateRefresh is the package-level wrapper.
func GenerateRefresh(callID string) (*parser.SIPMsg, error) { return DefaultSST().GenerateRefresh(callID) }

// AddSession is the package-level wrapper.
func AddSession(s *Session) { DefaultSST().AddSession(s) }

// RemoveSession is the package-level wrapper.
func RemoveSession(callID string) { DefaultSST().RemoveSession(callID) }

// GetSession is the package-level wrapper.
func GetSession(callID string) *Session { return DefaultSST().GetSession(callID) }

// CleanupExpired is the package-level wrapper.
func CleanupExpired() { DefaultSST().CleanupExpired() }
