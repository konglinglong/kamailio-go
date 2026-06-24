// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SipDump module - dump SIP messages to an in-memory ring.
 * Port of the kamailio sipdump module (src/modules/sipdump).
 *
 * When enabled, every message handed to Dump() is recorded as a DumpEntry
 * tagged with a direction ("in"/"out") and the raw payload. Dumping is
 * gated by an enabled flag, mirroring Kamailio's trace toggle.
 *
 * It is safe for concurrent use.
 */
package sipdump

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// DumpEntry is a single dumped SIP message.
type DumpEntry struct {
	Timestamp time.Time
	Direction string
	Payload   string
}

// SipDumpModule implements the sipdump module.
// C: struct module sipdump
type SipDumpModule struct {
	mu      sync.RWMutex
	entries []*DumpEntry
	enabled atomic.Bool
}

// NewSipDumpModule creates a SipDumpModule with dumping disabled by default
// (matching Kamailio, where dumping must be explicitly enabled).
func NewSipDumpModule() *SipDumpModule {
	return &SipDumpModule{}
}

// Dump records msg with the given direction when dumping is enabled. It is
// a no-op when disabled or when msg is nil.
// C: sipdump()
func (m *SipDumpModule) Dump(msg *parser.SIPMsg, direction string) {
	if msg == nil || !m.enabled.Load() {
		return
	}
	payload := ""
	if len(msg.Buf) > 0 {
		payload = string(msg.Buf)
	}
	entry := &DumpEntry{
		Timestamp: time.Now(),
		Direction: direction,
		Payload:   payload,
	}
	m.mu.Lock()
	m.entries = append(m.entries, entry)
	m.mu.Unlock()
}

// SetEnabled toggles dumping on or off.
func (m *SipDumpModule) SetEnabled(enabled bool) {
	m.enabled.Store(enabled)
}

// IsEnabled reports whether dumping is currently enabled.
func (m *SipDumpModule) IsEnabled() bool {
	return m.enabled.Load()
}

// GetEntries returns a copy of all dumped entries in the order they were
// recorded.
func (m *SipDumpModule) GetEntries() []*DumpEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*DumpEntry, len(m.entries))
	copy(out, m.entries)
	return out
}

// Clear removes every dumped entry.
func (m *SipDumpModule) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = nil
}

// Count returns the number of dumped entries currently held.
func (m *SipDumpModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu      sync.RWMutex
	defaultSipDump *SipDumpModule
)

// DefaultSipDump returns the process-wide SipDumpModule, creating one on
// first use.
func DefaultSipDump() *SipDumpModule {
	defaultMu.RLock()
	m := defaultSipDump
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultSipDump == nil {
		defaultSipDump = NewSipDumpModule()
	}
	return defaultSipDump
}

// Init (re)initialises the process-wide SipDumpModule to a fresh state.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultSipDump = NewSipDumpModule()
}
