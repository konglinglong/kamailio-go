// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * IMS OCS module - Online Charging System quota management.
 * Port of the kamailio ims_ocs module (src/modules/ims_ocs).
 *
 * The Online Charging System grants units of service to a subscriber
 * for a given service. The ims_ocs module models a simple quota server:
 * a subscriber requests units for a service, the OCS grants them and
 * tracks usage; when the granted units are exhausted the subscriber may
 * request more. A session is terminated explicitly, after which no
 * further usage is accepted.
 *
 * It is safe for concurrent use.
 */

package ims_ocs

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Session status values.
const (
	StatusActive     = "active"
	StatusExhausted  = "exhausted"
	StatusTerminated = "terminated"
)

// DefaultGrantedUnits is the number of units granted per request when no
// explicit quota policy is configured.
const DefaultGrantedUnits = 1000

// OnlineChargingSession captures the state of a single online charging
// session.
type OnlineChargingSession struct {
	SessionID    string
	Subscriber   string
	ServiceID    string
	UsedUnits    int
	GrantedUnits int
	Status       string
	CreatedAt    time.Time
}

// OCSModule maintains the set of online charging sessions.
type OCSModule struct {
	mu       sync.RWMutex
	sessions map[string]*OnlineChargingSession
	counter  atomic.Uint64
}

// NewOCSModule creates an OCSModule with empty session storage.
func NewOCSModule() *OCSModule {
	return &OCSModule{sessions: make(map[string]*OnlineChargingSession)}
}

// RequestUnits grants requestedUnits to subscriber for serviceID, creating
// a new session if none exists for the subscriber/service pair. When a
// session already exists and is still active, the granted units are added
// to the remaining quota. Returns an error when the subscriber or service
// is empty.
//
//	C: ocs_request_units()
func (m *OCSModule) RequestUnits(subscriber string, serviceID string, requestedUnits int) (*OnlineChargingSession, error) {
	if subscriber == "" {
		return nil, errors.New("ims_ocs: empty subscriber")
	}
	if serviceID == "" {
		return nil, errors.New("ims_ocs: empty service id")
	}
	if requestedUnits < 0 {
		requestedUnits = 0
	}
	key := sessionKey(subscriber, serviceID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions == nil {
		m.sessions = make(map[string]*OnlineChargingSession)
	}
	if s, ok := m.sessions[key]; ok {
		if s.Status == StatusTerminated {
			return nil, fmt.Errorf("ims_ocs: session %s is terminated", s.SessionID)
		}
		s.GrantedUnits += requestedUnits
		if s.Status == StatusExhausted && s.GrantedUnits > s.UsedUnits {
			s.Status = StatusActive
		}
		return s, nil
	}
	n := m.counter.Add(1)
	s := &OnlineChargingSession{
		SessionID:    fmt.Sprintf("ocs-%d", n),
		Subscriber:   subscriber,
		ServiceID:    serviceID,
		UsedUnits:    0,
		GrantedUnits: requestedUnits,
		Status:       StatusActive,
		CreatedAt:    time.Now(),
	}
	m.sessions[key] = s
	return s, nil
}

// UpdateUsage records that usedUnits have been consumed by the session
// identified by sessionID. When usage reaches the granted quota the
// session is marked exhausted. Returns an error when no such session
// exists or the session is terminated.
//
//	C: ocs_update_usage()
func (m *OCSModule) UpdateUsage(sessionID string, usedUnits int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.findBySessionID(sessionID)
	if s == nil {
		return fmt.Errorf("ims_ocs: no session %q", sessionID)
	}
	if s.Status == StatusTerminated {
		return fmt.Errorf("ims_ocs: session %s is terminated", sessionID)
	}
	s.UsedUnits += usedUnits
	if s.UsedUnits >= s.GrantedUnits {
		s.Status = StatusExhausted
	}
	return nil
}

// Terminate finalises the session identified by sessionID, marking it
// terminated so that no further usage is accepted. Returns an error when
// no such session exists.
//
//	C: ocs_terminate_session()
func (m *OCSModule) Terminate(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.findBySessionID(sessionID)
	if s == nil {
		return fmt.Errorf("ims_ocs: no session %q", sessionID)
	}
	s.Status = StatusTerminated
	return nil
}

// GetSession returns the session identified by sessionID, or nil.
func (m *OCSModule) GetSession(sessionID string) *OnlineChargingSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.findBySessionID(sessionID)
}

// Count returns the number of tracked sessions.
func (m *OCSModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// List returns a snapshot of all tracked sessions.
func (m *OCSModule) List() []*OnlineChargingSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*OnlineChargingSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	return out
}

// findBySessionID returns the session with the given SessionID. The
// caller must hold at least a read lock.
func (m *OCSModule) findBySessionID(sessionID string) *OnlineChargingSession {
	for _, s := range m.sessions {
		if s.SessionID == sessionID {
			return s
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

// sessionKey produces a stable key from a subscriber and service id.
func sessionKey(subscriber, serviceID string) string {
	return subscriber + "|" + serviceID
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultOM *OCSModule
)

// DefaultOCS returns the process-wide OCSModule, creating one on first use.
func DefaultOCS() *OCSModule {
	defaultMu.RLock()
	o := defaultOM
	defaultMu.RUnlock()
	if o != nil {
		return o
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultOM == nil {
		defaultOM = NewOCSModule()
	}
	return defaultOM
}

// Init (re)initialises the process-wide OCSModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultOM = NewOCSModule()
}
