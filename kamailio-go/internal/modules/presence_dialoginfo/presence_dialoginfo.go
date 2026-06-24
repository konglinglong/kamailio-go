// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * presence_dialoginfo module - dialog-info XML handling.
 *
 * Port of the kamailio presence_dialoginfo module
 * (src/modules/presence_dialoginfo). A DialogInfoModule parses and
 * builds RFC 4235 dialog-info documents and aggregates several dialogs
 * for the same entity into the single highest-priority state.
 *
 * State priority (low to high): terminated < early < confirmed <
 * proceeding. AggregateDialogs selects the dialog with the highest
 * priority state.
 *
 * The module is safe for concurrent use.
 */
package presence_dialoginfo

import (
	"fmt"
	"strings"
	"sync"
)

// DialogInfo represents a single parsed dialog-info document (RFC 4235).
type DialogInfo struct {
	Entity    string
	State     string
	Direction string
	LocalURI  string
	RemoteURI string
	CallID    string
	LocalTag  string
	RemoteTag string
}

// DialogInfoModule parses and builds dialog-info XML and aggregates
// dialogs. It mirrors the C presence_dialoginfo module, which
// registers the "dialog" event package with the presence server.
type DialogInfoModule struct {
	mu sync.RWMutex
}

// NewDialogInfoModule creates a DialogInfoModule with empty state.
func NewDialogInfoModule() *DialogInfoModule {
	return &DialogInfoModule{}
}

// ParseDialogInfo parses a dialog-info XML body into a DialogInfo.
// Missing elements yield empty strings; the function only returns an
// error when no <dialog-info> element is present at all.
func (m *DialogInfoModule) ParseDialogInfo(body string) (*DialogInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !strings.Contains(body, "<dialog-info") {
		return nil, fmt.Errorf("presence_dialoginfo: no <dialog-info> element")
	}
	info := &DialogInfo{
		Entity: extractAttr(body, "entity"),
	}

	// Locate the <dialog ...> opening tag, skipping <dialog-info>.
	dialogStart := findDialogOpen(body)
	if dialogStart >= 0 {
		dialogRest := body[dialogStart:]
		openEnd := strings.Index(dialogRest, ">")
		if openEnd >= 0 {
			openTag := dialogRest[:openEnd]
			info.CallID = extractAttr(openTag, "call-id")
			info.Direction = extractAttr(openTag, "direction")
			info.LocalTag = extractAttr(openTag, "local-tag")
			info.RemoteTag = extractAttr(openTag, "remote-tag")
		}
	}

	info.State = extractTagValue(body, "state")
	info.LocalURI = extractSectionTagValue(body, "local", "identity")
	info.RemoteURI = extractSectionTagValue(body, "remote", "identity")
	return info, nil
}

// BuildDialogInfo builds a dialog-info XML body from a DialogInfo.
func (m *DialogInfoModule) BuildDialogInfo(info *DialogInfo) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if info == nil {
		return ""
	}
	dialogID := "dialog"
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<dialog-info xmlns="urn:ietf:params:xml:ns:dialog-info" version="1" state="full" entity="%s">
  <dialog id="%s" call-id="%s" direction="%s" local-tag="%s" remote-tag="%s">
    <state>%s</state>
    <local>
      <identity>%s</identity>
    </local>
    <remote>
      <identity>%s</identity>
    </remote>
  </dialog>
</dialog-info>`,
		escapeXML(info.Entity),
		escapeXML(dialogID),
		escapeXML(info.CallID),
		escapeXML(info.Direction),
		escapeXML(info.LocalTag),
		escapeXML(info.RemoteTag),
		escapeXML(info.State),
		escapeXML(info.LocalURI),
		escapeXML(info.RemoteURI))
}

// StatePriority returns the relative priority of a dialog state.
// Priority order (low to high): terminated < early < confirmed <
// proceeding. Unknown states get the lowest priority (-1).
func (m *DialogInfoModule) StatePriority(state string) int {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "terminated":
		return 0
	case "early":
		return 1
	case "confirmed":
		return 2
	case "proceeding":
		return 3
	default:
		return -1
	}
}

// AggregateDialogs picks the dialog with the highest-priority state and
// returns a copy of it. Returns nil when dialogs is empty. Ties are
// broken by the first dialog encountered at the highest priority.
func (m *DialogInfoModule) AggregateDialogs(dialogs []*DialogInfo) *DialogInfo {
	if len(dialogs) == 0 {
		return nil
	}
	best := -1
	var bestDlg *DialogInfo
	for _, d := range dialogs {
		prio := m.StatePriority(d.State)
		if prio > best {
			best = prio
			bestDlg = d
		}
	}
	if bestDlg == nil {
		return nil
	}
	// Return a copy so callers cannot mutate the aggregate's source.
	out := *bestDlg
	return &out
}

// findDialogOpen returns the byte index of the <dialog ...> opening tag
// in s, skipping <dialog-info>. Returns -1 when no <dialog> element is
// present.
func findDialogOpen(s string) int {
	off := 0
	for {
		i := strings.Index(s[off:], "<dialog")
		if i < 0 {
			return -1
		}
		i += off
		rest := s[i+len("<dialog"):]
		if len(rest) > 0 {
			c := rest[0]
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '>' {
				return i
			}
		}
		off = i + len("<dialog")
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

// extractSectionTagValue extracts the text of <inner> within the first
// <section>...</section> block of s.
func extractSectionTagValue(s, section, inner string) string {
	open := "<" + section
	idx := strings.Index(s, open)
	if idx < 0 {
		return ""
	}
	rest := s[idx:]
	closeTag := "</" + section + ">"
	end := strings.Index(rest, closeTag)
	if end < 0 {
		return ""
	}
	sectionContent := rest[:end]
	return extractTagValue(sectionContent, inner)
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
	defaultMu  sync.RWMutex
	defaultDlg *DialogInfoModule
)

// DefaultDialogInfo returns the process-wide DialogInfoModule, creating
// one on first use.
func DefaultDialogInfo() *DialogInfoModule {
	defaultMu.RLock()
	d := defaultDlg
	defaultMu.RUnlock()
	if d != nil {
		return d
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultDlg == nil {
		defaultDlg = NewDialogInfoModule()
	}
	return defaultDlg
}

// Init (re)initialises the process-wide DialogInfoModule to a fresh
// state, mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultDlg = NewDialogInfoModule()
}
