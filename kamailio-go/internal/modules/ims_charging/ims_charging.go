// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * IMS Charging module - Ro interface online charging session tracking.
 * Port of the kamailio ims_charging module (src/modules/ims_charging).
 *
 * The ims_charging module implements the Ro reference point for online
 * charging: it creates a charging session when media is established for
 * a call, tracks the session by Call-ID / From-tag, and finalises it
 * when the dialog terminates. Each session records the subscriber, the
 * direction (originating / terminating) and the charging identifier
 * returned by the OCS.
 *
 * It is safe for concurrent use.
 */

package ims_charging

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// Charging session directions.
const (
	DirectionMO = "MO" // mobile originating
	DirectionMT = "MT" // mobile terminating
)

// Session status values.
const (
	StatusActive     = "active"
	StatusTerminated = "terminated"
	StatusPending    = "pending"
)

// DefaultTTL is the default lifetime of an idle charging session, after
// which CleanupExpired will reclaim it.
const DefaultTTL = 2 * time.Hour

// ChargingSession captures the state of a single Ro charging session.
type ChargingSession struct {
	ID         string
	CallID     string
	FromTag    string
	ToTag      string
	Subscriber string
	Direction  string
	Status     string
	StartedAt  time.Time
	EndedAt    time.Time
	ChargingID string

	// updatedAt tracks the last modification time, used by CleanupExpired.
	updatedAt time.Time
}

// ChargingModule maintains the set of active charging sessions.
type ChargingModule struct {
	mu       sync.RWMutex
	sessions map[string]*ChargingSession
	counter  atomic.Uint64
}

// NewChargingModule creates a ChargingModule with empty session storage.
func NewChargingModule() *ChargingModule {
	return &ChargingModule{sessions: make(map[string]*ChargingSession)}
}

// StartSession creates a new charging session for msg. The session is
// keyed by the message Call-ID and From-tag. If a session already exists
// for that key it is returned unchanged. Returns nil when msg is nil or
// has no Call-ID.
//
//	C: Ro_send_ccr() / create_charging_session()
func (m *ChargingModule) StartSession(msg *parser.SIPMsg, subscriber string, direction string) *ChargingSession {
	if msg == nil {
		return nil
	}
	callID := headerBody(msg, msg.CallID, parser.HdrCallID)
	if callID == "" {
		return nil
	}
	fromTag := extractTag(headerBody(msg, msg.From, parser.HdrFrom))
	toTag := extractTag(headerBody(msg, msg.To, parser.HdrTo))
	key := sessionKey(callID, fromTag)

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions == nil {
		m.sessions = make(map[string]*ChargingSession)
	}
	if s, ok := m.sessions[key]; ok {
		return s
	}
	if direction == "" {
		direction = DirectionMO
	}
	n := m.counter.Add(1)
	s := &ChargingSession{
		ID:         fmt.Sprintf("chg-%d", n),
		CallID:     callID,
		FromTag:    fromTag,
		ToTag:      toTag,
		Subscriber: subscriber,
		Direction:  direction,
		Status:     StatusActive,
		StartedAt:  time.Now(),
		updatedAt:  time.Now(),
		ChargingID: fmt.Sprintf("cid-%d", n),
	}
	m.sessions[key] = s
	return s
}

// EndSession finalises the session identified by callID and fromTag,
// recording the termination time and marking the status terminated.
// Returns an error when no such session exists.
//
//	C: Ro_ccr_terminate()
func (m *ChargingModule) EndSession(callID, fromTag string) error {
	key := sessionKey(callID, fromTag)
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[key]
	if !ok {
		return fmt.Errorf("ims_charging: no session for call-id %q tag %q", callID, fromTag)
	}
	s.Status = StatusTerminated
	s.EndedAt = time.Now()
	s.updatedAt = s.EndedAt
	return nil
}

// GetSession returns the session for the given dialog, or nil.
func (m *ChargingModule) GetSession(callID, fromTag string) *ChargingSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[sessionKey(callID, fromTag)]
}

// UpdateSession sets the status of the session identified by callID and
// fromTag. Returns true when a session was updated.
func (m *ChargingModule) UpdateSession(callID, fromTag, status string) bool {
	key := sessionKey(callID, fromTag)
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[key]
	if !ok {
		return false
	}
	s.Status = status
	s.updatedAt = time.Now()
	return true
}

// Count returns the number of tracked sessions.
func (m *ChargingModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// CountByDirection returns the number of sessions matching direction.
func (m *ChargingModule) CountByDirection(direction string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, s := range m.sessions {
		if s.Direction == direction {
			count++
		}
	}
	return count
}

// List returns a snapshot of all tracked sessions.
func (m *ChargingModule) List() []*ChargingSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*ChargingSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	return out
}

// CleanupExpired removes sessions whose last update is older than ttl.
func (m *ChargingModule) CleanupExpired(ttl time.Duration) {
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
// internal helpers
// ---------------------------------------------------------------------------

// sessionKey produces a stable key from a Call-ID and From-tag.
func sessionKey(callID, fromTag string) string {
	return callID + "|" + fromTag
}

// headerBody returns the body string of a header, looking it up by quick
// reference first, then by type.
func headerBody(msg *parser.SIPMsg, quick *parser.HdrField, ht parser.HdrType) string {
	if quick != nil {
		return quick.Body.String()
	}
	if msg != nil {
		if h := msg.GetHeaderByType(ht); h != nil {
			return h.Body.String()
		}
	}
	return ""
}

// extractTag scans a From/To header body and returns the value of the
// "tag" parameter, or the empty string if not present.
func extractTag(body string) string {
	if body == "" {
		return ""
	}
	lower := strings.ToLower(body)
	idx := strings.Index(lower, ";tag=")
	if idx < 0 {
		if strings.HasPrefix(lower, "tag=") {
			rest := body[4:]
			if semi := strings.IndexByte(rest, ';'); semi >= 0 {
				return strings.TrimSpace(rest[:semi])
			}
			return strings.TrimSpace(rest)
		}
		return ""
	}
	rest := body[idx+len(";tag="):]
	if semi := strings.IndexByte(rest, ';'); semi >= 0 {
		return strings.TrimSpace(rest[:semi])
	}
	return strings.TrimSpace(rest)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultCM *ChargingModule
)

// DefaultCharging returns the process-wide ChargingModule, creating one
// on first use.
func DefaultCharging() *ChargingModule {
	defaultMu.RLock()
	c := defaultCM
	defaultMu.RUnlock()
	if c != nil {
		return c
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultCM == nil {
		defaultCM = NewChargingModule()
	}
	return defaultCM
}

// Init (re)initialises the process-wide ChargingModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultCM = NewChargingModule()
}
