// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * KEMI extensions module - SIP message helpers for KEMI scripts.
 * Port of the kamailio kemix module (src/modules/kemix).
 *
 * The module provides helpers to inspect and modify the request URI and
 * headers of a SIP message. It is safe for concurrent use.
 */

package kemix

import (
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

// KEMIXModule provides SIP message helpers.
type KEMIXModule struct {
	mu sync.RWMutex
}

// New creates a KEMIXModule.
func New() *KEMIXModule {
	return &KEMIXModule{}
}

// GetURI returns a part of the request URI of msg. Supported parts are
// "user", "host", "port"; any other value (including "") returns the
// full request URI string.
//
//	C: kemix_get_uri()
func (m *KEMIXModule) GetURI(msg *parser.SIPMsg, part string) string {
	uri := requestURI(msg)
	if uri == "" {
		return ""
	}
	u, err := parser.ParseURI(uri)
	if err != nil {
		return ""
	}
	switch strings.ToLower(part) {
	case "user":
		return u.User.String()
	case "host":
		return u.Host.String()
	case "port":
		return u.Port.String()
	default:
		return uri
	}
}

// SetURI modifies a part of the request URI of msg in place. Supported
// parts are "user", "host" and "port".
//
//	C: kemix_set_uri()
func (m *KEMIXModule) SetURI(msg *parser.SIPMsg, part, value string) {
	if msg == nil || msg.FirstLine == nil || msg.FirstLine.Req == nil {
		return
	}
	uri := msg.FirstLine.Req.URI.String()
	if uri == "" {
		return
	}
	u, err := parser.ParseURI(uri)
	if err != nil {
		return
	}
	switch strings.ToLower(part) {
	case "user":
		u.User = str.Mk(value)
	case "host":
		u.Host = str.Mk(value)
	case "port":
		u.Port = str.Mk(value)
	default:
		return
	}
	msg.FirstLine.Req.URI = str.Mk(rebuildURI(u))
}

// GetHeader returns the body of the first header in msg whose name
// matches case-insensitively.
//
//	C: kemix_get_header()
func (m *KEMIXModule) GetHeader(msg *parser.SIPMsg, name string) string {
	if msg == nil || name == "" {
		return ""
	}
	for _, h := range msg.Headers {
		if h == nil {
			continue
		}
		if strings.EqualFold(h.Name.String(), name) {
			return strings.TrimSpace(h.Body.String())
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

// rebuildURI reconstructs a URI string from a parsed SIPURI.
func rebuildURI(u *parser.SIPURI) string {
	scheme := "sip"
	switch u.Type {
	case parser.SIPSURIT:
		scheme = "sips"
	case parser.TELURIT:
		scheme = "tel"
	case parser.TELSURIT:
		scheme = "tels"
	case parser.URNURIT:
		scheme = "urn"
	}
	var b strings.Builder
	b.WriteString(scheme)
	b.WriteByte(':')
	if u.User.String() != "" {
		b.WriteString(u.User.String())
		if u.Passwd.String() != "" {
			b.WriteByte(':')
			b.WriteString(u.Passwd.String())
		}
		b.WriteByte('@')
	}
	b.WriteString(u.Host.String())
	if u.Port.String() != "" {
		b.WriteByte(':')
		b.WriteString(u.Port.String())
	}
	if u.Params.String() != "" {
		b.WriteByte(';')
		b.WriteString(u.Params.String())
	}
	if u.Headers.String() != "" {
		b.WriteByte('?')
		b.WriteString(u.Headers.String())
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *KEMIXModule
)

// DefaultKEMIX returns the process-wide KEMIXModule.
func DefaultKEMIX() *KEMIXModule {
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

// Init (re)initialises the process-wide KEMIXModule.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// GetURI is the package-level wrapper around DefaultKEMIX().GetURI.
func GetURI(msg *parser.SIPMsg, part string) string { return DefaultKEMIX().GetURI(msg, part) }

// SetURI is the package-level wrapper around DefaultKEMIX().SetURI.
func SetURI(msg *parser.SIPMsg, part, value string) { DefaultKEMIX().SetURI(msg, part, value) }

// GetHeader is the package-level wrapper around DefaultKEMIX().GetHeader.
func GetHeader(msg *parser.SIPMsg, name string) string {
	return DefaultKEMIX().GetHeader(msg, name)
}
