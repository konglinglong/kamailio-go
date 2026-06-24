// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SipCapture module - capture raw SIP packets.
 *
 * Port of the kamailio sipcapture module (src/modules/sipcapture). A
 * SipCaptureModule records every captured packet as a CaptureEntry
 * keyed by a monotonically increasing id and indexed by a
 * CorrelationID so that the full flow of a related group of packets can
 * be retrieved.
 *
 * Capturing is gated by an enabled flag: when disabled, Capture() is a
 * no-op.
 */
package sipcapture

import (
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// CaptureEntry is a single captured packet, mirroring the columns of
// Kamailio's sip_capture table row.
type CaptureEntry struct {
	ID            int64
	Time          time.Time
	Proto         string
	SrcIP         string
	SrcPort       int
	DstIP         string
	DstPort       int
	Payload       []byte
	CorrelationID string
}

// SipCaptureModule implements the sipcapture module. It is safe for
// concurrent use: the entries slice and the correlation-id index are
// guarded by mu, the id counter is atomic, and the enabled flag is
// atomic.
type SipCaptureModule struct {
	mu       sync.RWMutex
	entries  []*CaptureEntry
	byCorr   map[string][]*CaptureEntry
	byID     map[int64]*CaptureEntry
	nextID   atomic.Int64
	enabled  atomic.Bool
}

// NewSipCaptureModule creates a new SipCaptureModule with capturing
// enabled by default.
func NewSipCaptureModule() *SipCaptureModule {
	m := &SipCaptureModule{
		byCorr: make(map[string][]*CaptureEntry),
		byID:   make(map[int64]*CaptureEntry),
	}
	m.enabled.Store(true)
	return m
}

// Capture records a single packet. The payload is copied so later
// mutation of the caller's slice does not affect the stored entry.
// Returns the new entry, or nil if capturing is disabled.
func (m *SipCaptureModule) Capture(proto string, srcIP string, srcPort int, dstIP string, dstPort int, payload []byte) *CaptureEntry {
	if !m.enabled.Load() {
		return nil
	}
	var payloadCopy []byte
	if len(payload) > 0 {
		payloadCopy = make([]byte, len(payload))
		copy(payloadCopy, payload)
	}
	entry := &CaptureEntry{
		ID:      m.nextID.Add(1),
		Time:    time.Now(),
		Proto:   proto,
		SrcIP:   srcIP,
		SrcPort: srcPort,
		DstIP:   dstIP,
		DstPort: dstPort,
		Payload: payloadCopy,
	}
	m.mu.Lock()
	m.entries = append(m.entries, entry)
	m.byID[entry.ID] = entry
	m.mu.Unlock()
	return entry
}

// SetCorrelationID associates an existing capture entry with a
// correlation id, adding it to the correlation index. This mirrors the
// Kamailio behaviour where the correlation id is computed from message
// headers and may be set after the capture.
func (m *SipCaptureModule) SetCorrelationID(id int64, correlationID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.byID[id]
	if !ok {
		return false
	}
	entry.CorrelationID = correlationID
	if correlationID != "" {
		m.byCorr[correlationID] = append(m.byCorr[correlationID], entry)
	}
	return true
}

// GetEntry returns the capture entry with the given id, or nil if no
// such entry exists.
func (m *SipCaptureModule) GetEntry(id int64) *CaptureEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.byID[id]
}

// GetByCorrelationID returns all capture entries that share the given
// correlation id, in the order they were recorded. Returns nil if none
// match.
func (m *SipCaptureModule) GetByCorrelationID(id string) []*CaptureEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*CaptureEntry, len(m.byCorr[id]))
	copy(out, m.byCorr[id])
	return out
}

// Count returns the total number of captured entries.
func (m *SipCaptureModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}

// List returns a copy of all capture entries in the order they were
// recorded.
func (m *SipCaptureModule) List() []*CaptureEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*CaptureEntry, len(m.entries))
	copy(out, m.entries)
	return out
}

// Clear removes all capture entries and resets the indexes. The id
// counter is not reset, so new entries keep getting larger ids.
func (m *SipCaptureModule) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = nil
	m.byCorr = make(map[string][]*CaptureEntry)
	m.byID = make(map[int64]*CaptureEntry)
}

// SetEnabled toggles capturing on or off. When disabled, Capture is a
// no-op.
func (m *SipCaptureModule) SetEnabled(enabled bool) {
	m.enabled.Store(enabled)
}

// IsEnabled reports whether capturing is currently enabled.
func (m *SipCaptureModule) IsEnabled() bool {
	return m.enabled.Load()
}

// Format returns a human-readable representation of the entry, useful
// for logging. It is safe to call on a nil entry.
func (e *CaptureEntry) Format() string {
	if e == nil {
		return "<nil>"
	}
	return e.Proto + " " + e.SrcIP + ":" + strconv.Itoa(e.SrcPort) +
		" -> " + e.DstIP + ":" + strconv.Itoa(e.DstPort) +
		" (" + strconv.Itoa(len(e.Payload)) + " bytes)"
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu         sync.RWMutex
	defaultSipCapture *SipCaptureModule
)

// DefaultSipCapture returns the process-wide SipCaptureModule, creating
// one on first use.
func DefaultSipCapture() *SipCaptureModule {
	defaultMu.RLock()
	m := defaultSipCapture
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultSipCapture == nil {
		defaultSipCapture = NewSipCaptureModule()
	}
	return defaultSipCapture
}

// Init (re)initialises the process-wide SipCaptureModule to a fresh
// state, mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultSipCapture = NewSipCaptureModule()
}
