// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Diversion module - Diversion header manipulation (RFC 5806).
 * Port of the kamailio diversion module (src/modules/diversion).
 *
 * diversion adds, removes and inspects Diversion headers on a parsed SIP
 * message. AddDiversion appends a Diversion header and returns the new
 * count; RemoveDiversion strips all Diversion headers and returns how many
 * were removed; GetDiversion returns their bodies; Count reports the
 * number present.
 *
 * The module is safe for concurrent use: a single mutex serialises all
 * header operations.
 */

package diversion

import (
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// DiversionModule manipulates Diversion headers.
type DiversionModule struct {
	mu sync.Mutex
}

// New creates a DiversionModule.
func New() *DiversionModule {
	return &DiversionModule{}
}

// AddDiversion appends a Diversion header carrying uri to msg and returns
// the resulting number of Diversion headers. A nil message yields 0.
func (m *DiversionModule) AddDiversion(msg *parser.SIPMsg, uri string) int {
	if msg == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	msg.AddHeader("Diversion", uri)
	return msg.CountHeadersByType(parser.HdrDiversion)
}

// RemoveDiversion removes all Diversion headers from msg and returns the
// number removed. A nil message yields 0.
func (m *DiversionModule) RemoveDiversion(msg *parser.SIPMsg) int {
	if msg == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	n := msg.CountHeadersByType(parser.HdrDiversion)
	if n > 0 {
		msg.RemoveHeadersByType(parser.HdrDiversion)
	}
	return n
}

// GetDiversion returns the bodies of all Diversion headers on msg in
// order. Returns nil for a nil message or when none are present.
func (m *DiversionModule) GetDiversion(msg *parser.SIPMsg) []string {
	if msg == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	hdrs := msg.GetAllHeadersByType(parser.HdrDiversion)
	out := make([]string, 0, len(hdrs))
	for _, h := range hdrs {
		out = append(out, h.Body.String())
	}
	return out
}

// Count returns the number of Diversion headers on msg.
func (m *DiversionModule) Count(msg *parser.SIPMsg) int {
	if msg == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return msg.CountHeadersByType(parser.HdrDiversion)
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *DiversionModule
)

// DefaultDiversion returns the process-wide module, creating it on first use.
func DefaultDiversion() *DiversionModule {
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

// AddDiversion is the package-level wrapper.
func AddDiversion(msg *parser.SIPMsg, uri string) int {
	return DefaultDiversion().AddDiversion(msg, uri)
}

// RemoveDiversion is the package-level wrapper.
func RemoveDiversion(msg *parser.SIPMsg) int {
	return DefaultDiversion().RemoveDiversion(msg)
}

// GetDiversion is the package-level wrapper.
func GetDiversion(msg *parser.SIPMsg) []string {
	return DefaultDiversion().GetDiversion(msg)
}

// Count is the package-level wrapper.
func Count(msg *parser.SIPMsg) int {
	return DefaultDiversion().Count(msg)
}
