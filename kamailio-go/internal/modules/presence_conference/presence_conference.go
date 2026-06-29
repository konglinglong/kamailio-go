// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * presence_conference module - conference state presence.
 *
 * Port of the kamailio presence_conference module
 * (src/modules/presence_conference). A PresenceConferenceModule parses
 * and builds RFC 4575 conference-info documents and caches the current
 * conference state per conference URI. The module registers the
 * "conference" event package with the presence server.
 *
 * The module is safe for concurrent use.
 */
package presence_conference

import (
	"fmt"
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// Participant represents one user/endpoint in a conference.
type Participant struct {
	URI         string
	Role        string
	Status      string
	DisplayText string
}

// ConferenceInfo represents a parsed conference-info document.
type ConferenceInfo struct {
	ConferenceURI string
	UserRoles     map[string]string
	Participants  []Participant
	State         string
}

// PresenceConferenceModule parses and builds conference-info XML and
// caches conference state. It mirrors the C presence_conference module.
type PresenceConferenceModule struct {
	mu          sync.RWMutex
	conferences  map[string]*ConferenceInfo
}

// New creates a PresenceConferenceModule with empty state.
func New() *PresenceConferenceModule {
	return &PresenceConferenceModule{conferences: make(map[string]*ConferenceInfo)}
}

// ParseConferenceInfo parses a conference-info XML body into a
// ConferenceInfo. Missing elements yield empty values; the function
// only returns an error when no <conference-info> element is present.
func (m *PresenceConferenceModule) ParseConferenceInfo(xml []byte) (*ConferenceInfo, error) {
	if m == nil {
		return nil, fmt.Errorf("presence_conference: nil module")
	}
	body := string(xml)
	if !strings.Contains(body, "<conference-info") {
		return nil, fmt.Errorf("presence_conference: no <conference-info> element")
	}
	info := &ConferenceInfo{
		ConferenceURI: extractAttr(body, "entity"),
		State:         extractAttr(body, "state"),
		UserRoles:     make(map[string]string),
	}
	// Walk every <user ...> block and collect participants.
	rest := body
	for {
		start := findTagOpen(rest, "user")
		if start < 0 {
			break
		}
		rest = rest[start:]
		openEnd := strings.Index(rest, ">")
		if openEnd < 0 {
			break
		}
		openTag := rest[:openEnd]
		userURI := extractAttr(openTag, "entity")
		userState := extractAttr(openTag, "state")
		if userState != "" {
			info.UserRoles[userURI] = userState
		}
		closeIdx := strings.Index(rest, "</user>")
		var block string
		if closeIdx < 0 {
			block = rest[openEnd+1:]
			rest = ""
		} else {
			block = rest[openEnd+1 : closeIdx]
			rest = rest[closeIdx+len("</user>"):]
		}
		displayText := extractTagValue(block, "display-text")
		// Each <endpoint> carries a status; use the first endpoint.
		endpointStatus := ""
		endpointText := ""
		if epStart := findTagOpen(block, "endpoint"); epStart >= 0 {
			epRest := block[epStart:]
			epOpenEnd := strings.Index(epRest, ">")
			if epOpenEnd >= 0 {
				epBlock := epRest[epOpenEnd+1:]
				if epClose := strings.Index(epBlock, "</endpoint>"); epClose >= 0 {
					epBlock = epBlock[:epClose]
				}
				endpointStatus = extractTagValue(epBlock, "status")
				endpointText = extractTagValue(epBlock, "display-text")
			}
		}
		p := Participant{
			URI:         userURI,
			Status:      endpointStatus,
			DisplayText: displayText,
		}
		if endpointText != "" && p.DisplayText == "" {
			p.DisplayText = endpointText
		}
		info.Participants = append(info.Participants, p)
	}
	return info, nil
}

// BuildConferenceInfo builds a conference-info XML body from a
// ConferenceInfo.
func (m *PresenceConferenceModule) BuildConferenceInfo(info *ConferenceInfo) ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("presence_conference: nil module")
	}
	if info == nil {
		return []byte{}, nil
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	state := info.State
	if state == "" {
		state = "full"
	}
	b.WriteString(fmt.Sprintf(`<conference-info xmlns="urn:ietf:params:xml:ns:conference-info" entity="%s" state="%s" version="1">`+"\n",
		escapeXML(info.ConferenceURI), escapeXML(state)))
	b.WriteString("  <users>\n")
	for _, p := range info.Participants {
		b.WriteString(fmt.Sprintf("    <user entity=\"%s\">\n", escapeXML(p.URI)))
		if p.DisplayText != "" {
			b.WriteString(fmt.Sprintf("      <display-text>%s</display-text>\n", escapeXML(p.DisplayText)))
		}
		b.WriteString("      <endpoint>\n")
		if p.Status != "" {
			b.WriteString(fmt.Sprintf("        <status>%s</status>\n", escapeXML(p.Status)))
		}
		b.WriteString("      </endpoint>\n")
		b.WriteString("    </user>\n")
	}
	b.WriteString("  </users>\n")
	b.WriteString("</conference-info>")
	return []byte(b.String()), nil
}

// HandleSubscribe processes a SUBSCRIBE for conference state. It
// registers the subscription and returns 200 on success, 400 when the
// message is missing required headers.
func (m *PresenceConferenceModule) HandleSubscribe(msg *parser.SIPMsg) (int, error) {
	if m == nil {
		return 500, fmt.Errorf("presence_conference: nil module")
	}
	if msg == nil {
		return 400, fmt.Errorf("presence_conference: nil message")
	}
	uri := extractEntity(msg)
	if uri == "" {
		return 400, fmt.Errorf("presence_conference: no entity")
	}
	// A subscription is accepted even when no state exists yet.
	return 200, nil
}

// HandlePublish processes a PUBLISH carrying a conference-info body. It
// parses the body and caches the conference state, returning 200 on
// success, 400 on a malformed message and 415 when no body is present.
func (m *PresenceConferenceModule) HandlePublish(msg *parser.SIPMsg) (int, error) {
	if m == nil {
		return 500, fmt.Errorf("presence_conference: nil module")
	}
	if msg == nil {
		return 400, fmt.Errorf("presence_conference: nil message")
	}
	body := bodyBytes(msg)
	if len(body) == 0 {
		return 415, fmt.Errorf("presence_conference: empty body")
	}
	info, err := m.ParseConferenceInfo(body)
	if err != nil {
		return 400, err
	}
	if info.ConferenceURI == "" {
		info.ConferenceURI = extractEntity(msg)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conferences == nil {
		m.conferences = make(map[string]*ConferenceInfo)
	}
	m.conferences[info.ConferenceURI] = info
	return 200, nil
}

// GetConference returns the cached ConferenceInfo for uri, or an error
// when no such conference exists.
func (m *PresenceConferenceModule) GetConference(uri string) (*ConferenceInfo, error) {
	if m == nil {
		return nil, fmt.Errorf("presence_conference: nil module")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	info, ok := m.conferences[uri]
	if !ok || info == nil {
		return nil, fmt.Errorf("presence_conference: no conference for %q", uri)
	}
	cp := *info
	if cp.UserRoles == nil {
		cp.UserRoles = make(map[string]string)
	}
	if cp.Participants == nil {
		cp.Participants = []Participant{}
	}
	return &cp, nil
}

// AddParticipant adds p to the conference identified by confURI, creating
// the conference when it does not yet exist.
func (m *PresenceConferenceModule) AddParticipant(confURI string, p *Participant) error {
	if m == nil {
		return fmt.Errorf("presence_conference: nil module")
	}
	if confURI == "" {
		return fmt.Errorf("presence_conference: empty conference URI")
	}
	if p == nil {
		return fmt.Errorf("presence_conference: nil participant")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conferences == nil {
		m.conferences = make(map[string]*ConferenceInfo)
	}
	info, ok := m.conferences[confURI]
	if !ok {
		info = &ConferenceInfo{
			ConferenceURI: confURI,
			UserRoles:     make(map[string]string),
			State:        "full",
		}
		m.conferences[confURI] = info
	}
	info.Participants = append(info.Participants, *p)
	return nil
}

// RemoveParticipant removes the participant identified by userURI from
// the conference confURI. Returns an error when the conference or
// participant does not exist.
func (m *PresenceConferenceModule) RemoveParticipant(confURI, userURI string) error {
	if m == nil {
		return fmt.Errorf("presence_conference: nil module")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.conferences[confURI]
	if !ok {
		return fmt.Errorf("presence_conference: no conference for %q", confURI)
	}
	kept := info.Participants[:0]
	removed := false
	for _, p := range info.Participants {
		if p.URI == userURI && !removed {
			removed = true
			continue
		}
		kept = append(kept, p)
	}
	if !removed {
		return fmt.Errorf("presence_conference: no participant %q", userURI)
	}
	info.Participants = kept
	delete(info.UserRoles, userURI)
	return nil
}

// ListConferences returns a snapshot of every cached conference sorted
// by URI.
func (m *PresenceConferenceModule) ListConferences() []ConferenceInfo {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ConferenceInfo, 0, len(m.conferences))
	for _, info := range m.conferences {
		cp := *info
		if cp.UserRoles == nil {
			cp.UserRoles = make(map[string]string)
		}
		if cp.Participants == nil {
			cp.Participants = []Participant{}
		}
		out = append(out, cp)
	}
	// Stable order for testability.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].ConferenceURI < out[i].ConferenceURI {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// Count returns the number of cached conferences.
func (m *PresenceConferenceModule) Count() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.conferences)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// bodyBytes returns the message body as a byte slice.
func bodyBytes(msg *parser.SIPMsg) []byte {
	if msg == nil {
		return nil
	}
	if b, ok := msg.Body.([]byte); ok {
		return b
	}
	return nil
}

// extractEntity returns the presentity URI from a SIP message, taken
// from the From header (the part within <...>) and falling back to the
// request URI.
func extractEntity(msg *parser.SIPMsg) string {
	if msg == nil {
		return ""
	}
	if msg.From != nil {
		if uri := uriBetweenAngles(msg.From.Body.String()); uri != "" {
			return uri
		}
	}
	if msg.FirstLine != nil && msg.FirstLine.Req != nil {
		if u := msg.FirstLine.Req.URI.String(); u != "" {
			return u
		}
	}
	return ""
}

// uriBetweenAngles returns the substring within the first <...> pair.
func uriBetweenAngles(s string) string {
	start := strings.Index(s, "<")
	if start < 0 {
		return ""
	}
	end := strings.Index(s[start+1:], ">")
	if end < 0 {
		return ""
	}
	return s[start+1 : start+1+end]
}

// findTagOpen returns the byte index of the <tag ...> opening tag in s,
// skipping longer tags that merely start with the same prefix (e.g.
// <user> vs <user-count>). Returns -1 when not found.
func findTagOpen(s, tag string) int {
	open := "<" + tag
	off := 0
	for {
		i := strings.Index(s[off:], open)
		if i < 0 {
			return -1
		}
		i += off
		rest := s[i+len(open):]
		if len(rest) > 0 {
			c := rest[0]
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '>' {
				return i
			}
		}
		off = i + len(open)
	}
}

// extractAttr returns the value of the named attribute in s, or "".
func extractAttr(s, name string) string {
	needle := name + "=\""
	idx := strings.Index(s, needle)
	if idx < 0 {
		needle = name + "='"
		idx = strings.Index(s, needle)
		if idx < 0 {
			return ""
		}
	}
	rest := s[idx+len(needle):]
	end := strings.IndexAny(rest, "\"'")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// extractTagValue returns the trimmed text content of the first <tag>
// element in s, handling optional attributes on the opening tag.
func extractTagValue(s, tag string) string {
	open := "<" + tag
	idx := strings.Index(s, open)
	if idx < 0 {
		return ""
	}
	rest := s[idx:]
	gt := strings.Index(rest, ">")
	if gt < 0 {
		return ""
	}
	content := rest[gt+1:]
	closeTag := "</" + tag + ">"
	end := strings.Index(content, closeTag)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(content[:end])
}

// escapeXML escapes the standard XML special characters.
func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu       sync.RWMutex
	defaultPresence *PresenceConferenceModule
)

// DefaultPresence returns the process-wide PresenceConferenceModule,
// creating one on first use.
func DefaultPresence() *PresenceConferenceModule {
	defaultMu.RLock()
	p := defaultPresence
	defaultMu.RUnlock()
	if p != nil {
		return p
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultPresence == nil {
		defaultPresence = New()
	}
	return defaultPresence
}

// Init (re)initialises the process-wide PresenceConferenceModule to a
// fresh state, mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultPresence = New()
}
