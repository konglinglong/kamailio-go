// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * IMS Dialog module - dialog state tracking for the IMS core.
 * Port of the kamailio ims_dialog module (src/modules/ims_dialog).
 *
 * The ims_dialog module tracks SIP dialogs as they are established by
 * the IMS core. Each dialog is identified by its Call-ID and From-tag
 * and progresses through the states early -> confirmed -> terminated.
 * The module records the From/To URIs, the Request-URI at dialog
 * creation, the route set, the Contact and the direction (originating
 * / terminating). Expired dialogs are reclaimed by CleanupExpired.
 *
 * It is safe for concurrent use.
 */

package ims_dialog

import (
	"strings"
	"sync"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// Dialog states.
const (
	StateEarly      = "early"
	StateConfirmed  = "confirmed"
	StateTerminated = "terminated"
)

// Dialog directions.
const (
	DirOriginating = "originating"
	DirTerminating = "terminating"
)

// DefaultTTL is the default lifetime of an idle dialog, after which
// CleanupExpired will reclaim it.
const DefaultTTL = 2 * time.Hour

// IMSDialog captures the state of a single SIP dialog.
type IMSDialog struct {
	CallID    string
	FromTag   string
	ToTag     string
	FromURI   string
	ToURI     string
	RURI      string
	Direction string
	State     string
	CreatedAt time.Time
	UpdatedAt time.Time
	RouteSet  []string
	Contact   string
}

// IMSDialogModule maintains the set of tracked dialogs.
type IMSDialogModule struct {
	mu      sync.RWMutex
	dialogs map[string]*IMSDialog
}

// NewIMSDialogModule creates an IMSDialogModule with empty storage.
func NewIMSDialogModule() *IMSDialogModule {
	return &IMSDialogModule{dialogs: make(map[string]*IMSDialog)}
}

// Create records a new dialog from msg, keyed by the message Call-ID and
// From-tag. If a dialog already exists for that key it is returned
// unchanged. Returns nil when msg is nil or has no Call-ID.
//
//	C: dlg_create_dialog()
func (m *IMSDialogModule) Create(msg *parser.SIPMsg, direction string) *IMSDialog {
	if msg == nil {
		return nil
	}
	callID := headerBody(msg, msg.CallID, parser.HdrCallID)
	if callID == "" {
		return nil
	}
	fromBody := headerBody(msg, msg.From, parser.HdrFrom)
	toBody := headerBody(msg, msg.To, parser.HdrTo)
	fromTag := extractTag(fromBody)
	toTag := extractTag(toBody)
	ruri := requestURI(msg)
	contact := headerBody(msg, msg.Contact, parser.HdrContact)
	routeSet := collectRoutes(msg)
	key := dialogKey(callID, fromTag)

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dialogs == nil {
		m.dialogs = make(map[string]*IMSDialog)
	}
	if d, ok := m.dialogs[key]; ok {
		return d
	}
	if direction == "" {
		direction = DirOriginating
	}
	now := time.Now()
	d := &IMSDialog{
		CallID:    callID,
		FromTag:   fromTag,
		ToTag:     toTag,
		FromURI:   fromBody,
		ToURI:     toBody,
		RURI:      ruri,
		Direction: direction,
		State:     StateEarly,
		CreatedAt: now,
		UpdatedAt: now,
		RouteSet:  routeSet,
		Contact:   contact,
	}
	m.dialogs[key] = d
	return d
}

// Get returns the dialog for the given Call-ID and From-tag, or nil.
func (m *IMSDialogModule) Get(callID, fromTag string) *IMSDialog {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.dialogs[dialogKey(callID, fromTag)]
}

// Update sets the state of the dialog identified by callID and fromTag.
// Returns true when a dialog was updated.
func (m *IMSDialogModule) Update(callID, fromTag, state string) bool {
	key := dialogKey(callID, fromTag)
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.dialogs[key]
	if !ok {
		return false
	}
	d.State = state
	d.UpdatedAt = time.Now()
	return true
}

// Delete removes the dialog identified by callID and fromTag. Returns
// true when a dialog was removed.
func (m *IMSDialogModule) Delete(callID, fromTag string) bool {
	key := dialogKey(callID, fromTag)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.dialogs[key]; !ok {
		return false
	}
	delete(m.dialogs, key)
	return true
}

// Count returns the number of tracked dialogs.
func (m *IMSDialogModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.dialogs)
}

// CountByState returns the number of dialogs matching state.
func (m *IMSDialogModule) CountByState(state string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, d := range m.dialogs {
		if d.State == state {
			count++
		}
	}
	return count
}

// List returns a snapshot of all tracked dialogs.
func (m *IMSDialogModule) List() []*IMSDialog {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*IMSDialog, 0, len(m.dialogs))
	for _, d := range m.dialogs {
		out = append(out, d)
	}
	return out
}

// CleanupExpired removes dialogs whose last update is older than ttl.
func (m *IMSDialogModule) CleanupExpired(ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for key, d := range m.dialogs {
		if now.Sub(d.UpdatedAt) > ttl {
			delete(m.dialogs, key)
		}
	}
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

// dialogKey produces a stable key from a Call-ID and From-tag.
func dialogKey(callID, fromTag string) string {
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

// requestURI returns the request URI string from msg's request line.
func requestURI(msg *parser.SIPMsg) string {
	if msg == nil || msg.FirstLine == nil || msg.FirstLine.Req == nil {
		return ""
	}
	return msg.FirstLine.Req.URI.String()
}

// collectRoutes returns the bodies of all Route headers in msg.
func collectRoutes(msg *parser.SIPMsg) []string {
	if msg == nil {
		return nil
	}
	routes := msg.GetAllHeadersByType(parser.HdrRoute)
	out := make([]string, 0, len(routes))
	for _, h := range routes {
		if b := strings.TrimSpace(h.Body.String()); b != "" {
			out = append(out, b)
		}
	}
	return out
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
	defaultDM *IMSDialogModule
)

// DefaultIMSDialog returns the process-wide IMSDialogModule, creating one
// on first use.
func DefaultIMSDialog() *IMSDialogModule {
	defaultMu.RLock()
	d := defaultDM
	defaultMu.RUnlock()
	if d != nil {
		return d
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultDM == nil {
		defaultDM = NewIMSDialogModule()
	}
	return defaultDM
}

// Init (re)initialises the process-wide IMSDialogModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultDM = NewIMSDialogModule()
}
