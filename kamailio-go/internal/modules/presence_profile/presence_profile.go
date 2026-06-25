// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * presence_profile module - rich presence (RPID) profile.
 *
 * Port of the kamailio presence_profile module
 * (src/modules/presence_profile). A PresenceProfileModule parses and
 * builds RFC 4480 RPID (Rich Presence Extensions to PIDF) documents and
 * caches the current profile per user. RPID augments PIDF with
 * activities, mood, sphere, icon and other rich presence elements.
 *
 * The module is safe for concurrent use.
 */
package presence_profile

import (
	"fmt"
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// Activity represents one RPID activity. Type is the activity element
// name (e.g. "away", "busy", "on-the-phone"); Description is the
// optional human-readable text.
type Activity struct {
	Type        string
	Description string
}

// MoodInfo represents the RPID mood element.
type MoodInfo struct {
	Moods []string
	Text  string
}

// ProfileInfo represents a parsed RPID profile.
type ProfileInfo struct {
	UserURI    string
	Activities []Activity
	Mood       MoodInfo
	Sphere     string
	Icon       string
	Timestamp  string
}

// PresenceProfileModule parses and builds RPID XML and caches profile
// state. It mirrors the C presence_profile module.
type PresenceProfileModule struct {
	mu       sync.RWMutex
	profiles map[string]*ProfileInfo
}

// New creates a PresenceProfileModule with empty state.
func New() *PresenceProfileModule {
	return &PresenceProfileModule{profiles: make(map[string]*ProfileInfo)}
}

// ParseRPID parses an RPID/PIDF XML body into a ProfileInfo. The
// activities, mood, sphere, icon and timestamp elements are extracted
// regardless of namespace prefix. Returns an error when no <presence>
// element is present.
func (m *PresenceProfileModule) ParseRPID(xml []byte) (*ProfileInfo, error) {
	if m == nil {
		return nil, fmt.Errorf("presence_profile: nil module")
	}
	body := string(xml)
	if !strings.Contains(body, "<presence") {
		return nil, fmt.Errorf("presence_profile: no <presence> element")
	}
	info := &ProfileInfo{}
	info.UserURI = extractAttr(body, "entity")

	// Activities: <activities>...<away/><on-the-phone/>...</activities>
	if block := firstBlock(body, "activities"); block != "" {
		info.Activities = extractActivities(block)
	}

	// Mood: <mood><happy/><text>...</text></mood>
	if block := firstBlock(body, "mood"); block != "" {
		info.Mood = extractMood(block)
	}

	info.Sphere = strings.TrimSpace(firstBlock(body, "sphere"))
	info.Icon = strings.TrimSpace(firstBlock(body, "icon"))
	info.Timestamp = strings.TrimSpace(firstBlock(body, "timestamp"))
	return info, nil
}

// BuildRPID builds an RPID/PIDF XML body from a ProfileInfo.
func (m *PresenceProfileModule) BuildRPID(info *ProfileInfo) ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("presence_profile: nil module")
	}
	if info == nil {
		return []byte{}, nil
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(fmt.Sprintf(`<presence xmlns="urn:ietf:params:xml:ns:pidf" xmlns:rpid="urn:ietf:params:xml:ns:pidf:rpid" entity="%s">`+"\n",
		escapeXML(info.UserURI)))
	b.WriteString("  <tuple id=\"rp1\">\n")
	b.WriteString("    <status><basic>open</basic></status>\n")
	if len(info.Activities) > 0 {
		b.WriteString("    <rpid:activities>\n")
		for _, a := range info.Activities {
			if a.Description != "" {
				b.WriteString(fmt.Sprintf("      <rpid:%s>%s</rpid:%s>\n",
					escapeXML(a.Type), escapeXML(a.Description), escapeXML(a.Type)))
			} else {
				b.WriteString(fmt.Sprintf("      <rpid:%s/>\n", escapeXML(a.Type)))
			}
		}
		b.WriteString("    </rpid:activities>\n")
	}
	if len(info.Mood.Moods) > 0 || info.Mood.Text != "" {
		b.WriteString("    <rpid:mood>\n")
		for _, mood := range info.Mood.Moods {
			b.WriteString(fmt.Sprintf("      <rpid:%s/>\n", escapeXML(mood)))
		}
		if info.Mood.Text != "" {
			b.WriteString(fmt.Sprintf("      <rpid:text>%s</rpid:text>\n", escapeXML(info.Mood.Text)))
		}
		b.WriteString("    </rpid:mood>\n")
	}
	if info.Sphere != "" {
		b.WriteString(fmt.Sprintf("    <rpid:sphere>%s</rpid:sphere>\n", escapeXML(info.Sphere)))
	}
	if info.Icon != "" {
		b.WriteString(fmt.Sprintf("    <rpid:icon>%s</rpid:icon>\n", escapeXML(info.Icon)))
	}
	if info.Timestamp != "" {
		b.WriteString(fmt.Sprintf("    <timestamp>%s</timestamp>\n", escapeXML(info.Timestamp)))
	}
	b.WriteString("  </tuple>\n")
	b.WriteString("</presence>")
	return []byte(b.String()), nil
}

// HandlePublish processes a PUBLISH carrying an RPID body. It parses
// the body and caches the profile, returning 200 on success, 400 on a
// malformed message and 415 when no body is present.
func (m *PresenceProfileModule) HandlePublish(msg *parser.SIPMsg) (int, error) {
	if m == nil {
		return 500, fmt.Errorf("presence_profile: nil module")
	}
	if msg == nil {
		return 400, fmt.Errorf("presence_profile: nil message")
	}
	body := bodyBytes(msg)
	if len(body) == 0 {
		return 415, fmt.Errorf("presence_profile: empty body")
	}
	info, err := m.ParseRPID(body)
	if err != nil {
		return 400, err
	}
	if info.UserURI == "" {
		info.UserURI = extractEntity(msg)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.profiles == nil {
		m.profiles = make(map[string]*ProfileInfo)
	}
	m.profiles[info.UserURI] = info
	return 200, nil
}

// GetProfile returns the cached ProfileInfo for userURI, or an error
// when no profile exists.
func (m *PresenceProfileModule) GetProfile(userURI string) (*ProfileInfo, error) {
	if m == nil {
		return nil, fmt.Errorf("presence_profile: nil module")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	info, ok := m.profiles[userURI]
	if !ok || info == nil {
		return nil, fmt.Errorf("presence_profile: no profile for %q", userURI)
	}
	return cloneProfile(info), nil
}

// SetActivity sets the activity for userURI, creating the profile when
// it does not yet exist.
func (m *PresenceProfileModule) SetActivity(userURI string, activity *Activity) error {
	if m == nil {
		return fmt.Errorf("presence_profile: nil module")
	}
	if userURI == "" {
		return fmt.Errorf("presence_profile: empty user URI")
	}
	if activity == nil {
		return fmt.Errorf("presence_profile: nil activity")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.profiles == nil {
		m.profiles = make(map[string]*ProfileInfo)
	}
	info, ok := m.profiles[userURI]
	if !ok {
		info = &ProfileInfo{UserURI: userURI}
		m.profiles[userURI] = info
	}
	info.Activities = []Activity{*activity}
	return nil
}

// SetMood sets the mood for userURI, creating the profile when it does
// not yet exist.
func (m *PresenceProfileModule) SetMood(userURI string, mood *MoodInfo) error {
	if m == nil {
		return fmt.Errorf("presence_profile: nil module")
	}
	if userURI == "" {
		return fmt.Errorf("presence_profile: empty user URI")
	}
	if mood == nil {
		return fmt.Errorf("presence_profile: nil mood")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.profiles == nil {
		m.profiles = make(map[string]*ProfileInfo)
	}
	info, ok := m.profiles[userURI]
	if !ok {
		info = &ProfileInfo{UserURI: userURI}
		m.profiles[userURI] = info
	}
	info.Mood = *mood
	return nil
}

// SetSphere sets the sphere for userURI, creating the profile when it
// does not yet exist.
func (m *PresenceProfileModule) SetSphere(userURI, sphere string) error {
	if m == nil {
		return fmt.Errorf("presence_profile: nil module")
	}
	if userURI == "" {
		return fmt.Errorf("presence_profile: empty user URI")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.profiles == nil {
		m.profiles = make(map[string]*ProfileInfo)
	}
	info, ok := m.profiles[userURI]
	if !ok {
		info = &ProfileInfo{UserURI: userURI}
		m.profiles[userURI] = info
	}
	info.Sphere = sphere
	return nil
}

// ClearProfile removes the cached profile for userURI. Returns an error
// when no profile exists.
func (m *PresenceProfileModule) ClearProfile(userURI string) error {
	if m == nil {
		return fmt.Errorf("presence_profile: nil module")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.profiles[userURI]; !ok {
		return fmt.Errorf("presence_profile: no profile for %q", userURI)
	}
	delete(m.profiles, userURI)
	return nil
}

// Count returns the number of cached profiles.
func (m *PresenceProfileModule) Count() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.profiles)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// cloneProfile returns a deep copy of info.
func cloneProfile(info *ProfileInfo) *ProfileInfo {
	if info == nil {
		return nil
	}
	cp := *info
	if info.Activities != nil {
		cp.Activities = make([]Activity, len(info.Activities))
		copy(cp.Activities, info.Activities)
	}
	if info.Mood.Moods != nil {
		cp.Mood.Moods = make([]string, len(info.Mood.Moods))
		copy(cp.Mood.Moods, info.Mood.Moods)
	}
	return &cp
}

// extractActivities walks an <activities> block and returns each child
// activity element as an Activity. Self-closing (<away/>) and paired
// (<away>...</away>) elements are both supported.
func extractActivities(block string) []Activity {
	var out []Activity
	rest := block
	for {
		idx := strings.Index(rest, "<")
		if idx < 0 {
			break
		}
		rest = rest[idx+1:]
		// Skip closing tags and comments.
		if strings.HasPrefix(rest, "/") || strings.HasPrefix(rest, "!--") {
			if gt := strings.Index(rest, ">"); gt >= 0 {
				rest = rest[gt+1:]
			}
			continue
		}
		// Read the element name up to whitespace, '/' or '>'.
		nameEnd := strings.IndexAny(rest, " \t\n\r/>")
		if nameEnd < 0 {
			break
		}
		name := stripNS(rest[:nameEnd])
		rest = rest[nameEnd:]
		// Find the end of this element.
		gt := strings.Index(rest, ">")
		if gt < 0 {
			break
		}
		tag := rest[:gt]
		selfClose := strings.HasSuffix(tag, "/")
		rest = rest[gt+1:]
		if name == "" {
			continue
		}
		desc := ""
		if !selfClose {
			closeIdx := findCloseTag(rest, name)
			if closeIdx >= 0 {
				desc = strings.TrimSpace(rest[:closeIdx])
				rest = rest[closeIdx:]
				if end := strings.Index(rest, ">"); end >= 0 {
					rest = rest[end+1:]
				}
			}
		}
		out = append(out, Activity{Type: name, Description: desc})
	}
	return out
}

// extractMood walks a <mood> block and returns the mood elements and
// optional <text> content.
func extractMood(block string) MoodInfo {
	info := MoodInfo{}
	info.Text = strings.TrimSpace(firstBlock(block, "text"))
	rest := block
	for {
		idx := strings.Index(rest, "<")
		if idx < 0 {
			break
		}
		rest = rest[idx+1:]
		// Skip closing tags and comments.
		if strings.HasPrefix(rest, "/") || strings.HasPrefix(rest, "!--") {
			if gt := strings.Index(rest, ">"); gt >= 0 {
				rest = rest[gt+1:]
			}
			continue
		}
		// Read the element name up to whitespace, '/' or '>'.
		nameEnd := strings.IndexAny(rest, " \t\n\r/>")
		if nameEnd < 0 {
			break
		}
		name := stripNS(rest[:nameEnd])
		rest = rest[nameEnd:]
		gt := strings.Index(rest, ">")
		if gt < 0 {
			break
		}
		tag := rest[:gt]
		selfClose := strings.HasSuffix(tag, "/")
		rest = rest[gt+1:]
		if name == "" {
			continue
		}
		// Skip the <text>...</text> element entirely (already captured).
		if name == "text" {
			if !selfClose {
				closeIdx := findCloseTag(rest, "text")
				if closeIdx >= 0 {
					rest = rest[closeIdx:]
					if end := strings.Index(rest, ">"); end >= 0 {
						rest = rest[end+1:]
					}
				}
			}
			continue
		}
		info.Moods = append(info.Moods, name)
	}
	return info
}

// findCloseTag returns the index of </name> (or </ns:name>) in s.
func findCloseTag(s, name string) int {
	needle := "</" + name + ">"
	if idx := strings.Index(s, needle); idx >= 0 {
		return idx
	}
	// Search for any namespaced close tag ending in :name>.
	rest := s
	off := 0
	for {
		idx := strings.Index(rest, "</")
		if idx < 0 {
			return -1
		}
		idx += off
		end := strings.Index(s[idx:], ">")
		if end < 0 {
			return -1
		}
		tagName := s[idx+2 : idx+end]
		if strings.HasSuffix(tagName, ":"+name) || tagName == name {
			return idx
		}
		off = idx + end + 1
		rest = s[off:]
	}
}

// stripNS removes an XML namespace prefix (e.g. "rpid:away" -> "away").
func stripNS(s string) string {
	if i := strings.LastIndex(s, ":"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// firstBlock returns the content of the first <tag>...</tag> block,
// matching the tag with or without a namespace prefix.
func firstBlock(s, tag string) string {
	rest := s
	for {
		idx := strings.Index(rest, "<")
		if idx < 0 {
			return ""
		}
		rest = rest[idx:]
		gt := strings.Index(rest, ">")
		if gt < 0 {
			return ""
		}
		name := rest[1:gt]
		// Strip attributes.
		if sp := strings.IndexAny(name, " \t"); sp >= 0 {
			name = name[:sp]
		}
		name = stripNS(name)
		if name == tag {
			content := rest[gt+1:]
			closeIdx := findCloseTag(content, tag)
			if closeIdx < 0 {
				return content
			}
			return content[:closeIdx]
		}
		rest = rest[gt+1:]
	}
}

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
	defaultMu        sync.RWMutex
	defaultProfile *PresenceProfileModule
)

// DefaultProfile returns the process-wide PresenceProfileModule,
// creating it on first use.
func DefaultProfile() *PresenceProfileModule {
	defaultMu.RLock()
	p := defaultProfile
	defaultMu.RUnlock()
	if p != nil {
		return p
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultProfile == nil {
		defaultProfile = New()
	}
	return defaultProfile
}

// Init (re)initialises the process-wide PresenceProfileModule to a
// fresh state, mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultProfile = New()
}
