// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SipTrace module - trace SIP messages.
 *
 * Port of the kamailio siptrace module (src/modules/siptrace). A
 * SipTraceModule records every traced SIP message as a TraceEntry keyed
 * by a monotonically increasing id and indexed by Call-ID so that the
 * full message flow of a dialog can be retrieved.
 *
 * Tracing is gated by an enabled flag (matching Kamailio's
 * trace_is_off() check): when disabled, Trace() is a no-op.
 */
package siptrace

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// TraceType identifies whether a traced message is a request or a reply.
type TraceType string

const (
	// TraceRequest marks a traced SIP request.
	TraceRequest TraceType = "req"
	// TraceReply marks a traced SIP reply.
	TraceReply TraceType = "rpl"
)

// TraceEntry is a single traced SIP message, mirroring the columns of
// Kamailio's sip_trace table row.
type TraceEntry struct {
	ID      int64
	Time    time.Time
	Type    TraceType
	Method  string
	Status  int
	FromURI string
	ToURI   string
	CallID  string
	SrcIP   string
	DstIP   string
	Payload string
}

// SipTraceModule implements the siptrace module. It is safe for
// concurrent use: the entries slice and the call-id index are guarded by
// mu, the id counter is atomic, and the enabled flag is atomic.
type SipTraceModule struct {
	mu      sync.RWMutex
	entries []*TraceEntry
	byCall  map[string][]*TraceEntry
	byID    map[int64]*TraceEntry
	nextID  atomic.Int64
	enabled atomic.Bool
}

// NewSipTraceModule creates a new SipTraceModule with tracing enabled by
// default (matching Kamailio, where sip_trace() records unless the
// message carries the FL_SIPTRACE flag).
func NewSipTraceModule() *SipTraceModule {
	m := &SipTraceModule{
		byCall: make(map[string][]*TraceEntry),
		byID:   make(map[int64]*TraceEntry),
	}
	m.enabled.Store(true)
	return m
}

// Trace traces a message, recording it as a request or reply depending
// on the first line. Returns the new entry, or nil if tracing is
// disabled or msg is nil.
func (m *SipTraceModule) Trace(msg *parser.SIPMsg, srcIP, dstIP string) *TraceEntry {
	if msg == nil || !m.enabled.Load() {
		return nil
	}
	if msg.IsReply() {
		return m.TraceReply(msg, srcIP, dstIP)
	}
	return m.TraceRequest(msg, srcIP, dstIP)
}

// TraceRequest traces a SIP request, extracting method, From, To and
// Call-ID from the parsed message.
func (m *SipTraceModule) TraceRequest(msg *parser.SIPMsg, srcIP, dstIP string) *TraceEntry {
	if msg == nil || !m.enabled.Load() {
		return nil
	}
	method := ""
	if msg.FirstLine != nil && msg.FirstLine.Req != nil {
		method = msg.FirstLine.Req.Method.String()
	}
	return m.record(msg, TraceRequest, method, 0, srcIP, dstIP)
}

// TraceReply traces a SIP reply, extracting the status code from the
// first line.
func (m *SipTraceModule) TraceReply(msg *parser.SIPMsg, srcIP, dstIP string) *TraceEntry {
	if msg == nil || !m.enabled.Load() {
		return nil
	}
	status := 0
	if msg.FirstLine != nil && msg.FirstLine.Reply != nil {
		status = int(msg.FirstLine.Reply.StatusCode)
	}
	return m.record(msg, TraceReply, "", status, srcIP, dstIP)
}

// record builds a TraceEntry from the message, indexes it and appends
// it to the entry list. The caller has already checked the enabled flag.
func (m *SipTraceModule) record(msg *parser.SIPMsg, typ TraceType, method string, status int, srcIP, dstIP string) *TraceEntry {
	var fromURI, toURI, callID, payload string
	if msg.From != nil {
		fromURI = msg.From.Body.String()
	}
	if msg.To != nil {
		toURI = msg.To.Body.String()
	}
	if msg.CallID != nil {
		callID = msg.CallID.Body.String()
	}
	if len(msg.Buf) > 0 {
		payload = string(msg.Buf)
	}
	entry := &TraceEntry{
		ID:      m.nextID.Add(1),
		Time:    time.Now(),
		Type:    typ,
		Method:  method,
		Status:  status,
		FromURI: fromURI,
		ToURI:   toURI,
		CallID:  callID,
		SrcIP:   srcIP,
		DstIP:   dstIP,
		Payload: payload,
	}
	m.mu.Lock()
	m.entries = append(m.entries, entry)
	m.byID[entry.ID] = entry
	if entry.CallID != "" {
		m.byCall[entry.CallID] = append(m.byCall[entry.CallID], entry)
	}
	m.mu.Unlock()
	return entry
}

// GetTrace returns the trace entry with the given id, or nil if no such
// entry exists.
func (m *SipTraceModule) GetTrace(id int64) *TraceEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.byID[id]
}

// GetByCallID returns all trace entries that share the given Call-ID,
// in the order they were recorded. Returns nil if none match.
func (m *SipTraceModule) GetByCallID(callID string) []*TraceEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*TraceEntry, len(m.byCall[callID]))
	copy(out, m.byCall[callID])
	return out
}

// Count returns the total number of traced entries.
func (m *SipTraceModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}

// List returns a copy of all trace entries in the order they were
// recorded.
func (m *SipTraceModule) List() []*TraceEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*TraceEntry, len(m.entries))
	copy(out, m.entries)
	return out
}

// Clear removes all trace entries and resets the indexes. The id
// counter is not reset, so new entries keep getting larger ids.
func (m *SipTraceModule) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = nil
	m.byCall = make(map[string][]*TraceEntry)
	m.byID = make(map[int64]*TraceEntry)
}

// SetEnabled toggles tracing on or off. When disabled, Trace and its
// variants become no-ops.
func (m *SipTraceModule) SetEnabled(enabled bool) {
	m.enabled.Store(enabled)
}

// IsEnabled reports whether tracing is currently enabled.
func (m *SipTraceModule) IsEnabled() bool {
	return m.enabled.Load()
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu      sync.RWMutex
	defaultSipTrace *SipTraceModule
)

// DefaultSipTrace returns the process-wide SipTraceModule, creating
// one on first use.
func DefaultSipTrace() *SipTraceModule {
	defaultMu.RLock()
	m := defaultSipTrace
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultSipTrace == nil {
		defaultSipTrace = NewSipTraceModule()
	}
	return defaultSipTrace
}

// Init (re)initialises the process-wide SipTraceModule to a fresh
// state, mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultSipTrace = NewSipTraceModule()
}
