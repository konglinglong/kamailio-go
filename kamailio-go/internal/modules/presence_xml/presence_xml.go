// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * presence_xml module - PIDF/XML presence document handling.
 *
 * Port of the kamailio presence_xml module (src/modules/presence_xml).
 * A PresenceXMLModule parses PIDF (Presence Information Data Format,
 * RFC 3863) bodies into PresenceDoc tuples, rebuilds PIDF bodies from
 * tuples, and aggregates several published presences into a single
 * composite state.
 *
 * The module is safe for concurrent use.
 */
package presence_xml

import (
	"fmt"
	"strings"
	"sync"
)

// PresenceDoc represents a single parsed presence tuple (Kamailio
// presentity tuple). It is the Go counterpart of a PIDF <tuple>
// element.
type PresenceDoc struct {
	Entity    string
	State     string
	TupleID   string
	Contact   string
	Status    string
	Basic     string
	Timestamp string
}

// PresenceXMLModule parses and builds PIDF/XML presence documents and
// aggregates multiple published presences. It mirrors the C
// presence_xml module, which registers PIDF handling with the presence
// server.
type PresenceXMLModule struct {
	mu sync.RWMutex
}

// NewPresenceXMLModule creates a PresenceXMLModule with empty state.
func NewPresenceXMLModule() *PresenceXMLModule {
	return &PresenceXMLModule{}
}

// ParseBody parses a PIDF/XML body into one PresenceDoc per <tuple>.
// The entity attribute of <presence> is copied to every tuple. An
// empty or malformed body yields an empty slice and a nil error; a
// body without any tuple yields an empty slice.
func (m *PresenceXMLModule) ParseBody(body string) ([]*PresenceDoc, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entity := extractAttr(body, "entity")

	docs := make([]*PresenceDoc, 0)
	rest := body
	for {
		start := strings.Index(rest, "<tuple")
		if start < 0 {
			break
		}
		rest = rest[start:]
		openEnd := strings.Index(rest, ">")
		if openEnd < 0 {
			break
		}
		openTag := rest[:openEnd]
		tupleID := extractAttr(openTag, "id")

		closeIdx := strings.Index(rest, "</tuple>")
		if closeIdx < 0 {
			break
		}
		tupleContent := rest[openEnd+1 : closeIdx]
		rest = rest[closeIdx+len("</tuple>"):]

		doc := &PresenceDoc{
			Entity:  entity,
			TupleID: tupleID,
			Basic:   extractTagValue(tupleContent, "basic"),
			Contact: extractTagValue(tupleContent, "contact"),
			Status:  extractTagValue(tupleContent, "status"),
		}
		// <status> wraps <basic>; if Status tag itself has no text but
		// basic does, keep basic as the canonical presence value.
		doc.Timestamp = extractTagValue(tupleContent, "timestamp")
		docs = append(docs, doc)
	}
	return docs, nil
}

// BuildBody builds a PIDF body from one or more PresenceDoc tuples. All
// tuples are emitted inside a single <presence> element using the
// entity of the first doc (or an empty entity when none is set).
func (m *PresenceXMLModule) BuildBody(docs []*PresenceDoc) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entity := ""
	if len(docs) > 0 {
		entity = docs[0].Entity
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(fmt.Sprintf(`<presence xmlns="urn:ietf:params:xml:ns:pidf" entity="%s">`+"\n", escapeXML(entity)))
	for _, d := range docs {
		tupleID := d.TupleID
		if tupleID == "" {
			tupleID = fmt.Sprintf("tuple-%p", d)
		}
		basic := d.Basic
		if basic == "" {
			basic = "closed"
		}
		b.WriteString(fmt.Sprintf("  <tuple id=\"%s\">\n", escapeXML(tupleID)))
		b.WriteString("    <status>\n")
		b.WriteString(fmt.Sprintf("      <basic>%s</basic>\n", escapeXML(basic)))
		b.WriteString("    </status>\n")
		if d.Contact != "" {
			b.WriteString(fmt.Sprintf("    <contact>%s</contact>\n", escapeXML(d.Contact)))
		}
		if d.Timestamp != "" {
			b.WriteString(fmt.Sprintf("    <timestamp>%s</timestamp>\n", escapeXML(d.Timestamp)))
		}
		b.WriteString("  </tuple>\n")
	}
	b.WriteString("</presence>")
	return b.String()
}

// Aggregate combines multiple published presences for the same entity
// into a single PresenceDoc. The aggregate is considered present
// (basic "open") if any of the inputs is open, otherwise "closed".
// Returns nil when docs is empty.
func (m *PresenceXMLModule) Aggregate(docs []*PresenceDoc) *PresenceDoc {
	if len(docs) == 0 {
		return nil
	}
	agg := &PresenceDoc{
		Entity:  docs[0].Entity,
		Basic:   "closed",
		TupleID: "aggregate",
	}
	for _, d := range docs {
		if isBasicOpen(d.Basic) {
			agg.Basic = "open"
		}
		if d.Contact != "" && agg.Contact == "" {
			agg.Contact = d.Contact
		}
		if d.Timestamp != "" && agg.Timestamp == "" {
			agg.Timestamp = d.Timestamp
		}
	}
	agg.Status = agg.Basic
	agg.State = agg.Basic
	return agg
}

// IsPresent reports whether the document's basic presence is "open".
// The comparison is case-insensitive.
func (m *PresenceXMLModule) IsPresent(doc *PresenceDoc) bool {
	if doc == nil {
		return false
	}
	return isBasicOpen(doc.Basic)
}

// GetStatus returns the document status, falling back to the basic
// presence value when Status is unset.
func (m *PresenceXMLModule) GetStatus(doc *PresenceDoc) string {
	if doc == nil {
		return ""
	}
	if doc.Status != "" {
		return doc.Status
	}
	return doc.Basic
}

// isBasicOpen reports whether a basic presence value means "open".
func isBasicOpen(basic string) bool {
	return strings.EqualFold(strings.TrimSpace(basic), "open")
}

// extractAttr returns the value of the named attribute in s, or "".
// It searches for `name="value"` (single or double quotes).
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
	defaultMu      sync.RWMutex
	defaultPresXML *PresenceXMLModule
)

// DefaultPresenceXML returns the process-wide PresenceXMLModule,
// creating one on first use.
func DefaultPresenceXML() *PresenceXMLModule {
	defaultMu.RLock()
	p := defaultPresXML
	defaultMu.RUnlock()
	if p != nil {
		return p
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultPresXML == nil {
		defaultPresXML = NewPresenceXMLModule()
	}
	return defaultPresXML
}

// Init (re)initialises the process-wide PresenceXMLModule to a fresh
// state, mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultPresXML = NewPresenceXMLModule()
}
