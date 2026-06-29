// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * presence_reginfo module - registration event package.
 *
 * Port of the kamailio presence_reginfo module
 * (src/modules/presence_reginfo). A PresenceReginfoModule parses and
 * builds RFC 3680 reginfo documents and caches the current registration
 * state per address-of-record (AOR). The module registers the "reg" event
 * package with the presence server.
 *
 * The module is safe for concurrent use.
 */
package presence_reginfo

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// RegContact represents one registered contact for an AOR.
type RegContact struct {
	URI       string
	State     string // registered, unregistered, trying, waiting
	Event     string // registered, created, expired, terminated
	Expires   int
	Timestamp string
}

// RegInfo represents the cached registration state for one AOR.
type RegInfo struct {
	AOR      string
	Contacts []RegContact
}

// PresenceReginfoModule parses and builds reginfo XML and caches
// registration state. It mirrors the C presence_reginfo module.
type PresenceReginfoModule struct {
	mu   sync.RWMutex
	aors map[string]*RegInfo
}

// New creates a PresenceReginfoModule with empty state.
func New() *PresenceReginfoModule {
	return &PresenceReginfoModule{aors: make(map[string]*RegInfo)}
}

// ParseRegInfo parses a reginfo XML body into a RegInfo. The first
// <registration> element's AOR is used; its <contact> children are
// collected. Returns an error when no <reginfo> element is present.
func (m *PresenceReginfoModule) ParseRegInfo(xml []byte) (*RegInfo, error) {
	if m == nil {
		return nil, fmt.Errorf("presence_reginfo: nil module")
	}
	body := string(xml)
	if !strings.Contains(body, "<reginfo") {
		return nil, fmt.Errorf("presence_reginfo: no <reginfo> element")
	}
	info := &RegInfo{}
	// Find the first <registration ...> block.
	regBlock := firstBlock(body, "registration")
	if regBlock == "" {
		// A reginfo with no registration is valid but empty.
		return info, nil
	}
	// The AOR is an attribute on the <registration> opening tag, which
	// firstBlock strips; recover it from the raw body.
	info.AOR = extractAttr(findOpenTag(body, "registration"), "aor")
	// Walk every <contact ...> block within the registration.
	rest := regBlock
	for {
		start := findTagOpen(rest, "contact")
		if start < 0 {
			break
		}
		rest = rest[start:]
		openEnd := strings.Index(rest, ">")
		if openEnd < 0 {
			break
		}
		openTag := rest[:openEnd]
		closeIdx := strings.Index(rest, "</contact>")
		var block string
		if closeIdx < 0 {
			block = rest[openEnd+1:]
			rest = ""
		} else {
			block = rest[openEnd+1 : closeIdx]
			rest = rest[closeIdx+len("</contact>"):]
		}
		expires, _ := strconv.Atoi(extractAttr(openTag, "expires"))
		info.Contacts = append(info.Contacts, RegContact{
			URI:       extractTagValue(block, "uri"),
			State:     extractAttr(openTag, "state"),
			Event:     extractAttr(openTag, "event"),
			Expires:   expires,
			Timestamp: extractTagValue(block, "timestamp"),
		})
	}
	return info, nil
}

// BuildRegInfo builds a reginfo XML body from a RegInfo.
func (m *PresenceReginfoModule) BuildRegInfo(info *RegInfo) ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("presence_reginfo: nil module")
	}
	if info == nil {
		return []byte{}, nil
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<reginfo xmlns="urn:ietf:params:xml:ns:reginfo" version="1" state="full">` + "\n")
	b.WriteString(fmt.Sprintf(`  <registration aor="%s" id="reg1" state="active">`+"\n",
		escapeXML(info.AOR)))
	for i, c := range info.Contacts {
		b.WriteString(fmt.Sprintf(`    <contact id="c%d" state="%s" event="%s" expires="%d">`+"\n",
			i+1, escapeXML(c.State), escapeXML(c.Event), c.Expires))
		b.WriteString(fmt.Sprintf("      <uri>%s</uri>\n", escapeXML(c.URI)))
		if c.Timestamp != "" {
			b.WriteString(fmt.Sprintf("      <timestamp>%s</timestamp>\n", escapeXML(c.Timestamp)))
		}
		b.WriteString("    </contact>\n")
	}
	b.WriteString("  </registration>\n")
	b.WriteString("</reginfo>")
	return []byte(b.String()), nil
}

// HandleSubscribe processes a SUBSCRIBE for registration state. Returns
// 200 on success, 400 when the message is missing required headers.
func (m *PresenceReginfoModule) HandleSubscribe(msg *parser.SIPMsg) (int, error) {
	if m == nil {
		return 500, fmt.Errorf("presence_reginfo: nil module")
	}
	if msg == nil {
		return 400, fmt.Errorf("presence_reginfo: nil message")
	}
	uri := extractEntity(msg)
	if uri == "" {
		return 400, fmt.Errorf("presence_reginfo: no entity")
	}
	return 200, nil
}

// HandlePublish processes a PUBLISH carrying a reginfo body. It parses
// the body and caches the registration state, returning 200 on success,
// 400 on a malformed message and 415 when no body is present.
func (m *PresenceReginfoModule) HandlePublish(msg *parser.SIPMsg) (int, error) {
	if m == nil {
		return 500, fmt.Errorf("presence_reginfo: nil module")
	}
	if msg == nil {
		return 400, fmt.Errorf("presence_reginfo: nil message")
	}
	body := bodyBytes(msg)
	if len(body) == 0 {
		return 415, fmt.Errorf("presence_reginfo: empty body")
	}
	info, err := m.ParseRegInfo(body)
	if err != nil {
		return 400, err
	}
	if info.AOR == "" {
		info.AOR = extractEntity(msg)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.aors == nil {
		m.aors = make(map[string]*RegInfo)
	}
	m.aors[info.AOR] = info
	return 200, nil
}

// GetRegInfo returns the cached RegInfo for aor, or an error when no
// registration exists.
func (m *PresenceReginfoModule) GetRegInfo(aor string) (*RegInfo, error) {
	if m == nil {
		return nil, fmt.Errorf("presence_reginfo: nil module")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	info, ok := m.aors[aor]
	if !ok || info == nil {
		return nil, fmt.Errorf("presence_reginfo: no registration for %q", aor)
	}
	cp := *info
	if cp.Contacts == nil {
		cp.Contacts = []RegContact{}
	}
	return &cp, nil
}

// AddContact adds contact to the AOR identified by aor, creating the
// registration when it does not yet exist.
func (m *PresenceReginfoModule) AddContact(aor string, contact *RegContact) error {
	if m == nil {
		return fmt.Errorf("presence_reginfo: nil module")
	}
	if aor == "" {
		return fmt.Errorf("presence_reginfo: empty AOR")
	}
	if contact == nil {
		return fmt.Errorf("presence_reginfo: nil contact")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.aors == nil {
		m.aors = make(map[string]*RegInfo)
	}
	info, ok := m.aors[aor]
	if !ok {
		info = &RegInfo{AOR: aor}
		m.aors[aor] = info
	}
	info.Contacts = append(info.Contacts, *contact)
	return nil
}

// RemoveContact removes the contact identified by contactURI from the
// AOR aor. Returns an error when the AOR or contact does not exist.
func (m *PresenceReginfoModule) RemoveContact(aor, contactURI string) error {
	if m == nil {
		return fmt.Errorf("presence_reginfo: nil module")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.aors[aor]
	if !ok {
		return fmt.Errorf("presence_reginfo: no registration for %q", aor)
	}
	kept := info.Contacts[:0]
	removed := false
	for _, c := range info.Contacts {
		if c.URI == contactURI && !removed {
			removed = true
			continue
		}
		kept = append(kept, c)
	}
	if !removed {
		return fmt.Errorf("presence_reginfo: no contact %q", contactURI)
	}
	info.Contacts = kept
	return nil
}

// UpdateContactState updates the state of the contact identified by
// contactURI within aor. Returns an error when the AOR or contact does
// not exist.
func (m *PresenceReginfoModule) UpdateContactState(aor, contactURI, state string) error {
	if m == nil {
		return fmt.Errorf("presence_reginfo: nil module")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.aors[aor]
	if !ok {
		return fmt.Errorf("presence_reginfo: no registration for %q", aor)
	}
	for i := range info.Contacts {
		if info.Contacts[i].URI == contactURI {
			info.Contacts[i].State = state
			return nil
		}
	}
	return fmt.Errorf("presence_reginfo: no contact %q", contactURI)
}

// ListAORs returns the AORs of every cached registration in sorted order.
func (m *PresenceReginfoModule) ListAORs() []string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.aors))
	for aor := range m.aors {
		out = append(out, aor)
	}
	sort.Strings(out)
	return out
}

// Count returns the number of cached AORs.
func (m *PresenceReginfoModule) Count() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.aors)
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
// skipping longer tags that merely start with the same prefix. Returns
// -1 when not found.
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

// findOpenTag returns the raw opening tag (including < and >) for the
// first <tag ...> element in s, or "" when not found.
func findOpenTag(s, tag string) string {
	idx := findTagOpen(s, tag)
	if idx < 0 {
		return ""
	}
	rest := s[idx:]
	gt := strings.Index(rest, ">")
	if gt < 0 {
		return ""
	}
	return rest[:gt+1]
}

// firstBlock returns the content of the first <tag>...</tag> block in
// s, or "" when not found.
func firstBlock(s, tag string) string {
	idx := findTagOpen(s, tag)
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
	return content[:end]
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
	defaultReginfo *PresenceReginfoModule
)

// DefaultReginfo returns the process-wide PresenceReginfoModule,
// creating it on first use.
func DefaultReginfo() *PresenceReginfoModule {
	defaultMu.RLock()
	p := defaultReginfo
	defaultMu.RUnlock()
	if p != nil {
		return p
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultReginfo == nil {
		defaultReginfo = New()
	}
	return defaultReginfo
}

// Init (re)initialises the process-wide PresenceReginfoModule to a
// fresh state, mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultReginfo = New()
}
