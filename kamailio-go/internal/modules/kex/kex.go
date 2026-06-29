// SPDX-License-Identifier-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * KEX module - Kamailio extensions.
 * Port of the kamailio kex module (src/modules/kex).
 *
 * kex exposes a few helpers that classify a parsed SIP message (request
 * vs reply, method name, status code) and a small IsMyURI check against
 * a configured set of "my" URIs (hosts the proxy considers local).
 *
 * The module is safe for concurrent use.
 */

package kex

import (
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// KEXModule provides Kamailio extension helpers.
type KEXModule struct {
	mu   sync.RWMutex
	myURIs map[string]struct{}
}

// New creates an empty KEXModule.
func New() *KEXModule {
	return &KEXModule{myURIs: make(map[string]struct{})}
}

// AddMyURI registers a URI (or host) that IsMyURI should treat as local.
func (m *KEXModule) AddMyURI(uri string) {
	if uri == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.myURIs[strings.ToLower(strings.TrimSpace(uri))] = struct{}{}
}

// GetMsgType returns "request", "reply" or "unknown" for msg.
func (m *KEXModule) GetMsgType(msg *parser.SIPMsg) string {
	if msg == nil || msg.FirstLine == nil {
		return "unknown"
	}
	if msg.FirstLine.IsRequest() {
		return "request"
	}
	if msg.FirstLine.IsReply() {
		return "reply"
	}
	return "unknown"
}

// GetMsgMethod returns the request method name (e.g. "INVITE"), or "" for
// replies or nil messages.
func (m *KEXModule) GetMsgMethod(msg *parser.SIPMsg) string {
	if msg == nil || msg.FirstLine == nil || msg.FirstLine.Req == nil {
		return ""
	}
	return msg.FirstLine.Req.Method.String()
}

// GetMsgStatus returns the reply status code, or 0 for requests / nil.
func (m *KEXModule) GetMsgStatus(msg *parser.SIPMsg) int {
	if msg == nil || msg.FirstLine == nil || msg.FirstLine.Reply == nil {
		return 0
	}
	return int(msg.FirstLine.Reply.StatusCode)
}

// IsMyURI reports whether uri matches one of the registered "my" URIs.
// Matching is case-insensitive and ignores leading/trailing whitespace.
func (m *KEXModule) IsMyURI(uri string) bool {
	uri = strings.ToLower(strings.TrimSpace(uri))
	if uri == "" {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.myURIs[uri]; ok {
		return true
	}
	// Also accept a host-only match when the URI contains a user part.
	if i := strings.IndexByte(uri, '@'); i >= 0 {
		host := strings.TrimSpace(uri[i+1:])
		if _, ok := m.myURIs[host]; ok {
			return true
		}
	}
	return false
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *KEXModule
)

// DefaultKEX returns the process-wide KEXModule, creating it on first use.
func DefaultKEX() *KEXModule {
	defaultMu.RLock()
	m := defaultM
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM == nil {
		defaultM = New()
	}
	return defaultM
}

// Init (re)initialises the process-wide KEXModule to a fresh state.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// GetMsgType is the package-level wrapper.
func GetMsgType(msg *parser.SIPMsg) string { return DefaultKEX().GetMsgType(msg) }

// GetMsgMethod is the package-level wrapper.
func GetMsgMethod(msg *parser.SIPMsg) string { return DefaultKEX().GetMsgMethod(msg) }

// GetMsgStatus is the package-level wrapper.
func GetMsgStatus(msg *parser.SIPMsg) int { return DefaultKEX().GetMsgStatus(msg) }

// IsMyURI is the package-level wrapper.
func IsMyURI(uri string) bool { return DefaultKEX().IsMyURI(uri) }
