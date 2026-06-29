// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * presence_dfks module - Direct Facility Feature Key Supervision.
 *
 * Port of the kamailio presence_dfks module
 * (src/modules/presence_dfks). DFKS is a Nortel/Avaya feature that
 * supervises feature keys (do-not-disturb, call forwarding) via SIP
 * using the ECMA-323/CSTA XML event "as-feature-event"
 * (application/x-as-feature-event+xml).
 *
 * A PresenceDFKSModule parses and builds DFKS XML bodies and caches the
 * current feature-key state per user. The module is safe for concurrent
 * use.
 */
package presence_dfks

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// FeatureKey represents one supervised feature key.
type FeatureKey struct {
	Name      string
	Value     string
	Timestamp time.Time
}

// DFKSInfo represents the cached DFKS state for one user.
type DFKSInfo struct {
	UserURI     string
	FeatureKeys map[string]string
	Status      string
}

// PresenceDFKSModule parses and builds DFKS XML and caches feature-key
// state. It mirrors the C presence_dfks module.
type PresenceDFKSModule struct {
	mu    sync.RWMutex
	users map[string]*DFKSInfo
}

// New creates a PresenceDFKSModule with empty state.
func New() *PresenceDFKSModule {
	return &PresenceDFKSModule{users: make(map[string]*DFKSInfo)}
}

// ParseDFKS parses a DFKS (as-feature-event) XML body into a DFKSInfo.
// The body may contain SetDoNotDisturb, SetForwarding,
// DoNotDisturbEvent or ForwardingEvent elements; each is collapsed into
// a FeatureKey. Returns an error when the body contains no recognised
// CSTA element.
func (m *PresenceDFKSModule) ParseDFKS(xml []byte) (*DFKSInfo, error) {
	if m == nil {
		return nil, fmt.Errorf("presence_dfks: nil module")
	}
	body := string(xml)
	info := &DFKSInfo{FeatureKeys: make(map[string]string)}
	found := false

	// SetDoNotDisturb / DoNotDisturbEvent -> DoNotDisturb key.
	if block := firstBlock(body, "SetDoNotDisturb", "DoNotDisturbEvent"); block != "" {
		val := extractTagValue(block, "doNotDisturbOn")
		if val == "" {
			val = extractTagValue(block, "doNotDisturbOn")
		}
		if val != "" {
			info.FeatureKeys["DoNotDisturb"] = val
			info.Status = val
			found = true
		}
	}

	// SetForwarding / ForwardingEvent -> Forwarding key.
	if block := firstBlock(body, "SetForwarding", "ForwardingEvent"); block != "" {
		fwdType := extractTagValue(block, "forwardingType")
		fwdStatus := extractTagValue(block, "forwardStatus")
		activate := extractTagValue(block, "activateForward")
		fwdTo := extractTagValue(block, "forwardTo")
		fwdDN := extractTagValue(block, "forwardDN")
		val := fwdStatus
		if val == "" {
			val = activate
		}
		if fwdType != "" {
			if val != "" {
				val = fwdType + ":" + val
			} else {
				val = fwdType
			}
		}
		if fwdTo != "" {
			val = val + "->" + fwdTo
		} else if fwdDN != "" {
			val = val + "->" + fwdDN
		}
		if val != "" {
			info.FeatureKeys["Forwarding"] = val
			found = true
		}
	}

	if !found {
		return nil, fmt.Errorf("presence_dfks: no recognised CSTA element")
	}
	return info, nil
}

// BuildDFKS builds a DFKS XML body from a DFKSInfo. Each feature key is
// emitted using its CSTA event form.
func (m *PresenceDFKSModule) BuildDFKS(info *DFKSInfo) ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("presence_dfks: nil module")
	}
	if info == nil {
		return []byte{}, nil
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="ISO-8859-1"?>` + "\n")
	for name, val := range info.FeatureKeys {
		switch name {
		case "DoNotDisturb":
			b.WriteString(fmt.Sprintf(`<DoNotDisturbEvent xmlns="http://www.ecma-international.org/standards/ecma-323/csta/ed3">`+"\n"))
			b.WriteString(fmt.Sprintf("<device>%s</device>\n", escapeXML(info.UserURI)))
			b.WriteString(fmt.Sprintf("<doNotDisturbOn>%s</doNotDisturbOn>\n", escapeXML(val)))
			b.WriteString("</DoNotDisturbEvent>\n")
		case "Forwarding":
			fwdType := val
			fwdTo := ""
			if i := strings.Index(val, "->"); i >= 0 {
				fwdType = val[:i]
				fwdTo = val[i+2:]
			}
			status := "true"
			if strings.HasSuffix(fwdType, ":false") {
				status = "false"
				fwdType = strings.TrimSuffix(fwdType, ":false")
			} else if strings.HasSuffix(fwdType, ":true") {
				fwdType = strings.TrimSuffix(fwdType, ":true")
			}
			b.WriteString(fmt.Sprintf(`<ForwardingEvent xmlns="http://www.ecma-international.org/standards/ecma-323/csta/ed3">`+"\n"))
			b.WriteString(fmt.Sprintf("<device>%s</device>\n", escapeXML(info.UserURI)))
			b.WriteString(fmt.Sprintf("<forwardingType>%s</forwardingType>\n", escapeXML(fwdType)))
			b.WriteString(fmt.Sprintf("<forwardStatus>%s</forwardStatus>\n", escapeXML(status)))
			b.WriteString(fmt.Sprintf("<forwardTo>%s</forwardTo>\n", escapeXML(fwdTo)))
			b.WriteString("</ForwardingEvent>\n")
		default:
			b.WriteString(fmt.Sprintf("<%s>%s</%s>\n", escapeXML(name), escapeXML(val), escapeXML(name)))
		}
	}
	return []byte(b.String()), nil
}

// HandleSubscribe processes a SUBSCRIBE for DFKS state. Returns 200 on
// success, 400 when the message is missing required headers.
func (m *PresenceDFKSModule) HandleSubscribe(msg *parser.SIPMsg) (int, error) {
	if m == nil {
		return 500, fmt.Errorf("presence_dfks: nil module")
	}
	if msg == nil {
		return 400, fmt.Errorf("presence_dfks: nil message")
	}
	uri := extractEntity(msg)
	if uri == "" {
		return 400, fmt.Errorf("presence_dfks: no entity")
	}
	return 200, nil
}

// HandlePublish processes a PUBLISH carrying a DFKS body. It parses the
// body and caches the feature-key state, returning 200 on success, 400
// on a malformed message and 415 when no body is present.
func (m *PresenceDFKSModule) HandlePublish(msg *parser.SIPMsg) (int, error) {
	if m == nil {
		return 500, fmt.Errorf("presence_dfks: nil module")
	}
	if msg == nil {
		return 400, fmt.Errorf("presence_dfks: nil message")
	}
	body := bodyBytes(msg)
	if len(body) == 0 {
		return 415, fmt.Errorf("presence_dfks: empty body")
	}
	info, err := m.ParseDFKS(body)
	if err != nil {
		return 400, err
	}
	info.UserURI = extractEntity(msg)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.users == nil {
		m.users = make(map[string]*DFKSInfo)
	}
	m.users[info.UserURI] = info
	return 200, nil
}

// GetFeatureKeys returns the cached feature keys for userURI. Returns
// an error when no state exists for the user.
func (m *PresenceDFKSModule) GetFeatureKeys(userURI string) ([]FeatureKey, error) {
	if m == nil {
		return nil, fmt.Errorf("presence_dfks: nil module")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	info, ok := m.users[userURI]
	if !ok || info == nil {
		return nil, fmt.Errorf("presence_dfks: no state for %q", userURI)
	}
	out := make([]FeatureKey, 0, len(info.FeatureKeys))
	for name, val := range info.FeatureKeys {
		out = append(out, FeatureKey{Name: name, Value: val})
	}
	// Stable order for testability.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Name < out[i].Name {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

// SetFeatureKey sets a feature key for userURI, creating the user state
// when it does not yet exist.
func (m *PresenceDFKSModule) SetFeatureKey(userURI, keyName, keyValue string) error {
	if m == nil {
		return fmt.Errorf("presence_dfks: nil module")
	}
	if userURI == "" {
		return fmt.Errorf("presence_dfks: empty user URI")
	}
	if keyName == "" {
		return fmt.Errorf("presence_dfks: empty key name")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.users == nil {
		m.users = make(map[string]*DFKSInfo)
	}
	info, ok := m.users[userURI]
	if !ok {
		info = &DFKSInfo{UserURI: userURI, FeatureKeys: make(map[string]string)}
		m.users[userURI] = info
	}
	info.FeatureKeys[keyName] = keyValue
	info.Status = keyValue
	return nil
}

// ClearFeatureKey removes a feature key for userURI. Returns an error
// when the user or key does not exist.
func (m *PresenceDFKSModule) ClearFeatureKey(userURI, keyName string) error {
	if m == nil {
		return fmt.Errorf("presence_dfks: nil module")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.users[userURI]
	if !ok {
		return fmt.Errorf("presence_dfks: no state for %q", userURI)
	}
	if _, ok := info.FeatureKeys[keyName]; !ok {
		return fmt.Errorf("presence_dfks: no key %q", keyName)
	}
	delete(info.FeatureKeys, keyName)
	return nil
}

// Count returns the number of cached users.
func (m *PresenceDFKSModule) Count() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.users)
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

// firstBlock returns the content of the first <tag>...</tag> block for
// any of the candidate tags, or "" when none is present.
func firstBlock(s string, tags ...string) string {
	for _, tag := range tags {
		open := "<" + tag
		idx := strings.Index(s, open)
		if idx < 0 {
			continue
		}
		rest := s[idx:]
		gt := strings.Index(rest, ">")
		if gt < 0 {
			continue
		}
		content := rest[gt+1:]
		closeTag := "</" + tag + ">"
		end := strings.Index(content, closeTag)
		if end < 0 {
			return content
		}
		return content[:end]
	}
	return ""
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
	defaultMu    sync.RWMutex
	defaultDFKS *PresenceDFKSModule
)

// DefaultDFKS returns the process-wide PresenceDFKSModule, creating it
// on first use.
func DefaultDFKS() *PresenceDFKSModule {
	defaultMu.RLock()
	p := defaultDFKS
	defaultMu.RUnlock()
	if p != nil {
		return p
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultDFKS == nil {
		defaultDFKS = New()
	}
	return defaultDFKS
}

// Init (re)initialises the process-wide PresenceDFKSModule to a fresh
// state, mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultDFKS = New()
}
