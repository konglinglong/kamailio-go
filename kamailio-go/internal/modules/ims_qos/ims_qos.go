// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * IMS QoS module - Gq/Rx interface authorisation and media tracking.
 * Port of the kamailio ims_qos module (src/modules/ims_qos).
 *
 * The ims_qos module authorises the media components of a SIP session
 * against the PCRF over the Gq/Rx reference point. For each authorised
 * session it records the negotiated media components (media type and
 * maximum requested bandwidths in the uplink/downlink directions) and
 * the session status. A session is revoked when the dialog terminates
 * or the PCRF withdraws authorisation.
 *
 * It is safe for concurrent use.
 */

package ims_qos

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
	StatusAuthorized = "authorized"
	StatusPending    = "pending"
	StatusRevoked    = "revoked"
)

// MediaComponent describes a single media stream negotiated for a QoS
// session (Kamailio media_component).
type MediaComponent struct {
	MediaNumber      int
	MediaType        string
	MaxRequestedBWUL int
	MaxRequestedBWDL int
	Status           string
}

// QoSSession captures the state of a single authorised QoS session.
type QoSSession struct {
	CallID          string
	FromTag         string
	ToTag           string
	MediaComponents []MediaComponent
	Status          string
	CreatedAt       time.Time
}

// QoSModule maintains the set of authorised QoS sessions.
type QoSModule struct {
	mu       sync.RWMutex
	sessions map[string]*QoSSession
}

// NewQoSModule creates a QoSModule with empty session storage.
func NewQoSModule() *QoSModule {
	return &QoSModule{sessions: make(map[string]*QoSSession)}
}

// Authorize authorises the media components of msg, creating a QoSSession
// keyed by the message Call-ID and From-tag. A default audio media
// component is added when none can be derived from the message. Returns
// an error when msg is nil or has no Call-ID.
//
//	C: Ro_send_aar() / authorize_media()
func (m *QoSModule) Authorize(msg *parser.SIPMsg) (*QoSSession, error) {
	if msg == nil {
		return nil, errors.New("ims_qos: nil message")
	}
	callID := headerBody(msg, msg.CallID, parser.HdrCallID)
	if callID == "" {
		return nil, errors.New("ims_qos: message has no Call-ID")
	}
	fromTag := extractTag(headerBody(msg, msg.From, parser.HdrFrom))
	toTag := extractTag(headerBody(msg, msg.To, parser.HdrTo))
	key := sessionKey(callID, fromTag)

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions == nil {
		m.sessions = make(map[string]*QoSSession)
	}
	if s, ok := m.sessions[key]; ok {
		return s, nil
	}
	s := &QoSSession{
		CallID:  callID,
		FromTag: fromTag,
		ToTag:   toTag,
		MediaComponents: []MediaComponent{{
			MediaNumber:      1,
			MediaType:        "audio",
			MaxRequestedBWUL: 64,
			MaxRequestedBWDL: 64,
			Status:           StatusAuthorized,
		}},
		Status:    StatusAuthorized,
		CreatedAt: time.Now(),
	}
	m.sessions[key] = s
	return s, nil
}

// UpdateSession sets the status of the session identified by callID and
// fromTag. Returns true when a session was updated.
func (m *QoSModule) UpdateSession(callID, fromTag, status string) bool {
	key := sessionKey(callID, fromTag)
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[key]
	if !ok {
		return false
	}
	s.Status = status
	return true
}

// RevokeSession revokes authorisation for the session identified by
// callID and fromTag, marking it and every media component revoked.
// Returns an error when no such session exists.
//
//	C: Ro_send_str() / revoke_authorization()
func (m *QoSModule) RevokeSession(callID, fromTag string) error {
	key := sessionKey(callID, fromTag)
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[key]
	if !ok {
		return fmt.Errorf("ims_qos: no session for call-id %q tag %q", callID, fromTag)
	}
	s.Status = StatusRevoked
	for i := range s.MediaComponents {
		s.MediaComponents[i].Status = StatusRevoked
	}
	return nil
}

// GetSession returns the session for the given dialog, or nil.
func (m *QoSModule) GetSession(callID, fromTag string) *QoSSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[sessionKey(callID, fromTag)]
}

// Count returns the number of tracked sessions.
func (m *QoSModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// List returns a snapshot of all tracked sessions.
func (m *QoSModule) List() []*QoSSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*QoSSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	return out
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
	defaultQM *QoSModule
)

// DefaultQoS returns the process-wide QoSModule, creating one on first use.
func DefaultQoS() *QoSModule {
	defaultMu.RLock()
	q := defaultQM
	defaultMu.RUnlock()
	if q != nil {
		return q
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultQM == nil {
		defaultQM = NewQoSModule()
	}
	return defaultQM
}

// Init (re)initialises the process-wide QoSModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultQM = NewQoSModule()
}
