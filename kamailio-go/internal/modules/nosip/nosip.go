// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * NoSIP module - handling of non-SIP (e.g. MSRP/HTTP-like) messages.
 * Port of the kamailio nosip module (src/modules/nosip).
 *
 * nosip detects messages that are not SIP (the first line does not carry
 * the SIP version), reports their protocol, and "processes" them by
 * recording the event. This implementation inspects the parsed first
 * line's protocol flags and the raw request URI scheme.
 *
 * The module is safe for concurrent use.
 */

package nosip

import (
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// NoSIPModule detects and processes non-SIP messages.
type NoSIPModule struct {
	mu       sync.Mutex
	processed int
}

// New creates a NoSIPModule.
func New() *NoSIPModule {
	return &NoSIPModule{}
}

// IsNoSIP reports whether msg is not a SIP message. A nil message or one
// whose first line lacks the SIP protocol flag is treated as non-SIP.
func (m *NoSIPModule) IsNoSIP(msg *parser.SIPMsg) bool {
	if msg == nil || msg.FirstLine == nil {
		return true
	}
	return !msg.FirstLine.IsSIP()
}

// GetProtocol returns the protocol of msg: "sip" for SIP messages, the
// request-URI scheme (e.g. "msrp", "http") for non-SIP requests, or
// "unknown".
func (m *NoSIPModule) GetProtocol(msg *parser.SIPMsg) string {
	if msg == nil || msg.FirstLine == nil {
		return "unknown"
	}
	if msg.FirstLine.IsSIP() {
		return "sip"
	}
	if msg.FirstLine.Req != nil {
		uri := msg.FirstLine.Req.URI.String()
		if i := strings.IndexByte(uri, ':'); i > 0 {
			return strings.ToLower(uri[:i])
		}
		return "unknown"
	}
	return "unknown"
}

// ProcessNoSIP records that a non-SIP message was processed. It returns
// an error when the message is actually SIP (nothing to do) or nil.
func (m *NoSIPModule) ProcessNoSIP(msg *parser.SIPMsg) error {
	if msg == nil {
		return nil
	}
	if !m.IsNoSIP(msg) {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processed++
	return nil
}

// ProcessedCount returns the number of non-SIP messages processed.
func (m *NoSIPModule) ProcessedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.processed
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *NoSIPModule
)

// DefaultNoSIP returns the process-wide module, creating it on first use.
func DefaultNoSIP() *NoSIPModule {
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

// Init (re)initialises the process-wide module.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// IsNoSIP is the package-level wrapper.
func IsNoSIP(msg *parser.SIPMsg) bool { return DefaultNoSIP().IsNoSIP(msg) }

// ProcessNoSIP is the package-level wrapper.
func ProcessNoSIP(msg *parser.SIPMsg) error { return DefaultNoSIP().ProcessNoSIP(msg) }

// GetProtocol is the package-level wrapper.
func GetProtocol(msg *parser.SIPMsg) string { return DefaultNoSIP().GetProtocol(msg) }
