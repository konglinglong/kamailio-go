// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * IMS QoS NPN module - QoS handling for non-3GPP access (WiFi/WiMAX/LTE).
 * Port of the kamailio ims_qos_npn module (src/modules/ims_qos_npn).
 *
 * The ims_qos_npn module authorises and tracks media components for
 * sessions that arrive over a non-3GPP access network (e.g. untrusted
 * WiFi). Unlike the 3GPP ims_qos module, the PCRF interaction is
 * mediated by an AAA/auth server and the bandwidth limits are derived
 * from the access type rather than the radio bearer. Each session is
 * keyed by Call-ID and carries a set of media components with their
 * negotiated bandwidth and flow status.
 *
 * It is safe for concurrent use.
 */

package ims_qos_npn

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// Session status values.
const (
	StatusPending    = "pending"
	StatusAuthorized = "authorized"
	StatusActive     = "active"
	StatusTerminated = "terminated"
)

// Media flow status values.
const (
	FlowEnabled  = "enabled"
	FlowDisabled = "disabled"
	FlowRemoved  = "removed"
)

// Supported non-3GPP access types.
const (
	NpnTypeWifi  = "wifi"
	NpnTypeWimax = "wimax"
	NpnTypeLTE   = "lte"
)

// DefaultTTL is the default lifetime of an idle QoS session, after
// which CleanupExpired will reclaim it.
const DefaultTTL = 2 * time.Hour

// Config holds the non-3GPP QoS configuration.
type Config struct {
	DefaultBandwidth int  // kbps
	MaxBandwidth     int
	DefaultPriority  int
	AuthServerAddr   string
	NpnType          string // "wifi", "wimax", "lte"
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() Config {
	return Config{
		DefaultBandwidth: 64,
		MaxBandwidth:     10000,
		DefaultPriority:  0,
		AuthServerAddr:   "127.0.0.1:3868",
		NpnType:          NpnTypeWifi,
	}
}

// MediaComponent describes a single media stream negotiated for a QoS
// session over a non-3GPP access.
type MediaComponent struct {
	CompID     int
	MediaDesc  string
	Bandwidth  int // kbps
	Priority   int
	FlowStatus string // "enabled", "disabled", "removed"
}

// QoSSession captures the state of a single non-3GPP QoS session.
type QoSSession struct {
	CallID          string
	UserID          string
	MediaComponents map[int]*MediaComponent // comp ID -> component
	NpnType         string
	Status          string // "pending", "authorized", "active", "terminated"
	CreatedAt       time.Time
	updatedAt       time.Time
}

// IMSQoSNpnModule maintains the set of non-3GPP QoS sessions.
type IMSQoSNpnModule struct {
	mu       sync.RWMutex
	config   Config
	sessions map[string]*QoSSession
}

// NewIMSQoSNpnModule creates an IMSQoSNpnModule with the default
// configuration and empty session storage.
func NewIMSQoSNpnModule() *IMSQoSNpnModule {
	return &IMSQoSNpnModule{
		config:   DefaultConfig(),
		sessions: make(map[string]*QoSSession),
	}
}

// NewIMSQoSNpnModuleWithConfig creates an IMSQoSNpnModule with the
// supplied configuration.
func NewIMSQoSNpnModuleWithConfig(cfg Config) *IMSQoSNpnModule {
	m := NewIMSQoSNpnModule()
	m.config = cfg
	return m
}

// SetConfig replaces the module configuration.
func (m *IMSQoSNpnModule) SetConfig(cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = cfg
}

// GetConfig returns a copy of the current configuration.
func (m *IMSQoSNpnModule) GetConfig() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// HandleRequest processes a SIP message carrying media for a non-3GPP
// access. It creates a pending session keyed by the message Call-ID when
// none exists and seeds a default media component. Returns the session
// status code (1 = created/active, 2 = updated existing) and an error
// when msg is nil or has no Call-ID.
//
//	C: rx_process_aar() / npn_handle_request()
func (m *IMSQoSNpnModule) HandleRequest(msg *parser.SIPMsg) (int, error) {
	if msg == nil {
		return 0, errors.New("ims_qos_npn: nil message")
	}
	callID := headerBody(msg, msg.CallID, parser.HdrCallID)
	if callID == "" {
		return 0, errors.New("ims_qos_npn: message has no Call-ID")
	}
	userID := extractUser(headerBody(msg, msg.From, parser.HdrFrom))

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions == nil {
		m.sessions = make(map[string]*QoSSession)
	}
	if s, ok := m.sessions[callID]; ok {
		s.updatedAt = time.Now()
		if s.Status == StatusPending {
			s.Status = StatusActive
		}
		return 2, nil
	}
	npnType := m.config.NpnType
	bw := m.config.DefaultBandwidth
	prio := m.config.DefaultPriority

	now := time.Now()
	s := &QoSSession{
		CallID:          callID,
		UserID:          userID,
		MediaComponents: map[int]*MediaComponent{
			1: {
				CompID:     1,
				MediaDesc:  "audio",
				Bandwidth:  bw,
				Priority:   prio,
				FlowStatus: FlowEnabled,
			},
		},
		NpnType:   npnType,
		Status:    StatusActive,
		CreatedAt:  now,
		updatedAt: now,
	}
	m.sessions[callID] = s
	return 1, nil
}

// AuthorizeSession marks the session identified by callID as authorised
// for the given user. Returns an error when no such session exists.
//
//	C: npn_authorize_session()
func (m *IMSQoSNpnModule) AuthorizeSession(callID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[callID]
	if !ok {
		return fmt.Errorf("ims_qos_npn: no session for call-id %q", callID)
	}
	s.UserID = userID
	s.Status = StatusAuthorized
	s.updatedAt = time.Now()
	return nil
}

// AddMediaComponent adds or replaces a media component on the session
// identified by callID. Returns an error when no such session exists.
func (m *IMSQoSNpnModule) AddMediaComponent(callID string, comp *MediaComponent) error {
	if comp == nil {
		return errors.New("ims_qos_npn: nil media component")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[callID]
	if !ok {
		return fmt.Errorf("ims_qos_npn: no session for call-id %q", callID)
	}
	if s.MediaComponents == nil {
		s.MediaComponents = make(map[int]*MediaComponent)
	}
	if comp.FlowStatus == "" {
		comp.FlowStatus = FlowEnabled
	}
	if comp.Bandwidth <= 0 {
		comp.Bandwidth = m.config.DefaultBandwidth
	}
	s.MediaComponents[comp.CompID] = comp
	s.updatedAt = time.Now()
	return nil
}

// RemoveMediaComponent removes the media component identified by compID
// from the session identified by callID. Returns an error when no such
// session or component exists.
func (m *IMSQoSNpnModule) RemoveMediaComponent(callID string, compID int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[callID]
	if !ok {
		return fmt.Errorf("ims_qos_npn: no session for call-id %q", callID)
	}
	if _, ok := s.MediaComponents[compID]; !ok {
		return fmt.Errorf("ims_qos_npn: no media component %d for call-id %q", compID, callID)
	}
	delete(s.MediaComponents, compID)
	s.updatedAt = time.Now()
	return nil
}

// UpdateBandwidth sets the bandwidth of the media component identified
// by compID on the session identified by callID. Bandwidths exceeding
// the configured maximum are clamped. Returns an error when no such
// session or component exists.
func (m *IMSQoSNpnModule) UpdateBandwidth(callID string, compID int, bandwidth int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[callID]
	if !ok {
		return fmt.Errorf("ims_qos_npn: no session for call-id %q", callID)
	}
	comp, ok := s.MediaComponents[compID]
	if !ok {
		return fmt.Errorf("ims_qos_npn: no media component %d for call-id %q", compID, callID)
	}
	if m.config.MaxBandwidth > 0 && bandwidth > m.config.MaxBandwidth {
		bandwidth = m.config.MaxBandwidth
	}
	if bandwidth < 0 {
		bandwidth = 0
	}
	comp.Bandwidth = bandwidth
	s.updatedAt = time.Now()
	return nil
}

// GetSession returns the session identified by callID, or nil.
func (m *IMSQoSNpnModule) GetSession(callID string) *QoSSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[callID]
}

// RemoveSession removes the session identified by callID.
func (m *IMSQoSNpnModule) RemoveSession(callID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, callID)
}

// Count returns the number of tracked sessions.
func (m *IMSQoSNpnModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// ListSessions returns a snapshot of all tracked sessions.
func (m *IMSQoSNpnModule) ListSessions() []*QoSSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*QoSSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	return out
}

// CleanupExpired removes sessions whose last update is older than ttl.
func (m *IMSQoSNpnModule) CleanupExpired() {
	m.CleanupExpiredTTL(DefaultTTL)
}

// CleanupExpiredTTL removes sessions whose last update is older than ttl.
func (m *IMSQoSNpnModule) CleanupExpiredTTL(ttl time.Duration) {
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

// extractUser returns the user portion of a From/To header URI.
func extractUser(body string) string {
	if body == "" {
		return ""
	}
	inner := body
	if idx := strings.Index(inner, "<"); idx >= 0 {
		end := strings.Index(inner[idx:], ">")
		if end >= 0 {
			inner = inner[idx+1 : idx+end]
		} else {
			inner = inner[idx+1:]
		}
	}
	if idx := strings.Index(inner, ":"); idx >= 0 {
		inner = inner[idx+1:]
	}
	atIdx := strings.Index(inner, "@")
	if atIdx < 0 {
		return ""
	}
	return strings.TrimSpace(inner[:atIdx])
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultNM *IMSQoSNpnModule
)

// DefaultQoSNpn returns the process-wide IMSQoSNpnModule, creating one
// on first use.
func DefaultQoSNpn() *IMSQoSNpnModule {
	defaultMu.RLock()
	n := defaultNM
	defaultMu.RUnlock()
	if n != nil {
		return n
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultNM == nil {
		defaultNM = NewIMSQoSNpnModule()
	}
	return defaultNM
}

// Init (re)initialises the process-wide IMSQoSNpnModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultNM = NewIMSQoSNpnModule()
}
